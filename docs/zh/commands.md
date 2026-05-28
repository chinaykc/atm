# 命令参考

[English](../en/commands.md)

ATM 是 **Agent Task Markdown**：一种基于 Markdown 的 Agent 任务调度 DSL（领域专用语言）。普通 Markdown 承载上下文和说明，`/task` 和斜杠控制命令定义可执行任务。

大多数 atm 文件只需要普通提示词。只有在需要继续会话、传递工具参数、重复执行、按条件停止或后台运行时，才加命令行。

命令必须写在任务块开头，并位于提示词正文之前。

## 任务边界

ATM 对 `.txt` 和 Markdown 文件使用同一套 Markdown-native 任务边界模型。

纯文本按文档/背景上下文解析。可执行任务从 `/task`、任务启动控制命令，或带后续 prompt 的 task header 命令开始。要在纯文本文件中执行普通 prompt，需要显式写 `/task`：

```txt
/task
发送给工具的提示词正文。

/task
下一个任务块。
```

空行会保留在当前 prompt 中。只有当空行后的下一个非注释根层行是任务启动/control 命令时，它才辅助形成新任务边界。整行注释会被忽略：

```txt
# 被忽略的行
<!-- 被忽略的 HTML 注释块 -->
[//]: # (被忽略的引用式注释)
[comment]: <> (被忽略的引用式注释)
---
===
```

只由三个或更多 `-` 或 `=` 组成的独立 Markdown 分隔线也会被忽略。不支持行内注释。提示词行中间出现的 `#`、`---` 或 `<!-- ... -->` 会保留为提示词正文。

在 Markdown 任务文档中，heading 只定义上下文和作用域，不会自己启动任务。任务可以由 `/task`、任务启动控制命令，或带后续 prompt 的 task header 命令开始：

```md
# 背景

这里是普通 Markdown 文档，不会执行。

## Verify

/for 2
运行 go test ./...，并修复失败。

/task
运行 go vet ./...，修复可操作的问题。

## Discuss

/task
这个任务会看到 Discuss heading 作为上下文。

## 备注

这里又是普通文档。
```

任务前的普通 Markdown 会保留在文件中，并作为 section context 传给任务。任务持续到空行后的下一个根层任务启动/控制命令、同级或更高级 heading、report block 或文档结束。只有 `/let` 等 header 命令且没有 prompt 的块是当前 Markdown scope 内的声明；同样的 `/let` 后面跟 prompt 时，它就是当前任务的 header。更深层 heading 仍属于当前任务 prompt。

如果更深层 heading 内出现自己的任务启动命令，ATM 会把该命令解析为 child-heading task。child-heading task 会继承父任务 root prompt，以及自己所在 heading 路径上的普通 Markdown；不会继承 sibling child-heading 的正文或任务：

```md
# Review

/task
Review backend.

### Scope 1

API and migrations.

/for 2
Fix tests {{n}}.

### Scope 2

Docs.

/task
Fix docs.
```

Scope 1 任务会看到 `Review backend.`、`Scope 1` 和 `API and migrations.`。Scope 2 任务会看到 `Review backend.`、`Scope 2` 和 `Docs.`。Child-heading task 也会继承父 task header 中的 `/let` 绑定，包括 lazy provider；child section 中的同名 `/let` 可以遮蔽这个继承值。ATM 会先执行尚未完成的 child-heading task，再执行父任务。父任务真正运行时，prompt 会在 `Completed child task reports` 小节中包含已完成子任务的人类可见 `> [!ATM]` report 摘要。被 skipped 的子任务不会嵌入父任务 prompt。

可以用下面的命令规范化格式：

```sh
atm format todo.txt
```

只有提示词正文开始前的斜杠命令会被识别。正文里的斜杠命令会报错，除非它在空行之后作为根层 sibling task 开始。

Task header 命令可以写在同一行，也可以拆成多行。`/task`、`/fork`、`/args`、`/output`、`/db use`、`/skill use`、`/webhook use` 这类声明命令会合并到当前任务配置中；`/cd`、`/bash`、`/call`、`/webhook name`、`/for`、`/go`、`/wait` 这类流程命令按出现顺序执行。

未引用且本身是命令的 token 会启动下一个 header 命令；希望把它作为数据传入时需要加引号，例如 `/bash printf "%s" "/task"`。带 fenced schema 的 `/output` 和带 fenced payload 的 `/webhook` 必须单独占一条 header 行，该行只包含这个 payload 命令及其参数。

## 命令

### `/task [name]`

开始一个 prompt 任务。任务名会在任务成功后记录所选 runner 的 session id，后续任务可以继续这个精确会话。

```txt
/task review
审查支付 API，并修复可操作的问题。
```

任务名使用和变量相同的标识符形状：字母、数字、`_` 和 `-`，首字符不能是数字。

### `/resume name`

用 `/task name` 记录的 session 通过 runner 的 resume 模式运行提示词。

```txt
/task review
审查支付 API，并修复可操作的问题。

/resume review
继续 review 任务会话，并验证修复。
```

ATM 会从 Codex 和 Claude 的结构化输出中读取 session id，并写入本次 run 的状态。`/resume name` 要求同一托管 run 中已有完成的具名任务，并且所选工具适配器一致。

对 Codex 来说，这等价于：

```sh
codex exec --json resume <thread-id> -
```

对 Claude Code 来说，这等价于：

```sh
claude --resume <session-id> -p "提示词"
```

### `/fork name`

把 `/task name` 记录的 session 分叉，并从该 session 历史运行当前 prompt。

```txt
/task base
分析支付 API，并列出最稳妥的实现计划。

/fork base
尝试最快实现路径。

/task option_a /fork base
尝试最小实现路径。

/task option_b /fork base
尝试兼容优先的实现路径。
```

匿名 `/fork name` 会执行分支，但不会创建可复用的任务 session 名。`/task new_name /fork name` 会把新分叉出的 session 记录到 `new_name` 下。Claude Code 使用 `--resume <session-id> --fork-session`。对于 Codex，ATM 在本地生成包含父会话历史的新 rollout，再以 `codex exec --json resume <新 thread id> -` 执行当前提示词。Codex fork 快照只复制完整 rollout 记录；如果父会话历史停在执行中的 turn，ATM 会在 fork 快照中把该 turn 标记为 interrupted。

