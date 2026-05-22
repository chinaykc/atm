package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"atm/pkg/dsl"
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
	Databases []dsl.DBRuntime `json:"databases"`
}

type dbServer struct {
	dbs      map[string]dsl.DBRuntime
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
	var config dbServerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse db config: %w", err)
	}
	return ServeDB(stdin, stdout, config.Databases, readonly)
}

func ServeDB(stdin io.Reader, stdout io.Writer, dbs []dsl.DBRuntime, readonly bool) error {
	server := dbServer{dbs: map[string]dsl.DBRuntime{}, readonly: readonly}
	for _, db := range dbs {
		if db.Name == "" || db.Path == "" {
			continue
		}
		if readonly {
			db.Access = dsl.DBAccessRead
		}
		server.dbs[db.Name] = db
	}
	scanner := bufio.NewScanner(stdin)
	writer := json.NewEncoder(stdout)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		if req.ID == nil {
			continue
		}
		resp := server.handle(req)
		if err := writer.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s dbServer) handle(req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "atm-db",
				"version": "1",
			},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.tools()}
	case "tools/call":
		result, err := s.call(req.Params)
		if err != nil {
			resp.Error = &rpcError{Code: -32602, Message: err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found"}
	}
	return resp
}

func (s dbServer) tools() []any {
	return []any{
		dbTool(DBListToolName, "List ATM databases available to this task, including usage and access."),
		dbTool(DBGetToolName, "Read one key from an ATM database."),
		dbTool(DBScanToolName, "Scan ATM database keys using glob syntax. Use ** to match across slash-separated segments."),
		dbTool(DBAppendToolName, "Append string values to one database key. Requires append, write, or admin access."),
		dbTool(DBSetToolName, "Replace all string values for one database key. Requires write or admin access."),
		dbTool(DBDeleteToolName, "Delete one key, or selected values from one key. Requires admin access."),
	}
}

func dbTool(name, description string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		},
	}
}

func (s dbServer) call(raw json.RawMessage) (any, error) {
	var params toolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	switch params.Name {
	case DBListToolName:
		return contentResult(s.list())
	case DBGetToolName:
		var args struct {
			DB  string `json:"db"`
			Key string `json:"key"`
		}
		if err := decodeDBArgs(params.Arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, dsl.DBAccessRead)
		if err != nil {
			return nil, err
		}
		values, found, err := readDBKey(db.Path, args.Key)
		if err != nil {
			return nil, err
		}
		return contentResult(map[string]any{"db": db.Name, "key": args.Key, "found": found, "values": values})
	case DBScanToolName:
		var args struct {
			DB      string `json:"db"`
			Pattern string `json:"pattern"`
			Limit   int    `json:"limit"`
			Cursor  string `json:"cursor"`
		}
		if err := decodeDBArgs(params.Arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, dsl.DBAccessRead)
		if err != nil {
			return nil, err
		}
		result, err := scanDB(db.Path, args.Pattern, args.Limit, args.Cursor)
		if err != nil {
			return nil, err
		}
		return contentResult(result)
	case DBAppendToolName:
		var args dbWriteArgs
		if err := decodeDBArgs(params.Arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, dsl.DBAccessAppend)
		if err != nil {
			return nil, err
		}
		values, err := mutateDB(db.Path, args.Key, func(items []string, exists bool) ([]string, bool, error) {
			return append(items, args.Values...), true, nil
		})
		if err != nil {
			return nil, err
		}
		return contentResult(map[string]any{"db": db.Name, "key": args.Key, "values": values})
	case DBSetToolName:
		var args dbWriteArgs
		if err := decodeDBArgs(params.Arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, dsl.DBAccessWrite)
		if err != nil {
			return nil, err
		}
		values, err := mutateDB(db.Path, args.Key, func(_ []string, _ bool) ([]string, bool, error) {
			return append([]string{}, args.Values...), true, nil
		})
		if err != nil {
			return nil, err
		}
		return contentResult(map[string]any{"db": db.Name, "key": args.Key, "values": values})
	case DBDeleteToolName:
		var args dbWriteArgs
		if err := decodeDBArgs(params.Arguments, &args); err != nil {
			return nil, err
		}
		db, err := s.requireDB(args.DB, dsl.DBAccessAdmin)
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
		return contentResult(map[string]any{"db": db.Name, "key": args.Key, "values": values})
	default:
		return nil, fmt.Errorf("unknown tool %q", params.Name)
	}
}

