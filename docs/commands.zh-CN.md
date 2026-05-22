# 命令参考

[English](commands.md)

ATM 是 **Agent Task Markdown**：一种基于 Markdown 的 Agent 任务调度 DSL（领域专用语言）。普通 Markdown 承载上下文和说明，以 `/` 开头的 heading section 和斜杠命令定义可执行任务。

大多数 todo 文件只需要普通提示词。只有在需要继续会话、传递工具参数、重复执行、按条件停止或后台运行时，才加命令行。

命令必须写在任务块开头，并位于提示词正文之前。

## 任务边界

ATM 有两种解析模式。

在旧模式中，一个任务是一个文本块：

```txt
/command
/another-command
发送给工具的提示词正文。

下一个任务块。
```

空行分隔任务；可以使用任意数量的空行或只包含空白字符的行。整行注释会被忽略：

```txt
# 被忽略的行
<!-- 被忽略的 HTML 注释块 -->
[//]: # (被忽略的引用式注释)
[comment]: <> (被忽略的引用式注释)
---
===
```

只由三个或更多 `-` 或 `=` 组成的独立 Markdown 分隔线也会被忽略。不支持行内注释。提示词行中间出现的 `#`、`---` 或 `<!-- ... -->` 会保留为提示词正文。

在 Markdown task mode 中，标题内容以 `/` 开头的 Markdown heading 会定义可执行 section：

```md
# 背景

这里是普通 Markdown 文档，不会执行。

## //verify

运行 go test ./...，并修复失败。

运行 go vet ./...，修复可操作的问题。

## /discuss

整个 section 是一个 prompt。

空行会保留在 prompt 中。

## 备注

这里又是普通文档。
```

如果文件里至少有一个 slash heading（`#{1,6} /...` 或 `#{1,6} //...`），则只解析 slash-heading section。可执行 section 会持续到下一个同级或更高级 heading 为止。

- `# /name`：单任务 section。整个 section body 是一个 task prompt，空行会保留在 prompt 中。
- `# //name`：任务列表 section。section body 使用旧 block 规则，空行会拆分多个任务。

在单任务 `# /name` section 中，下级 Markdown heading 会作为 prompt 内容保留。在任务列表 `# //name` section 中，每个任务块继续使用旧的注释和空行拆分规则。

可以用下面的命令规范化格式：

```sh
atm format -file todo.txt
```

只有提示词正文开始前的斜杠命令会被识别。正文里的斜杠命令会作为普通提示词内容发送给工具。

## 命令

### `/resume`

使用工具的 resume 模式运行提示词。

对 Codex 来说，这等价于：

```sh
codex exec resume --last -
```

对 Claude Code 来说，这等价于：

```sh
claude -c -p "提示词"
```

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

路径会先用当前任务变量渲染，再相对当前任务工作区或原始 todo 文件所在目录解析。解析后的路径必须留在原始 todo 文件目录内。`/cd` 会影响 Codex/Claude、`/bash`、`/let ... /bash`、自然语言 `until` 检查和本地 CEL 文件函数；不会改变 ATM 写状态块、输出产物或 DB 文件的位置。

### `/let name value`

定义模板变量。只包含 `/let` 命令的独立任务块会为后续任务块定义全局变量。未使用的 `/let` 是允许的，但临时绕过任务时使用注释更清晰。

```txt
/let suite go test ./...

/for 3 until 测试通过
运行 {{suite}} 并修复失败。
```

在任务块内部，`/let` 定义局部变量。与变量名相同的斜杠命令会把变量内容插入到提示词前：

```txt
/let context 先阅读 README.md。
/context
审查安装说明。
```

`/let` 也可以捕获 bash 的 stdout。渲染前会移除 stdout 末尾换行：

```txt
/let branch /bash git branch --show-current
总结 {{branch}} 的发布风险。
```

### 模板渲染

ATM 使用 Go `text/template` 渲染 prompt、`/bash` 脚本、`until` 条件、`/args` 参数值和 `/cd` 路径。

已有变量占位符继续支持：

```txt
第 {{N}} 次审查 {{path}}。
```

这个旧写法等价于：

```gotemplate
第 {{var "N"}} 次审查 {{var "path"}}。
```

