package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chinaykc/atm/pkg/integration/agent"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chinaykc/atm/pkg/lang/compiler"
	"github.com/chinaykc/atm/pkg/runtime/store"
	urfavecli "github.com/urfave/cli/v3"
)

type dynamicCommand struct {
	Name        string
	File        string
	Description string
	Flags       []compiler.FlagDecl
}

func discoverDynamicCommands() ([]dynamicCommand, error) {
	registry, err := loadMergedDynamicRegistry()
	if err != nil {
		return nil, err
	}
	return dynamicCommandsFromRegistry(registry)
}

func dynamicCommandsFromRegistry(registry dynamicIndex) ([]dynamicCommand, error) {
	builtins := map[string]struct{}{}
	for _, cmd := range rootCommands(newCommandEnv(os.Stdout, os.Stderr), nil) {
		builtins[cmd.Name] = struct{}{}
	}
	var out []dynamicCommand
	seen := map[string]string{}
	for _, command := range registry.Commands {
		item, err := dynamicCommandFromFile(command.Name, command.File, command.Description)
		if err != nil {
			return nil, err
		}
		if _, ok := builtins[item.Name]; ok {
			return nil, fmt.Errorf("dynamic command %q conflicts with a built-in command", item.Name)
		}
		if first, ok := seen[item.Name]; ok {
			return nil, fmt.Errorf("dynamic command %q from %s conflicts with %s", item.Name, item.File, first)
		}
		seen[item.Name] = item.File
		out = append(out, item)
	}
	return out, nil
}

type dynamicIndex struct {
	Commands []dynamicRegistration `json:"commands"`
}

type dynamicRegistration struct {
	Name        string `json:"name"`
	File        string `json:"file"`
	Description string `json:"description,omitempty"`
}

func dynamicRegistryPathForScope(global bool) (string, error) {
	return atmRegistryPath(global, "flag", "index.json")
}

func loadDynamicRegistry(global bool) (dynamicIndex, error) {
	path, err := dynamicRegistryPathForScope(global)
	if err != nil {
		return dynamicIndex{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return dynamicIndex{}, nil
	}
	if err != nil {
		return dynamicIndex{}, err
	}
	var registry dynamicIndex
	if err := json.Unmarshal(data, &registry); err != nil {
		return dynamicIndex{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return registry, nil
}

func loadMergedDynamicRegistry() (dynamicIndex, error) {
	global, err := loadDynamicRegistry(true)
	if err != nil {
		return dynamicIndex{}, err
	}
	local, err := loadDynamicRegistry(false)
	if err != nil {
		return dynamicIndex{}, err
	}
	merged := dynamicIndex{Commands: append([]dynamicRegistration(nil), global.Commands...)}
	for _, entry := range local.Commands {
		var addErr error
		merged, addErr = addDynamicRegistration(merged, entry)
		if addErr != nil {
			return dynamicIndex{}, addErr
		}
	}
	return merged, nil
}

func saveDynamicRegistry(registry dynamicIndex, global bool) error {
	path, err := dynamicRegistryPathForScope(global)
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

func lockDynamicRegistry(global bool) (*store.LockFile, error) {
	path, err := dynamicRegistryPathForScope(global)
	if err != nil {
		return nil, err
	}
	return store.LockPath(path + ".lock")
}

func updateDynamicRegistry(global bool, update func(dynamicIndex) (dynamicIndex, error)) (dynamicIndex, error) {
	lock, err := lockDynamicRegistry(global)
	if err != nil {
		return dynamicIndex{}, err
	}
	defer lock.Close()
	registry, err := loadDynamicRegistry(global)
	if err != nil {
		return dynamicIndex{}, err
	}
	registry, err = update(registry)
	if err != nil {
		return dynamicIndex{}, err
	}
	if err := saveDynamicRegistry(registry, global); err != nil {
		return dynamicIndex{}, err
	}
	return registry, nil
}

func dynamicCommandFromFile(name, file, description string) (dynamicCommand, error) {
	if strings.TrimSpace(name) == "" {
		return dynamicCommand{}, fmt.Errorf("dynamic command for %s requires a name", file)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return dynamicCommand{}, fmt.Errorf("read dynamic command %s: %w", file, err)
	}
	flags, err := compiler.ParseFlagDecls(file, string(data))
	if err != nil {
		return dynamicCommand{}, err
	}
	return dynamicCommand{Name: name, File: file, Description: description, Flags: flags}, nil
}

func dynamicCLICommand(item dynamicCommand, env commandEnv) *urfavecli.Command {
	flags := executeFlags()
	flags = append(flags, cliFlagsForDocumentFlags(item.Flags)...)
	usage := item.Description
	if usage == "" {
		usage = "run " + item.File
	}
	return &urfavecli.Command{
		Name:  item.Name,
		Usage: usage,
		Flags: flags,
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if cmd.Args().Len() > 0 {
				return fmt.Errorf("unexpected argument %q", cmd.Args().Get(0))
			}
			opts, err := executeOptions(cmd, env)
			if err != nil {
				return err
			}
			runner, err := agent.NewRunner(opts.ToolName, agent.Config{CodexPath: opts.CodexPath, ClaudePath: opts.ClaudePath})
			if err != nil {
				return err
			}
			opts.Runner = runner
			vars, err := valuesForDocumentFlags(cmd, item.Flags)
			if err != nil {
				return err
			}
			opts.Vars = vars
			runDir, err := commandRunDir(item.Name)
			if err != nil {
				return err
			}
			return runEphemeralFile(ctx, opts, item.File, runDir)
		},
	}
}

func commandRunDir(name string) (string, error) {
	base := filepath.Join(".", ".atm", "commands", sanitizePathPart(name))
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

func isTodoFileName(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(name, ".todo.md") || strings.HasSuffix(name, ".todo.txt") || strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".txt")
}

func commandNameFromFile(path string) string {
	name := filepath.Base(path)
	for _, suffix := range []string{".todo.md", ".todo.txt", ".md", ".txt"} {
		if strings.HasSuffix(strings.ToLower(name), suffix) {
			return strings.TrimSuffix(name, name[len(name)-len(suffix):])
		}
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}
