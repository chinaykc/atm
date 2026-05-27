package syntax

type Position struct {
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`
}

type Span struct {
	Block int      `json:"block,omitempty"`
	Start Position `json:"start,omitempty"`
	End   Position `json:"end,omitempty"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Source   string `json:"source,omitempty"`
	Block    int    `json:"block,omitempty"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Message  string `json:"message"`
}

type SourceSpan struct {
	Block  int
	Line   int
	Column int
}

type Document struct {
	SourcePath  string       `json:"sourcePath,omitempty"`
	Blocks      []Block      `json:"blocks,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type BlockKind string

const (
	BlockTask       BlockKind = "task"
	BlockLet        BlockKind = "let"
	BlockPool       BlockKind = "pool"
	BlockDB         BlockKind = "db"
	BlockSkill      BlockKind = "skill"
	BlockMCP        BlockKind = "mcp"
	BlockImport     BlockKind = "import"
	BlockDefinition BlockKind = "definition"
	BlockControl    BlockKind = "control"
	BlockMarkdown   BlockKind = "markdown"
)

type Block struct {
	Kind        BlockKind `json:"kind"`
	Prefix      string    `json:"prefix,omitempty"`
	Body        string    `json:"body"`
	Sep         string    `json:"sep,omitempty"`
	Context     string    `json:"context,omitempty"`
	Scope       []string  `json:"scope,omitempty"`
	StartLine   int       `json:"startLine,omitempty"`
	HasParent   bool      `json:"hasParent,omitempty"`
	ParentIndex int       `json:"parentIndex,omitempty"`
	Commands    []Command `json:"commands,omitempty"`
	Span        Span      `json:"span,omitempty"`
}

type CommandKind string

const (
	CommandTask   CommandKind = "task"
	CommandResume CommandKind = "resume"
	CommandFork   CommandKind = "fork"
	CommandArgs   CommandKind = "args"
	CommandCd     CommandKind = "cd"
	CommandLet    CommandKind = "let"
	CommandBash   CommandKind = "bash"
	CommandIf     CommandKind = "if"
	CommandElse   CommandKind = "else"
	CommandCall   CommandKind = "call"
	CommandFor    CommandKind = "for"
	CommandGo     CommandKind = "go"
	CommandWait   CommandKind = "wait"
	CommandOutput CommandKind = "output"
	CommandDB     CommandKind = "db"
	CommandSkill  CommandKind = "skill"
	CommandMCP    CommandKind = "mcp"
	CommandDef    CommandKind = "def"
	CommandReturn CommandKind = "return"
	CommandImport CommandKind = "import"
	CommandPool   CommandKind = "pool"
	CommandOther  CommandKind = "other"
)

type Command struct {
	Kind CommandKind `json:"kind"`
	Raw  string      `json:"raw"`
	Name string      `json:"name,omitempty"`
	Args []string    `json:"args,omitempty"`
	Line int         `json:"line,omitempty"`
}
