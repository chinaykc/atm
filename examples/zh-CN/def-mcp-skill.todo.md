/skill new release_reviewer from ../skills/release-reviewer

/def inspect_area area
/skill use release_reviewer
审查 {{area}} 发布区域。
只返回一份紧凑的发布审查结果，包含：
- risk
- evidence
- next_action
/return {{agent.last_message}}


/cd .atm/demo/def-mcp-skill
/skill use release_reviewer
/mcp def use inspect_area
使用可用的 ATM definition MCP 工具审查这些发布区域：
1. api
2. docs
每个区域调用一次 definition 工具。然后用普通文本汇总整体发布风险。
