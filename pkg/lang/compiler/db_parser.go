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
	if len(fields) < 2 {
		return DBTaskConfig{}, true, fmt.Errorf("/db requires subcommand")
	}
	var out DBTaskConfig
	switch fields[1] {
	case "use":
		names, access, err := parseDBNamesWithOptionalAccess(fields[2:])
		if err != nil {
			return DBTaskConfig{}, true, err
		}
		if len(names) == 0 {
			return DBTaskConfig{}, true, fmt.Errorf("/db use requires at least one name")
		}
		out.Use = append(out.Use, DBUse{Names: names, Access: access})
	case "access":
		if len(fields) < 4 {
			return DBTaskConfig{}, true, fmt.Errorf("/db access requires name(s) and access level")
		}
		access, err := parseDBAccess(fields[len(fields)-1])
		if err != nil {
			return DBTaskConfig{}, true, err
		}
		names := slices.Clone(fields[2 : len(fields)-1])
		if err := validateDBNamesOrStar(names); err != nil {
			return DBTaskConfig{}, true, err
		}
		out.Access = append(out.Access, DBAccessRule{Names: names, Access: access})
	case "ignore":
		if len(fields) == 2 {
			out.IgnoreAll = true
			return out, true, nil
		}
		names := slices.Clone(fields[2:])
		if err := validateDBNames(names); err != nil {
			return DBTaskConfig{}, true, err
		}
		out.Ignore = append(out.Ignore, names...)
	case "new":
		return DBTaskConfig{}, true, fmt.Errorf("/db new must be written as a standalone global block")
	default:
		return DBTaskConfig{}, true, fmt.Errorf("unsupported /db subcommand %q", fields[1])
	}
	return out, true, nil
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
