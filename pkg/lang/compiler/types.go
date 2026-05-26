package compiler

import (
	"github.com/chinaykc/atm/pkg/lang/ir"
	"github.com/chinaykc/atm/pkg/lang/marker"
)

type Block struct {
	Prefix      string
	Body        string
	Sep         string
	Context     string
	Scope       []string
	StartLine   int
	HasParent   bool
	ParentIndex int
}

type Plan = ir.Plan
type GlobalBinding = ir.GlobalBinding
type Task = ir.Task
type FlowKind = ir.FlowKind
type FlowNode = ir.FlowNode
type For = ir.For
type ExecutionCursor = ir.ExecutionCursor
type FlatOpKind = ir.FlatOpKind
type FlatOp = ir.FlatOp

const (
	FlowSeq     = ir.FlowSeq
	FlowCd      = ir.FlowCd
	FlowBash    = ir.FlowBash
	FlowFor     = ir.FlowFor
	FlowIf      = ir.FlowIf
	FlowGo      = ir.FlowGo
	FlowWait    = ir.FlowWait
	FlowCall    = ir.FlowCall
	FlowReturn  = ir.FlowReturn
	FlowExecute = ir.FlowExecute

	FlatOpCd      = ir.FlatOpCd
	FlatOpBash    = ir.FlatOpBash
	FlatOpFor     = ir.FlatOpFor
	FlatOpIf      = ir.FlatOpIf
	FlatOpGo      = ir.FlatOpGo
	FlatOpWait    = ir.FlatOpWait
	FlatOpCall    = ir.FlatOpCall
	FlatOpReturn  = ir.FlatOpReturn
	FlatOpExecute = ir.FlatOpExecute
)

type BashCommand = ir.BashCommand
type CdCommand = ir.CdCommand
type OutputSpec = ir.OutputSpec
type RunOptions = ir.RunOptions
type ConditionKind = ir.ConditionKind
type Condition = ir.Condition
type IfBlock = ir.IfBlock
type ElseBlock = ir.ElseBlock
type ControlBlock = ir.ControlBlock
type If = ir.If
type Call = ir.Call
type ReturnKind = ir.ReturnKind
type ReturnSpec = ir.ReturnSpec
type OutputMessage = ir.OutputMessage
type LetBinding = ir.LetBinding
type PoolDecl = ir.PoolDecl
type Definition = ir.Definition
type DefinitionBlock = ir.DefinitionBlock
type ImportDecl = ir.ImportDecl
type DBScope = ir.DBScope
type DBPersistence = ir.DBPersistence
type DBAccess = ir.DBAccess
type DBDecl = ir.DBDecl
type DBUse = ir.DBUse
type DBAccessRule = ir.DBAccessRule
type DBTaskConfig = ir.DBTaskConfig
type DBRuntime = ir.DBRuntime
type SkillDecl = ir.SkillDecl
type SkillTaskConfig = ir.SkillTaskConfig
type SkillRuntime = ir.SkillRuntime
type MCPDecl = ir.MCPDecl
type MCPTaskConfig = ir.MCPTaskConfig
type MCPRuntime = ir.MCPRuntime
type DefMCPRuntime = ir.DefMCPRuntime
type DefinitionRef = ir.DefinitionRef

const (
	ConditionNone    = ir.ConditionNone
	ConditionNatural = ir.ConditionNatural
	ConditionExpr    = ir.ConditionExpr
	ConditionCall    = ir.ConditionCall

	ReturnTemplate   = ir.ReturnTemplate
	ReturnBash       = ir.ReturnBash
	ReturnStructured = ir.ReturnStructured

	DBScopeLocal  = ir.DBScopeLocal
	DBScopeGlobal = ir.DBScopeGlobal

	DBPersistRun     = ir.DBPersistRun
	DBPersistProject = ir.DBPersistProject

	DBAccessRead   = ir.DBAccessRead
	DBAccessAppend = ir.DBAccessAppend
	DBAccessWrite  = ir.DBAccessWrite
	DBAccessAdmin  = ir.DBAccessAdmin
)

type DoneInfo = marker.DoneInfo
type FailedInfo = marker.FailedInfo
type SkippedInfo = marker.SkippedInfo
type RunningInfo = marker.RunningInfo
