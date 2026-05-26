package compiler

type commandKind string

const (
	commandTask      commandKind = "task"
	commandResume    commandKind = "resume"
	commandArgs      commandKind = "args"
	commandCd        commandKind = "cd"
	commandLet       commandKind = "let"
	commandBash      commandKind = "bash"
	commandIf        commandKind = "if"
	commandElse      commandKind = "else"
	commandCall      commandKind = "call"
	commandFor       commandKind = "for"
	commandGo        commandKind = "go"
	commandWait      commandKind = "wait"
	commandPrefixVar commandKind = "prefix-var"
)

type command struct {
	Kind      commandKind
	Options   RunOptions
	For       forAST
	Condition Condition
	Cd        CdCommand
	Bash      BashCommand
	Call      Call
	Pool      string
	LetName   string
	LetValue  string
	PrefixVar string
}