模板数据会把当前变量作为顶层 key 暴露（变量名必须是合法 Go template 标识符），同时也提供 `.Vars` 进行 map 访问：

```gotemplate
{{if .N}}第 {{.N}} 次{{end}}
{{index .Vars "path"}}
{{var "path"}}
{{has "path"}}
```

如果变量名包含 `-`，请使用 `{{var "name"}}` 或 `{{index .Vars "name"}}`。未知的旧占位符（如 `{{future}}`）会保留为原文；非法 Go template 语法会让任务失败。

### 系统提供的模板值

ATM 在不同渲染上下文中提供不同的模板值。

| 上下文 | 可用值 |
| --- | --- |
| 普通 prompt、`/bash`、`/args`、`/cd`、`until`、`/return` 和 `/output` schema | 来自 `/let`、`/let ... /call`、`/for` 循环变量和 definition 参数的用户变量。 |
| 仅 `/return` | 当前 definition 调用中的 `{{agent.message}}`、`{{agent.last_message}}`、`{{agent.messages}}` 和 `{{agent.messages_json}}`。 |
| 后台分支里的 `/output` 文件名 | `{{agent_index}}`、`{{agent}}` 和 `{{agent_label}}`，以及普通变量。 |

`agent.last_message` 不是全局 prompt 变量。它只在渲染 `/return` 时存在，因为此时 ATM 已经执行完 definition body，并收集到了最近的 assistant 消息。想在后续 prompt 中使用 agent 消息，需要先 return 并绑定：

```txt
/let note /call reviewer api
使用这条审查消息：
{{note}}
```

### `/output [file]`

把当前任务结果保存到本次运行 output 目录。`/output` 会绑定到最近的当前任务块，可以写在该块内任意位置，并且一个任务块里最多只能出现一次。它可以写在 `/for`、`/go`、`/wait`、`/bash` 或 prompt 文本的前面或后面。

不写 fenced schema block 时，`/output` 会把最近一条 assistant 消息保存为文本：

```txt
写一份发布经理可以直接转发的风险说明。

/output release-note
```

紧跟 fenced schema block 时，`/output` 会要求当前任务通过临时 MCP 工具产出结构化 JSON。同行可选文件名用于指定 ATM 保存 JSON 的文件名；`name` 和 `name.json` 都会保存为 `name.json`。如果不写，ATM 会生成一个包含时间的 JSON 文件名，并在任务日志中报告路径。

