# 命令手册

本页是面向用户的 DSL（领域专用语言）命令速查。更底层的完整参考见 [../../commands.zh-CN.md](../../commands.zh-CN.md)。

## 任务命令总览

| 命令 | 作用 | 常见位置 |
| --- | --- | --- |
| `/resume` | 继续所选工具最近会话 | 任务开头 |
| `/args ...` | 给 Codex/Claude 追加参数 | 任务开头 |
| `/cd path` | 准备并进入任务工作区；默认创建目录 | 任务开头 |
| `/let name value` | 定义变量 | 任务开头或 scoped 声明块 |
| `/let name /bash ...` | 懒执行 bash 并捕获 stdout | 任务开头或 scoped 声明块 |
| `/let name /call ...` | 懒调用定义并绑定返回值 | 任务开头 |
| `/bash ...` | 执行 bash，失败则任务失败 | 任务开头 |
| `/context #Heading` | 引入其他 Markdown section 的普通文档上下文 | Markdown task header |
| `/doc text` 或 `/doc` + fenced block | 写只给人看的说明，不进入 agent 上下文 | Markdown section 普通区域 |
| `/output [file]` | 保存文本输出或要求结构化 JSON 输出 | task header |
| `/db new ...` | 声明本地任务数据库 | scoped 声明块 |
| `/skill new name from path` | 声明本地 skill | scoped 声明块 |
| `/skill use/ignore ...` | 控制当前任务的 skill 视图 | 任务开头 |
| `/mcp new name` | 声明临时 MCP server | scoped 声明块 |
| `/mcp use/ignore ...` | 控制当前任务的 MCP 视图 | 任务开头 |
| `/mcp def use name...` | 暴露可见定义为 MCP 工具 | 任务开头 |
| `/db use/access/ignore ...` | 控制当前任务块的数据库可见性和权限 | 任务开头 |
| `/def name ...` | 定义可复用任务模板 | 定义块 |
| `/call name ...` | 调用定义 | 任务/header 命令 |
| `/return ...` | 从定义返回值 | 定义内部 |
| `/import ...` | 导入定义 | scoped 声明块 |
| `/for ...` | 循环、重试、遍历 | 任务开头 |
| `/pool name max [buffer]` | 声明工作池 | scoped 声明块或定义内部 |
| `/go [pool]` | 后台运行后续任务 suffix | 任务开头 |
| `/wait [pool]` | 等待后台任务 | 任务开头 |

## 模板变量

旧式：

```txt
审查 {{file}} 第 {{n}} 次。
```

Go template：

```gotemplate
{{if .n}}第 {{.n}} 次{{end}}
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
| 普通 prompt、`/bash`、`/args`、`/cd`、`until`、`/return`、`/output` schema | `/let` 变量、读取后的懒 `/let ... /bash` 与 `/let ... /call`、`/for` 变量、definition 参数 |
| `/return` | `{{agent.message}}`、`{{agent.last_message}}`、`{{agent.messages}}`、`{{agent.messages_json}}` |
| 后台 `/output` 文件名 | `{{agent_index}}`、`{{agent}}`、`{{agent_label}}` |

`/let name /bash ...` 和 `/let name /call ...` 只有在变量被 prompt、`/bash`、`/args`、`/cd`、`until`、`/return`、`/output` schema 或表达式实际读取时才执行。同一次任务调用内第一次读取后会缓存；未使用的绑定不会在普通执行中触发。静态 `plan/check` 会用 warning 标出 lazy bash 的潜在副作用和 lazy call 的渲染期依赖；plan flow 中显示为 `LazyBash(name)` 或 `LazyCall(def -> name)`。`plan --preview` 是显式例外：它可能执行 lazy bash，以及不需要运行 agent 就能返回的纯 lazy call。

独立 `/let` 块按 Markdown 词法作用域可见：根部 `/let` 对全文后续任务可见，heading 内 `/let` 只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 读取，也不能在声明前读取。Task header 里的 `/let` 只对当前 task block 及其 child-heading task 可见；继承到的 lazy provider 在 child task invocation 内解析并缓存，不和 parent 共享缓存；child section 中的同名 `/let` 会遮蔽父 task header 的值。

`agent.*` 不是普通 prompt 的全局变量；它只在 `/return` 中表示“当前 definition 调用已经产生的最近 assistant 消息”。如果要在普通 prompt 里使用它，先通过 `/let name /call ...` 接收返回值。

## 常用组合

### 任务工作区

```txt
/cd services/payments
在这个目录下实现任务。
```

`/cd path` 会在目录不存在时自动创建。需要要求目录必须已存在时，写 `/cd --must-exist path`。解析后的路径必须留在原始 todo 文件所在目录内；`/cd` 会影响 agent、`/bash`、`/let ... /bash` 和 本地表达式文件函数。

### 重试直到完成

```txt
/for 3 until tests pass
运行测试并修复失败。
```

### 本地表达式条件

```txt
/for until(exist("result.json") && json(open("result.json")).passed)
持续生成并修复 result.json。
```

`until(...)` 使用本地表达式本地判断，必须返回 `bool`。常用函数包括 `exist`、`open`、`outputDir`、`json`、`yaml`、`toml`、`len`、`range`、`files`、`dirs`、`walkFiles` 和 `walkDirs`。

数字区间可以写成 `/for shard in range(1, 4)`。表达式 helper 支持 `range(stop)`、`range(start, stop)` 和 `range(start, stop, step)`；`step` 不能是 `0`。文件和目录枚举使用 `/for file in files()`、`/for file in walkFiles("src")` 和 `/for dir in dirs()`；这是跨版本稳定规则，旧的 `/for file`、`/for dir`、`/for path` 控制头无效。动态序列为空时会输出运行时 warning 并跳过循环体。固定次数 `/for number` 仍然支持，例如 `/for 10`，并绑定小写 `n`。

### 条件分支

```txt
/if (json(open("gate.json")).passed)
继续。

