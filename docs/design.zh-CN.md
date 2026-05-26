# 设计说明

[English](design.md)

ATM 是 **Agent Task Markdown**：一种基于 Markdown 的 Agent 任务调度 DSL（领域专用语言）。它刻意比工作流引擎更小：负责解释任务文档并调度 agent，但不接管任务文档之外的项目状态、调度策略、重试策略或工具专属配置。

当前语言模型已经是 Markdown 原生模型：标题提供文档、上下文和词法作用域；可执行工作从 `/task`、任务启动 slash 控制命令，或带后续 prompt 的 task header 命令开始。语法权威文档是 [commands.zh-CN.md](commands.zh-CN.md)。

## 架构

ATM 现在按一门小型 Markdown DSL（领域专用语言）的实现方式组织：

```txt
Markdown 任务文件
  -> DSL 前端
  -> AST
  -> IR
  -> dry-run plan 或执行计划
  -> 执行后端
  -> 工具适配器
  -> 生成结果块和日志
```

- `todo.txt`、`todo.md` 或其他 Markdown/纯文本任务文件是持久源码文件。
- DSL 前端把 Markdown section、任务块、作用域声明、命令、变量、lazy provider、bash 捕获和有序流程操作解析成 parser AST。
- 前端把 AST lower 成 `ir.go` 中的小型 IR。
- `atm plan` 把 IR 渲染成文本、JSON 或 HTML，默认不执行工具或 bash。`plan --preview` 可以执行适合预览的 lazy provider。
- 执行后端运行计划，管理 `/go` 分支和 `/wait`，并写入生成状态块。
- `toolRunner` 是适配器边界。Codex 和 Claude Code 适配器共享同一套后端行为。
- 每个任务的输出同时写到控制台和临时日志文件。Codex 和 Claude Code 适配器使用结构化输出模式，因此最近 assistant 消息可以渲染进生成的 `> [!ATM]` 块和 detail report。
- 后台任务通过任务块索引和清理后的正文建立 key，避免未变化任务块被重复启动。
- `/db` 声明由 DSL 解析，engine 解析成当前任务可见的 DB 配置，工具适配器再通过临时 MCP server 暴露给 agent。DB 文件是本地 JSON `map[string][]string` 文档。

## 前端和后端

前端/后端边界现在由真实 Go package 表达：

- **Compiler 包**：`pkg/lang/compiler` 负责源码编译、命令解析、import、definition、scope 和 validation。
- **Syntax 包**：`pkg/lang/syntax` 负责提供给 IDE、lint 和外部工具使用的源码 AST。
- **IR 包**：`pkg/lang/ir` 负责 runtime、plan view 和集成层消费的执行模型。
- **Document 包**：`pkg/lang/document` 负责任务文档 block 发现和 Markdown heading helper。
- **Marker 包**：`pkg/lang/marker` 负责生成的 ATM 状态/report 块。
- **Format 包**：`pkg/lang/format` 负责任务文档和 flow 格式化 helper。
- **Plan view 包**：`pkg/view/plan` 负责 dry-run plan 的文本、JSON 和 HTML 渲染。
- **CLI 包**：`pkg/app/cli` 负责参数解析和子命令编排。
- **Engine 包**：`pkg/runtime/engine` 负责按顺序解释 IR、`/go` 分支、`/wait`、执行状态和输出报告。
- **Store 包**：`pkg/runtime/store` 负责任务文档、块租约、锁、临时活跃文件和原子写回。
- **Agent 适配包**：`pkg/integration/agent` 负责 Codex、Claude、bash 和条件检查适配器。
- **MCP 集成包**：`pkg/integration/mcp` 负责本地 MCP 工具定义和服务。
- **Task document 包**：`pkg/workspace/taskdoc` 负责文件级 format、untag、append 和 repair helper。
- **公共包**：`atm.go` 是可 import 的集成门面，负责暴露 compile、plan、run 和 todo 文件维护能力。
- **命令入口**：`cmd/atm/main.go` 只调用公共 CLI 门面。

前端应该能够在不执行任务的情况下解释任务。外部工具可以通过 `compiler.ParseSyntax` 获取 `syntax.Document` 源码 AST。执行器运行 `ir.Task`，不再重新解释文本命令语法。运行时代码接收 `ir.Task`、`ir.FlowNode`、`ir.FlatOp`、`ir.For` 等导出的 IR 类型。

## 包结构

项目现在采用一个小命令加分层公开实现包：

