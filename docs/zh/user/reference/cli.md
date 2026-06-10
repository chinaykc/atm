# 命令行手册

## 总览

```sh
atm [run] [flags] [files...]
atm check todo.txt
atm check --plan todo.txt
atm report
atm report <run-id>
atm resume <run-id>
atm resume --last
atm resume <run-id> --restore-source
atm clean --repair-ids result.todo.md
printf '/task\n新任务\n' | atm append todo.txt
atm format todo.txt
atm flag register workflows/review.todo.md --name review
atm flag scan
atm serve [file] --addr 127.0.0.1:8080
atm serve register workflows/create.todo.md --path /user/create
atm serve scan
```

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 命令成功；对 `run` 表示本次计划任务已完成。 |
| `1` | 执行失败，例如 agent、bash、文件读写或工具适配器错误。 |
| `2` | CLI/DSL 输入校验失败，例如任务语法、表达式、重复 report id、未知 pool/db/skill 引用或 `check` 的 error 级诊断。 |
| `3` | 硬状态不一致，需要先检查或修复 `.atm/state.json`、主文档 report block 和任务详细报告的关系。 |
| `130` | 用户中断。POSIX 下其他终止信号按 `128 + signal` 返回。 |

`atm check` 发现审计产物不一致时通常输出 warning 并返回 `0`，因为这些问题需要人工确认但不一定阻止 DSL 编译。只有被实现判定为硬状态不一致的错误才使用退出码 `3`。

## `atm run`

执行 pending 任务。`run` 是默认子命令，也是 live/rescan 模式：

```sh
atm todo.txt
atm run todo.txt
```

常用参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-tool codex|claude|claude-code` | `codex` | 选择工具适配器 |
| `-codex PATH` | `codex` | Codex 可执行文件 |
| `-claude PATH` | `claude` | Claude Code 可执行文件 |
| `-danger` | `false` | 给每次 agent 调用追加所选 runner 的危险全权限参数：Codex 使用 `--dangerously-bypass-approvals-and-sandbox`，Claude Code 使用 `--dangerously-skip-permissions` |
| `-messages N` | `1` | 每个分支保留最近 N 条 assistant 消息 |
| `-retries N` | `3` | 重试临时 agent 失败；`0` 关闭 |
| `-output DIR` | `~/.atm/runs/<run-id>/outputs` | 输出产物目录 |
| `-o DIR` | 同 `-output` | `-output` 简写 |
| `-jobs N` | `NumCPU` | 所有池共享的全局后台并发上限 |

源 atm 文件必须作为位置参数显式给出；`atm run` 不会扫描当前目录中的 `todo.txt`、`todo.md` 或 `toto.md`，也不提供 `-file` 参数。省略 `run` 时，第一个位置文件仍会启动默认执行命令：

```sh
atm todo.txt rollout.md -jobs 4
```

如果 atm 文件中声明了 `/flag`，单文件运行时这些参数会成为该文件的动态 CLI 参数，并注入同名模板变量：

```txt
/flag string name 用户名

