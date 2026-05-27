package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/runtime/store"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	DBListToolName   = "atm_db_list"
	DBGetToolName    = "atm_db_get"
	DBScanToolName   = "atm_db_scan"
	DBAppendToolName = "atm_db_append"
	DBSetToolName    = "atm_db_set"
	DBDeleteToolName = "atm_db_delete"
)

func DBToolNames() []string {
	return []string{DBListToolName, DBGetToolName, DBScanToolName, DBAppendToolName, DBSetToolName, DBDeleteToolName}
}

type dbServerConfig struct {
	Databases []ir.DBRuntime `json:"databases"`
}

type dbServer struct {
	dbs      map[string]ir.DBRuntime
	readonly bool
}

func RunDBServerCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var configFile string
	var readonly bool
	flags := flag.NewFlagSet("atm mcp db", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&configFile, "config-file", "", "path to DB MCP config JSON")
	flags.BoolVar(&readonly, "readonly", false, "disable all write tools")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "atm mcp db runs a temporary stdio MCP server for ATM databases.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Usage of atm mcp db:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if configFile == "" {
		return fmt.Errorf("-config-file is required")
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("read db config: %w", err)
	}
	config, err := parseDBServerConfig(data)
	if err != nil {
		return fmt.Errorf("parse db config: %w", err)
	}
	return ServeDB(stdin, stdout, config.Databases, readonly)
}

func ServeDB(stdin io.Reader, stdout io.Writer, dbs []ir.DBRuntime, readonly bool) error {
	return runSDKServer(context.Background(), newDBSDKServer(dbs, readonly), stdin, stdout)
}

func RegisterNetworkDB(dbs []ir.DBRuntime, readonly bool) (NetworkEndpoint, error) {
	manager, err := DefaultNetworkManager()
	if err != nil {
		return NetworkEndpoint{}, err
	}
	return manager.Register(newDBSDKServer(dbs, readonly))
}

func RegisterNetworkDBConfig(configFile string, readonly bool) (NetworkEndpoint, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return NetworkEndpoint{}, fmt.Errorf("read db config: %w", err)
	}
	config, err := parseDBServerConfig(data)
	if err != nil {
		return NetworkEndpoint{}, fmt.Errorf("parse db config: %w", err)
	}
	return RegisterNetworkDB(config.Databases, readonly)
}

func parseDBServerConfig(data []byte) (dbServerConfig, error) {
	var config dbServerConfig
	if err := DecodeStrictJSON(data, &config); err != nil {
		return dbServerConfig{}, err
	}
	return config, nil
}

func newDBSDKServer(dbs []ir.DBRuntime, readonly bool) *mcpsdk.Server {
	db := newDBServer(dbs, readonly)
	server := NewSDKServer("atm-db")
	for _, tool := range db.toolDefinitions() {
		AddTool(server, tool, func(_ context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			result, err := db.callTool(req.Params.Name, req.Params.Arguments)
			if err != nil {
				return nil, err
			}
			return JSONTextResult(result)
		})
	}
	return server
}

func newDBServer(dbs []ir.DBRuntime, readonly bool) dbServer {
	server := dbServer{dbs: map[string]ir.DBRuntime{}, readonly: readonly}
	for _, db := range dbs {
		if db.Name == "" || db.Path == "" {
			continue
		}
		if readonly {
			db.Access = ir.DBAccessRead
		}
		server.dbs[db.Name] = db
	}
	return server
}

func (s dbServer) toolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		dbToolDefinition(DBListToolName, "List ATM databases available to this task, including usage and access.", dbListSchema()),
		dbToolDefinition(DBGetToolName, "Read one key from an ATM database.", dbKeySchema()),
		dbToolDefinition(DBScanToolName, "Scan ATM database keys using glob syntax. Use ** to match across slash-separated segments.", dbScanSchema()),
		dbToolDefinition(DBAppendToolName, "Append string values to one database key. Requires append, write, or admin access.", dbWriteSchema(true)),
		dbToolDefinition(DBSetToolName, "Replace all string values for one database key. Requires write or admin access.", dbWriteSchema(true)),
		dbToolDefinition(DBDeleteToolName, "Delete one key, or selected values from one key. Requires admin access.", dbWriteSchema(false)),
	}
}

func dbToolDefinition(name, description string, schema any) ToolDefinition {
	return ToolDefinition{Name: name, Description: description, InputSchema: schema}
}

