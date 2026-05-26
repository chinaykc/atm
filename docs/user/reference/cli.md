# 命令行手册

## 总览

```sh
atm [run] [flags] [files...]
atm exec todo.txt
atm plan todo.txt
atm check -file todo.txt
atm report -file todo.txt
atm clean todo.txt --reports --state --logs
atm repair-ids todo.txt
atm append -file todo.txt "新任务"
atm format -file todo.txt
atm untag -file todo.txt
atm mcp check -result-file /tmp/atm/check.json
atm mcp output -result-file /tmp/atm/out.json -schema-file schema.json
atm mcp db -config-file /tmp/atm/db-config.json
```

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 命令成功；对 `run`/`exec` 表示本次计划任务已完成。 |
| `1` | 执行失败，例如 agent、bash、文件读写或工具适配器错误。 |
| `2` | CLI/DSL 输入校验失败，例如任务语法、表达式、重复 report id、未知 pool/db/skill/MCP 引用或 `check` 的 error 级诊断。 |
| `3` | 硬状态不一致，需要先检查或修复 `.atm/state.json`、主文档 report block 和 `.atm/reports/` 的关系。 |
| `130` | 用户中断。POSIX 下其他终止信号按 `128 + signal` 返回。 |

`atm check` 发现审计产物不一致时通常输出 warning 并返回 `0`，因为这些问题需要人工确认但不一定阻止 DSL 编译。只有被实现判定为硬状态不一致的错误才使用退出码 `3`。

## `atm run`

执行 pending 任务。`run` 是默认子命令，也是 live/rescan 模式：

```sh
atm -file todo.txt
atm run -file todo.txt
```

