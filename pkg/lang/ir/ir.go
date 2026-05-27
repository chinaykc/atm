package ir

import (
	"slices"
	"strings"
	"time"

	"github.com/chinaykc/atm/pkg/lang/syntax"
)

type Plan struct {
	SourcePath  string
	Diagnostics []syntax.Diagnostic
	Flags       []FlagDecl
	Globals     []GlobalBinding
	Pools       []PoolDecl
	DBs         []DBDecl
	Skills      []SkillDecl
	MCPs        []MCPDecl
	Webhooks    []WebhookDecl
	Imports     []ImportDecl
	Controls    []ControlBlock
	Definitions []Definition
	Tasks       []Task
}

type FlagDecl struct {
	BlockIndex  int
	Type        string
	Name        string
	Description string
	Default     string
	HasDefault  bool
	SourcePath  string
	Scope       []string
	Line        int
}

type WebhookDecl struct {
	BlockIndex int
	Name       string
	Provider   string
	URL        string
	URLEnv     string
	Secret     string
	SecretEnv  string
	Keywords   []string
	SourcePath string
	Scope      []string
	Line       int
}

type WebhookCall struct {
	Name          string
	Message       string
	Payload       string
	PayloadFormat string
}

type WebhookTaskConfig struct {
	Use []string `json:"use,omitempty"`
}

func (c WebhookTaskConfig) IsZero() bool {
	return len(c.Use) == 0
}

type GlobalBinding struct {
	BlockIndex int
	Name       string
	Value      string
	BashScript string
	SourcePath string
	Scope      []string
	Line       int
}

type Task struct {
	BlockIndex       int
	Name             string
	SourcePath       string
	Scope            []string
	Line             int
	Context          string
	ContextRefs      []string
	SourcePromptHash string
	HasParent        bool
	ParentIndex      int
	Prompt           string
	Flow             FlowNode
	Vars             map[string]any
	Output           *OutputSpec
	Return           *ReturnSpec
	DB               DBTaskConfig
	Skill            SkillTaskConfig
	MCP              MCPTaskConfig
	Webhook          WebhookTaskConfig
	Cursor           ExecutionCursor
}

type FlowKind string

const (
	FlowSeq     FlowKind = "seq"
	FlowCd      FlowKind = "cd"
	FlowBash    FlowKind = "bash"
	FlowFor     FlowKind = "for"
	FlowIf      FlowKind = "if"
	FlowGo      FlowKind = "go"
	FlowWait    FlowKind = "wait"
	FlowCall    FlowKind = "call"
	FlowWebhook FlowKind = "webhook"
	FlowReturn  FlowKind = "return"
	FlowExecute FlowKind = "execute"
)

