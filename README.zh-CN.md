# ATM

[English](README.md)

**ATM** 是 **Agent Task Markdown**：一种基于 Markdown 的 Agent 任务调度 DSL（domain-specific language 领域专用语言）。

当你手上有几件想交给编码代理处理的事，又不想引入数据库、守护进程、Web UI 或工作流系统时，就可以用它。把提示词和斜杠命令写进 Markdown 或纯文本任务文件，用 `atm plan` 预览执行计划，`atm` 会执行任务并把完成状态写回同一个文件。

面向用户的完整手册见：[docs/user/README.md](docs/user/README.md)。

## 快速开始

构建 CLI：

```sh
go build -buildvcs=false -o atm ./cmd/atm
```

创建 `todo.txt`：

```txt
运行测试套件，并修复发现的问题。

/for 3 until 测试通过
整理仓库，让它达到可以发布的状态。

/go
审查 README，找出不清楚的安装步骤。

/wait
```

运行：

```sh
./atm
```

不传文件参数时，`atm` 会使用第一个存在的默认文件：`todo.txt`、`todo.md` 或 `toto.md`。

显式子命令写法等价：

```sh
./atm run -file todo.txt
```

`run` 是持续协作模式：它会反复重扫活跃 todo 文件，所以命令仍在运行时追加的任务可能被同一次 `run` 执行。需要一次性快照执行时使用 `exec`：

```sh
./atm exec todo.txt
```

`run` 和 `exec` 的区别只在任务集合策略：`run` 持续重扫活跃文档，`exec` 固定启动瞬间看到的 task block 快照。二者使用同一套执行器、工具参数、输出目录、状态文件、报告文件和锁机制。`exec` 启动后追加的任务仍保留在文档中，但留给后续的 `run` 或 `exec` 执行。

Windows PowerShell：

```powershell
.\atm.exe -file todo.txt
```

默认情况下，`atm` 通过 Codex 执行任务：

```sh
./atm -tool codex -file todo.txt
```

如果要使用 Claude Code：

```sh
./atm -tool claude -file todo.txt
```

也可以写成 `-tool claude-code`。如果可执行文件不在 `PATH` 中，可以用 `-codex /path/to/codex` 或 `-claude /path/to/claude` 指定路径。

一次运行可以把多个 todo 文件排队执行：

```sh
./atm run todo.txt rollout.md followup.md
./atm run -file todo.txt -file rollout.md
```

## 常见用法

- 按顺序跑一组发布检查任务。
- 用 `/for 3 until 测试通过` 让所选工具最多尝试 3 次，直到测试通过。
- 用 `/go` 启动互不依赖的审查任务，再用 `/wait` 等它们结束。
- 用 `atm plan` 在不执行任何命令的情况下预览执行顺序。
- 把任务状态留在一个可以编辑、审阅、提交或丢弃的文本文件里。

## Todo 格式

ATM 支持纯文本任务文件和 Markdown 任务文档。在 Markdown 中，标题只表示上下文和作用域，不再表示可执行任务。任务可以从 `/task`、任务启动控制命令（如 `/for`、`/go`、`/call`、`/bash`、`/wait`、`/if` 或 `/else`）开始，也可以由 `/let`、`/args`、`/cd`、`/output`、`/db use`、`/skill use`、`/mcp use` 等 task header 命令加后续 prompt 形成。`/task` 主要用于完全没有 header/control 命令的普通 prompt 任务。

任务前的普通 Markdown 会保留在文件中，并作为该 section 内任务的上下文。Prompt 正文从 task header 之后开始；正文中的 slash 命令会报错，除非它在空行之后作为根层新任务开始。

在 Markdown task header 中用 `/context #Heading` 可以把其他 section 的普通文档加入当前任务上下文；用 `/doc text` 或 `/doc` 后接 fenced block 可以写只给人看的说明，且不进入 agent 上下文。

旧式文本任务块中的整行注释会被忽略：第一个非空白字符是 `#` 的行、`<!-- ... -->` 这类 HTML 注释块，以及 `[//]: # (...)` 这类 Markdown 引用式注释。可执行内容中只由三个或更多 `-` 或 `=` 组成的独立 Markdown 分隔线会被忽略。

```txt
发送给所选工具的第一个提示词。

/for 2
把这个提示词执行两次。

/resume /for 3
继续上一次工具会话。
```

Markdown 任务文档示例：

```md
# 发布说明

这里是普通文档，不会执行。

## Verify

/for 2
运行 go test ./...，并修复失败。

/task
运行 go vet ./...，修复可操作的问题。

## discuss

/task
整个 section 是一个 prompt。

空行会保留在 prompt 中。
```

命令写在任务块顶部，位于提示词正文之前。支持的命令有：

