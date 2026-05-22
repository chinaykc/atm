/skill new release_reviewer from skills/release-reviewer

/def inspect_area area
/skill use release_reviewer
Inspect the {{area}} release area.
Return only a compact release review with:
- risk
- evidence
- next_action
/return {{agent.last_message}}


/cd .atm/demo/def-mcp-skill
/skill use release_reviewer
/mcp def use inspect_area
Use the available ATM definition MCP tool to inspect these release areas:
1. api
2. docs
Call the definition tool once per area. Then summarize the combined release risk in ordinary prose.