### `/args ...`

为当前任务块内的所有流程向所选工具追加 CLI 参数。

```txt
/args --yolo
用额外参数运行所选工具。
```

`/args` 也可以和 `/for` 写在同一行：

```txt
/args --yolo /for 3
带额外参数执行三次。
```

### `/cd path`

准备并进入当前任务工作区。目录不存在时，ATM 会按 `mkdir -p` 语义先创建缺失目录，再执行后续命令或 prompt。

```txt
/cd services/payments
在这个工作区里实现支付服务。
```

如果任务必须在已有目录中运行，用 `--must-exist`：

```txt
/cd --must-exist backend
运行后端检查。
```

路径会先用当前任务变量渲染，再相对当前任务工作区或原始 atm 文件所在目录解析。解析后的路径必须留在原始 atm 文件目录内。`/cd` 会影响 Codex/Claude、`/bash`、`/let ... /bash`、自然语言 `until` 检查和本地表达式文件函数；不会改变 ATM 写状态块、输出产物或 DB 文件的位置。

### `/let name value`

定义模板变量。只包含 `/let` 命令的独立块会为当前 Markdown scope 的后续任务块定义变量。文档根部的 `/let` 对整篇文档的后续任务可见；heading 下的 `/let` 只对该 heading 的后续任务和子 heading 可见。同级 heading 不继承，`/let` 也不会被前置提升。未使用的 `/let` 是允许的，但临时绕过任务时使用注释更清晰。

写在 task header 里的 `/let` 只对当前 task block 及其 child-heading task 可见。child task 继承到的 lazy provider 会在 child task invocation 内解析并缓存，不和 parent 共享缓存。child section 中的同名 `/let` 会遮蔽父 task header 的值。

```txt
/let suite go test ./...

/for 3 until 测试通过
运行 {{suite}} 并修复失败。
```

在任务块内部，`/let` 定义局部变量。Prompt 正文中用 `{{name}}` 渲染变量：

```txt
/let context 先阅读 README.md。
带着这段上下文审查安装说明：
{{context}}
```

在 Markdown 任务文档中，`/context #Heading` 是 task header 命令，用于把远处 section 的普通文档加入当前任务上下文；它可以和其他 task 命令写在同一 header 行里，并且不会渲染名为 `context` 的变量。

`/let` 也可以捕获 bash 的 stdout。`/let name /bash ...` 和 `/let name /call ...` 是懒 provider：ATM 在 task header 中记录它们，`atm check --plan` 和 `atm check` 不会执行；只有变量真正被渲染或被表达式读取时才执行。同一次任务调用内第一次读取后会缓存结果，所以重复写 `{{name}}` 只复用一次结果。需要多次执行定义或 shell 命令时，使用多个绑定，或者直接写 `/call`、`/bash` 命令。

因为 lazy provider 是渲染期依赖，`atm check` 和静态 `atm check --plan` 会把它们报告为 warning，而不是执行它们。Lazy bash provider 会标记为潜在副作用点；lazy call provider 会标记为 definition 依赖。计划 flow 中分别显示为 `LazyBash(name)` 或 `LazyCall(def -> name)`。

bash 值渲染前会移除 stdout 末尾换行：

```txt
/let branch /bash git branch --show-current
总结 {{branch}} 的发布风险。
```

### 模板渲染

ATM 使用 Go `text/template` 渲染 prompt、`/bash` 脚本、`until` 条件、`/args` 参数值和 `/cd` 路径。

已有变量占位符继续支持：

```txt
第 {{n}} 次审查 {{file}}。
```

这个旧写法等价于：

```gotemplate
第 {{var "n"}} 次审查 {{var "file"}}。
```

模板数据会把当前变量作为顶层 key 暴露（变量名必须是合法 Go template 标识符），同时也提供 `.Vars` 进行 map 访问：

```gotemplate
{{if .n}}第 {{.n}} 次{{end}}
{{index .Vars "path"}}
{{var "path"}}
{{has "path"}}
```

如果变量名包含 `-`，请使用 `{{var "name"}}` 或 `{{index .Vars "name"}}`。未知的旧占位符（如 `{{future}}`）会保留为原文；非法 Go template 语法会让任务失败。

### 系统提供的模板值

ATM 在不同渲染上下文中提供不同的模板值。

| 上下文 | 可用值 |
| --- | --- |
| 普通 prompt、`/bash`、`/args`、`/cd`、`until`、`/return` 和 `/output` schema | 来自 `/let`、读取后的懒 `/let ... /bash` 与 `/let ... /call`、`/for` 循环变量和 definition 参数的用户变量。 |
| 仅 `/return` | 当前 definition 调用中的 `{{agent.message}}`、`{{agent.last_message}}`、`{{agent.messages}}` 和 `{{agent.messages_json}}`。 |
| 后台分支里的 `/output` 文件名 | `{{agent_index}}`、`{{agent}}` 和 `{{agent_label}}`，以及普通变量。 |

`agent.last_message` 不是全局 prompt 变量。它只在渲染 `/return` 时存在，因为此时 ATM 已经执行完 definition body，并收集到了最近的 assistant 消息。想在后续 prompt 中使用 agent 消息，需要先 return 并绑定：

```txt
/let note /call reviewer api
使用这条审查消息：
{{note}}
```

### `/output [file]`

把当前任务结果保存到本次运行 output 目录。`/output` 是 task header 命令：必须写在 prompt 正文开始前，位于它要修饰的控制/header 命令之后。一个任务块里最多只能出现一次 `/output`；在 prompt 正文中写 `/output` 是错误。

不写 fenced schema block 时，`/output` 会把最近一条 assistant 消息保存为文本：

```txt
/output release-note

写一份发布经理可以直接转发的风险说明。
```

紧跟 fenced schema block 时，`/output` 会要求当前任务通过临时结构化工具产出结构化 JSON。同行可选文件名用于指定 ATM 保存 JSON 的文件名；`name` 和 `name.json` 都会保存为 `name.json`。如果不写，ATM 会生成一个包含时间的 JSON 文件名，并在任务日志中报告路径。