func (s dbServer) callTool(name string, arguments json.RawMessage) (any, error) {
	switch name {
	case DBListToolName:
		return s.list(), nil
	case DBGetToolName:
		var args struct {
			DB  string `json:"db"`
			Key string `json:"key"`
		}
		if err := decodeDBArgs(arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, ir.DBAccessRead)
		if err != nil {
			return nil, err
		}
		values, found, err := readDBKey(db.Path, args.Key)
		if err != nil {
			return nil, err
		}
		return map[string]any{"db": db.Name, "key": args.Key, "found": found, "values": values}, nil
	case DBScanToolName:
		var args struct {
			DB      string `json:"db"`
			Pattern string `json:"pattern"`
			Limit   int    `json:"limit"`
			Cursor  string `json:"cursor"`
		}
		if err := decodeDBArgs(arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, ir.DBAccessRead)
		if err != nil {
			return nil, err
		}
		result, err := scanDB(db.Path, args.Pattern, args.Limit, args.Cursor)
		if err != nil {
			return nil, err
		}
		return result, nil
	case DBAppendToolName:
		var args dbWriteArgs
		if err := decodeDBArgs(arguments, &args); err != nil {
			return nil, err
		}
		if err := args.requireValues(); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, ir.DBAccessAppend)
		if err != nil {
			return nil, err
		}
		values, err := mutateDB(db.Path, args.Key, func(items []string, exists bool) ([]string, bool, error) {
			return append(items, args.Values...), true, nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"db": db.Name, "key": args.Key, "values": values}, nil
	case DBSetToolName:
		var args dbWriteArgs
		if err := decodeDBArgs(arguments, &args); err != nil {
			return nil, err
		}
		if err := args.requireValues(); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, ir.DBAccessWrite)
		if err != nil {
			return nil, err
		}
		values, err := mutateDB(db.Path, args.Key, func(_ []string, _ bool) ([]string, bool, error) {
			return slices.Clone(args.Values), true, nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"db": db.Name, "key": args.Key, "values": values}, nil
	case DBDeleteToolName:
		var args dbWriteArgs
		if err := decodeDBArgs(arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, ir.DBAccessAdmin)
		if err != nil {
			return nil, err
		}
		values, err := mutateDB(db.Path, args.Key, func(items []string, exists bool) ([]string, bool, error) {
			if len(args.Values) == 0 {
				return nil, false, nil
			}
			remove := map[string]struct{}{}
			for _, value := range args.Values {
				remove[value] = struct{}{}
			}
			out := items[:0]
			for _, value := range items {
				if _, ok := remove[value]; !ok {
					out = append(out, value)
				}
			}
			return out, len(out) > 0 || exists, nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"db": db.Name, "key": args.Key, "values": values}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

type dbWriteArgs struct {
	DB     string   `json:"db"`
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

func (a dbWriteArgs) requireValues() error {
	if a.Values == nil {
		return fmt.Errorf("values is required and must be an array of strings")
	}
	return nil
}

func decodeDBArgs(data json.RawMessage, target any) error {
	if !json.Valid(data) {
		return fmt.Errorf("arguments must be valid JSON")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func dbListSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	}
}

func dbKeySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"db":  map[string]any{"type": "string", "description": "Database name."},
			"key": map[string]any{"type": "string", "description": "Key to read."},
		},
		"required":             []string{"db", "key"},
		"additionalProperties": false,
	}
}

func dbScanSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"db":      map[string]any{"type": "string", "description": "Database name."},
			"pattern": map[string]any{"type": "string", "description": "Glob pattern for keys. Use ** to match across slash-separated segments."},
			"limit":   map[string]any{"type": "integer", "description": "Maximum number of items to return. Defaults to 100 and is capped at 500."},
			"cursor":  map[string]any{"type": "string", "description": "Pagination cursor returned by the previous scan."},
		},
		"required":             []string{"db"},
		"additionalProperties": false,
	}
}

