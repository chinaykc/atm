# 命令手册

本页是面向用户的 DSL（领域专用语言）命令速查。更底层的完整参考见 [../../commands.zh-CN.md](../../commands.zh-CN.md)。

## 任务命令总览

| 命令 | 作用 | 常见位置 |
| --- | --- | --- |
| `/resume` | 继续所选工具最近会话 | 任务开头 |
| `/args ...` | 给 Codex/Claude 追加参数 | 任务开头 |
| `/cd path` | 准备并进入任务工作区；默认创建目录 | 任务开头 |
| `/let name value` | 定义变量 | 任务开头或全局块 |
| `/let name /bash ...` | 执行 bash 并捕获 stdout | 任务开头或全局块 |
| `/let name /call ...` | 调用定义并绑定返回值 | 任务开头 |
| `/bash ...` | 执行 bash，失败则任务失败 | 任务开头 |
| `/output [file]` | 保存文本输出或要求结构化 JSON 输出 | 任务块内任意位置 |
| `/db new ...` | 声明本地任务数据库 | 全局块 |
| `/db use/access/ignore ...` | 控制当前任务块的数据库可见性和权限 | 任务开头 |
| `/def name ...` | 定义单任务模板 | 定义块或 Markdown heading |
| `//def name ...` | 定义多任务模板 | Markdown heading |
| `/call name ...` | 调用定义 | 任务块或 prompt 独立行 |
| `/return ...` | 从定义返回值 | 定义内部 |
| `/import ...` | 导入定义 | 全局块 |
| `/for ...` | 循环、重试、遍历 | 任务开头 |
| `/pool name max [buffer]` | 声明工作池 | 全局块或定义内部 |
| `/go [pool]` | 后台运行后续任务 suffix | 任务开头 |
| `/wait [pool]` | 等待后台任务 | 任务开头 |

## 模板变量

旧式：

```txt
审查 {{path}} 第 {{N}} 次。
```

Go template：

```gotemplate
{{if .N}}第 {{.N}} 次{{end}}
{{index .Vars "path"}}
{{var "name-with-dash"}}
{{has "path"}}
```

结构化返回值字段：

```txt
/let gate /call check_release
发布是否通过：{{gate.passed}}
原因：{{gate.reason}}
```

## 系统提供的渲染上下文

| 上下文 | 系统值 |
| --- | --- |
| 普通 prompt、`/bash`、`/args`、`/cd`、`until`、`/return`、`/output` schema | `/let` 变量、`/for` 变量、definition 参数、`/let ... /call` 返回值 |
| `/return` | `{{agent.message}}`、`{{agent.last_message}}`、`{{agent.messages}}`、`{{agent.messages_json}}` |
| 后台 `/output` 文件名 | `{{agent_index}}`、`{{agent}}`、`{{agent_label}}` |

`agent.*` 不是普通 prompt 的全局变量；它只在 `/return` 中表示“当前 definition 调用已经产生的最近 assistant 消息”。如果要在普通 prompt 里使用它，先通过 `/let name /call ...` 接收返回值。

## 常用组合

### 任务工作区

```txt
/cd services/payments
在这个目录下实现任务。
```

`/cd path` 会在目录不存在时自动创建。需要要求目录必须已存在时，写 `/cd --must-exist path`。解析后的路径必须留在原始 todo 文件所在目录内；`/cd` 会影响 agent、`/bash`、`/let ... /bash` 和 CEL 文件函数。

### 重试直到完成

```txt
/for 3 until tests pass
运行测试并修复失败。
```

### 本地 CEL 条件

```txt
/for until(exists("result.json") && json("result.json").passed)
持续生成并修复 result.json。
```

`until(...)` 使用 CEL 本地判断，必须返回 `bool`。常用函数包括 `exists`、`read`、`json`、`existsOutput`、`readOutput`、`jsonOutput` 和 `len`。

### 条件分支

```txt
/if (json("gate.json").passed)
继续。

/else
停止并说明原因。
```

`/if(...)` 使用本地 CEL；`/if 自然语言条件` 使用 agent MCP check。`/if` 和 `/else` 是任务块级控制流，未选中的块会标记为 skipped。嵌套时使用 header-only `/if`，并且必须写匹配的 `/else`；`/else` 匹配最近的未匹配 `/if`。

### 并行审查

```txt
/pool reviewer 3

/for area in [api docs tests] /go reviewer
审查 {{area}}。

/wait reviewer

汇总审查结果。
```

动态 planner 分发：

```txt
/for plan in(/call plan_shards)
/go reviewer
{{plan}}
```

### 结构化输出

````txt
判断发布门禁。

/output gate
```
passed:boolean:是否通过
reason:string:原因
```
````

### 数据库黑板

声明一个本次 run 内共享的黑板：

```txt
/db new review_board scope:global persist:run access:append
并行 reviewer 追加发现。Key 使用 findings/<area>。
```

并行任务追加，汇总任务只读：

```txt
/for area in [api docs tests] /go
审查 {{area}}，把发现追加到 review_board 的 findings/{{area}}。

/wait

/db access review_board read
读取 review_board 的 findings/**，汇总阻塞风险。
```

常用 `/db` 子命令：

| 命令 | 含义 |
| --- | --- |
| `/db new name [scope:local/global] [persist:run/project] [access:read/append/write/admin]` | 声明 DB |
| `/db use name [access:level]` | 当前任务启用 local DB，或覆盖可见 DB 权限 |
| `/db access name level` | 当前任务调整已可见 DB 权限 |
| `/db access * level` | 当前任务调整所有可见 DB 权限 |
| `/db ignore name...` | 当前任务禁用指定 DB |
| `/db ignore` | 当前任务禁用所有 DB |

DB MCP 工具包括 `atm_db_list`、`atm_db_get`、`atm_db_scan`、`atm_db_append`、`atm_db_set` 和 `atm_db_delete`。`scan` 支持 glob；`**` 可以跨 `/` 分段匹配。

### Skill 和 MCP

声明本地 skill 和临时 MCP server，然后在任务中启用：

````txt
/skill new reviewer from .atm/skills/reviewer

/mcp new helper
```json
{"command":"helper-mcp","args":["--stdio"]}
```

/cd work
/skill use reviewer
/mcp use helper
/mcp def use review
执行需要 reviewer skill 和 helper MCP 的任务。
````

`/skill use` 要求源目录已存在且包含 `SKILL.md`。`/mcp def use` 会把选中的 `/def` 暴露成 agent 可调用的临时 MCP 工具。

### 可复用定义

```md
## /def city

判断当前城市。只输出城市名。

/return {{agent.last_message}}

## /weather

查询
/call city
天气。
```

## 语义细节

- 命令只在 prompt 开始前识别。
- 正文独立行 `/call` 是例外，它会被执行并替换成返回值。
- `/go` 会把后续 suffix 放入后台分支；推荐复杂控制流换行写，例如 `/for ...` 下一行 `/go reviewer`。
- `/wait name` 只等待指定池此前提交的任务。
- 进程退出前，ATM 默认等待所有剩余后台任务。
- `/output` 一个任务块最多一个。
- `/db ignore` 不带参数时不能和同一任务块的 `/db use` 或 `/db access` 混用。
- `/db access` 只能降低权限，不能超过声明时的最大 `access`。
- `/return` 支持普通文本、bash、多行文本和结构化 JSON；作为返回值时优先级高于 `/output` fallback。
