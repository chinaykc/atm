# 设计说明

[English](design.md)

ATM 是 **Agent Task Markdown**：一种基于 Markdown 的 Agent 任务调度 DSL（领域专用语言）。它刻意比工作流引擎更小：负责解释任务文档并调度 agent，但不接管任务文档之外的项目状态、调度策略、重试策略或工具专属配置。

面向 Markdown 原生任务编排的语言层设计见 [ATM v2 DSL（领域专用语言）设计草案](../v2/dsl.zh-CN.md)。本文主要记录当前代码架构和运行时边界。

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
- DSL 前端把任务块、命令、变量、bash 捕获和有序流程操作解析成 parser AST。
- 前端把 AST lower 成 `ir.go` 中的小型 IR。
- `atm plan` 把 IR 渲染成文本或 JSON，不执行工具或 bash。
- 执行后端运行计划，管理 `/go` 分支和 `/wait`，并写入生成状态块。
- `toolRunner` 是适配器边界。Codex 和 Claude Code 适配器共享同一套后端行为。
- 每个任务的输出同时写到控制台和临时日志文件。Codex 和 Claude Code 适配器使用结构化输出模式，因此最近 assistant 消息可以渲染进生成的 `> [!ATM]` 块。
- 后台任务通过任务块索引和清理后的正文建立 key，避免未变化任务块被重复启动。
- `/db` 声明由 DSL 解析，engine 解析成当前任务可见的 DB 配置，工具适配器再通过临时 MCP server 暴露给 agent。DB 文件是本地 JSON `map[string][]string` 文档。

## 前端和后端

前端/后端边界现在由真实 Go package 表达：

- **DSL 包**：`pkg/dsl` 负责块解析、命令解析、AST、IR、标记、模板渲染和 dry-run plan 输出。
- **CLI 包**：`pkg/cli` 负责参数解析和子命令编排。
- **Engine 包**：`pkg/engine` 负责按顺序解释 IR、`/go` 分支、`/wait`、执行状态和输出报告。
- **Store 包**：`pkg/store` 负责 todo 文档、块租约、锁、临时活跃文件和原子写回。
- **Tools 包**：`pkg/tools` 负责 Codex、Claude、bash 和条件检查适配器。
- **命令入口**：`cmd/atm/main.go` 只调用 `cli.Run`。

前端应该能够在不执行任务的情况下解释任务。执行器运行 `dsl.Task` IR，不再重新解释文本命令语法。Parser AST 类型只留在 `pkg/dsl` 内部；运行时代码接收 `dsl.Task`、`dsl.Op`、`dsl.For` 等导出的 IR 类型。

## 包结构

项目现在采用一个小命令加分层公开实现包：

```txt
cmd/atm/        标准命令入口
pkg/dsl/        语言前端、AST、IR、标记和 plan 渲染
pkg/engine/     有序 IR 解释器、后台分支、等待和报告
pkg/store/      todo 文档、块租约、锁和活跃文件持久化
pkg/tools/      Codex、Claude、bash 和检查适配器
pkg/cli/        参数解析和子命令编排
docs/           面向用户的命令和设计文档
examples/       可直接编辑的 todo 文件
v2/             下一代 DSL 和工具设计草案
```

`pkg/dsl` 是语言包：

- `ast.go` 负责 parser AST 类型。
- `ir.go` 负责前端输出类型：plan、task、operations 和 execution cursor。
- `parser.go` 负责命令解析和 AST 构造。
- `program.go` 负责把源码文本编译成 IR program。
- `plan.go` 负责 dry-run 计划渲染，包括 `-json`。
- `markers.go` 负责生成状态块。
- `types.go` 负责导出的语言数据类型。

新代码应优先落在这些包的职责中。只有职责清晰且稳定时才增加新包。

## 工具适配器边界

适配器实现 `tools.Runner`：

- `Execute` 接收活跃 todo 路径、渲染后的提示词、运行选项和输出 writer。
- `Check` 接收活跃 todo 路径、渲染后的提示词、渲染后的条件、运行选项和输出 writer，并返回布尔结果。

todo 语言刻意保持工具无关。除非提示词本身需要，否则工具专属参数不应进入任务块语法。

## 可移植性

运行时代码只使用 Go 标准库。平台差异隔离在 `platform_*.go`：

- 恢复信号：POSIX 监听 interrupt 和 terminate；Windows 监听 interrupt。
- 跨设备重命名回退：优先尝试 `os.Rename`，平台报告跨设备移动时复制并替换。

## 文件安全

todo 改写使用目标目录中的临时文件，然后通过 `os.Rename` 替换。短生命周期锁文件会串行化本进程族的本地读写。

运行期间，原 todo 文件会移动到临时活跃路径。正常退出或可处理的中断发生时，活跃文件会被移回。

生成的输出日志位于系统临时目录。它们是诊断产物，不是持久项目状态。

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
