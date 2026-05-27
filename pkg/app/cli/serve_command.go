package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/runtime/engine"
	"github.com/chinaykc/atm/pkg/runtime/store"
	urfavecli "github.com/urfave/cli/v3"
)

type apiEndpoint struct {
	Route string
	File  string
	Flags []compiler.FlagDecl
}

type apiRegistry struct {
	APIs []apiRegistration `json:"apis"`
}

type apiRegistration struct {
	Path string `json:"path"`
	File string `json:"file"`
}

type apiJob struct {
	JobID     string         `json:"jobId"`
	Status    string         `json:"status"`
	Params    map[string]any `json:"params,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	StartedAt time.Time      `json:"startedAt,omitempty"`
	EndedAt   time.Time      `json:"endedAt,omitempty"`
	Result    any            `json:"result,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type apiServer struct {
	env      commandEnv
	opts     engine.Options
	routes   map[string]apiEndpoint
	jobsDir  string
	runsDir  string
	jobs     map[string]*apiJob
	jobsMu   sync.Mutex
	basePath string
}

func serveCommand(env commandEnv) *urfavecli.Command {
	flags := executeFlags()
	flags = append(flags, &urfavecli.StringFlag{Name: "addr", Value: "127.0.0.1:8080", Usage: "HTTP listen address"})
	return &urfavecli.Command{
		Name:      "serve",
		Usage:     "serve ATM files as HTTP APIs",
		ArgsUsage: "[file]",
		Flags:     flags,
		Commands: []*urfavecli.Command{
			serveRegisterCommand(env),
			serveScanCommand(env),
			serveUnregisterCommand(env),
			serveListCommand(env),
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			opts, err := executeOptions(cmd, env)
			if err != nil {
				return err
			}
			runner, err := agent.NewRunner(opts.ToolName, agent.Config{CodexPath: opts.CodexPath, ClaudePath: opts.ClaudePath})
			if err != nil {
				return err
			}
			opts.Runner = runner
			routes, err := discoverAPIRoutes(cmd.Args().Slice())
			if err != nil {
				return err
			}
			jobsDir := filepath.Join(".", ".atm", "api", "jobs")
			if err := os.MkdirAll(jobsDir, 0o755); err != nil {
				return err
			}
			runsDir := filepath.Join(".", ".atm", "api", "runs")
			if err := os.MkdirAll(runsDir, 0o755); err != nil {
				return err
			}
			server := &apiServer{env: env, opts: opts, routes: routes, jobsDir: jobsDir, runsDir: runsDir, jobs: map[string]*apiJob{}}
			fmt.Fprintf(env.Stderr, "atm serve listening on http://%s\n", cmd.String("addr"))
			return http.ListenAndServe(cmd.String("addr"), server)
		},
	}
}

func serveScanCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "scan",
		Usage: "scan ./.atm/api once and register ATM files",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			if err := rejectArgs(cmd); err != nil {
				return err
			}
			global := cmd.Bool("global")
			entries, err := scanAPIRegistrations(filepath.Join(".", ".atm", "api"), global)
			if err != nil {
				return err
			}
			if _, err := updateAPIRegistry(global, func(registry apiRegistry) (apiRegistry, error) {
				for _, entry := range entries {
					var addErr error
					registry, addErr = addAPIRegistration(registry, entry)
					if addErr != nil {
						return apiRegistry{}, addErr
					}
				}
				if _, err := routesFromRegistry(registry); err != nil {
					return apiRegistry{}, err
				}
				return registry, nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(env.Stdout, "atm serve scan registered %d API file(s) in %s registry\n", len(entries), registryScopeName(global))
			return nil
		},
	}
}

func serveRegisterCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "register",
		Usage:     "register an ATM file for atm serve",
		ArgsUsage: "<file>",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
			&urfavecli.StringFlag{Name: "path", Usage: "HTTP route path; defaults to the file basename"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			file, err := singleTodoFileArg(cmd)
			if err != nil {
				return err
			}
			if !isTodoFileName(file) {
				return fmt.Errorf("API file %q must be a .md or .txt ATM file", file)
			}
			if _, err := flagsForFile(file); err != nil {
				return err
			}
			route := cmd.String("path")
			if route == "" {
				route = "/" + stripTodoSuffix(filepath.Base(file))
			}
			route = canonicalAPIRoute(route)
			global := cmd.Bool("global")
			storedFile, err := registryFilePath(file, global)
			if err != nil {
				return err
			}
			entry := apiRegistration{Path: route, File: storedFile}
			if _, err := updateAPIRegistry(global, func(registry apiRegistry) (apiRegistry, error) {
				registry, err := addAPIRegistration(registry, entry)
				if err != nil {
					return apiRegistry{}, err
				}
				if _, err := routesFromRegistry(registry); err != nil {
					return apiRegistry{}, err
				}
				return registry, nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(env.Stdout, "atm serve registered (%s): %s -> %s\n", registryScopeName(global), route, storedFile)
			return nil
		},
	}
}

func serveUnregisterCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "unregister",
		Usage:     "remove an API route registration",
		ArgsUsage: "<path-or-file>",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			target, err := singleTodoFileArg(cmd)
			if err != nil {
				return err
			}
			global := cmd.Bool("global")
			if _, err := updateAPIRegistry(global, func(registry apiRegistry) (apiRegistry, error) {
				var kept []apiRegistration
				for _, entry := range registry.APIs {
					pathMatch := strings.HasPrefix(target, "/") && canonicalAPIRoute(target) == canonicalAPIRoute(entry.Path)
					fileMatch := filepath.Clean(target) == filepath.Clean(entry.File)
					if !pathMatch && !fileMatch {
						kept = append(kept, entry)
					}
				}
				if len(kept) == len(registry.APIs) {
					return apiRegistry{}, fmt.Errorf("API registration not found: %s", target)
				}
				registry.APIs = kept
				return registry, nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(env.Stdout, "atm serve unregistered (%s): %s\n", registryScopeName(global), target)
			return nil
		},
	}
}

func serveListCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "list",
		Usage: "list registered API ATM files",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			if err := rejectArgs(cmd); err != nil {
				return err
			}
			registry, err := loadAPIRegistry(cmd.Bool("global"))
			if err != nil {
				return err
			}
			for _, entry := range registry.APIs {
				fmt.Fprintf(env.Stdout, "%s\t%s\n", canonicalAPIRoute(entry.Path), entry.File)
			}
			return nil
		},
	}
}

func discoverAPIRoutes(args []string) (map[string]apiEndpoint, error) {
	if len(args) > 1 {
		return nil, fmt.Errorf("serve accepts at most one file")
	}
	routes := map[string]apiEndpoint{}
	if len(args) == 1 {
		file := args[0]
		flags, err := flagsForFile(file)
		if err != nil {
			return nil, err
		}
		return addAPIFileRoutes(routes, "/"+stripTodoSuffix(filepath.Base(file)), file, flags)
	}
	registry, err := loadMergedAPIRegistry()
	if err != nil {
		return nil, err
	}
	routes, err = routesFromRegistry(registry)
	if err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("no API ATM file specified or registered; use atm serve register <file>")
	}
	return routes, nil
}

func routesFromRegistry(registry apiRegistry) (map[string]apiEndpoint, error) {
	routes := map[string]apiEndpoint{}
	for _, entry := range registry.APIs {
		flags, err := flagsForFile(entry.File)
		if err != nil {
			return nil, fmt.Errorf("read registered API file %s: %w", entry.File, err)
		}
		if _, err := addAPIFileRoutes(routes, entry.Path, entry.File, flags); err != nil {
			return nil, err
		}
	}
	return routes, nil
}

func addAPIRegistration(registry apiRegistry, entry apiRegistration) (apiRegistry, error) {
	route := canonicalAPIRoute(entry.Path)
	for i := range registry.APIs {
		if canonicalAPIRoute(registry.APIs[i].Path) != route {
			continue
		}
		if filepath.Clean(registry.APIs[i].File) != filepath.Clean(entry.File) {
			return apiRegistry{}, fmt.Errorf("API path conflict %s maps to both %s and %s", route, registry.APIs[i].File, entry.File)
		}
		registry.APIs[i] = apiRegistration{Path: route, File: entry.File}
		return registry, nil
	}
	registry.APIs = append(registry.APIs, apiRegistration{Path: route, File: entry.File})
	return registry, nil
}