```txt
atm.go          公共集成门面
cmd/atm/        标准命令入口
pkg/app/cli/             参数解析和子命令编排
pkg/lang/compiler/       源码编译、命令解析、import、definition、scope 和 validation
pkg/lang/document/       任务文档 block 发现和 Markdown heading helper
pkg/lang/expr/           /if、/for 和 output helper 使用的本地表达式求值器
pkg/lang/format/         任务文档和 flow 格式化 helper
pkg/lang/ir/             runtime、plan 和集成层共享的执行模型
pkg/lang/marker/         生成的 ATM 状态/report 块
pkg/lang/syntax/         对外源码 AST 和 diagnostics
pkg/runtime/engine/      有序 IR 解释器、后台分支、等待和报告
pkg/runtime/store/       任务文档、块租约、锁和活跃文件持久化
pkg/integration/agent/   Codex、Claude、bash 和检查适配器
pkg/integration/mcp/     本地 MCP 工具定义和服务
pkg/view/plan/           dry-run plan 文本、JSON 和 HTML 渲染
pkg/workspace/taskdoc/   文件级 format、untag、append 和 repair helper
docs/           面向用户的命令和设计文档
examples/       可直接编辑的 todo 文件
```

`pkg/lang/compiler` 是语言编译包：

- `ast.go` 负责 compiler-local parser AST 类型。
- `ir.go` 负责把 compiler-local AST lowering 成导出的 IR 模型。
- `blocks.go` 和 `markdown_parser.go` 负责 Markdown/legacy task block 发现。
- `command_*.go`、`*_parser.go` 和 `task_command_scan.go` 负责命令解析和 task AST 构造。
- `program.go` 负责把源码文本编译成 IR program。
- `scope.go` 和 `symbols.go` 负责 Markdown 词法可见性和符号解析。
- `validate.go` 负责 `atm check` 和 program compilation 共用的静态校验。
- `syntax.go` 负责把解析后的文档映射成公共 `syntax.Document` AST。

新代码应优先落在这些包的职责中。只有职责清晰且稳定时才增加新包。

外部集成优先使用根目录 `atm` 包；只有确实需要更底层契约时再直接使用 `pkg/*`。根门面是嵌入 CLI、编译 todo 内容、渲染 plan、运行 engine 和调用文件级维护 helper 的稳定入口。

## 工具适配器边界

适配器实现 `agent.Runner`：

- `Execute` 接收活跃 todo 路径、渲染后的提示词、运行选项和输出 writer。
- `Check` 接收活跃 todo 路径、渲染后的提示词、渲染后的条件、运行选项和输出 writer，并返回布尔结果。

todo 语言刻意保持工具无关。除非提示词本身需要，否则工具专属参数不应进入任务块语法。

## 可移植性

运行时代码只使用 Go 标准库。平台差异隔离在 `platform_*.go`：

- 恢复信号：POSIX 监听 interrupt 和 terminate；Windows 监听 interrupt。
- 跨设备重命名回退：优先尝试 `os.Rename`，平台报告跨设备移动时复制并替换。

## 文件安全

todo 改写使用目标目录中的临时文件，然后通过 `os.Rename` 替换。源 todo 旁的短生命周期 `.atm/lock` 文件会串行化本进程族的本地读写。

运行期间，原 todo 文件会移动到临时活跃路径。正常退出或可处理的中断发生时，活跃文件会被移回。

人类可读任务日志位于源 todo 文件旁的 `.atm/logs/`，这样 state 和 report 可以指向稳定的审计路径。agent 原生事件流、结构化输出、run-local DB 文件和 `result.md` 位于本次选择的 run output 目录。

`/db persist:project` 文件位于源 todo 文件项目目录下的 `.atm/db`。`/db persist:run` 文件位于本次 run 的 output 目录。DB 写入使用旁路 lock 文件加临时文件 rename，串行化本地 MCP 工具写操作。

## 非目标

- 不提供守护进程或后台服务。
- 不提供外部数据库服务或守护进程。`/db` 使用本地 JSON 文件保存任务侧 agent 状态。
- 不提供远程队列。
- 不提供内置项目模板。
- 核心二进制不内置项目专属工具适配器。

## 兼容性契约

- Todo 文件是用户可见 API。已有命令和生成状态块应能被未来版本读取。
- 新增适配器不应改变任务块语法。
- 运行时行为应持续支持 Linux、macOS、Windows。
- 除非依赖能显著减少复杂度，否则二进制应保持小型且仅使用标准库。