- `/resume`：继续所选工具最近一次会话。
- `/args ...`：向所选工具追加 CLI 参数，例如 `/args --yolo`。
- `/cd path`：准备并进入当前任务工作区；目录不存在时默认创建。用 `/cd --must-exist path` 要求目录必须已存在。
- `/let name value`：定义模板变量；单独的 `/let` 块对当前 Markdown scope 内的后续任务可见。
- `/let name /bash script`：把 bash stdout 定义为懒变量；只有变量被渲染或读取时才执行。
- `/let name /call def [args...]`：懒调用可复用定义，并把返回值绑定为变量。
- `/bash script`：在提示词执行前运行 bash；多行脚本支持 heredoc 写法。
- `/context #Heading`：把另一个 Markdown section 的普通文档加入当前任务上下文。
- `/doc text` 或 `/doc` 加 fenced block：写只给人看的说明，不进入 agent 上下文。
- `/output [file]`：要求通过临时 MCP 工具提交结构化 JSON，并保存为输出产物。
- `/db new/use/access/ignore ...`：声明任务数据库，并控制每个任务块的 MCP 访问权限。
- `/skill new/use/ignore ...`：声明本地 skill，并把选中的 skill 挂载到 `/cd` 工作区。
- `/mcp new/use/ignore ...` 和 `/mcp def use ...`：注入临时 MCP server，并把选中的定义暴露成 MCP 工具。
- `/def name [params...]` 加 `/return`：定义可复用任务模板。
- `/call name [args...]`：作为任务/header 命令执行定义；需要把返回值渲染进 prompt 时，在 prompt 前用 `/let name /call ...` 绑定。
- `/return ...`：在定义中返回文本、bash 输出或多行模板。
- `/import [namespace from] path`：从其他 todo 文件导入定义。
- `/pool name max [buffer]`：声明一个具名后台工作池。
- `/for 3 [until condition]`：最多执行 `3` 次；`{{n}}` 会渲染为从 `0` 开始的循环序号。
- `/for until(expr)`：一直执行，直到本地表达式为 true。带括号的 `until(...)` 是确定性本地控制流；自然语言 `until condition` 仍走工具侧 MCP 检查。
- `/if (expr)` 或 `/if 自然语言`：用本地表达式或 MCP check 选择一个分支；`/if` 和 `/else` 不嵌套。
- `/for name in files()`、`/for name in walkFiles("src")`、`/for name in dirs()` 或 `/for name in [a b]`：按文件、目录或显式列表逐项执行。文件和目录枚举只能通过 `files()`、`dirs()`、`walkFiles()`、`walkDirs()` 表达式 helper 表达；旧的 `/for file`、`/for dir` 和 `/for path` 控制头无效。
- `/go [pool]`：在后台启动这个任务，也可以提交到具名池。
- `/wait [pool]`：等待之前的 `/go` 任务，也可以只等待某个具名池。

Prompt、`/bash` 脚本、`until` 条件、`/args` 参数值和 `/cd` 路径都会用 Go `text/template` 渲染。`{{n}}`、`{{file}}`、`{{branch}}` 这类变量在进入作用域后可用。新模板也可以使用 `{{.n}}`、`{{index .Vars "file"}}`、`{{var "file"}}`，以及 `{{if .n}}...{{end}}` 这类控制结构。

对于 `until`，`atm` 会挂载名为 `atm_report_check` 的结构化临时 MCP 检查工具；检查 agent 必须通过该工具提交结果。完成和运行中状态会写成以 `> [!ATM]` 开头的 Markdown 引用块。Codex 和 Claude 的输出会从结构化流中读取，所以运行时控制台会展示当前任务行号范围、工具调用名和 assistant 消息，并默认把最近 1 条 assistant 消息呈现在引用块里。

`/db` 会挂载临时 MCP server 作为任务数据库。用 `scope` 控制哪些任务块能看到数据库，用 `persist` 选择本次 run 内存储或项目级存储，用 `access` 控制当前任务块是只读、可追加、可写还是可删除。

后台分支会受全局并发上限控制。默认是 `NumCPU`，可用 `-jobs N` 修改。具名 `/pool` 会增加单个池子的并发限制，但仍共享全局上限。

每次运行还会默认把产物写到 `.atm/YYYYMMDDHHMMSS[-N]`。可以用 `-output DIR` 或 `-o DIR` 指定目录。产物包括每次 agent 运行的原生 JSONL 事件流、`db/` 下的 run-local 数据库文件，以及 `result.md`，也就是执行结束时 todo 文档的副本，便于 `clean` 或 `untag` 后追查。

## 常用命令

当 `atm` 仍在运行时追加任务。如果当前 run 已经退出，需要再次运行 `atm` 才会执行追加任务：

```sh
./atm append -file todo.txt "为解析器补充聚焦测试。"
```

格式化 todo 文件：

```sh
./atm format -file todo.txt
```

移除生成的状态块：

```sh
./atm untag -file todo.txt
```

汇总当前任务报告和审计状态：

```sh
./atm report todo.txt
```

只清理主文档生成状态块，保留审计产物：

```sh
./atm clean todo.txt
```

显式清理选定审计产物：

```sh
./atm clean todo.txt --reports --state --logs
```

复制任务块后修复重复的生成 report id：

```sh
./atm repair-ids todo.txt
```

只预览执行计划，不运行 bash 或所选工具：

```sh
./atm plan todo.txt
```

显式预览 lazy bash provider 的值：

```sh
./atm plan --preview todo.txt
```

写出适合浏览器查看的 HTML 流程图：

```sh
./atm plan -file todo.txt -html plan.html
```

用默认浏览器打开流程图：

```sh
./atm plan -file todo.txt -open
```

如果需要给其他工具读取，可以输出 JSON：

```sh
./atm plan -json -file todo.txt
```

## 更多

- 完整命令参考：[docs/commands.zh-CN.md](docs/commands.zh-CN.md)
- 可直接改的示例：[快速开始](examples/quick-start.zh-CN.todo.md)、[简单](examples/simple.zh-CN.todo.md)、[复杂](examples/complex.zh-CN.todo.md)
- 设计说明：[docs/design.zh-CN.md](docs/design.zh-CN.md)
- 安全政策：[SECURITY.md](SECURITY.md)

`atm` 使用 Go 编写，支持 Linux、macOS 和 Windows。

## 许可证

MIT。见 [LICENSE](LICENSE)。