func addAPIFileRoutes(routes map[string]apiEndpoint, route, file string, flags []compiler.FlagDecl) (map[string]apiEndpoint, error) {
	route = canonicalAPIRoute(route)
	if _, err := addAPIRoute(routes, route, file, flags); err != nil {
		return nil, err
	}
	return addAPIRoute(routes, route+todoFileSuffix(file), file, flags)
}

func canonicalAPIRoute(route string) string {
	return "/" + strings.Trim(pathpkg.Clean("/"+strings.TrimSpace(route)), "/")
}

func todoFileSuffix(file string) string {
	lower := strings.ToLower(file)
	for _, suffix := range []string{".todo.md", ".todo.txt", ".md", ".txt"} {
		if strings.HasSuffix(lower, suffix) {
			return file[len(file)-len(suffix):]
		}
	}
	return filepath.Ext(file)
}

func apiRegistryPathForScope(global bool) (string, error) {
	return atmRegistryPath(global, "api", "index.json")
}

func loadAPIRegistry(global bool) (apiRegistry, error) {
	path, err := apiRegistryPathForScope(global)
	if err != nil {
		return apiRegistry{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return apiRegistry{}, nil
	}
	if err != nil {
		return apiRegistry{}, err
	}
	var registry apiRegistry
	if err := json.Unmarshal(data, &registry); err != nil {
		return apiRegistry{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return registry, nil
}

func loadMergedAPIRegistry() (apiRegistry, error) {
	global, err := loadAPIRegistry(true)
	if err != nil {
		return apiRegistry{}, err
	}
	local, err := loadAPIRegistry(false)
	if err != nil {
		return apiRegistry{}, err
	}
	merged := apiRegistry{APIs: append([]apiRegistration(nil), global.APIs...)}
	for _, entry := range local.APIs {
		var addErr error
		merged, addErr = addAPIRegistration(merged, entry)
		if addErr != nil {
			return apiRegistry{}, addErr
		}
	}
	return merged, nil
}

func saveAPIRegistry(registry apiRegistry, global bool) error {
	path, err := apiRegistryPathForScope(global)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o644)
}

func lockAPIRegistry(global bool) (*store.LockFile, error) {
	path, err := apiRegistryPathForScope(global)
	if err != nil {
		return nil, err
	}
	return store.LockPath(path + ".lock")
}

func updateAPIRegistry(global bool, update func(apiRegistry) (apiRegistry, error)) (apiRegistry, error) {
	lock, err := lockAPIRegistry(global)
	if err != nil {
		return apiRegistry{}, err
	}
	defer lock.Close()
	registry, err := loadAPIRegistry(global)
	if err != nil {
		return apiRegistry{}, err
	}
	registry, err = update(registry)
	if err != nil {
		return apiRegistry{}, err
	}
	if err := saveAPIRegistry(registry, global); err != nil {
		return apiRegistry{}, err
	}
	return registry, nil
}

func scanAPIRegistrations(root string, global bool) ([]apiRegistration, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var out []apiRegistration
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "jobs", "runs":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !isTodoFileName(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		storedFile, err := registryFilePath(path, global)
		if err != nil {
			return err
		}
		out = append(out, apiRegistration{
			Path: "/" + filepath.ToSlash(stripTodoSuffix(rel)),
			File: storedFile,
		})
		return nil
	})
	return out, err
}

func addAPIRoute(routes map[string]apiEndpoint, path, file string, flags []compiler.FlagDecl) (map[string]apiEndpoint, error) {
	path = canonicalAPIRoute(path)
	if path == "/" {
		return nil, fmt.Errorf("invalid API route for %s", file)
	}
	if path == "/openapi.json" || path == "/jobs" || strings.HasPrefix(path, "/jobs/") {
		return nil, fmt.Errorf("API path %s is reserved", path)
	}
	if existing, ok := routes[path]; ok && filepath.Clean(existing.File) != filepath.Clean(file) {
		return nil, fmt.Errorf("API path conflict %s maps to both %s and %s", path, existing.File, file)
	}
	routes[path] = apiEndpoint{Route: path, File: file, Flags: flags}
	return routes, nil
}

func flagsForFile(file string) ([]compiler.FlagDecl, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return compiler.ParseFlagDecls(file, string(data))
}

func (s *apiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet && r.URL.Path == "/openapi.json" {
		_ = json.NewEncoder(w).Encode(s.openapi(r))
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/jobs/") {
		s.handleJob(w, r)
		return
	}
	endpoint, ok := s.routes[r.URL.Path]
	if !ok {
		httpError(w, http.StatusNotFound, "not_found", "API path not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleSync(w, r, endpoint)
	case http.MethodPost:
		s.handleAsync(w, r, endpoint)
	default:
		httpError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET or POST")
	}
}

func (s *apiServer) requestVars(r *http.Request, endpoint apiEndpoint) (map[string]any, error) {
	raw := map[string][]string{}
	for name, values := range r.URL.Query() {
		raw[name] = append([]string(nil), values...)
	}
	if r.Method == http.MethodPost && r.Body != nil {
		var body map[string]any
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		if len(strings.TrimSpace(string(data))) > 0 {
			if err := json.Unmarshal(data, &body); err != nil {
				return nil, fmt.Errorf("parse JSON body: %w", err)
			}
			for name, value := range body {
				switch v := value.(type) {
				case []any:
					raw[name] = nil
					for _, item := range v {
						raw[name] = append(raw[name], fmt.Sprint(item))
					}
				default:
					raw[name] = []string{fmt.Sprint(v)}
				}
			}
		}
	}
	return compiler.CoerceFlagValues(endpoint.Flags, raw)
}

func stripTodoSuffix(path string) string {
	lower := strings.ToLower(path)
	for _, suffix := range []string{".todo.md", ".todo.txt", ".md", ".txt"} {
		if strings.HasSuffix(lower, suffix) {
			return path[:len(path)-len(suffix)]
		}
	}
	return strings.TrimSuffix(path, filepath.Ext(path))
}

func (s *apiServer) handleSync(w http.ResponseWriter, r *http.Request, endpoint apiEndpoint) {
	vars, err := s.requestVars(r, endpoint)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	runDir, err := s.syncRunDir(endpoint.Route)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "run_dir_failed", err.Error())
		return
	}
	result, err := s.runAPI(r.Context(), endpoint.File, vars, runDir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "execution_failed", err.Error())
		return
	}
	writeAPIResult(w, result)
}