ATM 按 Markdown 的 fence 长度规则识别，所以可以用 ```` 包裹内部包含 ``` 的 schema 文本。Schema fence 必须使用反引号，不能使用波浪线。ATM 支持普通 fence、`schema`、`json`、`yaml` 和 `yml`：

````txt
/output summary.json

判断发布是否准备好。
```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["reason"],
  "properties": {
    "reason": {"type": "string", "description": "描述详细原因"}
  }
}
```
````

YAML fence 使用同样的 JSON Schema 结构，只是写成 YAML：

````txt
/output

报告当前天气。
```yaml
type: object
required:
  - weather
properties:
  weather:
    type: string
    description: 天气状态
```
````

普通 fence 或 `schema` fence 也可以使用紧凑字段列表。每个非空行是 `name:type:description`；`type` 可省略，默认是 `string`，所以 `weather:天气状态` 和 `weather::天气状态` 都有效：

````txt
/output result

返回发布门禁结果。
```
reason:string:描述详细原因
weather:天气状态
passed:boolean:发布门禁是否通过
```
````

对 Codex 和 Claude，ATM 会挂载一个临时任务工具服务，并暴露 `atm_report_output` 工具。该工具的 input schema 由 `/output` 的 schema block 生成，所以 agent 需要通过调用这个工具提交结构化数据。如果工具没有被调用，任务会失败，不会被标记为 done。

文件名会用和 prompt 相同的模板上下文渲染。循环变量如 `{{n}}`、`{{area}}`、`{{dir}}`、`{{file}}` 都可用。后台分支还会暴露 `{{agent_index}}`、`{{agent}}`（文件名安全）和 `{{agent_label}}`（人类可读）。当 `/output` 在 `/go` 分支中运行时，ATM 会自动给文件名追加分支后缀，避免多个 agent 覆盖同一个文件。

### `/db`

声明轻量任务数据库，供 agent 作为记忆或黑板使用。数据库存储 `map[string][]string`，并通过 任务工具服务 暴露给 Codex 和 Claude。`/db new` 是 scoped 声明块，不会运行 agent：

```txt
/db new decisions scope:global persist:project access:write
用于持久记录发布决策。Key 使用 decisions/<topic>。
```

声明语法：

```txt
/db new name [scope:local|global] [persist:run|project] [access:read|append|write|admin]
usage description
```

默认值是 `scope:global`、`persist:run` 和 `access:admin`。

| 选项 | 含义 |
| --- | --- |
| `scope:global` | 在该声明可见范围内，后续任务块默认挂载。 |
| `scope:local` | 只声明，不默认挂载；可见范围内的任务块必须写 `/db use name`。 |
| `persist:run` | 存到本次 run 的 output 目录，例如 `.atm/20260521103000/db/name.json`。 |
| `persist:project` | 存到项目目录 `.atm/db/name.json`，后续 run 可继续使用。 |
| `access:read` | 最大权限为只读。 |
| `access:append` | 只读加追加写。 |
| `access:write` | 只读、追加、替换 key，但不能删除。 |
| `access:admin` | 完整读写删除权限。 |

`/db new ...` 后面的正文是 usage description。ATM 会把它传给 DB 任务工具服务，agent 调用 `atm_db_list` 时也能看到。

`/db new` 按 Markdown 词法作用域可见：文档根部声明对全文后续任务可见；heading 下的声明只对该 heading 的后续任务和子 heading 可见。同级 heading 不继承，声明也不会被前置提升。`scope:global` 和 `scope:local` 只决定在这个可见声明集合内是否默认挂载。

任务块可以调整自己的 DB 视图：

```txt
/db use scratch access:append
/db access decisions read
/db ignore obsolete
审查并更新共享记录。
```

任务级语法：

| 命令 | 含义 |
| --- | --- |
| `/db use name name2... [access:level]` | 挂载 local DB，或覆盖当前任务可见 DB 的权限。 |
| `/db access name name2... level` | 设置命名可见 DB 的任务级权限。 |
| `/db access * level` | 设置当前任务所有可见 DB 的任务级权限。 |
| `/db ignore name name2...` | 对当前任务隐藏指定 DB。 |
| `/db ignore` | 对当前任务隐藏所有 DB。 |

任务级权限只能在声明的最大权限内降低或选择，不能提升。`/db ignore` 不带名字时，不能和同一任务块的 `/db use` 或 `/db access` 混用。

权限决定 agent 可用的 DB 任务工具能力：

| 权限 | 任务工具能力 |
| --- | --- |
| `read` | `atm_db_list`、`atm_db_get`、`atm_db_scan` |
| `append` | `read` 加 `atm_db_append` |
| `write` | `append` 加 `atm_db_set` |
| `admin` | `write` 加 `atm_db_delete` |

任务工具：

| 工具 | 参数 | 结果 |
| --- | --- | --- |
| `atm_db_list` | `{}` | 可见 DB、usage、access 和 capabilities。 |
| `atm_db_get` | `{"db":"name","key":"k"}` | 一个 key 及其字符串数组。 |
| `atm_db_scan` | `{"db":"name","pattern":"findings/**","limit":100,"cursor":""}` | 排序后的匹配 key，支持分页。 |
| `atm_db_append` | `{"db":"name","key":"k","values":["v"]}` | 追加后的 key values。 |
| `atm_db_set` | `{"db":"name","key":"k","values":["v"]}` | 替换后的 key values。 |
| `atm_db_delete` | `{"db":"name","key":"k","values":["v"]}` 或不传 `values` | 剩余 values，或删除整个 key。 |

`scan` 支持 glob。`*` 匹配一个 `/` 分段内的内容，`**` 可以跨 `/` 分段匹配。写操作用 DB lock 文件串行化，并通过临时文件 rename 原子写入，所以并发 `append` 不会互相覆盖。

自然语言 `until` 和 `/if` 检查会收到只读 DB 任务工具服务，即使任务本身有写权限，检查 agent 也只能读取。

### `/skill`

先声明本地 skill，再在任务块中显式启用：

````txt
/skill new reviewer from .atm/skills/reviewer

/cd work/release
/skill use reviewer
准备发布说明。
````

`/skill use` 会把选中的 skill 目录复制到当前 `/cd` 工作区中，并使用所选 adapter 期望的布局：Codex 使用 `.agents/skills/<name>`，Claude 使用 `.claude/skills/<name>`。源目录必须已经存在并包含 `SKILL.md`；目标 adapter 目录由 ATM 自动创建。

`/skill new` 是 scoped 声明块。文档根部声明对全文后续任务可见；heading 下的声明只对该 heading 的后续任务和子 heading 可见。同级 heading 不继承，声明也不会被前置提升。`/skill use` 必须引用当前任务可见的声明；例外是 `/skill use` 可以直接给 path-like 值。


### `/def`、`/call` 和 `/return`

用 `/def` 定义可复用任务模板，用 `/call` 调用。定义本身不会执行，只会在调用点执行。

`/def` 是带显式 `/return` 边界的 definition block。Definition body 可以包含普通 task block，这些内部任务不会进入外层执行计划：

```md
/def whereami

