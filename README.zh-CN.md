# ATM

[English](README.md)

<p align="center">
  <img src="docs/assets/atm-logo-cny.svg" alt="ATM" width="420">
</p>

**ATM** 是 **Agent Task Markdown** 的缩写：一种面向编码代理的 Markdown runbook 格式。

它适合放在一次性 prompt 和完整工作流系统之间：当一个项目需要让 agent 做多件事，例如运行检查、审查文档、验证示例、修复问题时，聊天窗口很快会变得难以审计、难以复用，也难以看清任务之间的关系。ATM 让你把这些工作写进一个可读的 `todo.md`：项目背景仍然是普通 Markdown，少量斜杠命令负责描述任务如何执行。

ATM 会把这个文件编译成 agent 执行计划。任务可以顺序执行、按条件重试、并行检查、等待汇合，并产出结构化结果。你可以在启动任何 agent 前先预览计划，再交给 Codex 或 Claude Code 执行；工作流还可以复用为本地命令或 HTTP 接口。

## 适合场景

- 让 AI 起草和迭代 ATM 文件，让工作流随着项目一起演进，而不是只留在聊天记录里。
- 把重复 prompt 工作沉淀成可复用 runbook，减少反复交代上下文，释放 agent 的执行能力。
- 轻松验证不同的协作模式，从顺序审查、并行检查到 multi-agent 风格的交接，而不必先搭建工作流服务。
- 把复杂项目流程写成 Markdown，让人类和 agent 都能阅读、审查、执行和改进。
- 当某个 agent 工作流变成日常操作时，把它复用为本地命令或小型 HTTP API。

## 前置条件

- 默认 runner 需要 `codex` 在 `PATH` 中；使用 `--tool claude` 时需要 Claude Code 在 `PATH` 中。
- 支持 Linux、macOS 和 Windows。
- 只有从源码构建时才需要 Go 1.25 或更高版本。

如果 runner 不在 `PATH` 中，可以用 `--codex /path/to/codex` 或 `--claude /path/to/claude` 指定可执行文件路径。

## 安装

从 GitHub Releases 下载对应平台的压缩包，解压后把 `atm` 可执行文件放到 `PATH` 中即可。

检查已安装的二进制：

```sh
atm --version
```

如果要从源码构建：

```sh
go build -o atm ./cmd/atm
```

## 快速开始

创建 `todo.md`：

```txt
/for 3 until 测试通过
运行项目测试套件，并修复发现的问题。

/go
检查安装和使用文档，找出缺失、过期或不清楚的步骤。
把发现的问题写入 `checks/setup.md`。

/go
检查示例、脚本和配置文件，找出已经无法工作的命令。
把发现的问题写入 `checks/commands.md`。

/wait

/task
读取 `checks/setup.md` 和 `checks/commands.md`，并修复确认存在的项目问题。
```

先预览执行计划。`check` 过程不会启动 agent：

```sh
./atm check --plan todo.md
```

在浏览器中打开计划流程图：

```sh
./atm check --open todo.md
```

执行任务：

```sh
./atm run todo.md
```

默认命令也是 `run`，所以下面写法等价：

```sh
./atm todo.md
```

Windows PowerShell：

```powershell
.\atm.exe run todo.md
```

显式选择 runner：

```sh
./atm run --tool codex todo.md
./atm run --tool claude todo.md
```

也可以写成 `--tool claude-code`。一次运行可以把多个 ATM 文件排队执行：

```sh
./atm run --jobs 4 todo.md rollout.md followup.md
```

## ATM 文件格式

在 Markdown ATM 文件中，标题提供上下文和作用域，标题本身不会启动任务。可执行任务从 `/task`、`/for`、`/go`、`/wait`、`/if`、`/else` 等控制命令开始，也可以从带后续 prompt 的任务头命令开始。

```md
# 项目背景

这里的普通 Markdown 会作为本节任务的上下文。

## 文档

/task docs
结合上面的本节上下文审查文档。
```

任务命令速查：