func (s *apiServer) handleAsync(w http.ResponseWriter, r *http.Request, endpoint apiEndpoint) {
	vars, err := s.requestVars(r, endpoint)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	id := newJobID()
	job := &apiJob{JobID: id, Status: "queued", Params: vars, CreatedAt: time.Now()}
	s.saveJob(job)
	go func() {
		job.Status = "running"
		job.StartedAt = time.Now()
		s.saveJob(job)
		result, err := s.runAPI(context.Background(), endpoint.File, vars, filepath.Join(s.jobsDir, id))
		job.EndedAt = time.Now()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			job.Status = "succeeded"
			job.Result = apiResultValue(result)
		}
		s.saveJob(job)
	}()
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"jobId": id, "status": "queued", "statusUrl": "/jobs/" + id})
}

func (s *apiServer) handleJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	s.jobsMu.Lock()
	job := s.jobs[id]
	s.jobsMu.Unlock()
	if job == nil {
		data, err := os.ReadFile(filepath.Join(s.jobsDir, id, "job.json"))
		if err != nil {
			httpError(w, http.StatusNotFound, "not_found", "job not found")
			return
		}
		var loaded apiJob
		if err := json.Unmarshal(data, &loaded); err != nil {
			httpError(w, http.StatusInternalServerError, "job_read_failed", err.Error())
			return
		}
		job = &loaded
	}
	_ = json.NewEncoder(w).Encode(job)
}

