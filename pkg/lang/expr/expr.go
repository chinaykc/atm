package expr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	exprlang "github.com/expr-lang/expr"
	exprast "github.com/expr-lang/expr/ast"
	exprbuiltin "github.com/expr-lang/expr/builtin"
	exprparser "github.com/expr-lang/expr/parser"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

type Context struct {
	Vars      map[string]any
	TodoFile  string
	Root      string
	OutputDir string
}

type pathRef struct {
	root    string
	path    string
	display string
}

var errPathEscape = errors.New("path escapes root")

func EvalBool(expression string, ctx Context) (bool, error) {
	value, err := Eval(expression, ctx)
	if err != nil {
		return false, err
	}
	out, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("expression must return bool, got %T", value)
	}
	return out, nil
}

func EvalList(expression string, ctx Context) ([]any, error) {
	value, err := Eval(expression, ctx)
	if err != nil {
		return nil, err
	}
	return listValue(value)
}

func Eval(expression string, ctx Context) (any, error) {
	if err := ValidateSyntax(expression); err != nil {
		return nil, err
	}
	value, err := exprlang.Eval(expression, activation(ctx))
	if err != nil {
		return nil, hintExpressionError(err)
	}
	return normalizeNative(value), nil
}

func ValidateSyntax(expression string) error {
	if strings.TrimSpace(expression) == "" {
		return fmt.Errorf("expression cannot be empty")
	}
	tree, err := exprparser.Parse(expression)
	if err != nil {
		return hintExpressionError(err)
	}
	if removed := firstRemovedFunctionCall(tree); removed != "" {
		return fmt.Errorf("unknown function %q", removed)
	}
	if unsupported := firstUnsupportedFunctionCall(tree); unsupported != "" {
		return fmt.Errorf("unknown function %q", unsupported)
	}
	return nil
}

func activation(ctx Context) map[string]any {
	out := make(map[string]any, len(ctx.Vars)+14)
	userVars := make(map[string]any, len(ctx.Vars))
	for name, value := range ctx.Vars {
		normalized := exprValue(name, normalizeNative(value))
		userVars[name] = normalized
		if isIdentifier(name) {
			out[name] = normalized
		}
	}
	out["vars"] = userVars
	out["todo_file"] = ctx.TodoFile
	out["root"] = ctx.Root
	out["output_dir"] = ctx.OutputDir
	out["exist"] = func(path any) (bool, error) {
		resolved, display, err := resolvePathArg(ctx, path)
		if err != nil {
			if errors.Is(err, errPathEscape) {
				return false, nil
			}
			return false, fmt.Errorf("exist(%s): %w", display, err)
		}
		_, err = os.Stat(resolved)
		if err == nil {
			return true, nil
		}
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("exist(%s): %w", display, err)
	}
	out["open"] = func(path any) (string, error) {
		resolved, display, err := resolvePathArg(ctx, path)
		if err != nil {
			return "", fmt.Errorf("open(%s): %w", display, err)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return "", fmt.Errorf("open(%s): %w", display, err)
		}
		return string(data), nil
	}
	out["outputDir"] = func(path string) (pathRef, error) {
		ref := pathRef{root: ctx.OutputDir, path: path, display: fmt.Sprintf("outputDir(%q)", path)}
		if _, _, err := resolvePathRef(ref); err != nil {
			return pathRef{}, fmt.Errorf("outputDir(%q): %w", path, err)
		}
		return ref, nil
	}
	out["json"] = func(text string) (any, error) {
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return nil, fmt.Errorf("json(): %w", err)
		}
		return normalizeNative(value), nil
	}
	out["yaml"] = func(text string) (any, error) {
		var value any
		if err := yaml.Unmarshal([]byte(text), &value); err != nil {
			return nil, fmt.Errorf("yaml(): %w", err)
		}
		return normalizeNative(value), nil
	}
	out["toml"] = func(text string) (any, error) {
		var value any
		if err := toml.Unmarshal([]byte(text), &value); err != nil {
			return nil, fmt.Errorf("toml(): %w", err)
		}
		return normalizeNative(value), nil
	}
	out["range"] = exprRange
	out["dirs"] = func(args ...any) ([]string, error) {
		return listDirs(ctx, args...)
	}
	out["files"] = func(args ...any) ([]string, error) {
		return listFiles(ctx, args...)
	}
	out["walkDirs"] = func(args ...any) ([]string, error) {
		return walkDirs(ctx, args...)
	}
	out["walkFiles"] = func(args ...any) ([]string, error) {
		return walkFiles(ctx, args...)
	}
	return out
}

