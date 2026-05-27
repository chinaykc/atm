package compiler

type commandKind string

const (
	commandTask       commandKind = "task"
	commandResume     commandKind = "resume"
	commandFork       commandKind = "fork"
	commandArgs       commandKind = "args"
	commandCd         commandKind = "cd"
	commandLet        commandKind = "let"
	commandBash       commandKind = "bash"
	commandIf         commandKind = "if"
	commandElse       commandKind = "else"
	commandCall       commandKind = "call"
	commandWebhook    commandKind = "webhook"
	commandWebhookUse commandKind = "webhook-use"
	commandOutput     commandKind = "output"
	commandDB         commandKind = "db"
	commandSkill      commandKind = "skill"
	commandMCP        commandKind = "mcp"
	commandContext    commandKind = "context"
	commandFor        commandKind = "for"
	commandGo         commandKind = "go"
	commandWait       commandKind = "wait"
	commandPrefixVar  commandKind = "prefix-var"
)

type command struct {
	Kind         commandKind
	Options      RunOptions
	For          forAST
	Condition    Condition
	Cd           CdCommand
	Bash         BashCommand
	Call         Call
	Webhook      WebhookCall
	WebhookUse   WebhookTaskConfig
	Output       *OutputSpec
	DB           DBTaskConfig
	Skill        SkillTaskConfig
	MCP          MCPTaskConfig
	ContextRefs  []string
	Pool         string
	TaskName     string
	ResumeTarget string
	ForkTarget   string
	LetName      string
	LetValue     string
	PrefixVar    string
}