func (s *apiServer) saveJob(job *apiJob) {
	s.jobsMu.Lock()
	s.jobs[job.JobID] = job
	s.jobsMu.Unlock()
	dir := filepath.Join(s.jobsDir, job.JobID)
	_ = os.MkdirAll(dir, 0o755)
	data, _ := json.MarshalIndent(job, "", "  ")
	_ = writeFileAtomic(filepath.Join(dir, "job.json"), data, 0o644)
}

func (s *apiServer) runAPI(ctx context.Context, source string, vars map[string]any, runDir string) (engine.Result, error) {
	opts := s.opts
	opts.Vars = vars
	return runEphemeralFileCapture(ctx, opts, source, runDir)
}

func (s *apiServer) syncRunDir(route string) (string, error) {
	base := filepath.Join(s.runsDir, sanitizePathPart(strings.Trim(route, "/")))
	stamp := time.Now().Format("20060102150405")
	for i := 0; ; i++ {
		dir := filepath.Join(base, stamp)
		if i > 0 {
			dir = filepath.Join(base, fmt.Sprintf("%s-%d", stamp, i))
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(dir, "source.todo.md")); os.IsNotExist(err) {
			return dir, nil
		}
	}
}

func writeAPIResult(w http.ResponseWriter, result engine.Result) {
	value := apiResultValue(result)
	_ = json.NewEncoder(w).Encode(value)
}

func apiResultValue(result engine.Result) any {
	if len(result.StructuredOutputs) == 1 {
		var value any
		if err := json.Unmarshal(result.StructuredOutputs[0], &value); err == nil {
			return value
		}
		return json.RawMessage(result.StructuredOutputs[0])
	}
	if len(result.StructuredOutputs) > 1 {
		outputs := make([]json.RawMessage, len(result.StructuredOutputs))
		for i := range result.StructuredOutputs {
			outputs[i] = json.RawMessage(result.StructuredOutputs[i])
		}
		return map[string]any{"outputs": outputs}
	}
	return map[string]any{"value": latestAPIMessage(result.Messages)}
}

func latestAPIMessage(messages []compiler.OutputMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if text := strings.TrimSpace(messages[i].Text); text != "" {
			return text
		}
	}
	return ""
}

func httpError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func newJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (s *apiServer) openapi(r *http.Request) map[string]any {
	paths := map[string]any{}
	for path, endpoint := range s.routes {
		parameters := openAPIParameters(endpoint.Flags)
		paths[path] = map[string]any{
			"get": map[string]any{
				"parameters": parameters,
				"responses":  map[string]any{"200": map[string]any{"description": "OK"}},
			},
			"post": map[string]any{
				"parameters": parameters,
				"responses":  map[string]any{"202": map[string]any{"description": "Queued"}},
			},
		}
	}
	return map[string]any{"openapi": "3.0.0", "info": map[string]any{"title": "ATM API", "version": "1.0.0"}, "paths": paths}
}

func openAPIParameters(flags []compiler.FlagDecl) []map[string]any {
	var out []map[string]any
	for _, flag := range flags {
		param := map[string]any{
			"name":        flag.Name,
			"in":          "query",
			"description": flag.Description,
			"required":    !flag.HasDefault && flag.Type != "bool",
			"schema":      openAPIFlagSchema(flag),
		}
		out = append(out, param)
	}
	return out
}

func openAPIFlagSchema(flag compiler.FlagDecl) map[string]any {
	schema := map[string]any{}
	switch flag.Type {
	case "int", "[]int":
		schema["type"] = "integer"
	case "number", "[]number":
		schema["type"] = "number"
	case "bool":
		schema["type"] = "boolean"
	default:
		schema["type"] = "string"
	}
	if strings.HasPrefix(flag.Type, "[]") {
		itemType := schema["type"]
		schema = map[string]any{"type": "array", "items": map[string]any{"type": itemType}}
	}
	if flag.HasDefault {
		schema["default"] = flag.Default
	}
	return schema
}