func exprRange(args ...int) ([]any, error) {
	var start, stop, step int
	switch len(args) {
	case 1:
		start, stop, step = 0, args[0], 1
	case 2:
		start, stop, step = args[0], args[1], 1
	case 3:
		start, stop, step = args[0], args[1], args[2]
	default:
		return nil, fmt.Errorf("range expects 1 to 3 integer arguments")
	}
	if step == 0 {
		return nil, fmt.Errorf("range step cannot be 0")
	}
	var out []any
	if step > 0 {
		for i := start; i < stop; i += step {
			out = append(out, i)
		}
		return out, nil
	}
	for i := start; i > stop; i += step {
		out = append(out, i)
	}
	return out, nil
}

func listDirs(ctx Context, args ...any) ([]string, error) {
	root, display, err := resolveEnumRoot(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("dirs(%s): %w", display, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("dirs(%s): %w", display, err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && !shouldSkipWalkDir(entry.Name()) {
			dirs = append(dirs, entry.Name())
		}
	}
	slices.Sort(dirs)
	return dirs, nil
}

func listFiles(ctx Context, args ...any) ([]string, error) {
	root, display, err := resolveEnumRoot(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("files(%s): %w", display, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("files(%s): %w", display, err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, entry.Name())
	}
	slices.Sort(files)
	return files, nil
}

func walkDirs(ctx Context, args ...any) ([]string, error) {
	root, display, err := resolveEnumRoot(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("walkDirs(%s): %w", display, err)
	}
	var dirs []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() && shouldSkipWalkDir(name) {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		dirs = append(dirs, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walkDirs(%s): %w", display, err)
	}
	slices.Sort(dirs)
	return dirs, nil
}

func walkFiles(ctx Context, args ...any) ([]string, error) {
	root, display, err := resolveEnumRoot(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("walkFiles(%s): %w", display, err)
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() && shouldSkipWalkDir(name) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walkFiles(%s): %w", display, err)
	}
	slices.Sort(files)
	return files, nil
}

func shouldSkipWalkDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build":
		return true
	}
	return false
}

func listValue(value any) ([]any, error) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil, fmt.Errorf("expression must return list, got %T", value)
	}
	out := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, normalizeNative(rv.Index(i).Interface()))
	}
	return out, nil
}

func normalizeNative(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeNative(item)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = normalizeNative(item)
		}
		return out
	case []interface{}:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeNative(item)
		}
		return out
	default:
		return value
	}
}

func exprValue(name string, value any) any {
	if name == "n" {
		if s, ok := value.(string); ok {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return n
			}
		}
	}
	return value
}

func resolvePathArg(ctx Context, value any) (string, string, error) {
	switch v := value.(type) {
	case pathRef:
		return resolvePathRef(v)
	case string:
		resolved, err := resolveUnder(ctx.Root, v)
		return resolved, fmt.Sprintf("%q", v), err
	default:
		return "", fmt.Sprintf("%v", value), fmt.Errorf("path must be string, got %T", value)
	}
}