/else
停止并说明原因。
```

`/if(...)` 使用本地表达式；`/if 自然语言条件` 使用 agent MCP check。`/if` 和 `/else` 是任务块级控制流，未选中的块会标记为 skipped。`/if` 可以在控制链中组合，例如 `/for 10 /if(n % 2 == 0) /go`；命令顺序决定控制流。`/if` 和 `/else` 不嵌套，复杂分支应通过 `/def` 封装。紧跟对应 `/if` 分支的空 `/else` 合法，表示 false 分支 no-op；`atm check` 会 warning，通常直接省略更清晰。

自然语言 `/if` 和 `until` 条件较长时，可以紧跟 fenced text 参数：

````txt
/if
```
发布门禁已打开
并且检查都通过
```
继续。

/for 3 until
```
测试通过
并且 lint 通过
```
修复失败项。
````

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
/output gate

判断发布门禁。

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

`/db new` 按 Markdown 词法作用域可见：根部声明对全文后续任务可见，heading 内声明只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 使用，也不能在声明前使用。`scope:global` 只表示在这个可见范围内默认挂载；`scope:local` 表示只声明，任务仍要显式 `/db use name`。

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

`/skill new` 和 `/mcp new` 按 Markdown 词法作用域可见：根部声明对全文后续任务可见，heading 内声明只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 读取，也不能在声明前读取。`/skill use` 要求源目录已存在且包含 `SKILL.md`；引用具名 skill 时名称必须在当前任务可见，直接写 path-like 值时按路径加载。`/mcp use` 必须引用当前任务可见的 MCP 声明。`/mcp def use` 会把选中的 `/def` 暴露成 agent 可调用的临时 MCP 工具。

### 可复用定义

```md
/def city

判断当前城市。只输出城市名。

/return {{agent.last_message}}

## weather

/let current_city /call city
查询 {{current_city}} 天气。
```

## 语义细节

- 命令只在 prompt 开始前识别。
- 正文中只能渲染变量，不能执行 slash 命令；需要调用定义时先用 `/let name /call ...` 绑定，再用 `{{name}}` 渲染。
- `/context #Heading` 只能写在 Markdown task header 中；它按 heading 标题匹配普通文档 section，并把该 section 内容追加到当前 task context。
- `/doc` 只影响普通 Markdown 上下文，不影响当前 task prompt 自身；`/doc` 的行内文本或 fenced block 不进入默认上下文，也不会被 `/context` 展开。
- 固定次数 `/for` 只绑定小写 `n`，并且从 `0` 开始计数；不会生成大写 `N`。
- `/go` 会把后续 suffix 放入后台分支；推荐复杂控制流换行写，例如 `/for ...` 下一行 `/go reviewer`。
- `/wait name` 只等待指定池此前提交的任务。
- `/wait` 带 prompt 时是 wait coordinator task：prompt 会带上等待范围、待等待后台任务列表、当前可见 report/status、日志路径和取消能力说明，用来观察、汇总和报告后台任务结果。
- 没有显式 `/wait` 就不等待剩余后台任务；`atm check` 和 `atm plan` 会用 warning 提示未等待的后台任务，但不把它当作 error。`atm run` 在没有前台任务后退出，未汇合的后台 block 可能保持 `running`。
- `/output` 只能写在 task header 中，并且一个任务块最多一个；prompt 正文里的 `/output` 是错误。
- `/db ignore` 不带参数时不能和同一任务块的 `/db use` 或 `/db access` 混用。
- `/db access` 只能降低权限，不能超过声明时的最大 `access`。
- `/output` 和结构化 `/return` 的 schema fence 必须使用反引号，不能使用波浪线。
- `/return` 支持普通文本、bash、多行文本和结构化 JSON；definition 必须显式写 `/return`，不要依赖 `/output` fallback。`/return` 只允许写在 `/def` body 内。多行文本和多行 bash 也必须使用反引号 fenced 参数。`/def` body 里的 Markdown heading 是 prompt 文本，不是 definition 边界；`atm check` 会 warning。
- `/def` 按 Markdown 词法作用域可见：根部 definition 对全文后续任务可见，heading 内 definition 只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 调用，也不能在声明前调用。
- `/import` 只导入 definition，并按导入声明所在的 Markdown scope 对后续任务可见；同级 heading 不继承导入结果，也不会导入被导入文件里的 DB/skill/MCP 资源声明。