type dbWriteArgs struct {
	DB     string   `json:"db"`
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

func decodeDBArgs(data json.RawMessage, target any) error {
	if !json.Valid(data) {
		return fmt.Errorf("arguments must be valid JSON")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	return nil
}

func (s dbServer) list() map[string]any {
	names := make([]string, 0, len(s.dbs))
	for name := range s.dbs {
		names = append(names, name)
	}
	sort.Strings(names)
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

func (s dbServer) requireDB(name string, access dsl.DBAccess) (dsl.DBRuntime, error) {
	if name == "" {
		return dsl.DBRuntime{}, fmt.Errorf("db is required")
	}
	db, ok := s.dbs[name]
	if !ok {
		return dsl.DBRuntime{}, fmt.Errorf("db %q is not available", name)
	}
	if !dbAccessAllows(db.Access, access) {
		return dsl.DBRuntime{}, fmt.Errorf("db %q requires %s access for this operation; current access is %s", name, access, db.Access)
	}
	return db, nil
}

func dbCapabilities(access dsl.DBAccess) []string {
	out := []string{"list", "get", "scan"}
	if dbAccessAllows(access, dsl.DBAccessAppend) {
		out = append(out, "append")
	}
	if dbAccessAllows(access, dsl.DBAccessWrite) {
		out = append(out, "set")
	}
	if dbAccessAllows(access, dsl.DBAccessAdmin) {
		out = append(out, "delete")
	}
	return out
}

func dbAccessAllows(actual, required dsl.DBAccess) bool {
	return dbAccessRank(actual) >= dbAccessRank(required)
}

func dbAccessRank(access dsl.DBAccess) int {
	switch access {
	case dsl.DBAccessRead:
		return 1
	case dsl.DBAccessAppend:
		return 2
	case dsl.DBAccessWrite:
		return 3
	case dsl.DBAccessAdmin:
		return 4
	default:
		return 0
	}
}

func contentResult(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": string(data)},
		},
	}, nil
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
	return append([]string{}, values...), ok, nil
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
	keys := make([]string, 0, len(data))
	for key := range data {
		if match(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	start := 0
	if cursor != "" {
		for start < len(keys) && keys[start] <= cursor {
			start++
		}
	}
	end := start + limit
	if end > len(keys) {
		end = len(keys)
	}
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
	next, keep, err := mutate(append([]string{}, current...), exists)
	if err != nil {
		return nil, err
	}
	if keep {
		data[key] = append([]string{}, next...)
	} else {
		delete(data, key)
	}
	if err := writeDBFile(dbPath, data); err != nil {
		return nil, err
	}
	return append([]string{}, data[key]...), nil
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
	file *os.File
	path string
}

func lockDB(dbPath string) (*dbLock, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	lockPath := dbPath + ".lock"
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
		if err == nil {
			fmt.Fprintf(file, "pid=%d time=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
			return &dbLock{file: file, path: lockPath}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		removeStaleDBLock(lockPath, 2*time.Second)
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("lock db %q: timed out waiting for existing lock", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func removeStaleDBLock(lockPath string, maxAge time.Duration) {
	info, err := os.Stat(lockPath)
	if err == nil && time.Since(info.ModTime()) > maxAge {
		_ = os.Remove(lockPath)
	}
}

func (l *dbLock) Close() error {
	closeErr := l.file.Close()
	removeErr := os.Remove(l.path)
	if closeErr != nil {
		return closeErr
	}
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}
	return nil
}
