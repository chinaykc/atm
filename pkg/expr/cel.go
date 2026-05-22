package expr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
)

type Context struct {
	Vars      map[string]any
	TodoFile  string
	Root      string
	OutputDir string
}

func EvalBool(expression string, ctx Context) (bool, error) {
	env, err := cel.NewEnv(envOptions(ctx)...)
	if err != nil {
		return false, err
	}
	ast, issues := env.Compile(expression)
	if issues.Err() != nil {
		return false, hintCELCompileError(issues.Err())
	}
	if ast.OutputType() != cel.BoolType && ast.OutputType() != cel.DynType {
		return false, fmt.Errorf("CEL expression must return bool, got %s", ast.OutputType())
	}
	program, err := env.Program(ast)
	if err != nil {
		return false, err
	}
	out, _, err := program.Eval(activation(ctx))
	if err != nil {
		return false, err
	}
	value, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("CEL expression must return bool, got %T", out.Value())
	}
	return value, nil
}

func EvalList(expression string, ctx Context) ([]any, error) {
	env, err := cel.NewEnv(envOptions(ctx)...)
	if err != nil {
		return nil, err
	}
	ast, issues := env.Compile(expression)
	if issues.Err() != nil {
		return nil, hintCELCompileError(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		return nil, err
	}
	out, _, err := program.Eval(activation(ctx))
	if err != nil {
		return nil, err
	}
	return listValue(out)
}

func listValue(value ref.Val) ([]any, error) {
	if types.IsError(value) {
		return nil, fmt.Errorf("%v", value)
	}
	if list, ok := value.(traits.Lister); ok {
		size := int(list.Size().(types.Int))
		out := make([]any, 0, size)
		it := list.Iterator()
		for it.HasNext() == types.True {
			item, err := nativeValue(it.Next())
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	}
	native, err := nativeValue(value)
	if err != nil {
		return nil, err
	}
	rv := reflect.ValueOf(native)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil, fmt.Errorf("CEL expression must return list, got %T", native)
	}
	out := make([]any, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out, nil
}

func nativeValue(value ref.Val) (any, error) {
	if types.IsError(value) {
		return nil, fmt.Errorf("%v", value)
	}
	switch v := value.(type) {
	case types.String:
		return string(v), nil
	case types.Bytes:
		return []byte(v), nil
	case types.Bool:
		return bool(v), nil
	case types.Int:
		return int64(v), nil
	case types.Uint:
		return uint64(v), nil
	case types.Double:
		return float64(v), nil
	}
	anyType := reflect.TypeOf((*any)(nil)).Elem()
	native, err := value.ConvertToNative(anyType)
	if err == nil {
		return normalizeNative(native), nil
	}
	return normalizeNative(value.Value()), nil
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

func envOptions(ctx Context) []cel.EnvOption {
	opts := []cel.EnvOption{
		cel.Variable("vars", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("todo_file", cel.StringType),
		cel.Variable("root", cel.StringType),
		cel.Variable("output_dir", cel.StringType),
		stringFunc("exists", cel.BoolType, func(path string) ref.Val {
			_, err := os.Stat(resolvePath(ctx.Root, path))
			if err == nil {
				return types.Bool(true)
			}
			if os.IsNotExist(err) {
				return types.Bool(false)
			}
			return types.NewErr("exists(%q): %v", path, err)
		}),
		stringFunc("read", cel.StringType, func(path string) ref.Val {
			data, err := os.ReadFile(resolvePath(ctx.Root, path))
			if err != nil {
				return types.NewErr("read(%q): %v", path, err)
			}
			return types.String(string(data))
		}),
		stringFunc("json", cel.DynType, func(path string) ref.Val {
			data, err := os.ReadFile(resolvePath(ctx.Root, path))
			if err != nil {
				return types.NewErr("json(%q): %v", path, err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				return types.NewErr("json(%q): %v", path, err)
			}
			return types.DefaultTypeAdapter.NativeToValue(value)
		}),
		stringFunc("existsOutput", cel.BoolType, func(path string) ref.Val {
			_, err := os.Stat(resolvePath(ctx.OutputDir, path))
			if err == nil {
				return types.Bool(true)
			}
			if os.IsNotExist(err) {
				return types.Bool(false)
			}
			return types.NewErr("existsOutput(%q): %v", path, err)
		}),
		stringFunc("readOutput", cel.StringType, func(path string) ref.Val {
			data, err := os.ReadFile(resolvePath(ctx.OutputDir, path))
			if err != nil {
				return types.NewErr("readOutput(%q): %v", path, err)
			}
			return types.String(string(data))
		}),
		stringFunc("jsonOutput", cel.DynType, func(path string) ref.Val {
			data, err := os.ReadFile(resolvePath(ctx.OutputDir, path))
			if err != nil {
				return types.NewErr("jsonOutput(%q): %v", path, err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				return types.NewErr("jsonOutput(%q): %v", path, err)
			}
			return types.DefaultTypeAdapter.NativeToValue(value)
		}),
		cel.Function("len",
			cel.Overload("atm_len_dyn", []*cel.Type{cel.DynType}, cel.IntType,
				cel.UnaryBinding(func(arg ref.Val) ref.Val {
					switch v := arg.(type) {
					case types.String:
						return types.Int(len(string(v)))
					case types.Bytes:
						return types.Int(len([]byte(v)))
					case traits.Sizer:
						return types.Int(v.Size().(types.Int))
					default:
						native := arg.Value()
						rv := reflect.ValueOf(native)
						switch rv.Kind() {
						case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
							return types.Int(rv.Len())
						default:
							return types.NewErr("len() does not support %T", native)
						}
					}
				}),
			),
		),
	}
	for name := range ctx.Vars {
		if isCELIdentifier(name) {
			opts = append(opts, cel.Variable(name, cel.DynType))
		}
	}
	return opts
}

func stringFunc(name string, result *cel.Type, fn func(string) ref.Val) cel.EnvOption {
	return cel.Function(name,
		cel.Overload("atm_"+name+"_string", []*cel.Type{cel.StringType}, result,
			cel.UnaryBinding(func(arg ref.Val) ref.Val {
				return fn(string(arg.(types.String)))
			}),
		),
	)
}

func activation(ctx Context) map[string]any {
	vars := make(map[string]any, len(ctx.Vars))
	for name, value := range ctx.Vars {
		vars[name] = celValue(name, value)
	}
	out := make(map[string]any, len(vars)+4)
	out["vars"] = vars
	out["todo_file"] = ctx.TodoFile
	out["root"] = ctx.Root
	out["output_dir"] = ctx.OutputDir
	for name, value := range vars {
		if isCELIdentifier(name) {
			out[name] = value
		}
	}
	return out
}

func celValue(name string, value any) any {
	if name == "N" {
		if s, ok := value.(string); ok {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return n
			}
		}
	}
	return value
}

func resolvePath(base, path string) string {
	if base == "" {
		base = "."
	}
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return filepath.Join(base, path)
	}
	var candidate string
	if filepath.IsAbs(path) {
		candidate = filepath.Clean(path)
	} else {
		candidate = filepath.Join(cleanBase, path)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return filepath.Join(cleanBase, path)
	}
	rel, err := filepath.Rel(cleanBase, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join(cleanBase, "__atm_path_escape_denied__")
	}
	return candidate
}

func hintCELCompileError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "undeclared reference to 'result'") || strings.Contains(msg, "undeclared reference") {
		return fmt.Errorf("%w; file paths in CEL functions must be quoted, for example read(\"result.json\")", err)
	}
	return err
}

func isCELIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z') {
				continue
			}
			return false
		}
		if r == '_' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			continue
		}
		return false
	}
	return true
}