根据仓库上下文或可用环境判断当前城市。

/return {{agent.last_message}}

/def release_reviews area

/go reviewer
审查 {{area}} 的实现风险。

/go reviewer
审查 {{area}} 的文档风险。

/wait reviewer

/return {{area}} 的审查已完成。
```

Definition 必须包含且只包含一个 `/return`，并且 `/return` 必须是 definition 的最后一个 block。返回值由 `/return` 明确产生。`/return` 写在 `/def` body 之外是错误；普通任务如果只是想提到这个词，应写成普通 Markdown 文本。

`/def` body 内的 Markdown heading 会被当作 prompt 文本，而不是 definition 边界。因为这种写法容易和外层文档结构混淆，`atm check` 会给 warning。

Definition 使用 Markdown 词法作用域。文档根部的 `/def` 对整篇文档的后续任务可见；heading 下的 `/def` 只对该 heading 的后续任务和子 heading 可见，同级 heading 不会继承。需要跨 sibling section 复用时，把 definition 放到共同父级的任务之前，或放在文档根部。Definition 不会前置提升：任务不能调用文档后面才出现的 definition。

调用参数按位置绑定到定义参数：

```txt
/call release_reviews checkout
```

当 `/call` 是独立任务/header 命令时，ATM 会执行定义；除非显式赋值，否则返回值会被丢弃。Prompt 正文不执行 slash 命令；如果要把定义返回值渲染进 prompt，应在 prompt 前用 `/let name /call ...` 绑定：

```txt
/let city /call whereami
查询 {{city}} 的天气。
```

定义通过 `/return` 返回值。`/return` 支持单行、bash、多行和结构化返回。多行文本和多行 bash 必须使用反引号 fenced 参数，不支持 `/return` 后直接跟裸多行文本：

````txt
/return {{city}}
/return /bash pwd

/return
```
城市：{{city}}
最近消息：{{agent.last_message}}
```

/return /bash
```
git branch --show-current
```
````

结构化 `/return` 使用和结构化 `/output` 相同的 结构化输出机制，但 JSON 主要作为调用返回值使用。为了和文本返回区分，结构化返回必须使用明确的 `json`、`yaml`、`yml` 或 `schema` fence；`/return` 后的空 fence 表示普通文本：

````txt
判断发布门禁是否通过。

/return
```json
{
  "type": "object",
  "required": ["passed", "reason"],
  "properties": {
    "passed": {"type": "boolean"},
    "reason": {"type": "string"}
  }
}
```
````

Definition 必须显式写 `/return`。如果需要返回结构化 JSON，应写结构化 `/return`；不要依赖结构化 `/output` 作为 fallback 返回值。`/output` 更适合保存文件产物，`/return` 更适合把结果交给调用方继续渲染。

`/return` 模板可以读取当前定义调用里的最近 assistant 消息：

- `{{agent.last_message}}`：最近一条 assistant 消息文本。
- `{{agent.message}}`：`{{agent.last_message}}` 的别名。
- `{{agent.messages}}`：最近 N 条 assistant 消息拼成的文本。
- `{{agent.messages_json}}`：最近 N 条 assistant 消息的 JSON。

N 使用本次运行的 `-messages`，默认是 `1`。

定义里可以包含 `/pool`、`/go` 和 `/wait`。定义内部声明的 pool 只在本次调用中局部生效；无论局部池还是具名池，都共享全局 `-jobs` 并发上限。Definition 不会在 `/return` 前隐式插入 `/wait`；返回值依赖后台分支时必须显式写 `/wait`。

用 `/import` 从其他文件导入定义：

```txt
/import workflows/location.todo.md
/import weather from workflows/weather.todo.md
```


### `/bash script`

在提示词执行前运行 bash。命令会继承 `ATM_TODO_FILE`，并在当前 `/cd` 任务工作区中运行；退出码非 0 会让当前任务失败。

```txt
/bash go test ./...
总结测试结果，必要时修复失败项。
```

较长脚本可以使用 heredoc：

```txt
/let changes /bash <<EOF
git status --short
git diff --stat
EOF

/bash <<'SH'
go test ./...
go build ./...
SH
总结 {{changes}} 和验证结果。
```

### `/for`

通过一个或多个流程运行提示词。同一行上的命令会组合成同一个流程。

固定次数循环：

```txt
/for 3
第 {{n}} 次审查最终 diff。
```

固定次数循环绑定小写 `n`。取值从 `0` 开始，所以 `/for 3` 会以 `n = 0`、`n = 1`、`n = 2` 执行。

带完成条件的有限重试：

```txt
/for 3 until 测试通过
修复失败的测试。
```

每次执行后，`atm` 会让所选工具检查 `until` 条件。对 Codex 和 Claude，`atm` 会挂载一个临时任务工具服务，并暴露名为 `atm_report_check` 的工具，agent 必须通过该工具提交结果。

使用本地表达式的确定性本地重试：

```txt
/for 5 until(exist("result.json") && len(open("result.json")) > 0)
生成 result.json。
```

当 `until` 后面是括号表达式时，ATM 会在本地用表达式判断，而不是让 agent 判断自然语言。表达式必须返回 `bool`。表达式判断只读、无副作用，支持 `exist(path)`、`open(path)`、`outputDir(path)`、`json(text)`、`yaml(text)`、`toml(text)`、`len(value)`、`range(...)`、`files([root])`、`dirs([root])`、`walkFiles([root])` 和 `walkDirs([root])`。相对路径按当前 task workdir 解析；`outputDir(path)` 即使在 `/cd` 后也指向本次运行 output 目录。

不带次数的无界表达式重试写法如下：

```txt
/for until(exist("result.json") && json(open("result.json")).passed)
持续工作，直到 result.json 中 passed=true。
```

这个形式会一直运行，直到表达式为 true 或进程被中断。它和自然语言 `until` 明确分开：`/for until 测试通过` 是非法写法。本地表达式函数中的文件路径必须加引号，例如 `open("result.json")`；`open(result.json)` 会被本地表达式理解成变量访问。

### `/if` 和 `/else`

选择一个任务块执行。`/if` 是任务块级控制命令。`/else` 可以省略；如果写 `/else`，它必须紧跟 then 任务块。`/if` 和 `/else` 不嵌套；复杂分支体用 `/def` 封装。

```txt
/if (exist("gate.json") && json(open("gate.json")).passed)
继续发布。

