package compiler

import (
	"encoding/json"
	"fmt"
)

type SymbolTable struct {
	Definitions     map[string]Definition
	DefinitionItems []Definition
	Pools           map[string]PoolDecl
	PoolItems       []PoolDecl
	DBs             map[string]DBDecl
	DBItems         []DBDecl
	Skills          map[string]SkillDecl
	SkillItems      []SkillDecl
	MCPs            map[string]MCPDecl
	MCPItems        []MCPDecl
}

func BuildSymbolTable(plan Plan) (SymbolTable, error) {
	symbols := SymbolTable{
		Definitions: make(map[string]Definition, len(plan.Definitions)),
		Pools:       make(map[string]PoolDecl, len(plan.Pools)),
		DBs:         make(map[string]DBDecl, len(plan.DBs)),
		Skills:      make(map[string]SkillDecl, len(plan.Skills)),
		MCPs:        make(map[string]MCPDecl, len(plan.MCPs)),
	}
	for _, def := range plan.Definitions {
		symbols.Definitions[def.Name] = def
		symbols.DefinitionItems = append(symbols.DefinitionItems, def)
	}
	for _, pool := range plan.Pools {
		if existing, ok := symbols.Pools[pool.Name]; ok {
			return symbols, fmt.Errorf("pool %q already declared at block %d", pool.Name, existing.BlockIndex+1)
		}
		symbols.Pools[pool.Name] = pool
		symbols.PoolItems = append(symbols.PoolItems, pool)
	}
	for _, db := range plan.DBs {
		if existing, ok := symbols.DBs[db.Name]; ok {
			return symbols, fmt.Errorf("db %q already declared at block %d", db.Name, existing.BlockIndex+1)
		}
		symbols.DBs[db.Name] = db
		symbols.DBItems = append(symbols.DBItems, db)
	}
	for _, skill := range plan.Skills {
		if existing, ok := symbols.Skills[skill.Name]; ok {
			return symbols, fmt.Errorf("skill %q already declared at block %d", skill.Name, existing.BlockIndex+1)
		}
		symbols.Skills[skill.Name] = skill
		symbols.SkillItems = append(symbols.SkillItems, skill)
	}
	for _, mcp := range plan.MCPs {
		if isBuiltinMCPName(mcp.Name) {
			return symbols, fmt.Errorf("mcp name %q conflicts with an ATM builtin MCP server", mcp.Name)
		}
		if !json.Valid([]byte(mcp.Config)) {
			return symbols, fmt.Errorf("mcp %q config must be valid JSON", mcp.Name)
		}
		if existing, ok := symbols.MCPs[mcp.Name]; ok {
			return symbols, fmt.Errorf("mcp %q already declared at block %d", mcp.Name, existing.BlockIndex+1)
		}
		symbols.MCPs[mcp.Name] = mcp
		symbols.MCPItems = append(symbols.MCPItems, mcp)
	}
	return symbols, nil
}