func resolveEnumRoot(ctx Context, args []any) (string, string, error) {
	if len(args) > 1 {
		return "", "", fmt.Errorf("expected 0 or 1 arguments, got %d", len(args))
	}
	if len(args) == 0 {
		root, err := cleanRoot(ctx.Root)
		return root, "", err
	}
	return resolvePathArg(ctx, args[0])
}

func resolvePathRef(ref pathRef) (string, string, error) {
	resolved, err := resolveUnder(ref.root, ref.path)
	display := ref.display
	if display == "" {
		display = fmt.Sprintf("%q", ref.path)
	}
	return resolved, display, err
}

func cleanRoot(root string) (string, error) {
	if root == "" {
		root = "."
	}
	return filepath.Abs(filepath.Clean(root))
}

func resolveUnder(base, path string) (string, error) {
	if base == "" {
		base = "."
	}
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	cleanBase = filepath.Clean(cleanBase)
	var candidate string
	if filepath.IsAbs(path) {
		candidate = filepath.Clean(path)
	} else {
		candidate = filepath.Join(cleanBase, path)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cleanBase, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errPathEscape
	}
	return candidate, nil
}

func hintExpressionError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "unknown name read") ||
		strings.Contains(msg, "unknown name exists") ||
		strings.Contains(msg, "unknown name read_output") ||
		strings.Contains(msg, "unknown name json_output") ||
		strings.Contains(msg, "unknown name exists_output") ||
		strings.Contains(msg, "unknown name readOutput") ||
		strings.Contains(msg, "unknown name jsonOutput") ||
		strings.Contains(msg, "unknown name existsOutput") {
		return err
	}
	if strings.Contains(msg, "unknown name result") ||
		strings.Contains(msg, "unknown name") ||
		strings.Contains(msg, "cannot fetch") {
		return fmt.Errorf("%w; file paths in expression functions must be quoted, for example open(\"result.json\")", err)
	}
	return err
}

func firstRemovedFunctionCall(tree *exprparser.Tree) string {
	visitor := removedFunctionVisitor{}
	exprast.Walk(&tree.Node, &visitor)
	return visitor.name
}

func firstUnsupportedFunctionCall(tree *exprparser.Tree) string {
	visitor := unsupportedFunctionVisitor{}
	exprast.Walk(&tree.Node, &visitor)
	return visitor.name
}

type removedFunctionVisitor struct {
	name string
}

func (v *removedFunctionVisitor) Visit(node *exprast.Node) {
	if v.name != "" || node == nil || *node == nil {
		return
	}
	call, ok := (*node).(*exprast.CallNode)
	if !ok {
		return
	}
	ident, ok := call.Callee.(*exprast.IdentifierNode)
	if !ok {
		return
	}
	if removedExpressionFunctions[ident.Value] {
		v.name = ident.Value
	}
}

var removedExpressionFunctions = map[string]bool{
	"read":          true,
	"exists":        true,
	"read_output":   true,
	"json_output":   true,
	"exists_output": true,
	"readOutput":    true,
	"jsonOutput":    true,
	"existsOutput":  true,
}

type unsupportedFunctionVisitor struct {
	name string
}

func (v *unsupportedFunctionVisitor) Visit(node *exprast.Node) {
	if v.name != "" || node == nil || *node == nil {
		return
	}
	call, ok := (*node).(*exprast.CallNode)
	if !ok {
		return
	}
	ident, ok := call.Callee.(*exprast.IdentifierNode)
	if !ok {
		return
	}
	if !supportedExpressionFunctions[ident.Value] {
		v.name = ident.Value
	}
}

var supportedExpressionFunctions = func() map[string]bool {
	out := map[string]bool{
		"exist":     true,
		"open":      true,
		"outputDir": true,
		"json":      true,
		"yaml":      true,
		"toml":      true,
		"range":     true,
		"dirs":      true,
		"files":     true,
		"walkDirs":  true,
		"walkFiles": true,
	}
	for _, name := range exprbuiltin.Names {
		out[name] = true
	}
	return out
}()

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
			continue
		}
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