func dbWriteSchema(requireValues bool) map[string]any {
	required := []string{"db", "key"}
	if requireValues {
		required = append(required, "values")
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"db":  map[string]any{"type": "string", "description": "Database name."},
			"key": map[string]any{"type": "string", "description": "Key to write."},
			"values": map[string]any{
				"type":        "array",
				"description": "String values to set, append, or delete.",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required":             required,
		"additionalProperties": false,
	}
}

func (s dbServer) list() map[string]any {
	names := slices.Sorted(maps.Keys(s.dbs))
	items := make([]any, 0, len(names))
	for _, name := range names {
		db := s.dbs[name]
		items = append(items, map[string]any{
			"name":         db.Name,
			"scope":        db.Scope,
			"persist":      db.Persist,
			"access":       db.Access,
			"capabilities": dbCapabilities(db.Access),
			"usage":        db.Usage,
		})
	}
	return map[string]any{"databases": items}
}

func (s dbServer) requireDB(name string, access ir.DBAccess) (ir.DBRuntime, error) {
	if name == "" {
		return ir.DBRuntime{}, fmt.Errorf("db is required")
	}
	db, ok := s.dbs[name]
	if !ok {
		return ir.DBRuntime{}, fmt.Errorf("db %q is not available", name)
	}
	if !dbAccessAllows(db.Access, access) {
		return ir.DBRuntime{}, fmt.Errorf("db %q requires %s access for this operation; current access is %s", name, access, db.Access)
	}
	return db, nil
}

func dbCapabilities(access ir.DBAccess) []string {
	out := []string{"list", "get", "scan"}
	if dbAccessAllows(access, ir.DBAccessAppend) {
		out = append(out, "append")
	}
	if dbAccessAllows(access, ir.DBAccessWrite) {
		out = append(out, "set")
	}
	if dbAccessAllows(access, ir.DBAccessAdmin) {
		out = append(out, "delete")
	}
	return out
}

func dbAccessAllows(actual, required ir.DBAccess) bool {
	return dbAccessRank(actual) >= dbAccessRank(required)
}

func dbAccessRank(access ir.DBAccess) int {
	switch access {
	case ir.DBAccessRead:
		return 1
	case ir.DBAccessAppend:
		return 2
	case ir.DBAccessWrite:
		return 3
	case ir.DBAccessAdmin:
		return 4
	default:
		return 0
	}
}

func readDBKey(dbPath, key string) ([]string, bool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, false, fmt.Errorf("key is required")
	}
	data, err := readDBFile(dbPath)
	if err != nil {
		return nil, false, err
	}
	values, ok := data[key]
	return slices.Clone(values), ok, nil
}

func scanDB(dbPath, pattern string, limit int, cursor string) (map[string]any, error) {
	if pattern == "" {
		pattern = "**"
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if !strings.Contains(pattern, "**") {
		if _, err := path.Match(pattern, "probe"); err != nil {
			return nil, err
		}
	}
	match, err := dbGlobMatcher(pattern)
	if err != nil {
		return nil, err
	}
	data, err := readDBFile(dbPath)
	if err != nil {
		return nil, err
	}
	var keys []string
	for key := range data {
		if match(key) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	start := 0
	if cursor != "" {
		for start < len(keys) && keys[start] <= cursor {
			start++
		}
	}
	end := min(start+limit, len(keys))
	items := make([]any, 0, end-start)
	for _, key := range keys[start:end] {
		items = append(items, map[string]any{"key": key, "values": data[key]})
	}
	next := ""
	if end < len(keys) && end > start {
		next = keys[end-1]
	}
	return map[string]any{"items": items, "next_cursor": next}, nil
}

func dbGlobMatcher(pattern string) (func(string) bool, error) {
	if !strings.Contains(pattern, "**") {
		return func(key string) bool {
			ok, _ := path.Match(pattern, key)
			return ok
		}, nil
	}
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString(`[^/]*`)
			}
		case '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteByte('$')
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, err
	}
	return re.MatchString, nil
}

func mutateDB(dbPath, key string, mutate func([]string, bool) ([]string, bool, error)) ([]string, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("key is required")
	}
	lock, err := lockDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	data, err := readDBFile(dbPath)
	if err != nil {
		return nil, err
	}
	current, exists := data[key]
	next, keep, err := mutate(slices.Clone(current), exists)
	if err != nil {
		return nil, err
	}
	if keep {
		data[key] = slices.Clone(next)
	} else {
		delete(data, key)
	}
	if err := writeDBFile(dbPath, data); err != nil {
		return nil, err
	}
	return slices.Clone(data[key]), nil
}

func readDBFile(dbPath string) (map[string][]string, error) {
	file, err := os.Open(dbPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string][]string{}, nil
		}
		return nil, err
	}
	defer file.Close()
	var data map[string][]string
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return nil, err
	}
	if data == nil {
		data = map[string][]string{}
	}
	return data, nil
}

func writeDBFile(dbPath string, data map[string][]string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dbPath), ".db-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(data)
	closeErr := tmp.Close()
	if encodeErr != nil {
		_ = os.Remove(tmpName)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return closeErr
	}
	if err := os.Rename(tmpName, dbPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

type dbLock struct {
	lock *store.LockFile
}

func lockDB(dbPath string) (*dbLock, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	lock, err := store.LockPath(dbPath + ".lock")
	if err != nil {
		return nil, err
	}
	return &dbLock{lock: lock}, nil
}

func (l *dbLock) Close() error {
	if l == nil || l.lock == nil {
		return nil
	}
	return l.lock.Close()
}
