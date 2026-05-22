package dsl

import (
	"regexp"
	"strings"
	"time"
)

var doneMarker = regexp.MustCompile(`^\[done(?:\|[^\]]*)?\]$`)
var doneSuffix = regexp.MustCompile(`[ \t]*\[done(?:\|[^\]]*)?\]$`)
var runningLineMarker = regexp.MustCompile(`^\[running\|[^\]]+\]$`)
var runningMarker = regexp.MustCompile(`^\[running\|([0-9]{8}-[0-9]{2}:[0-9]{2})\|step=([0-9]+)\|step-runs=([0-9]+)x\|total=([0-9]+)x\]$`)
var legacyRunningMarker = regexp.MustCompile(`^\[running\|([0-9]{8}-[0-9]{2}:[0-9]{2})\|([0-9]+)x\]$`)
var runningSuffix = regexp.MustCompile(`[ \t]*\[running\|[^\]]+\]$`)
var atmQuoteLine = regexp.MustCompile(`^[ \t]*> ?`)

type Block struct {
	Prefix string
	Body   string
	Sep    string
}

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
	Resume   bool
	Args     []string
	Output   *OutputSpec
	DBs      []DBRuntime
	Workdir  string
	Skills   []SkillRuntime
	MCPs     []MCPRuntime
	DefMCP   *DefMCPRuntime
	DefDepth int
}

type ConditionKind string

const (
	ConditionNone    ConditionKind = ""
	ConditionNatural ConditionKind = "natural"
	ConditionCEL     ConditionKind = "cel"
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

type DoneInfo struct {
	Start    time.Time
	End      time.Time
	Runs     int
	Messages []OutputMessage
}

type SkippedInfo struct {
	Time   time.Time
	Reason string
}

type RunningInfo struct {
	Active    bool
	Start     time.Time
	StepIndex int
	StepRuns  int
	TotalRuns int
	Messages  []OutputMessage
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
}

type Definition struct {
	Name       string
	Params     []string
	Blocks     []string
	SourcePath string
	BlockIndex int
	List       bool
}

type ImportDecl struct {
	BlockIndex int
	Path       string
	Namespace  string
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
	Name   string `json:"name"`
	Config string `json:"config"`
}

type DefMCPRuntime struct {
	TodoPath    string          `json:"todo_path"`
	Definitions []string        `json:"definitions"`
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
