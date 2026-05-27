package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
)

func flagCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "flag",
		Usage: "register ATM files as dynamic commands",
		Commands: []*urfavecli.Command{
			flagRegisterCommand(env),
			flagScanCommand(env),
			flagUnregisterCommand(env),
			flagListCommand(env),
		},
	}
}

func flagRegisterCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "register",
		Usage:     "register an ATM file as a dynamic command",
		ArgsUsage: "<file>",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
			&urfavecli.StringFlag{Name: "name", Usage: "command name; defaults to the file basename"},
			&urfavecli.StringFlag{Name: "description", Usage: "help text for the dynamic command"},
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			file, err := singleTodoFileArg(cmd)
			if err != nil {
				return err
			}
			name := strings.TrimSpace(cmd.String("name"))
			if name == "" {
				name = commandNameFromFile(file)
			}
			description := cmd.String("description")
			global := cmd.Bool("global")
			storedFile, err := registryFilePath(file, global)
			if err != nil {
				return err
			}
			if _, err := updateDynamicRegistry(global, func(registry dynamicIndex) (dynamicIndex, error) {
				if _, err := dynamicCommandFromFile(name, storedFile, description); err != nil {
					return dynamicIndex{}, err
				}
				registry, err := addDynamicRegistration(registry, dynamicRegistration{Name: name, File: storedFile, Description: description})
				if err != nil {
					return dynamicIndex{}, err
				}
				if _, err := dynamicCommandsFromRegistry(registry); err != nil {
					return dynamicIndex{}, err
				}
				return registry, nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(env.Stdout, "atm flag registered (%s): %s -> %s\n", registryScopeName(global), name, storedFile)
			return nil
		},
	}
}

func flagScanCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "scan",
		Usage: "scan ./.atm/flag once and register ATM files",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			if err := rejectArgs(cmd); err != nil {
				return err
			}
			global := cmd.Bool("global")
			entries, err := scanDynamicRegistrations(filepath.Join(".", ".atm", "flag"), global)
			if err != nil {
				return err
			}
			if _, err := updateDynamicRegistry(global, func(registry dynamicIndex) (dynamicIndex, error) {
				for _, entry := range entries {
					var addErr error
					registry, addErr = addDynamicRegistration(registry, entry)
					if addErr != nil {
						return dynamicIndex{}, addErr
					}
				}
				if _, err := dynamicCommandsFromRegistry(registry); err != nil {
					return dynamicIndex{}, err
				}
				return registry, nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(env.Stdout, "atm flag scan registered %d command(s) in %s registry\n", len(entries), registryScopeName(global))
			return nil
		},
	}
}

func flagUnregisterCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "unregister",
		Usage:     "remove a dynamic command registration",
		ArgsUsage: "<name-or-file>",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			target, err := singleTodoFileArg(cmd)
			if err != nil {
				return err
			}
			global := cmd.Bool("global")
			if _, err := updateDynamicRegistry(global, func(registry dynamicIndex) (dynamicIndex, error) {
				var kept []dynamicRegistration
				for _, entry := range registry.Commands {
					nameMatch := entry.Name == target
					fileMatch := filepath.Clean(entry.File) == filepath.Clean(target)
					if !nameMatch && !fileMatch {
						kept = append(kept, entry)
					}
				}
				if len(kept) == len(registry.Commands) {
					return dynamicIndex{}, fmt.Errorf("dynamic command registration not found: %s", target)
				}
				registry.Commands = kept
				return registry, nil
			}); err != nil {
				return err
			}
			fmt.Fprintf(env.Stdout, "atm flag unregistered (%s): %s\n", registryScopeName(global), target)
			return nil
		},
	}
}

func flagListCommand(env commandEnv) *urfavecli.Command {
	return &urfavecli.Command{
		Name:  "list",
		Usage: "list registered dynamic commands",
		Flags: []urfavecli.Flag{
			registryScopeFlag(),
		},
		Action: func(_ context.Context, cmd *urfavecli.Command) error {
			if err := rejectArgs(cmd); err != nil {
				return err
			}
			registry, err := loadDynamicRegistry(cmd.Bool("global"))
			if err != nil {
				return err
			}
			for _, entry := range registry.Commands {
				if entry.Description == "" {
					fmt.Fprintf(env.Stdout, "%s\t%s\n", entry.Name, entry.File)
					continue
				}
				fmt.Fprintf(env.Stdout, "%s\t%s\t%s\n", entry.Name, entry.File, entry.Description)
			}
			return nil
		},
	}
}

func addDynamicRegistration(registry dynamicIndex, entry dynamicRegistration) (dynamicIndex, error) {
	if strings.TrimSpace(entry.Name) == "" {
		return dynamicIndex{}, fmt.Errorf("dynamic command for %s requires a name", entry.File)
	}
	for i := range registry.Commands {
		if registry.Commands[i].Name != entry.Name {
			continue
		}
		if filepath.Clean(registry.Commands[i].File) != filepath.Clean(entry.File) {
			return dynamicIndex{}, fmt.Errorf("dynamic command %q maps to both %s and %s", entry.Name, registry.Commands[i].File, entry.File)
		}
		registry.Commands[i] = entry
		return registry, nil
	}
	registry.Commands = append(registry.Commands, entry)
	slices.SortFunc(registry.Commands, func(a, b dynamicRegistration) int {
		return strings.Compare(a.Name, b.Name)
	})
	return registry, nil
}

func scanDynamicRegistrations(root string, global bool) ([]dynamicRegistration, error) {
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
	var out []dynamicRegistration
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isTodoFileName(path) {
			return nil
		}
		storedFile, err := registryFilePath(path, global)
		if err != nil {
			return err
		}
		out = append(out, dynamicRegistration{Name: commandNameFromFile(path), File: storedFile})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b dynamicRegistration) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}