/else
根据 gate.json 写发布阻塞说明。
```

`/if(...)` 和 `/if (...)` 使用本地表达式，和 `until(...)` 对齐。`/if 自然语言条件` 使用所选工具的结构化 结构化检查，和自然语言 `until` 对齐：

```txt
/if 发布门禁已经打开
继续发布。
```

较长的自然语言 `/if` 和 `until` 条件可以紧跟 fenced text 参数：

````txt
/if
```
发布门禁已打开
并且检查都通过
```
继续发布。

/for 3 until
```
测试通过
并且 lint 通过
```
修复失败项。
````

如果条件为 true，ATM 执行 `/if` 分支，并把 `/else` 分支标记为 skipped。如果条件为 false，ATM 把 `/if` 分支标记为 skipped，并在存在 `/else` 时执行 `/else` 分支。没有 `/else` 时，false 的行内 `/if` 块只会被标记为 skipped。Skipped 块使用生成状态块：

```txt
> [!ATM]
> status: skipped
> time: 2026-05-22 10:30
> reason: if condition evaluated false
```

紧跟对应 `/if` 分支的空 `/else` 合法，表示 false 分支 no-op。但它通常没有必要；`atm check` 会给 warning，提示可以直接省略。

`/if` 也可以出现在控制链中。命令顺序直接决定控制流：`/for /if /go`、`/for /go /if` 和 `/if /go /for` 是三个不同结构。多行控制头等价于把同一组命令写在一行；复杂时推荐换行写：

```txt
/for 10
/if(n % 2 == 0)
/go

审查偶数分片 {{n}}。

/wait
```

这个例子会先过滤循环项，再分发后台工作。紧跟的 `/else` block 仍然可以为同一个 conditional task 提供不同 prompt body：

```txt
/for 3 /if(n == 1)
审查被选中的分片 {{n}}。

/else
说明为什么跳过分片 {{n}}。
```

如果某个分支需要多个任务或嵌套选择，把流程写成 `/def`，再在分支中 `/call`。

目录和文件循环：

```txt
/for dir in dirs()
审查 {{dir}}。

/for file in files()
审查 {{file}}。
```

显式列表循环：

```txt
/for area in [api docs tests]
审查 {{area}}。
```

动态表达式列表循环：

```txt
/for plan in(/call plan_shards)
{{plan}}
```

数字区间表达式循环：

```txt
/for shard in range(1, 4)
审查分片 {{shard}}。
```

`in expr`、`in(expr)` 和 `in (expr)` 都可以。表达式会在运行期求值。它可以是返回 list 的本地表达式，也可以是返回 list 的带括号 `/call name`，例如 `in(/call plan_shards)`。如果 `/call` 返回带 `plans` 数组的对象，ATM 会展开这个数组。标量项用 `{{plan}}` 渲染；对象项可以用 `{{area.name}}` 这类字段访问。需要整数区间时可以用 Python 风格 `range(stop)`、`range(start, stop)` 或 `range(start, stop, step)`；`step` 不能是 `0`。用 `files()` 和 `dirs()` 枚举当前 task workdir 下一层条目，用 `walkFiles()` 和 `walkDirs()` 递归枚举；它们都支持可选 root，例如 `walkFiles("src")` 或 `walkFiles(outputDir("reports"))`。`.git`、`node_modules`、`vendor`、`dist`、`build` 等生成或依赖目录会被跳过。文件和目录枚举使用表达式形式；动态序列为空时，ATM 会输出运行时 warning 并跳过循环体。

循环变量可通过 `{{n}}`、`{{dir}}`、`{{file}}`、`{{area}}` 或动态对象项字段渲染。固定次数 `/for number`，例如 `/for 10`，继续支持，并且只绑定小写 `n`。

`/for` 和 `/go` 写在同一行时，命令顺序有意义：

```txt
/for 3 /go
审查分片 {{n}}。
```

这会为每个循环项启动一个后台分支。反过来写时，循环会留在同一个后台任务内部：

```txt
/go /for 3
审查分片 {{n}}。
```

两种形式仍然只会为这个 todo block 写一个生成的 `> [!ATM]` 结果块。对于 `/for ... /go`，每个循环项会标注成一个独立 agent 分支，例如 `n=0` 或 `area=api`；`-messages N` 会为每个分支保留最近 N 条结构化 assistant 消息，所以默认 `-messages 1` 也能看到每个并行分支的一条消息。

### `/pool name max [buffer]`

声明一个具名后台工作池。`/pool` 是 scoped 声明块：文档根部的 `/pool` 对全文后续任务可见；heading 下的 `/pool` 只对该 heading 的后续任务和子 heading 可见。同级 heading 不继承，`/pool` 也不会被前置提升。它只配置后续 `/go poolName` 的调度，不会启动 agent。

```txt
/pool tester 5
```

第二个参数表示这个具名池最多同时运行多少个后台分支。默认情况下，等待队列不设上限，提交更多分支不会阻塞流程。第三个参数可以限制等待队列长度：

```txt
/pool tester 5 10
```

具名池同时也受全局后台并发上限约束。全局上限默认是 `NumCPU`，可通过 `atm run -jobs N` 修改。

### `/go`

在后台启动任务，并继续扫描 atm 文件。

```txt
/go
审查文档。
```

使用 `/go poolName` 可以把后台分支提交到具名池：

```txt
/pool tester 2

