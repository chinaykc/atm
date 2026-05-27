package compiler

type commandClass string

const (
	commandClassTaskDeclaration commandClass = "task-declaration"
	commandClassTaskBinding     commandClass = "task-binding"
	commandClassTaskFlow        commandClass = "task-flow"
	commandClassTaskConfig      commandClass = "task-config"
	commandClassBlock           commandClass = "block"
	commandClassMarkdown        commandClass = "markdown"
)

type commandSpec struct {
	Token string
	Class commandClass
}

var commandSpecs = map[string]commandSpec{
	"/task":    {Token: "/task", Class: commandClassTaskDeclaration},
	"/resume":  {Token: "/resume", Class: commandClassTaskDeclaration},
	"/fork":    {Token: "/fork", Class: commandClassTaskDeclaration},
	"/args":    {Token: "/args", Class: commandClassTaskDeclaration},
	"/output":  {Token: "/output", Class: commandClassTaskConfig},
	"/db":      {Token: "/db", Class: commandClassTaskConfig},
	"/skill":   {Token: "/skill", Class: commandClassTaskConfig},
	"/mcp":     {Token: "/mcp", Class: commandClassTaskConfig},
	"/webhook": {Token: "/webhook", Class: commandClassTaskFlow},
	"/cd":      {Token: "/cd", Class: commandClassTaskFlow},
	"/let":     {Token: "/let", Class: commandClassTaskBinding},
	"/bash":    {Token: "/bash", Class: commandClassTaskFlow},
	"/for":     {Token: "/for", Class: commandClassTaskFlow},
	"/if":      {Token: "/if", Class: commandClassTaskFlow},
	"/else":    {Token: "/else", Class: commandClassTaskFlow},
	"/call":    {Token: "/call", Class: commandClassTaskFlow},
	"/go":      {Token: "/go", Class: commandClassTaskFlow},
	"/wait":    {Token: "/wait", Class: commandClassTaskFlow},
	"/return":  {Token: "/return", Class: commandClassBlock},
	"/def":     {Token: "/def", Class: commandClassBlock},
	"/import":  {Token: "/import", Class: commandClassBlock},
	"/pool":    {Token: "/pool", Class: commandClassBlock},
	"/flag":    {Token: "/flag", Class: commandClassBlock},
	"/context": {Token: "/context", Class: commandClassMarkdown},
	"/doc":     {Token: "/doc", Class: commandClassMarkdown},
}

func commandTokenSpec(token string) (commandSpec, bool) {
	spec, ok := commandSpecs[token]
	return spec, ok
}

func isCommandToken(token string) bool {
	_, ok := commandTokenSpec(token)
	return ok
}