ATM 按 Markdown 的 fence 长度规则识别，所以可以用 ```` 包裹内部包含 ``` 的 schema 文本。ATM 支持普通 fence、`json`、`yaml` 和 `yml`：

````txt
判断发布是否准备好。

/output summary.json
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
报告当前天气。

/output
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

普通 fence 也可以使用紧凑字段列表。每个非空行是 `name:type:description`；`type` 可省略，默认是 `string`，所以 `weather:天气状态` 和 `weather::天气状态` 都有效：

````txt
返回发布门禁结果。

/output result
```
reason:string:描述详细原因
weather:天气状态
passed:boolean:发布门禁是否通过
```
````

对 Codex 和 Claude，ATM 会挂载一个临时 MCP server，并暴露 `atm_report_output` 工具。该工具的 input schema 由 `/output` 的 schema block 生成，所以 agent 需要通过调用这个工具提交结构化数据。如果工具没有被调用，任务会失败，不会被标记为 done。

文件名会用和 prompt 相同的模板上下文渲染。循环变量如 `{{N}}`、`{{area}}`、`{{dir}}`、`{{path}}` 都可用。后台分支还会暴露 `{{agent_index}}`、`{{agent}}`（文件名安全）和 `{{agent_label}}`（人类可读）。当 `/output` 在 `/go` 分支中运行时，ATM 会自动给文件名追加分支后缀，避免多个 agent 覆盖同一个文件。

### `/db`

声明轻量任务数据库，供 agent 作为记忆或黑板使用。数据库存储 `map[string][]string`，并通过 MCP server 暴露给 Codex 和 Claude。`/db new` 是独立全局声明块，不会运行 agent：

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
| `scope:global` | 声明点之后的任务块默认可见。 |
| `scope:local` | 只声明，不默认挂载；任务块必须写 `/db use name`。 |
| `persist:run` | 存到本次 run 的 output 目录，例如 `.atm/20260521103000/db/name.json`。 |
| `persist:project` | 存到项目目录 `.atm/db/name.json`，后续 run 可继续使用。 |
| `access:read` | 最大权限为只读。 |
| `access:append` | 只读加追加写。 |
| `access:write` | 只读、追加、替换 key，但不能删除。 |
| `access:admin` | 完整读写删除权限。 |

`/db new ...` 后面的正文是 usage description。ATM 会把它传给 DB MCP server，agent 调用 `atm_db_list` 时也能看到。

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

权限决定 agent 可用的 DB MCP 能力：

| 权限 | MCP 能力 |
| --- | --- |
| `read` | `atm_db_list`、`atm_db_get`、`atm_db_scan` |
| `append` | `read` 加 `atm_db_append` |
| `write` | `append` 加 `atm_db_set` |
| `admin` | `write` 加 `atm_db_delete` |

MCP 工具：

| 工具 | 参数 | 结果 |
| --- | --- | --- |
| `atm_db_list` | `{}` | 可见 DB、usage、access 和 capabilities。 |
| `atm_db_get` | `{"db":"name","key":"k"}` | 一个 key 及其字符串数组。 |
| `atm_db_scan` | `{"db":"name","pattern":"findings/**","limit":100,"cursor":""}` | 排序后的匹配 key，支持分页。 |
| `atm_db_append` | `{"db":"name","key":"k","values":["v"]}` | 追加后的 key values。 |
| `atm_db_set` | `{"db":"name","key":"k","values":["v"]}` | 替换后的 key values。 |
| `atm_db_delete` | `{"db":"name","key":"k","values":["v"]}` 或不传 `values` | 剩余 values，或删除整个 key。 |

`scan` 支持 glob。`*` 匹配一个 `/` 分段内的内容，`**` 可以跨 `/` 分段匹配。写操作用 DB lock 文件串行化，并通过临时文件 rename 原子写入，所以并发 `append` 不会互相覆盖。

自然语言 `until` 和 `/if` 检查会收到只读 DB MCP server，即使任务本身有写权限，检查 agent 也只能读取。

### `/skill` 和 `/mcp`

先声明本地 skill 和临时 MCP server，再在任务块中显式启用：

````txt
/skill new reviewer from .atm/skills/reviewer

/mcp new helper
```json
{"command":"helper-mcp","args":["--stdio"]}
```

/cd work/release
/skill use reviewer
/mcp use helper
/mcp def use release_gate
准备发布说明。
````

`/skill use` 会把选中的 skill 目录复制到当前 `/cd` 工作区中，并使用所选 adapter 期望的布局：Codex 使用 `.agents/skills/<name>`，Claude 使用 `.claude/skills/<name>`。源目录必须已经存在并包含 `SKILL.md`；目标 adapter 目录由 ATM 自动创建。

`/mcp use` 通过临时 runner 配置注入具名 MCP server，不会写入 `.mcp.json` 或持久 Codex 配置。`/mcp def use name...` 会把选中的定义暴露成 agent 可调用的 MCP 工具，工具名形如 `atm_def_<name>`，每个 definition 参数对应一个 string 参数。通过这种方式调用的 def 会继承当前任务的 workdir、DB、skill 和 MCP，但默认不会在嵌套 agent 中继续暴露 def-MCP，以避免递归自调度。

### `/def`、`//def`、`/call` 和 `/return`

用 `/def` 定义可复用任务模板，用 `/call` 调用。定义本身不会执行，只会在调用点执行。

在 Markdown task mode 中，`/def` 和 `//def` 对应普通 `/` 和 `//` task heading 的块语义：

```md
## /def whereami

根据仓库上下文或可用环境判断当前城市。

/return {{agent.last_message}}

## //def release_reviews area

/go reviewer
审查 {{area}} 的实现风险。

/go reviewer
审查 {{area}} 的文档风险。

/wait reviewer

/return {{area}} 的审查已完成。
```

`/def` 把整个 section 当作一个任务模板，保留 Markdown 段落空行。`//def` 把 section 继续拆成多个任务块，调用时按顺序执行。旧式 todo 文件中，`/def name [params...]` 定义一个单任务模板。