/for area in [api docs tests] /go tester
审查 {{area}}。
```

同一个未变化的任务块在后台待收集期间不会被重复启动。

`/go` 不会隐式汇合。文档里如果有后台任务但后面没有匹配的 `/wait`，`atm check` 会给 warning，但仍接受该文件。`atm run` 在没有前台任务后会退出；未匹配的后台 block 可能保持 `running` 状态，并在日志中报告为没有 `/wait` 的后台工作。这适合有意启动监控类后台 agent 的场景；普通 fan-out 工作流建议显式写 `/wait`。

### `/wait`

等待之前所有 `/go` 后台任务。

独立等待块：

```txt
/wait
```

没有 prompt 的 `/wait` 是纯 join。有 prompt 的 `/wait` 会先等待匹配的后台任务完成，再执行这个 prompt，并附加少量 wait 结果上下文：等待范围、匹配后台任务 id、block、pool、最终状态、日志路径、错误和可见 report。

```txt
/wait
观察后台审查，汇总失败和后续工作，并说明是否需要人工介入。
```

使用 `/wait poolName` 可以只等待该具名池中此前提交的任务：

```txt
/wait tester
汇总 tester 结果，其他池中的任务可以继续运行。
```

`atm check` 会在后台任务没有后续匹配 `/wait` 时给出 warning；工作流依赖后台结果时必须写 `/wait`。

## Check Plan


```sh
atm check --plan todo.txt
atm check --plan todo.txt --preview
```

需要在计划里看到 provider 实际值时，显式使用 `--preview`。Preview 模式可能执行 lazy `/let name /bash ...` provider，也可能执行不需要运行 agent 就能返回的纯 lazy `/let name /call ...` provider，并把结果写入文本或 JSON 输出；它仍不运行 agent、不写主文档 report、不更新 `.atm/state.json`。如果某个 lazy call provider 的 definition 需要运行期执行，preview 会把它列为未执行。

使用 `-html FILE` 保存适合浏览器查看的流程图，或使用 `-open` 生成流程图并用默认浏览器打开。HTML 视图会展示 parent/child task 关系、显式 `/wait` 汇合和未汇合后台任务；它不会展示隐式最终等待。

```sh
atm check --plan todo.txt -html plan.html
atm check --plan todo.txt -open
```

如果需要让其他工具消费 IR plan，可以加 `-json`：

```sh
atm check --plan todo.txt -json
```

JSON 格式作为面向工具的契约：

- `source`、`document`、`globals`、`tasks`、`tasks[].block`、`tasks[].prompt` 和 `tasks[].flow` 是稳定字段。`document.title` 是存在时的第一个一级标题；`document.sections` 是嵌套 Markdown 章节树，包含标题行号、层级、标题文本和 path。
- `tasks[].context` 汇总会 prepend 到 task prompt 的默认 Markdown 上下文。它只报告行数、字符数和预览，不重复输出完整上下文正文。
- `tasks[].decision` 给出该 task 的计划动作和原因。它区分前台执行、`/go` 后台 dispatch、纯 `/wait` join、`/wait` 后执行 prompt 的任务、条件执行、parent/child 依赖，以及分支 skipped/no-op 原因。
- `loops` 汇总每个 `/for` 节点，包含 task/block、变量名、可静态确定的 values/count、动态 source expression 或 call、`until` 条件和运行参数。静态循环会展示展开值；动态循环只展示来源，不执行它。
- `conditions` 列出条件 task 的条件类型/文本和静态分支结果：true 执行 then，并在有 else 时跳过 else；false 跳过 then，并在有 else 时执行 else，否则 no-op。
- `tasks[].variables` 列出 prompt 或 output 配置中出现的 `{{name}}` 引用。每项会标记来源为 `global-let`、`global-lazy-bash`、`task-let`、`task-lazy-bash`、`task-lazy-call`、`loop` 或 `unresolved`；能在不执行 provider 的情况下确定的字面值会直接给出。
- `tasks[].runtime` 汇总运行环境输入：`resume`、runner `args`、来自 `/cd` 的 `workdirs`、agent 前置 `bash` 命令和 lazy provider。同样信息仍保留在 `tasks[].flow`，`runtime` 是更方便的工具视图。
- `async.background` 列出启动后台工作的 task，`async.joins` 列出显式 `/wait` 汇合，`async.unjoined` 列出后续没有匹配 `/wait` 的后台工作；`fanout` 描述每个后台 dispatch 背后的循环或单分支来源。
- `tasks[].flow.kind` 以及嵌套的 `children`/`elseChildren` 描述控制顺序；未来小版本可能增加新的 flow kind。
- 现有 flow 字段保持追加兼容。消费方应忽略未知字段。
- block 编号从 1 开始，指向当前源码文件快照。

## 结果块

结果块是托管结果文档中的生成状态。直接 `run` 退出时会恢复源文件不变。

### Done

```txt
任务提示词
<!-- atm:report v=2 id=task-prompt-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md status=done -->
> [!ATM]
> status: done
> started: 2026-05-08 14:30
> finished: 2026-05-08 14:32
> duration: 2m
> runs: 1x
> id: task-prompt-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md
>
> messages:
> - assistant (codex):
>   已修复解析器测试。
<!-- /atm:report -->
```

字段包括状态、开始时间、结束时间、耗时、总执行次数、稳定 report id、任务 source hash、实际发送给 agent 的 rendered prompt hash、detail report 路径和最近 assistant 消息。Codex 会以结构化 JSON events 运行，Claude Code 会以 stream JSON 运行，因此 `atm` 不需要从普通终端文本里猜测 assistant 消息。

结果块由 `<!-- atm:report ... -->` 和 `<!-- /atm:report -->` 包住；ATM 替换时以这个边界为准。中间的 `> [!ATM]` 引用块是给人和 agent 看的可见摘要。`id` 是这个任务/report 的稳定身份；`source` 是写入结果块时根据任务源文本计算的 `sha256`；`rendered` 是变量和 lazy provider 解析后实际发送给 agent 的 prompt hash；`report` 指向 `~/.atm/runs/<run-id>/tasks/<task-id>/report.md` 下的详细 Markdown 报告。它们让 ATM 能在文档编辑后识别“这是哪个任务的报告”，而不是只依赖位置。如果同一文档中出现重复 `id`，`atm check` 和执行前解析都会报错，要求先修复重复身份再继续。

### Running

```txt
任务提示词
<!-- atm:report v=2 id=task-prompt-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md status=running -->
> [!ATM]
> status: running
> started: 2026-05-08 14:30
> step: 1
> step-runs: 1x
> total-runs: 1x
> id: task-prompt-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md
<!-- /atm:report -->
```

Running 结果块让中断或失败的循环从剩余工作继续，而不是从零开始。`atm` 只会在持有 block lease 时替换尾部生成的 `> [!ATM]` 块；如果任务正文被编辑，lease 会失效并重新扫描。`id`、`source`、`rendered` 和 `report` 会随 running/done/skipped 状态一起写入，用于后续去重和报告关联。任务完成后，ATM 会在该任务的运行产物目录写入 `report.md`；主文档只保留轻量摘要。

### Failed

```txt
任务提示词
<!-- atm:report v=2 id=task-prompt-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md status=failed -->
> [!ATM]
> status: failed
> started: 2026-05-08 14:30
> finished: 2026-05-08 14:31
> duration: 1m
> runs: 0x
> error: task 1 run failed: simulated failure
> id: task-prompt-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md
<!-- /atm:report -->
```

失败任务会在托管运行目录中写 `failed` 结果块、任务目录下的 `report.md` 详细报告和 `.atm/state.json` 状态。可以用 `atm resume <run-id>` 继续该运行目录，或修改保持不变的源文件后重新开始一次运行。

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 命令成功；对 `run` 表示本次计划任务已完成。 |
| `1` | 执行失败，例如 agent、bash、文件读写或工具适配器错误。 |
| `2` | CLI/DSL 输入校验失败，例如任务语法、表达式、重复 report id、未知 pool/db/skill 引用或 `check` 的 error 级诊断。 |
| `3` | 硬状态不一致，需要先检查或修复 `.atm/state.json`、主文档 report block 和任务详细报告的关系。 |
| `130` | 用户中断。POSIX 下其他终止信号按 `128 + signal` 返回。 |

`atm check` 发现审计产物不一致时通常输出 warning 并返回 `0`，因为这些问题需要人工确认但不一定阻止 DSL 编译。只有被实现判定为硬状态不一致的错误才使用退出码 `3`。

## 子命令

### `run`

运行待处理的提示词任务块。它也是默认命令，所以下面两个命令等价：

```sh
atm todo.txt
atm run todo.txt
```

直接执行必须传入一个或多个位置形式的 atm 文件。`atm run` 不会从当前目录自动发现 `todo.txt` 或 `todo.md`。

两种形式都可以搭配 `-tool`、`-codex` 和 `-claude` 使用。用 `-messages N` 可以设置每个生成结果块中每个执行分支保留最近几条结构化 assistant 消息；默认是 `1`。用 `-retries N` 可以重试限流、超时、网络错误和 5xx 响应等临时 agent 失败；默认是 `3`，设为 `0` 会关闭这类重试。用 `-jobs N` 可以设置所有池共享的全局后台分支并发上限；默认是 `NumCPU`。

多个位置文件参数会按顺序排队执行：

```sh
atm run todo.txt rollout.md followup.md
atm todo.txt rollout.md followup.md -jobs 4
```

直接执行会在 `~/.atm/runs/<run-id>/` 下创建托管运行目录。ATM 会把源文件和递归 `/import` 文件复制进去，执行期间用简短占位文件隐藏原路径，退出时恢复原文件不变。可用 `ATM_HOME` 改变基础目录。运行目录包含：

- `manifest.json`：run id、源路径、源/import 副本、状态和恢复命令。
- `sources/...`：执行前捕获的源文件和 import 文件副本。
- `work/...`：ATM 实际执行的工作副本，import 路径已改写到运行目录内。
- `result.todo.md`：最终 todo 文档快照，包含生成的 `> [!ATM]` 结果块。
- `tasks/<task-id>/report.md`：单个任务的详细 Markdown 报告。
- `tasks/<task-id>/logs/task-NNN-*.log`：该任务的人类可读 stdout/stderr 日志。
- `tasks/<task-id>/task-NNN-run-NNN-TOOL[-BRANCH].jsonl`：该次 agent 执行自身产生的原生结构化事件流。

工作副本旁还会维护 `.atm/state.json` 和短生命周期 `.atm/lock`。`state.json` 是按稳定 task/report id 组织的机器可恢复状态，记录 status、source/rendered prompt hash、plan hash、report 路径、日志路径和运行次数。`-output DIR` 只改变输出产物目录；源/import 备份、任务目录、manifest 和 `result.todo.md` 仍保存在托管运行目录。

如果任务运行期间对应 task block 被删除或改到 lease 失效，ATM 不会把完成结果写回主文档，但会继续写该任务的 `report.md`，在 `.atm/state.json` 里标记 `"orphan": true`，并在命令行输出 orphan report 提示。

`atm check` 会检查主文档 report block、`.atm/state.json` 和任务详细报告的明显不一致，并以 warning 报告缺失 detail report、state 中没有主文档对应项的 task id、主文档与 state 的 status/report 路径/source hash/rendered prompt hash 不一致，以及 orphan detail report。

如果多个输入文件共用同一个 `-output DIR`，`atm` 会在 `DIR` 下为每个文件创建一个带编号的输出子目录，避免原生事件流互相覆盖。

继续或恢复托管运行：

```sh
atm resume <run-id>
atm resume --last
atm resume --restore-source
atm resume <run-id> --restore-source
```

`atm resume <run-id>` 继续 `~/.atm/runs/<run-id>/manifest.json` 描述的托管运行。`--last` 从 `~/.atm/runs/index.json` 选择最近的未完成运行；多个项目有未完成运行时，可用 `--project` 或 `--source` 过滤。`--restore-source` 把保存的源副本恢复到原路径或可选目标路径；不传 run id 时，恢复模式默认查找当前工作目录最近一个运行副本，包含已成功和未完成的运行。目标存在且不是 ATM 占位文件时需要确认，非交互场景用 `--force`。

### `serve`

服务一个显式指定的 atm 文件，或服务当前项目中已经注册的 API 文件：

```sh
atm serve workflows/create.todo.md --addr 127.0.0.1:8080
atm serve register workflows/create.todo.md --path /user/create
atm serve scan
atm serve --addr 127.0.0.1:8080
atm serve list
atm serve unregister /user/create
```

注册表保存在 `.atm/api/index.json`。不传文件参数时，`serve` 只读取该注册表，启动时不扫描当前目录或 `.atm/api`。`atm serve scan` 是显式的一次性导入动作，只扫描当前项目 `./.atm/api` 并写入注册表，同时跳过生成目录 `runs/` 和 `jobs/`。每个路由同时提供 `/user/create` 和 `/user/create.todo.md` 这类有后缀/无后缀形式。`GET` 同步运行，`POST` 创建异步 job；两者都执行源文件副本，不会把状态写回注册源文档。GET 产物保存在 `.atm/api/runs/<route>/<timestamp>/`，POST job 状态和产物保存在 `.atm/api/jobs/<jobId>/`，这些生成路径永远不会被作为 API 注册项。

`serve register`、`serve scan`、`serve list` 和 `serve unregister` 都支持 `-g`，用于操作全局 API 注册表而不是当前项目注册表。全局注册表使用 Go 的跨平台 `os.UserConfigDir()` 解析用户配置目录；如果系统没有提供配置目录，则回退到用户 home 下的 `.atm`。

### `report`

汇总任务报告和 ATM 审计状态，不运行 agent、不执行 bash、不做条件检查：

```sh
atm report
atm report <run-id>
atm report --source todo.txt
atm report -json
```

不带位置参数时，`report` 会选择当前项目最近一个运行副本。位置参数可以是 run id、run 目录、manifest 路径或 todo/result 文件。`--last`、`--project` 和 `--source` 会从 `~/.atm/runs/index.json` 中选择最近的匹配运行。`report` 会读取托管结果文档、`.atm/state.json` 和任务详细报告，输出 `done`、`running`、`failed`、`skipped` 和 `draft` 数量，并列出失败任务、orphan report 和最近日志路径。`draft` 表示当前文档中仍会被编译为待执行工作的 task block。需要给工具或 CI 消费时使用 `-json`；任务项会包含可用的 `id`、`status`、`report`、`source` 和 `rendered` 字段。

### `clean`

清理 ATM 生成状态，不删除用户手写正文：

```sh
atm clean todo.txt
atm clean todo.txt --reports
atm clean todo.txt --state
atm clean todo.txt --logs
atm clean todo.txt --all
atm clean --repair-ids result.todo.md
```

不带清理选项时，`clean` 只移除目标文档中的生成 report/status block。因为直接 `run` 默认不改源文件，这通常只用于结果文档或手动复制过生成块的文档。显式选项用于删除目标文件旁边的审计产物：`--reports` 删除 `.atm/reports/`，`--state` 删除 `.atm/state.json`，`--logs` 删除 `.atm/logs/`，`--all` 同时清理文档状态块和这些 `.atm` 产物。`--repair-ids` 用于复制生成块后修复重复的 ATM report id。

### `append`

向 atm 文件追加一个或多个已格式化任务块：

```sh
printf '/task\n运行 go test ./... 并修复失败。\n' | atm append todo.txt
```

`append` 接收源 todo 路径，然后解析该源文件当前的 active 文件。如果该源文件仍在执行，追加内容会写入当前 active 工作文件，并可被 live/rescan 循环拾取；如果 run 已经退出，`append` 会写入源文件，留到下次执行。

追加内容必须至少包含一个任务块，例如 `/task` 加后续提示词。没有提示词参数时，`append` 会读取 stdin。如果 stdin 是终端，则打开 `$VISUAL`、`$EDITOR` 或一个小型平台默认编辑器。

### `format`

把生成状态改写成整洁的任务块布局：

```sh
atm format todo.txt
```

必要时，生成标签会被移动到单独一行。生成的 `> [!ATM]` 结果块使用块状格式。组合 task header 会规范化为每个命令一个 Markdown 段落，并在任务之间加入更易读的间距，同时保持合并后的配置和流程顺序。

## 环境变量

ATM 会尽量保持环境变量接口小而明确。下面列出 ATM 会设置或读取的所有环境变量。

### ATM 设置的变量

| 变量 | 对谁可见 | 含义 |
| --- | --- | --- |
| `ATM_TODO_FILE` | `/bash`、`/let ... /bash`、Codex、Claude，以及临时结构化工具进程 | 当前运行处理的 atm 文件路径。脚本或 agent 需要定位当前 todo 文档时使用它。 |

`ATM_TODO_FILE` 会在父进程环境变量之外额外注入给子进程。运行期间它指向托管运行目录中的工作副本，而不是原始位置文件参数对应的路径。任务使用 `/cd` 时，`/bash`、Codex、Claude 和检查 agent 会在该任务工作区运行，但 `ATM_TODO_FILE` 仍指向工作副本。

### ATM 读取的变量

| 变量 | 使用者 | 含义 |
| --- | --- | --- |
| `ATM_HOME` | 直接 `run`、`resume` | ATM home 目录；默认是当前操作系统用户 home 下的 `.atm`。 |
| `VISUAL` | `atm append` | 当 `append` 需要交互式输入且 stdin 是终端时，优先使用的编辑器。 |
| `EDITOR` | `atm append` | `VISUAL` 未设置时的备用编辑器。 |

ATM 也会继承普通操作系统环境变量，例如 `PATH`。这会影响 `codex`、`claude`、shell 命令和编辑器如何被查找。

## 运行中编辑

`atm run` 启动时，会在 `~/.atm/runs/<run-id>/` 创建托管运行目录，复制源文件和 import 文件，并在 agent 运行期间用简短占位文件隐藏原路径。占位文件不会暴露运行目录路径，只提示 agent 忽略当前文件。

运行期间如果要追加后续任务，可以对源 todo 路径执行 `append`。如果该源文件对应的 run 仍在执行，`append` 会写入当前 working file 并可被本次 live/rescan 拾取；如果 run 已经退出，则写入源文件，留给下次 run：

```sh
atm append todo.txt ...
```

## 跨平台说明

- 位置 todo 路径以及 `-codex`、`-claude` 路径使用当前 shell 的普通路径语法。
- 人类可读任务日志写入 `~/.atm/runs/<run-id>/tasks/<task-id>/logs/`。
- 处理中断时，ATM 会尽力恢复源文件；可用 `atm resume <run-id> --restore-source` 找回源副本。
- POSIX 系统处理 interrupt 和 terminate 信号；Windows 处理控制台 interrupt。