| 命令 | 用途 |
| --- | --- |
| `/task [name]` | 开始一个 prompt 任务，并可选地命名 agent 会话。 |
| `/resume name` | 继续此前命名的 agent 会话。 |
| `/fork name` | 从具名会话分叉，并基于该历史运行当前任务。 |
| `/args ...` | 给当前任务的所选 runner 追加 CLI 参数。 |
| `/cd path` | 准备并进入任务工作区。 |
| `/let name value` | 定义模板变量，或定义来自 `/bash`、`/call` 的 lazy 值。 |
| `/bash script` | 在 prompt 前运行 shell 脚本。 |
| `/output [file]` | 保存任务输出；带 schema fence 时要求结构化 JSON。 |
| `/db ...` | 挂载本地 JSON 任务数据库，作为记忆或黑板。 |
| `/skill ...` | 声明或挂载当前任务工作区可用的本地 skill。 |
| `/mcp ...` | 声明 MCP server、暴露定义，或授权任务访问 MCP 工具。 |
| `/webhook ...` | 声明 Webhook 目标或发送 Webhook 通知。 |
| `/def` + `/call` | 定义并复用任务模板。 |
| `/return ...` | 从定义中返回文本、bash 输出或多行模板。 |
| `/import ...` | 从其他 ATM 文件导入定义。 |
| `/pool name max [buffer]` | 声明具名后台工作池。 |
| `/if condition` | 用本地表达式或结构化检查选择分支。 |
| `/else` | 为 `/if` 提供另一条分支。 |
| `/for ...` | 重试、循环，或按文件、目录、范围、列表逐项执行。 |
| `/go [pool]` | 在后台启动任务。 |
| `/wait [pool]` | 等待此前启动的后台任务。 |
| `/flag ...` | 为 `atm run`、动态命令和 `serve` API 声明参数。 |
| `/context #Heading` | 把其他 Markdown section 的普通文档加入任务上下文。 |
| `/doc ...` | 写只给人看的说明，不进入 agent 上下文。 |

完整格式见 [用户手册](docs/zh/user/README.md) 和 [命令参考](docs/zh/commands.md)。

## 复用工作流

任意 ATM 文件都可以直接运行、注册成本地命令，或通过 `serve` 暴露为接口。需要让复用工作流接收 CLI 或 API 参数时，再用 `/flag` 声明参数。

```md
/flag string area 要审查的项目区域
/flag bool fix 是否应用安全修复 default:false

/task
审查 {{area}}。如果 {{fix}} 为 true，应用安全修复。
```

注册成本地命令：

```sh
./atm flag register workflows/review.md --name review
./atm review -area docs -fix
```

把 ATM 文件服务成 HTTP API：

```sh
./atm serve workflows/review.md --addr 127.0.0.1:8080
```

需要项目内可复用 API 时，可以注册路由：

```sh
./atm serve register workflows/review.md --path /review
./atm serve --addr 127.0.0.1:8080
```

`GET /review` 会同步运行。`POST /review` 会创建异步 job，`GET /jobs/{id}` 查询 job 状态。`GET /openapi.json` 返回自动生成的 OpenAPI 元数据。

## 运行结果

`atm run` 会使用托管 live 工作副本；运行退出时，原始源文件会恢复为执行前内容。

| 内容 | 默认位置 |
| --- | --- |
| 运行工作区 | `~/.atm/runs/<run-id>/` |
| 结果文档 | `~/.atm/runs/<run-id>/result.todo.md` |
| 任务报告、日志、事件流和输出 | `~/.atm/runs/<run-id>/tasks/<task-id>/` |
| 项目级动态命令产物 | `.atm/commands/<command>/<timestamp>/` |
| 项目级 API 产物 | `.atm/api/runs/...` 和 `.atm/api/jobs/...` |

可以用 `ATM_HOME` 改变 ATM home。`--output DIR` 或 `-o DIR` 可以改写共享输出目录，但源文件备份、任务目录和 `result.todo.md` 仍保存在托管运行工作区。

未完成的 run 可以继续执行：

```sh
./atm resume <run-id>
./atm resume --last
```

如果 run 中断时源文件仍被占位文件隐藏，可以恢复最近保存的源文件副本：

```sh
./atm resume --restore-source
```

## 工作原理

