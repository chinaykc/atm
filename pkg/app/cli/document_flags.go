package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chinaykc/atm/pkg/lang/compiler"
	urfavecli "github.com/urfave/cli/v3"
)

func addDocumentFlagsForArgs(root *urfavecli.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	name := args[0]
	if name != "run" {
		return nil
	}
	files := inferRunFilesFromArgs(args[1:])
	if len(files) == 0 {
		return nil
	}
	var all []compiler.FlagDecl
	seen := map[string]compiler.FlagDecl{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		flags, err := compiler.ParseFlagDecls(file, string(data))
		if err != nil {
			return err
		}
		for _, flag := range flags {
			if existing, ok := seen[flag.Name]; ok && existing.Type != flag.Type {
				return fmt.Errorf("flag %q has conflicting types across input files", flag.Name)
			}
			if _, ok := seen[flag.Name]; !ok {
				seen[flag.Name] = flag
				all = append(all, flag)
			}
		}
	}
	if len(all) == 0 {
		return nil
	}
	cmd := root.Command(name)
	if cmd == nil {
		return nil
	}
	cmd.Flags = append(cmd.Flags, cliFlagsForDocumentFlags(all)...)
	return nil
}

func inferRunFilesFromArgs(args []string) []string {
	var files []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			files = append(files, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			if flagLikelyTakesValue(arg) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		files = append(files, arg)
	}
	return files
}

func flagLikelyTakesValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	switch arg {
	case "-tool", "--tool", "-codex", "--codex", "-claude", "--claude", "-messages", "--messages", "-retries", "--retries", "-output", "--output", "-o", "-jobs", "--jobs":
		return true
	default:
		return false
	}
}

func cliFlagsForDocumentFlags(flags []compiler.FlagDecl) []urfavecli.Flag {
	var out []urfavecli.Flag
	for _, flag := range flags {
		usage := flag.Description
		if flag.HasDefault {
			usage += " (default: " + flag.Default + ")"
		}
		switch flag.Type {
		case "bool":
			out = append(out, &urfavecli.BoolFlag{Name: flag.Name, Usage: usage})
		default:
			out = append(out, &urfavecli.StringSliceFlag{Name: flag.Name, Usage: usage})
		}
	}
	return out
}

func valuesForDocumentFlags(cmd *urfavecli.Command, flags []compiler.FlagDecl) (map[string]any, error) {
	raw := map[string][]string{}
	for _, flag := range flags {
		if !cmd.IsSet(flag.Name) {
			continue
		}
		if flag.Type == "bool" {
			raw[flag.Name] = []string{fmt.Sprint(cmd.Bool(flag.Name))}
			continue
		}
		raw[flag.Name] = cmd.StringSlice(flag.Name)
	}
	return compiler.CoerceFlagValues(flags, raw)
}

func documentFlagsForFiles(files []string) ([]compiler.FlagDecl, error) {
	var flags []compiler.FlagDecl
	for _, file := range files {
		path := file
		if abs, err := filepath.Abs(file); err == nil {
			path = abs
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		decls, err := compiler.ParseFlagDecls(path, string(data))
		if err != nil {
			return nil, err
		}
		flags = append(flags, decls...)
	}
	return flags, nil
}