调用参数按位置绑定到定义参数：

```txt
/call release_reviews checkout
```

当 `/call` 是独立任务块时，ATM 只执行定义并忽略返回值。当 `/call` 作为 prompt 正文里的独立一行出现时，ATM 会先执行定义，把该行替换成返回文本，再执行外层任务：

```txt
查询
/call whereami
今天的天气。
```

`/let name /call ...` 会同步执行定义，并把返回值绑定成后续模板变量：

```txt
/let city /call whereami
查询 {{city}} 的天气。
```

定义通过 `/return` 返回值。`/return` 支持单行、bash、多行和结构化返回：

```txt
/return {{city}}
/return /bash pwd

/return
城市：{{city}}
最近消息：{{agent.last_message}}
```

结构化 `/return` 使用和结构化 `/output` 相同的 MCP output 机制，但 JSON 主要作为调用返回值使用：

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

如果定义同时有 `/return` 和 `/output`，作为调用返回值时 `/return` 优先。如果定义没有 `/return` 但有结构化 `/output`，结构化 output JSON 会作为 fallback 返回值。如果二者都没有，则没有返回值。需要值的调用点（例如 `/let name /call ...` 或 prompt 正文中的内联 `/call`）在定义无返回值时会失败。

`/return` 模板可以读取当前定义调用里的最近 assistant 消息：

- `{{agent.last_message}}`：最近一条 assistant 消息文本。
- `{{agent.message}}`：`{{agent.last_message}}` 的别名。
- `{{agent.messages}}`：最近 N 条 assistant 消息拼成的文本。
- `{{agent.messages_json}}`：最近 N 条 assistant 消息的 JSON。

N 使用本次运行的 `-messages`，默认是 `1`。

定义里可以包含 `/pool`、`/go` 和 `/wait`。定义内部声明的 pool 只在本次调用中局部生效；无论局部池还是具名池，都共享全局 `-jobs` 并发上限。定义返回前会等待本次调用内部尚未结束的后台分支。

用 `/import` 从其他文件导入定义：

```txt
/import workflows/location.todo.md
/import weather from workflows/weather.todo.md
```

Import 只加载定义，不会执行被导入文件里的普通任务。路径相对当前 todo 文件。带命名空间的导入通过 `weather.lookup` 调用。ATM 会检测递归定义调用，包括跨文件 import 形成的环，并在 plan/parse 阶段失败。

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
go build -buildvcs=false ./...
SH
总结 {{changes}} 和验证结果。
```

### `/for`

通过一个或多个流程运行提示词。同一行上的命令会组合成同一个流程。

固定次数循环：

```txt
/for 3
第 {{N}} 次审查最终 diff。
```

带完成条件的有限重试：

```txt
/for 3 until 测试通过
修复失败的测试。
```

每次执行后，`atm` 会让所选工具检查 `until` 条件。对 Codex 和 Claude，`atm` 会挂载一个临时 MCP server，并暴露名为 `atm_report_check` 的工具，agent 必须通过该工具提交结果。

使用 CEL 的确定性本地重试：

```txt
/for 5 until(exists("result.json") && len(read("result.json")) > 0)
生成 result.json。
```

当 `until` 后面是括号表达式时，ATM 会在本地用 CEL 判断，而不是让 agent 判断自然语言。表达式必须返回 `bool`。CEL 判断只读、无副作用，支持 `exists(path)`、`read(path)`、`json(path)`、`existsOutput(path)`、`readOutput(path)`、`jsonOutput(path)` 和 `len(value)`。相对路径按原始 todo 文件所在目录解析；`*Output` 函数按本次运行的 output 目录解析。

不带次数的无界 CEL 重试写法如下：

```txt
/for until(exists("result.json") && json("result.json").passed)
持续工作，直到 result.json 中 passed=true。
```

这个形式会一直运行，直到 CEL 表达式为 true 或进程被中断。它和自然语言 `until` 明确分开：`/for until 测试通过` 是非法写法。CEL 函数中的文件路径必须加引号，例如 `read("result.json")`；`read(result.json)` 会被 CEL 理解成变量访问。

### `/if` 和 `/else`

选择一个任务块执行。`/if` 必须是任务块第一条命令。行内分支可以省略 `/else`；用于嵌套的 header-only 分支必须成对写 `/else`。

```txt
/if (exists("gate.json") && json("gate.json").passed)
继续发布。