type FlowNode struct {
	Kind           FlowKind
	ExecuteOptions RunOptions
	Prompt         string
	For            For
	If             If
	Bash           BashCommand
	Cd             CdCommand
	Pool           string
	Call           Call
	Webhook        WebhookCall
	Return         ReturnSpec
	Children       []FlowNode
	ElseChildren   []FlowNode
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

type FlatOpKind string

type FlatOp struct {
	Kind           FlatOpKind
	ExecuteOptions RunOptions
	For            For
	If             If
	Bash           BashCommand
	Cd             CdCommand
	Pool           string
	Call           Call
	Return         ReturnSpec
	Webhook        WebhookCall
}

const (
	FlatOpCd      FlatOpKind = "cd"
	FlatOpBash    FlatOpKind = "bash"
	FlatOpFor     FlatOpKind = "for"
	FlatOpIf      FlatOpKind = "if"
	FlatOpGo      FlatOpKind = "go"
	FlatOpWait    FlatOpKind = "wait"
	FlatOpCall    FlatOpKind = "call"
	FlatOpWebhook FlatOpKind = "webhook"
	FlatOpReturn  FlatOpKind = "return"
	FlatOpExecute FlatOpKind = "execute"
)

type BashCommand struct {
	Name   string
	Script string
}

type CdCommand struct {
	Path      string
	MustExist bool
}

type OutputSpec struct {
	FileName     string
	Schema       string
	SchemaFormat string
	Structured   bool
}

func (s *OutputSpec) IsStructured() bool {
	return s != nil && (s.Structured || strings.TrimSpace(s.Schema) != "")
}

type RunOptions struct {
	Resume          bool
	ResumeTarget    string
	ResumeSessionID string
	Fork            bool
	ForkTarget      string
	Args            []string
	Output          *OutputSpec
	DBs             []DBRuntime
	Workdir         string
	Skills          []SkillRuntime
	MCPs            []MCPRuntime
	DefMCP          *DefMCPRuntime
	DefDepth        int
}

type ConditionKind string

const (
	ConditionNone    ConditionKind = ""
	ConditionNatural ConditionKind = "natural"
	ConditionExpr    ConditionKind = "expr"
	ConditionCall    ConditionKind = "call"
)

type Condition struct {
	Kind ConditionKind
	Text string
}

type IfBlock struct {
	Condition  Condition
	HeaderOnly bool
	Body       string
}

type ElseBlock struct {
	HeaderOnly bool
	Body       string
}

type ControlBlock struct {
	BlockIndex int
	Kind       string
	Condition  Condition
	HeaderOnly bool
}

type If struct {
	Condition Condition
}

type Call struct {
	Name   string
	Args   []string
	Assign string
}

type ReturnKind string

const (
	ReturnTemplate   ReturnKind = "template"
	ReturnBash       ReturnKind = "bash"
	ReturnStructured ReturnKind = "structured"
)

type ReturnSpec struct {
	Kind   ReturnKind
	Text   string
	Script string
	Output *OutputSpec
}

type OutputMessage struct {
	Tool  string
	Role  string
	Agent string
	Text  string
}

type LetBinding struct {
	Name       string
	Value      string
	BashScript string
}

type PoolDecl struct {
	BlockIndex int
	Name       string
	Max        int
	Buffer     int
	SourcePath string
	Scope      []string
	Line       int
}

type Definition struct {
	Name              string
	Params            []string
	Blocks            []DefinitionBlock
	SourcePath        string
	BlockIndex        int
	Scope             []string
	Line              int
	VisibleSourcePath string
	VisibleScope      []string
	VisibleLine       int
	List              bool
}

type DefinitionBlock struct {
	Body string
	Span syntax.SourceSpan `json:"-"`
}

type ImportDecl struct {
	BlockIndex int
	Path       string
	Namespace  string
	SourcePath string
	Scope      []string
	Line       int
}

type DBScope string

const (
	DBScopeLocal  DBScope = "local"
	DBScopeGlobal DBScope = "global"
)

type DBPersistence string

const (
	DBPersistRun     DBPersistence = "run"
	DBPersistProject DBPersistence = "project"
)

type DBAccess string

const (
	DBAccessRead   DBAccess = "read"
	DBAccessAppend DBAccess = "append"
	DBAccessWrite  DBAccess = "write"
	DBAccessAdmin  DBAccess = "admin"
)

type DBDecl struct {
	BlockIndex int
	Name       string
	Scope      DBScope
	Persist    DBPersistence
	Access     DBAccess
	Usage      string
	SourcePath string
	ScopePath  []string
	Line       int
}

type DBUse struct {
	Names  []string `json:"names"`
	Access DBAccess `json:"access,omitempty"`
}

type DBAccessRule struct {
	Names  []string `json:"names"`
	Access DBAccess `json:"access"`
}

type DBTaskConfig struct {
	IgnoreAll bool           `json:"ignoreAll,omitempty"`
	Ignore    []string       `json:"ignore,omitempty"`
	Use       []DBUse        `json:"use,omitempty"`
	Access    []DBAccessRule `json:"access,omitempty"`
}

func (c DBTaskConfig) IsZero() bool {
	return !c.IgnoreAll && len(c.Ignore) == 0 && len(c.Use) == 0 && len(c.Access) == 0
}

type DBRuntime struct {
	Name    string        `json:"name"`
	Path    string        `json:"path"`
	Scope   DBScope       `json:"scope"`
	Persist DBPersistence `json:"persist"`
	Access  DBAccess      `json:"access"`
	Usage   string        `json:"usage,omitempty"`
}

type SkillDecl struct {
	BlockIndex int
	Name       string
	Path       string
	SourcePath string
	Scope      []string
	Line       int
}

type SkillTaskConfig struct {
	IgnoreAll bool     `json:"ignoreAll,omitempty"`
	Use       []string `json:"use,omitempty"`
	Ignore    []string `json:"ignore,omitempty"`
}

func (c SkillTaskConfig) IsZero() bool {
	return !c.IgnoreAll && len(c.Use) == 0 && len(c.Ignore) == 0
}

type SkillRuntime struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type MCPDecl struct {
	BlockIndex int
	Name       string
	Config     string
	SourcePath string
	Scope      []string
	Line       int
}

type MCPTaskConfig struct {
	IgnoreAll bool     `json:"ignoreAll,omitempty"`
	Use       []string `json:"use,omitempty"`
	Ignore    []string `json:"ignore,omitempty"`
	DefUse    []string `json:"defUse,omitempty"`
}

func (c MCPTaskConfig) IsZero() bool {
	return !c.IgnoreAll && len(c.Use) == 0 && len(c.Ignore) == 0 && len(c.DefUse) == 0
}

type MCPRuntime struct {
	Name          string   `json:"name"`
	Config        string   `json:"config"`
	ApprovedTools []string `json:"approvedTools,omitempty"`
}

type DefMCPRuntime struct {
	TodoPath    string          `json:"todo_path"`
	Definitions []string        `json:"definitions"`
	URL         string          `json:"-"`
	Tool        string          `json:"tool"`
	CodexPath   string          `json:"codex_path,omitempty"`
	ClaudePath  string          `json:"claude_path,omitempty"`
	Workdir     string          `json:"workdir,omitempty"`
	DBs         []DBRuntime     `json:"dbs,omitempty"`
	Skills      []SkillRuntime  `json:"skills,omitempty"`
	MCPs        []MCPRuntime    `json:"mcps,omitempty"`
	Vars        map[string]any  `json:"vars,omitempty"`
	OutputDir   string          `json:"output_dir,omitempty"`
	Messages    int             `json:"messages,omitempty"`
	Jobs        int             `json:"jobs,omitempty"`
	Depth       int             `json:"depth"`
	Defs        []DefinitionRef `json:"defs,omitempty"`
}

type DefinitionRef struct {
	Name   string   `json:"name"`
	Params []string `json:"params"`
}

func CloneDBTaskConfig(config DBTaskConfig) DBTaskConfig {
	out := DBTaskConfig{IgnoreAll: config.IgnoreAll}
	out.Ignore = slices.Clone(config.Ignore)
	for _, use := range config.Use {
		out.Use = append(out.Use, DBUse{Names: slices.Clone(use.Names), Access: use.Access})
	}
	for _, rule := range config.Access {
		out.Access = append(out.Access, DBAccessRule{Names: slices.Clone(rule.Names), Access: rule.Access})
	}
	return out
}

func CloneSkillTaskConfig(config SkillTaskConfig) SkillTaskConfig {
	return SkillTaskConfig{
		IgnoreAll: config.IgnoreAll,
		Use:       slices.Clone(config.Use),
		Ignore:    slices.Clone(config.Ignore),
	}
}

func CloneMCPTaskConfig(config MCPTaskConfig) MCPTaskConfig {
	return MCPTaskConfig{
		IgnoreAll: config.IgnoreAll,
		Use:       slices.Clone(config.Use),
		Ignore:    slices.Clone(config.Ignore),
		DefUse:    slices.Clone(config.DefUse),
	}
}

func DefMCPToolName(name string) string {
	name = strings.ReplaceAll(name, ".", "__")
	var b strings.Builder
	b.WriteString("atm_def_")
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