/task
hello {{name}}
```

```sh
atm run greet.todo.md -name Ada
```

多文件运行不能传文档 flag。`bool` flag 未传时为 `false`；其他无默认值 flag 必填。

多个文件按顺序排队：

```sh
atm run todo.txt rollout.md followup.md
atm todo.txt rollout.md followup.md -jobs 4
```

同一个 `-output DIR` 搭配多个文件时，ATM 会为每个文件创建编号子目录，避免覆盖。无论是否指定 `-output`，源文件备份、manifest 和最终 `result.todo.md` 都保存在 `~/.atm/runs/<run-id>/`。

`run` 会在当前执行仍活跃时反复重扫工作副本。启动时，ATM 会复制源 todo 和递归 `/import` 文件到 `~/.atm/runs/<run-id>/`，把原路径替换成只提示 agent 忽略的占位文件，执行结束后恢复原文件不变。运行期间通过 `atm append <源 todo>` 追加任务时，ATM 会解析到当前 active 工作文件，因此本次 run 可以拾取新增 task block；如果 run 已经结束，`append` 会写入源文件，留到下次 `atm run` 执行。也可以直接编辑 CLI 输出的 working file。这个模式适合隔离 agent 对源文件的读写，同时保留可恢复的执行副本。

## `atm resume`

继续或恢复托管运行目录：

```sh
atm resume <run-id>
atm resume --last
atm resume --project /path/to/project --last
atm resume --source /path/to/todo.md --last
atm resume --restore-source
atm resume <run-id> --restore-source
atm resume <run-id> --restore-source ./todo.md
```

`run-id` 对应 `~/.atm/runs/<run-id>/manifest.json`。`atm resume <run-id>` 继续这个托管运行。`--last` 从 `~/.atm/runs/index.json` 选择最近的未完成运行；多个项目都有未完成运行时，可用 `--project` 或 `--source` 过滤。`--restore-source` 只恢复源副本；不传 `run-id` 时默认按当前工作目录查找最近一个运行副本，包含已成功和未完成的运行，也可用 `--project` 或 `--source` 过滤。不指定目标时恢复主源文件和所有 import 文件到原路径，指定目标时只把主源副本写到该目标。目标不存在时直接写；目标是 ATM 占位文件时直接替换；目标已存在且不是占位文件时需要交互确认，非交互环境需显式加 `--force`。

## 动态命令

动态命令是显式注册项。本地注册表保存在 `.atm/flag/index.json`；全局注册表使用 Go 的跨平台 `os.UserConfigDir()` 解析用户配置目录，如果系统没有提供配置目录，则回退到用户 home 下的 `.atm`。ATM 启动时只读取注册表，不扫描 `$HOME` 或 `./.atm/flag`。可以显式注册单个文件，也可以把当前项目的 `./.atm/flag` 扫描一次写入注册表：

```sh
atm flag register workflows/review.todo.md --name review --description "运行审查任务"
atm flag register workflows/review.todo.md --name review -g
atm flag scan
atm flag scan -g
atm flag list
```

动态命令会复用 `run` 的执行参数，并把目标 atm 文件中的 `/flag` 显示为 CLI 参数：

```sh
atm review -h
atm review -target api
```

动态命令不能和内置命令或其他动态命令重名。

通过动态命令执行时，ATM 不会把 `> [!ATM]` 状态块写回原始命令文档。它会先复制源文档再运行副本，产物按命令名组织：

```txt
.atm/commands/<command>/<timestamp>/source.todo.md
.atm/commands/<command>/<timestamp>/.atm/state.json
.atm/commands/<command>/<timestamp>/tasks/<task-id>/logs/
.atm/commands/<command>/<timestamp>/result/result.md
```

`source.todo.md` 是执行副本，`result/result.md` 是执行结束后的结果副本。原始 `.atm/flag/...` 文档保持不变。直接用 `atm run` 执行的源文件也默认保持不变，结果在 `~/.atm/runs/<run-id>/result.todo.md`。

## `atm serve`

把 atm 文件暴露成 HTTP API：

```sh
atm serve path/to/file.todo.md --addr 127.0.0.1:8080
atm serve register workflows/create.todo.md --path /user/create
atm serve scan
atm serve --addr 127.0.0.1:8080
```

`serve` 复用 `run` 的工具参数：`-tool`、`-codex`、`-claude`、`-danger`、`-jobs`、`-messages`、`-output`。启动时必须显式传入一个文件，或先用 `atm serve register` 把文件写入注册表；默认写当前项目的 `.atm/api/index.json`，加 `-g` 写全局注册表。`atm serve scan` 是显式的一次性导入动作，只扫描当前项目 `./.atm/api` 并写入注册表，同时跳过生成目录 `runs/` 和 `jobs/`；也可加 `-g` 导入到全局注册表。注册时不指定 `--path` 会使用文件 basename 去掉 todo 后缀后的路径，例如 `create.todo.md` 对应 `/create`。使用 `--path /user/create` 会暴露：

```txt
/user/create
/user/create.todo.md
```

带后缀和不带后缀的路径都会注册；任何路径冲突都会使服务启动失败。

```sh
atm serve list
atm serve unregister /user/create
```

`serve` 启动时不遍历 `.atm/api` 或任何当前目录 atm 文件。特别是执行生成的 `.atm/api/runs/` 和 `.atm/api/jobs/` 只能作为产物读取，不会在服务重启后成为新路由。

每个 API 文件默认有两个入口：

| 方法 | 行为 |
| --- | --- |
| `GET /path?...` | 同步执行临时副本，不修改源 API 文件 |
| `POST /path?...` | 创建异步 job；query 和 JSON body 都可传参，JSON body 覆盖 query |

`POST` 返回：

```json
{"jobId":"...","status":"queued","statusUrl":"/jobs/{jobId}"}
```

`GET /jobs/{jobId}` 返回 `queued`、`running`、`succeeded` 或 `failed`，并包含参数、时间、结果或错误。job 状态持久化到 `./.atm/api/jobs/<jobId>/job.json`。

API 源文件也不会被写回引用格式状态块。GET 和 POST 都运行源文件副本，产物位置如下：

```txt
# GET /user/create
.atm/api/runs/user-create/<timestamp>/source.todo.md
.atm/api/runs/user-create/<timestamp>/.atm/state.json
.atm/api/runs/user-create/<timestamp>/tasks/<task-id>/logs/
.atm/api/runs/user-create/<timestamp>/result/result.md