/else
根据 gate.json 写发布阻塞说明。
```

`/if(...)` 和 `/if (...)` 使用本地 CEL，和 `until(...)` 对齐。`/if 自然语言条件` 使用所选工具的结构化 MCP check，和自然语言 `until` 对齐：

```txt
/if 发布门禁已经打开
继续发布。
```

如果条件为 true，ATM 执行 `/if` 分支，并把 `/else` 分支标记为 skipped。如果条件为 false，ATM 把 `/if` 分支标记为 skipped，并在存在 `/else` 时执行 `/else` 分支。没有 `/else` 时，false 的行内 `/if` 块只会被标记为 skipped。Skipped 块使用生成状态块：

```txt
> [!ATM]
> status: skipped
> time: 2026-05-22 10:30
> reason: if condition evaluated false
```

嵌套分支使用 header-only 的 `/if` 和 `/else` 块。因为 ATM 不使用缩进，所以配对规则是结构化的：每个 `/else` 归属于最近一个尚未匹配的 header-only `/if`；header-only 嵌套 `/if` 必须写出匹配的 `/else`。

```txt
/if (exists("gate.json"))

/if (json("gate.json").passed)

发布。

/else
写门禁失败说明。

/else
先生成 gate.json。
```

这个例子里，第一个 `/else` 属于内层 `/if`，第二个 `/else` 属于外层 `/if`。

目录和路径循环：

```txt
/for dir
审查 {{dir}}。

