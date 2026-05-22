package dsl

import "time"

type Plan struct {
	SourcePath  string
	Globals     []GlobalBinding
	Pools       []PoolDecl
	DBs         []DBDecl
	Skills      []SkillDecl
	MCPs        []MCPDecl
	Imports     []ImportDecl
	Controls    []ControlBlock
	Definitions []Definition
	Tasks       []Task
}

type CompileOptions struct {
	Root string
}

type GlobalBinding struct {
	BlockIndex int
	Name       string
	Value      string
	BashScript string
}

type Task struct {
	BlockIndex int
	Prompt     string
	Ops        []Op
	Vars       map[string]any
	Output     *OutputSpec
	Return     *ReturnSpec
	DB         DBTaskConfig
	Skill      SkillTaskConfig
	MCP        MCPTaskConfig
	Cursor     ExecutionCursor
}

type OpKind string

type Op struct {
	Kind           OpKind
	ExecuteOptions RunOptions
	For            For
	Bash           BashCommand
	Cd             CdCommand
	Pool           string
	Call           Call
	Return         ReturnSpec
}

type For struct {
	Options   RunOptions
	Condition Condition
	MaxRuns   int
	VarName   string
	Values    []string
	Source    Condition
}

type ExecutionCursor struct {
	Active    bool
	Start     time.Time
	OpIndex   int
	RunIndex  int
	TotalRuns int
}

const (
	OpCd      OpKind = "cd"
	OpBash    OpKind = "bash"
	OpFor     OpKind = "for"
	OpGo      OpKind = "go"
	OpWait    OpKind = "wait"
	OpCall    OpKind = "call"
	OpReturn  OpKind = "return"
	OpExecute OpKind = "execute"
)

func lowerTaskASTToIR(index int, t taskAST) Task {
	ops := make([]Op, 0, len(t.flow)+1)
	hasFor := false
	for _, op := range t.flow {
		switch op.kind {
		case astOpCd:
			ops = append(ops, Op{Kind: OpCd, Cd: op.CdCommand})
		case astOpBash:
			ops = append(ops, Op{Kind: OpBash, Bash: op.BashCommand})
		case astOpFor:
			hasFor = true
			ops = append(ops, Op{Kind: OpFor, For: forFromAST(op.step)})
		case astOpGo:
			ops = append(ops, Op{Kind: OpGo, Pool: op.Pool})
		case astOpWait:
			ops = append(ops, Op{Kind: OpWait, Pool: op.Pool})
		case astOpCall:
			ops = append(ops, Op{Kind: OpCall, Call: op.Call})
		case astOpReturn:
			ops = append(ops, Op{Kind: OpReturn, Return: op.Return})
		}
	}

	executeOptions := RunOptions{}
	if len(t.steps) > 0 && !hasFor {
		executeOptions = t.steps[0].Options
	}
	ops = append(ops, Op{Kind: OpExecute, ExecuteOptions: executeOptions})
	if t.returnSpec != nil {
		ops = append(ops, Op{Kind: OpReturn, Return: *t.returnSpec})
	}

	return Task{
		BlockIndex: index,
		Prompt:     t.prompt,
		Ops:        ops,
		Vars:       CloneVars(t.vars),
		Output:     cloneOutputSpec(t.output),
		Return:     cloneReturnSpec(t.returnSpec),
		DB:         cloneDBTaskConfig(t.db),
		Skill:      cloneSkillTaskConfig(t.skill),
		MCP:        cloneMCPTaskConfig(t.mcp),
		Cursor:     cursorFromRunningInfo(t.running),
	}
}

func cloneDBTaskConfig(config DBTaskConfig) DBTaskConfig {
	out := DBTaskConfig{IgnoreAll: config.IgnoreAll}
	out.Ignore = append([]string{}, config.Ignore...)
	for _, use := range config.Use {
		out.Use = append(out.Use, DBUse{Names: append([]string{}, use.Names...), Access: use.Access})
	}
	for _, rule := range config.Access {
		out.Access = append(out.Access, DBAccessRule{Names: append([]string{}, rule.Names...), Access: rule.Access})
	}
	return out
}

func cloneSkillTaskConfig(config SkillTaskConfig) SkillTaskConfig {
	return SkillTaskConfig{
		IgnoreAll: config.IgnoreAll,
		Use:       append([]string{}, config.Use...),
		Ignore:    append([]string{}, config.Ignore...),
	}
}

func cloneMCPTaskConfig(config MCPTaskConfig) MCPTaskConfig {
	return MCPTaskConfig{
		IgnoreAll: config.IgnoreAll,
		Use:       append([]string{}, config.Use...),
		Ignore:    append([]string{}, config.Ignore...),
		DefUse:    append([]string{}, config.DefUse...),
	}
}

func cloneOutputSpec(spec *OutputSpec) *OutputSpec {
	if spec == nil {
		return nil
	}
	out := *spec
	return &out
}

func cloneReturnSpec(spec *ReturnSpec) *ReturnSpec {
	if spec == nil {
		return nil
	}
	out := *spec
	out.Output = cloneOutputSpec(spec.Output)
	return &out
}

func forFromAST(step forAST) For {
	return For{
		Options:   step.Options,
		Condition: step.Condition,
		MaxRuns:   step.MaxRuns,
		VarName:   step.VarName,
		Values:    append([]string{}, step.Values...),
		Source:    step.Source,
	}
}

func cursorFromRunningInfo(info RunningInfo) ExecutionCursor {
	return ExecutionCursor{
		Active:    info.Active,
		Start:     info.Start,
		OpIndex:   info.StepIndex,
		RunIndex:  info.StepRuns,
		TotalRuns: info.TotalRuns,
	}
}