# POST /user/create
.atm/api/jobs/<jobId>/job.json
.atm/api/jobs/<jobId>/source.todo.md
.atm/api/jobs/<jobId>/.atm/state.json
.atm/api/jobs/<jobId>/tasks/<task-id>/logs/
.atm/api/jobs/<jobId>/result/result.md
```

`GET /openapi.json` 根据显式文件或注册文件及其中的 `/flag` 自动生成 OpenAPI 描述；当前不提供 Swagger UI。

响应规则：

| 执行结果 | HTTP 响应 |
| --- | --- |
| 单个结构化 `/output` | 直接返回该 JSON |
| 多个结构化 `/output` | `{"outputs":[...]}` |
| 无结构化输出 | `{"value":"latest assistant text"}` |
| 执行失败 | 统一 JSON error |

## `atm check`

编译和校验 atm 文件，不运行 agent、不写状态。默认输出诊断摘要；加 `--plan` 时输出执行计划：

```sh
atm check todo.txt
atm check todo.txt -json
atm check first.todo.md second.todo.md
atm check --plan todo.txt
atm check --plan todo.txt --preview
atm check --plan todo.txt -html plan.html
atm check --plan todo.txt -open
atm check --plan todo.txt -json
```

`check` 会解析 Markdown section context、导入的 `/def`、本地表达式语法、定义调用参数以及 pool/db/skill 引用等。它也会检查结果文档、`.atm/state.json` 和 task 目录里报告文件的明显不一致。error 级诊断会返回非零状态；warning 级诊断会显示但不阻止通过，例如有 `/go` 没有后续匹配 `/wait`、lazy provider 是潜在副作用点或渲染期依赖，或审计产物不一致。`-json` 会输出 `diagnostics`，包含 `severity`、`source`、`block`、`line`、`column` 和消息。

`--plan` 适合检查 `/for /go` 顺序、条件控制块、定义调用、全局变量、DB/skill 声明和任务级配置、runner 参数以及输出配置。文本和 JSON plan 都会给出文档标题、Markdown 章节树、循环展开摘要、条件分支/skipped 摘要、可运行任务的源行号、Markdown scope/title path、默认上下文摘要、每个 task 的执行/跳过 decision 摘要、变量引用/来源摘要、task runtime environment、解析后的任务资源视图、异步 fan-out 和 `/wait` 汇合摘要。`--preview` 会进入 provider 预览模式：它可能执行 lazy `/let name /bash ...`，也可能执行不需要运行 agent 就能返回的纯 lazy `/let name /call ...`，但仍不运行 agent、不写 report、不更新 state。

## `atm report`

汇总运行状态，不运行 agent、不执行 bash、不写状态。默认读取当前项目最近一次 run：

```sh
atm report
atm report --last
atm report <run-id>
atm report --project /path/to/project
atm report --source /path/to/todo.md
atm report -json
```

`report` 会读取 `~/.atm/runs/index.json` 和目标 run 的 `manifest.json`，再汇总该 run 工作副本里的状态文档、`.atm/state.json` 和 `tasks/<task-id>/report.md`。它会统计 `done`、`running`、`failed`、`skipped` 和 `draft` 数量，列出失败任务、orphan report 和最近日志路径。`draft` 表示当前结果文档中仍会被编译为待执行任务的 task block。普通文本输出适合人快速查看；`-json` 输出 `files[].runId`、`files[].counts`、`failures`、`orphans` 和 `recent_logs`。

## `atm clean`

清理 ATM 生成内容，不删除用户正文：

```sh
atm clean todo.txt
atm clean todo.txt --reports
atm clean todo.txt --state
atm clean todo.txt --logs
atm clean todo.txt --all
atm clean --repair-ids result.todo.md
```

默认源文件保持干净，所以 `clean` 通常只用于处理 `result.todo.md` 或手动复制过 ATM 状态块的文档。不带清理选项时，`clean` 移除目标文档里的生成 report/status block。显式选项用于删除目标文件旁边的审计产物：`--reports` 删除 `.atm/reports/`，`--state` 删除 `.atm/state.json`，`--logs` 删除 `.atm/logs/`，`--all` 同时清理文档状态块和这些 `.atm` 产物。`--repair-ids` 修复重复的 ATM report id。

## `atm append`

向 atm 文件追加任务：

```sh
printf '/task\n运行测试并修复失败。\n' | atm append todo.txt
```

从 stdin 读取：

```sh
printf '/task\n审查 README。\n' | atm append todo.txt
```

追加内容必须至少包含一个任务块，例如 `/task` 加后续提示词。没有提示词参数且 stdin 是终端时，ATM 会打开 `$VISUAL`、`$EDITOR` 或平台默认编辑器。

`append` 接收源 todo 路径，但会先解析 active 文件。如果该源文件对应的 `run` 仍在执行，追加内容会写入当前 active 工作文件并可被本次 live/rescan 拾取；如果没有活跃 run，则写入给定源文件，留到下次执行。

## `atm format`

整理任务头与生成状态块：

```sh
atm format todo.txt
```

组合 task header 会规范化为每个命令一个 Markdown 段落，并在任务之间加入更易读的间距；配置合并结果和流程命令顺序保持不变。