/for path
审查 {{path}}。
```

显式列表循环：

```txt
/for area in [api docs tests]
审查 {{area}}。
```

循环变量可通过 `{{N}}`、`{{dir}}`、`{{path}}` 或 `{{area}}` 渲染。

`/for` 和 `/go` 写在同一行时，命令顺序有意义：

```txt
/for 3 /go
审查分片 {{N}}。
```

这会为每个循环项启动一个后台分支。反过来写时，循环会留在同一个后台任务内部：

```txt
/go /for 3
审查分片 {{N}}。
```

两种形式仍然只会为这个 todo block 写一个生成的 `> [!ATM]` 结果块。对于 `/for ... /go`，每个循环项会标注成一个独立 agent 分支，例如 `N=1` 或 `area=api`；`-messages N` 会为每个分支保留最近 N 条结构化 assistant 消息，所以默认 `-messages 1` 也能看到每个并行分支的一条消息。

### `/pool name max [buffer]`

声明一个具名后台工作池。`/pool` 是全局声明块，只配置后续 `/go poolName` 的调度，不会启动 agent。

```txt
/pool tester 5
```

第二个参数表示这个具名池最多同时运行多少个后台分支。默认情况下，等待队列不设上限，提交更多分支不会阻塞流程。第三个参数可以限制等待队列长度：

```txt
/pool tester 5 10
```

具名池同时也受全局后台并发上限约束。全局上限默认是 `NumCPU`，可通过 `atm run -jobs N` 修改。

### `/go`

在后台启动任务，并继续扫描 todo 文件。

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

### `/wait`

等待之前所有 `/go` 后台任务。

独立等待块：

```txt
/wait
```

在另一个提示词前等待：

```txt
/wait
在后台任务完成后再运行。
```

使用 `/wait poolName` 可以只等待该具名池中此前提交的任务：

```txt
/wait tester
汇总 tester 结果，其他池中的任务可以继续运行。
```

进程退出前，`atm` 默认会等待所有剩余后台任务结束。

## Plan Dry-Run

使用 `atm plan` 查看命令顺序会如何解释。它只解析 todo 文件并打印全局声明、条件控制块、每个可运行块的流程、任务级 DB/skill/MCP 配置和 runner 参数，不运行 bash，不调用所选工具，也不写入结果块。

```sh
atm plan -file todo.txt
```

`atm plan dry-run -file todo.txt` 也可以作为显式别名使用。

使用 `-html FILE` 保存适合浏览器查看的流程图，或使用 `-open` 生成流程图并用默认浏览器打开：

```sh
atm plan -file todo.txt -html plan.html
atm plan -file todo.txt -open
```

如果需要让其他工具消费 IR plan，可以加 `-json`：

```sh
atm plan -json -file todo.txt
```

JSON 格式作为面向工具的契约：

- `source`、`globals`、`tasks`、`tasks[].block`、`tasks[].prompt` 和 `tasks[].ops` 是稳定字段。
- `imports`、`dbs`、`skills`、`mcps` 和 `controls` 会在存在时暴露解析后的声明或控制块；`tasks[].db`、`tasks[].skill` 和 `tasks[].mcp` 会在存在时暴露任务级工具配置。
- `ops[].kind` 是稳定字段；未来小版本可能增加新的 operation kind。
- 现有 operation 字段保持追加兼容。消费方应忽略未知字段。
- block 编号从 1 开始，指向当前源码文件快照。

## 结果块

结果块是生成状态，可用 `atm untag` 移除。

### Done

```txt
任务提示词
> [!ATM]
> status: done
> started: 2026-05-08 14:30
> finished: 2026-05-08 14:32
> duration: 2m
> runs: 1x
>
> messages:
> - assistant (codex):
>   已修复解析器测试。
```

字段包括状态、开始时间、结束时间、耗时、总执行次数和最近 assistant 消息。Codex 会以结构化 JSON events 运行，Claude Code 会以 stream JSON 运行，因此 `atm` 不需要从普通终端文本里猜测 assistant 消息。

### Running

```txt
任务提示词
> [!ATM]
> status: running
> started: 2026-05-08 14:30
> step: 1
> step-runs: 1x
> total-runs: 1x
```

Running 结果块让中断或失败的循环从剩余工作继续，而不是从零开始。`atm` 只会在持有 block lease 时替换尾部生成的 `> [!ATM]` 块；如果任务正文被编辑，lease 会失效并重新扫描。

## 子命令

### `run`

运行待处理的提示词任务块。它也是默认命令，所以下面两个命令等价：

```sh
atm -file todo.txt
atm run -file todo.txt
```

不传文件参数时，`atm run` 会使用第一个存在的默认文件：`todo.txt`、`todo.md` 或 `toto.md`。

两种形式都可以搭配 `-tool`、`-codex` 和 `-claude` 使用。用 `-messages N` 可以设置每个生成结果块中每个执行分支保留最近几条结构化 assistant 消息；默认是 `1`。用 `-jobs N` 可以设置所有池共享的全局后台分支并发上限；默认是 `NumCPU`。

多个文件会按顺序排队执行。可以用位置参数传入，也可以重复 `-file`：

```sh
atm run todo.txt rollout.md followup.md
atm run -file todo.txt -file rollout.md
```

执行产物会写入输出目录。默认情况下，`atm` 会创建 `.atm/YYYYMMDDHHMMSS`，如果同一秒已有目录，则追加 `-1`、`-2` 等后缀。可以用 `-output DIR` 或 `-o DIR` 指定目录。目录中包含：

- `task-NNN-*.log`：某个任务块的人类可读 stdout/stderr 合并日志。
- `task-NNN-run-NNN-TOOL[-BRANCH].jsonl`：该次 agent 执行自身产生的原生结构化事件流。
- `result.md`：最终 todo 文档快照，包含生成的 `> [!ATM]` 结果块，便于之后执行 `untag` 后追查。

如果多个输入文件共用同一个 `-output DIR`，`atm` 会在 `DIR` 下为每个文件创建一个带编号的子目录，避免结果文档和原生事件流互相覆盖。

### `append`

向 todo 文件追加一个或多个已格式化任务块：

```sh
atm append -file todo.txt "运行 go test ./... 并修复失败。"
```

当主执行器把 `todo.txt` 移到活跃临时路径时，`append` 会自动解析并写入该活跃文件。如果当前 `atm run` 仍有任务在执行，追加任务会在后续重新扫描时被同一次 run 执行。如果 run 已经退出，`append` 只会写入 todo 文件；需要再次运行 `atm run` 才会执行追加内容。

没有提示词参数时，`append` 会读取 stdin。如果 stdin 是终端，则打开 `$VISUAL`、`$EDITOR` 或一个小型平台默认编辑器。

### `format`

把生成状态改写成整洁的任务块布局：

```sh
atm format -file todo.txt
```

必要时，旧版生成标签会被移动到单独一行。当前生成的 `> [!ATM]` 结果块已经是块状格式。`/resume /for 3` 这样的组合命令行会被保留，因为同一行命令共享同一个流程。

### `untag`

移除生成状态：

```sh
atm untag -file todo.txt
atm untag -file todo.txt -done=false
atm untag -file todo.txt -running=false
```

### `mcp check`

运行结构化 `until` 检查使用的临时 stdio MCP server：

```sh
atm mcp check -result-file /tmp/atm/check-result.json
```

它暴露一个工具：`atm_report_check`，输入格式是：

```json
{"passed": true, "summary": "简短依据"}
```

这个命令主要给 agent runtime 和测试使用。普通用户继续使用 `/for N until condition` 即可；Codex 和 Claude 检查会通过临时命令行 MCP 配置接收这个 server。

### `mcp output`

运行结构化 `/output` 和结构化 `/return` 使用的临时 stdio MCP server：

```sh
atm mcp output \
  -result-file /tmp/atm/output.json \
  -schema-file /tmp/atm/schema.json \
  -schema-format json
