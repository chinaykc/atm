package compiler

// taskAST is the parser-level representation. It preserves source command
// structure closely enough to lower into IR, but it is not executed directly.
type taskAST struct {
	name         string
	contextRefs  []string
	context      string
	prompt       string
	goRun        bool
	wait         bool
	steps        []forAST
	flow         []astOp
	vars         map[string]any
	bashCommands []BashCommand
	output       *OutputSpec
	returnSpec   *ReturnSpec
	running      RunningInfo
	db           DBTaskConfig
	skill        SkillTaskConfig
	mcp          MCPTaskConfig
	webhook      WebhookTaskConfig
}

type astOp struct {
	kind        string
	step        forAST
	Condition   Condition
	BashCommand BashCommand
	CdCommand   CdCommand
	Pool        string
	Call        Call
	Webhook     WebhookCall
	Return      ReturnSpec
}

const (
	astOpCd      = "cd"
	astOpBash    = "bash"
	astOpFor     = "for"
	astOpIf      = "if"
	astOpElse    = "else"
	astOpGo      = "go"
	astOpWait    = "wait"
	astOpCall    = "call"
	astOpWebhook = "webhook"
	astOpReturn  = "return"
)

type forAST struct {
	Options   RunOptions
	Condition Condition
	MaxRuns   int
	VarName   string
	Values    []string
	Source    Condition
}
