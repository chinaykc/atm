package compiler

import (
	"fmt"
	"github.com/chinaykc/atm/pkg/lang/marker"
	"slices"
	"strings"
)

func ParseGlobalDBBlock(body string) (DBDecl, bool, error) {
	body, _, err := marker.StripRunning(body)
	if err != nil {
		return DBDecl{}, false, err
	}
	line, rest, ok := firstNonBlankLineWithRest(body)
	if !ok {
		return DBDecl{}, false, nil
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/db" {
		return DBDecl{}, false, nil
	}
	if len(fields) < 3 || fields[1] != "new" {
		return DBDecl{}, false, nil
	}
	if len(fields) > 6 {
		return DBDecl{}, true, fmt.Errorf("/db new accepts name plus optional scope, persist, and access")
	}
	decl := DBDecl{
		Name:    fields[2],
		Scope:   DBScopeGlobal,
		Persist: DBPersistRun,
		Access:  DBAccessAdmin,
		Usage:   strings.TrimSpace(rest),
	}
	if !isVariableName(decl.Name) {
		return DBDecl{}, true, fmt.Errorf("invalid db name %q", decl.Name)
	}
	for _, field := range fields[3:] {
		key, value, ok := strings.Cut(field, ":")
		if !ok {
			return DBDecl{}, true, fmt.Errorf("unsupported /db new option %q", field)
		}
		switch key {
		case "scope":
			scope, err := parseDBScope(value)
			if err != nil {
				return DBDecl{}, true, err
			}
			decl.Scope = scope
		case "persist":
			persist, err := parseDBPersistence(value)
			if err != nil {
				return DBDecl{}, true, err
			}
			decl.Persist = persist
		case "access":
			access, err := parseDBAccess(value)
			if err != nil {
				return DBDecl{}, true, err
			}
			decl.Access = access
		default:
			return DBDecl{}, true, fmt.Errorf("unsupported /db new option %q", field)
		}
	}
	return decl, true, nil
}

func parseDBTaskLine(line string) (DBTaskConfig, bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 || fields[0] != "/db" {
		return DBTaskConfig{}, false, nil
	}
	out, next, err := parseDBTaskFields(fields, 0)
	if err != nil {
		return DBTaskConfig{}, true, err
	}
	if next != len(fields) {
		return DBTaskConfig{}, true, fmt.Errorf("unexpected command argument %q", fields[next])
	}
	return out, true, nil
}

func parseDBTaskFields(fields []string, start int) (DBTaskConfig, int, error) {
	if start >= len(fields) || fields[start] != "/db" {
		return DBTaskConfig{}, start, fmt.Errorf("expected /db")
	}
	if start+1 >= len(fields) || isCommandToken(fields[start+1]) {
		return DBTaskConfig{}, start, fmt.Errorf("/db requires subcommand")
	}
	var out DBTaskConfig
	switch fields[start+1] {
	case "use":
		args, next := collectCommandArgs(fields, start+2)
		names, access, err := parseDBNamesWithOptionalAccess(args)
		if err != nil {
			return DBTaskConfig{}, start, err
		}
		if len(names) == 0 {
			return DBTaskConfig{}, start, fmt.Errorf("/db use requires at least one name")
		}
		out.Use = append(out.Use, DBUse{Names: names, Access: access})
		return out, next, nil
	case "access":
		args, next := collectCommandArgs(fields, start+2)
		if len(args) < 2 {
			return DBTaskConfig{}, start, fmt.Errorf("/db access requires name(s) and access level")
		}
		access, err := parseDBAccess(args[len(args)-1])
		if err != nil {
			return DBTaskConfig{}, start, err
		}
		names := slices.Clone(args[:len(args)-1])
		if err := validateDBNamesOrStar(names); err != nil {
			return DBTaskConfig{}, start, err
		}
		out.Access = append(out.Access, DBAccessRule{Names: names, Access: access})
		return out, next, nil
	case "ignore":
		args, next := collectCommandArgs(fields, start+2)
		if len(args) == 0 {
			out.IgnoreAll = true
			return out, next, nil
		}
		names := slices.Clone(args)
		if err := validateDBNames(names); err != nil {
			return DBTaskConfig{}, start, err
		}
		out.Ignore = append(out.Ignore, names...)
		return out, next, nil
	case "new":
		return DBTaskConfig{}, start, fmt.Errorf("/db new must be written as a standalone global block")
	default:
		return DBTaskConfig{}, start, fmt.Errorf("unsupported /db subcommand %q", fields[start+1])
	}
}

func parseDBNamesWithOptionalAccess(fields []string) ([]string, DBAccess, error) {
	var names []string
	var access DBAccess
	for _, field := range fields {
		if strings.HasPrefix(field, "access:") {
			if access != "" {
				return nil, "", fmt.Errorf("/db use accepts access only once")
			}
			parsed, err := parseDBAccess(strings.TrimPrefix(field, "access:"))
			if err != nil {
				return nil, "", err
			}
			access = parsed
			continue
		}
		names = append(names, field)
	}
	if err := validateDBNames(names); err != nil {
		return nil, "", err
	}
	return names, access, nil
}

func validateDBNames(names []string) error {
	for _, name := range names {
		if !isVariableName(name) {
			return fmt.Errorf("invalid db name %q", name)
		}
	}
	return nil
}

func validateDBNamesOrStar(names []string) error {
	for _, name := range names {
		if name == "*" {
			continue
		}
		if !isVariableName(name) {
			return fmt.Errorf("invalid db name %q", name)
		}
	}
	return nil
}

func parseDBScope(value string) (DBScope, error) {
	switch DBScope(value) {
	case DBScopeLocal:
		return DBScopeLocal, nil
	case DBScopeGlobal:
		return DBScopeGlobal, nil
	default:
		return "", fmt.Errorf("invalid db scope %q", value)
	}
}

func parseDBPersistence(value string) (DBPersistence, error) {
	switch DBPersistence(value) {
	case DBPersistRun:
		return DBPersistRun, nil
	case DBPersistProject:
		return DBPersistProject, nil
	default:
		return "", fmt.Errorf("invalid db persistence %q", value)
	}
}

func parseDBAccess(value string) (DBAccess, error) {
	switch DBAccess(value) {
	case DBAccessRead:
		return DBAccessRead, nil
	case DBAccessAppend:
		return DBAccessAppend, nil
	case DBAccessWrite:
		return DBAccessWrite, nil
	case DBAccessAdmin:
		return DBAccessAdmin, nil
	default:
		return "", fmt.Errorf("invalid db access %q", value)
	}
}