```

它暴露 `atm_report_output`。schema 文件包含从任务 fenced `/output` block 生成的 JSON Schema。

### `mcp db`

运行 `/db` 使用的临时 stdio MCP server：

```sh
atm mcp db -config-file /tmp/atm/db-config.json
atm mcp db -config-file /tmp/atm/db-config.json -readonly
```

config 文件由 ATM 在运行时生成，列出当前任务可见的 DB。`-readonly` 会强制所有 DB 只读；ATM 会在自然语言检查 agent 中使用这个模式。

该 server 暴露 `atm_db_list`、`atm_db_get`、`atm_db_scan`、`atm_db_append`、`atm_db_set` 和 `atm_db_delete`。

## 环境变量

ATM 会尽量保持环境变量接口小而明确。下面列出 ATM 会设置或读取的所有环境变量。

### ATM 设置的变量

| 变量 | 对谁可见 | 含义 |
| --- | --- | --- |
| `ATM_TODO_FILE` | `/bash`、`/let ... /bash`、Codex、Claude，以及临时 MCP 工具进程 | 当前运行处理的 todo 文件路径。脚本或 agent 需要定位当前 todo 文档时使用它。 |

`ATM_TODO_FILE` 会在父进程环境变量之外额外注入给子进程。运行期间它可能指向 ATM 的活跃临时 todo 文件，而不一定是传给 `-file` 的原始路径。任务使用 `/cd` 时，`/bash`、Codex、Claude 和检查 agent 会在该任务工作区运行，但 `ATM_TODO_FILE` 仍指向活跃 todo 文件。

### ATM 读取的变量

| 变量 | 使用者 | 含义 |
| --- | --- | --- |
| `ATM_MCP_CHECK_LOG` | 临时 `atm_report_check` MCP server | 可选的 `until` 检查调试日志路径。设置后，ATM 会把它传给 check MCP server，server 会追加记录每次检查决策。 |
| `VISUAL` | `atm append` | 当 `append` 需要交互式输入且 stdin 是终端时，优先使用的编辑器。 |
| `EDITOR` | `atm append` | `VISUAL` 未设置时的备用编辑器。 |

ATM 也会继承普通操作系统环境变量，例如 `PATH`。这会影响 `codex`、`claude`、shell 命令和编辑器如何被查找。

## 运行中编辑

`atm` 运行时，会把原 todo 文件临时移动到系统临时目录并打印活跃路径。这样可以避免工具在退出恢复时同时编辑原路径。

运行期间建议使用：

```sh
atm append -file todo.txt ...
```

该命令会自动解析活跃临时文件。

## 跨平台说明

- `-file`、`-codex` 和 `-claude` 路径使用当前 shell 的普通路径语法。
- 输出日志写入操作系统临时目录。
- 处理中断时，活跃 todo 文件会在进程退出前恢复。
- POSIX 系统处理 interrupt 和 terminate 信号；Windows 处理控制台 interrupt。