常用参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-file PATH` | 自动查找 `todo.txt`、`todo.md`、`toto.md` | todo 文件，可重复 |
| `-tool codex|claude|claude-code` | `codex` | 选择工具适配器 |
| `-codex PATH` | `codex` | Codex 可执行文件 |
| `-claude PATH` | `claude` | Claude Code 可执行文件 |
| `-messages N` | `1` | 每个分支保留最近 N 条 assistant 消息 |
| `-output DIR` | `.atm/YYYYMMDDHHMMSS[-N]` | 输出产物目录 |
| `-o DIR` | 同 `-output` | `-output` 简写 |
| `-jobs N` | `NumCPU` | 所有池共享的全局后台并发上限 |

多个文件按顺序排队：

```sh
atm run todo.txt rollout.md followup.md
atm run -file todo.txt -file rollout.md
```

同一个 `-output DIR` 搭配多个文件时，ATM 会为每个文件创建编号子目录，避免覆盖。

`run` 会在当前执行仍活跃时反复重扫 todo 文件。通过 `atm append` 或手动编辑追加的新 task block，可能被同一次 `run` 拾取并执行。这个模式适合人和 agent 持续协作：先启动一批任务，再在运行中追加后续任务或协调任务。

## `atm exec`

执行启动时的 todo 快照，也就是 one-shot/snapshot 模式：

```sh
atm exec todo.txt
atm exec -file todo.txt
```

`exec` 使用和 `run` 相同的工具参数、输出目录、消息数量、并发参数、状态文件、报告文件和锁机制，但它会在启动时冻结当前 task block 集合。运行期间通过 `atm append` 或手动编辑追加的新 task block 会保留在文档中，但不会进入本次执行；需要再次运行 `atm run` 或 `atm exec` 才会执行。运行中编辑已有 task block 时，ATM 仍通过 source hash/block lease 判断是否可以安全写回，不能安全写回的旧快照结果会进入 orphan report。

简单区分：

| 命令 | 执行集合 | 运行中追加 task | 典型用途 |
| --- | --- | --- | --- |
| `atm run` | 活跃文档会被重扫 | 可能被同一次执行拾取 | 持续协作、边跑边追加任务 |
| `atm exec` | 启动时冻结的 task 快照 | 不进入本次执行 | 发布、CI、复现、一次性批处理 |

## `atm plan`

预览执行计划，不运行 agent、不写状态。默认 `plan` 是静态 dry-run，不执行 bash，也不解析 lazy provider 的实际值：

```sh
atm plan todo.txt
atm plan -file todo.txt
atm plan --preview -file todo.txt
atm plan -html plan.html -file todo.txt
atm plan -open -file todo.txt
atm plan -json -file todo.txt
atm plan dry-run -file todo.txt
```

适合检查 `/for /go` 顺序、条件控制块、定义调用、全局变量、DB/skill/MCP 声明和任务级挂载配置、runner 参数以及输出配置。文本和 JSON plan 都会给出文档标题、Markdown 章节树、循环展开摘要、条件分支/skipped 摘要、可运行任务的源行号、Markdown scope/title path、默认上下文摘要、每个 task 的执行/跳过 decision 摘要、变量引用/来源摘要、task runtime environment、解析后的任务资源视图、异步 fan-out 和 `/wait` 汇合摘要；decision 会区分前台执行、后台 dispatch、纯 join、wait coordinator、条件分支和 parent/child 依赖，runtime 会汇总 resume、args、`/cd`、前置 `/bash` 和 lazy provider，资源视图会展开默认 `scope:global` DB 挂载和显式 DB/skill/MCP/def-MCP 挂载。静态 plan 会用 warning 标出 lazy `/let ... /bash` 潜在副作用点和 lazy `/let ... /call` 渲染期依赖，但不会执行它们。`-html` 会保存单文件 HTML 流程图，展示 parent/child task、WaitAgent、显式 `/wait` 汇合和未汇合后台任务；`-open` 会生成临时 HTML 并用默认浏览器打开。

`--preview` 会显式进入 provider 预览模式：它可能执行 lazy `/let name /bash ...`，也可能执行不需要运行 agent 就能返回的纯 lazy `/let name /call ...`，把结果作为预览值显示在文本或 JSON 输出中，但仍不运行 agent、不写主文档 report、不更新 `.atm/state.json`。需要运行期执行的 lazy call 会在 preview 输出中列为未执行 provider。

## `atm check`

只编译和校验 todo 文件，不运行 agent、不执行 bash、不写状态：

```sh
atm check -file todo.txt
atm check -json -file todo.txt
atm check first.todo.md second.todo.md
```

`check` 会解析 Markdown section context、导入的 `/def`、本地表达式语法、定义调用参数、pool/db/skill/MCP 引用等。它也会检查 `.atm/state.json`、主文档 report block 和 `.atm/reports/*.md` 的明显不一致，例如缺失 detail report、state 中存在主文档没有的 task id、主文档和 state 的 status/report 路径/source hash/rendered prompt hash 不一致、orphan detail report。error 级诊断会返回非零状态；warning 级诊断会显示但不阻止通过，例如有 `/go` 没有后续匹配 `/wait`、lazy provider 是潜在副作用点或渲染期依赖，或审计产物不一致。`-json` 会输出 `diagnostics`，包含 `severity`、`source`、`block`、`line`、`column` 和消息。导入文件中的定义错误会指向导入文件本身。

## `atm report`

汇总主文档状态块、`.atm/state.json` 和 `.atm/reports/*.md`，不运行 agent、不执行 bash、不写状态：

```sh
atm report -file todo.txt
atm report -json -file todo.txt
```

`report` 会统计 `done`、`running`、`failed`、`skipped` 和 `draft` 数量，列出失败任务、orphan report 和最近日志路径。`draft` 表示当前文档中仍会被编译为待执行任务的 task block。普通文本输出适合人快速查看；`-json` 输出 `files[].counts`、`failures`、`orphans` 和 `recent_logs`，其中任务项包含可用的 `id`、`status`、`report`、`source` 和 `rendered`，适合工具或 CI 读取。

## `atm clean`

清理 ATM 生成内容，不删除用户正文：

```sh
atm clean todo.txt
atm clean todo.txt --reports
atm clean todo.txt --state
atm clean todo.txt --logs
atm clean todo.txt --all
```

不带清理选项时，`clean` 只移除主文档里的生成 report/status block，保留 `.atm/state.json`、`.atm/reports/` 和 `.atm/logs/`。显式选项用于删除审计产物：`--reports` 删除 `.atm/reports/`，`--state` 删除 `.atm/state.json`，`--logs` 删除 `.atm/logs/`，`--all` 同时清理主文档状态块和这些 `.atm` 产物。`clean` 支持位置文件参数，也支持 `-file todo.txt`。

## `atm repair-ids`

修复主文档里重复的 ATM report id：

```sh
atm repair-ids todo.txt
atm repair-ids -file todo.txt
```

如果用户复制了带 `<!-- atm:report ... -->` 的任务块，文档会出现重复 id，`atm check` 和执行前解析会报错。`repair-ids` 保留第一个 id，给后续重复 report 重新生成唯一 id、source hash 和 report 路径。它只改主文档中的 report identity，不删除 `.atm/state.json` 或 `.atm/reports/`；修复后仍可用 `atm report` 和 `atm check` 查看剩余审计 warning，再按需要执行 `atm clean`。

## `atm append`

向 todo 文件追加任务：

```sh
atm append -file todo.txt "运行测试并修复失败。"
```

从 stdin 读取：

```sh
printf '审查 README。' | atm append -file todo.txt
```

没有参数且 stdin 是终端时，ATM 会打开 `$VISUAL`、`$EDITOR` 或平台默认编辑器。

运行中的 ATM 会把 todo 文件移动到临时活跃路径；`append -file 原路径` 会自动解析并写入活跃文件。如果当前 `atm run` 仍有任务在执行，追加任务会在后续重新扫描时被当前 run 执行；如果 run 已经退出，则需要再次执行 `atm run`。

## `atm format`

整理生成状态块：

```sh
atm format -file todo.txt
```

## `atm untag`

移除生成状态：

```sh
atm untag -file todo.txt
atm untag -file todo.txt -done=false
atm untag -file todo.txt -running=false
```

参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-file PATH` | `todo.txt` | todo 文件 |
| `-done` | `true` | 是否移除 done 状态 |
| `-running` | `true` | 是否移除 running 状态 |

## `atm mcp`

`mcp` 子命令主要给 agent runtime 和测试使用。普通用户通常不直接调用。

### `atm mcp check`

运行 `until` 检查用的临时 stdio MCP server：

```sh
atm mcp check -result-file /tmp/atm/check-result.json
```

工具名：`atm_report_check`

输入：

```json
{"passed": true, "summary": "简短依据"}
```

### `atm mcp output`

运行结构化输出用的临时 stdio MCP server：

```sh
atm mcp output \
  -result-file /tmp/atm/output.json \
  -schema-file /tmp/atm/schema.json \
  -schema-format json
```

工具名：`atm_report_output`

输入 schema 来自 `/output` 的 fenced schema block。

### `atm mcp db`

运行 `/db` 使用的临时 stdio MCP server：

```sh
atm mcp db -config-file /tmp/atm/db-config.json
atm mcp db -config-file /tmp/atm/db-config.json -readonly
```

`-config-file` 是 ATM 运行时生成的 JSON 配置，普通用户通常不需要手写。配置包含当前任务可见的 DB 名称、数据文件路径、scope、persist、access 和 usage。

工具名：

| 工具 | 说明 |
| --- | --- |
| `atm_db_list` | 列出当前任务可见 DB |
| `atm_db_get` | 读取一个 key |
| `atm_db_scan` | 用 glob 遍历 key |
| `atm_db_append` | 追加字符串值 |
| `atm_db_set` | 替换 key 的字符串数组 |
| `atm_db_delete` | 删除 key 或 key 中指定值 |

`-readonly` 会把所有 DB 降为只读。ATM 在自然语言 `until` 和 `/if` 检查中使用这个模式。

### `atm mcp defs`

运行 `/mcp def use` 使用的临时 stdio MCP server：

```sh
atm mcp defs -config-file /tmp/atm/defs-config.json
```

`-config-file` 是 ATM 运行时生成的 JSON 配置，包含当前 todo 文件、允许暴露的 definition、当前 workdir、DB、skill、MCP 和 runner 配置。普通用户通常不需要手写。