ATM 会先把 ATM 文件编译成静态执行计划。`atm check --plan` 只打印计划，不运行 bash 脚本，也不启动 agent，所以可以先审查循环、分支、后台任务、工作池和任务依赖。

执行 `atm run` 时，ATM 会把源 ATM 文件和 import 文件复制到托管运行工作区，在 live 工作副本上执行任务，并在 run 退出时恢复原始源文件。状态块、报告、runner 事件流、结构化输出和日志都会写入运行工作区，方便审计和恢复。

自然语言检查，例如 `/for 3 until 测试通过`，通过结构化工具调用实现。每次尝试后，ATM 会向 runner 暴露一个作用域受限的 MCP 检查工具；当前默认通过本机 loopback streamable HTTP endpoint 提供，并要求模型用机器可读字段报告条件是否满足，而不是让系统从自由文本里猜结果。`/for until(expr)` 这类本地表达式形式不经过模型，直接由 ATM 确定性求值。

ATM 把需要结构化输出的边界都放到作用域受限的 MCP 工具里完成，原因是现代编码代理经过训练后，通常能比较可靠地使用工具和 MCP 风格 schema。凡是 ATM 需要结构化判断或结果的地方，例如 `until` 判断、带 schema 的 `/output`，或由 definition 暴露的 helper 输出，都会暴露一个作用域很窄的本地 MCP endpoint，并读取工具结果，而不是解析 assistant 的自然语言文本。`/db`、`/webhook` 等能力也会在 agent 需要受控访问本地状态或外部通知时使用作用域受限的工具。

## 常用命令

CLI 命令速查：

| 命令 | 用途 |
| --- | --- |
| `atm --version` | 输出 CLI 版本；release 构建会包含 commit 和构建时间。 |
| `atm run [files...]` | 执行待处理 prompt block；也是默认命令。 |
| `atm resume ...` | 继续未完成的托管 run，或恢复保存的源文件；支持 `--last` 和 `--restore-source`。 |
| `atm flag register/scan/unregister/list` | 管理注册为动态 CLI 命令的 ATM 文件。 |
| `atm append <file> [prompt...]` | 向源文件或 active run 追加格式化任务。 |
| `atm check [files...]` | 校验 ATM 文件，不启动 agent。 |
| `atm report ...` | 汇总任务报告和审计状态。 |
| `atm clean ...` | 移除生成状态/report block 或审计产物。 |
| `atm format <file>` | 规范化任务头和生成状态布局。 |
| `atm serve [file]` | 把单个 ATM 文件服务成 HTTP API。 |
| `atm serve register/scan/unregister/list` | 管理已注册的 API ATM 文件。 |

常用示例：

| 命令 | 用途 |
| --- | --- |
| `./atm check todo.md` | 校验 ATM 文件，不执行任务。 |
| `./atm check --plan todo.md` | 输出 dry-run 执行计划。 |
| `./atm check --plan --preview todo.md` | 在计划中包含可预览的 lazy provider 值。 |
| `./atm check --open todo.md` | 在浏览器中打开临时计划流程图。 |
| `./atm check --plan todo.md --html plan.html` | 把同一个流程图写入文件。 |
| `./atm append todo.md 'Review README.'` | 向源文件或 active run 追加格式化任务。 |
| `./atm format todo.md` | 规范化任务头和生成状态布局。 |
| `./atm report` | 汇总当前项目最近一次运行。 |
| `./atm clean result.todo.md` | 移除生成状态块，保留审计产物。 |
| `./atm clean --repair-ids result.todo.md` | 修复重复的生成 report id。 |

## 更多

- 用户手册：[docs/zh/user/README.md](docs/zh/user/README.md)
- CLI 手册：[docs/zh/user/reference/cli.md](docs/zh/user/reference/cli.md)
- 命令参考：[docs/zh/commands.md](docs/zh/commands.md)
- 可直接修改的示例：[快速开始](examples/zh/quick-start.todo.md)、[简单](examples/zh/simple.todo.md)、[复杂](examples/zh/complex.todo.md)
- 设计说明：[docs/zh/design.md](docs/zh/design.md)
- 安全政策：[SECURITY.md](SECURITY.md)

## 许可证

MIT。见 [LICENSE](LICENSE)。
