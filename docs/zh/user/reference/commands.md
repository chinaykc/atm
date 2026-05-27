# 命令手册

本页是面向用户的 DSL（领域专用语言）命令速查。更底层的完整参考见 [../../commands.md](../../commands.md)。

## 任务命令总览

| 命令 | 作用 | 常见位置 |
| --- | --- | --- |
| `/task [name]` | 开始 prompt 任务；带 name 时记录 agent 会话 | 任务开头 |
| `/resume name` | 继续具名任务记录的 agent 会话 | 任务开头 |
| `/fork name` | 从具名任务记录的 agent 会话分叉；当前任务具名时记录新会话 | 任务开头 |
| `/args ...` | 给 Codex/Claude 追加参数 | 任务开头 |
| `/cd path` | 准备并进入任务工作区；默认创建目录 | 任务开头 |
| `/let name value` | 定义变量 | 任务开头或 scoped 声明块 |
| `/let name /bash ...` | 懒执行 bash 并捕获 stdout | 任务开头或 scoped 声明块 |
| `/let name /call ...` | 懒调用定义并绑定返回值 | 任务开头 |
| `/flag type name ...` | 声明 CLI/API 参数并注入同名模板变量 | 文档任意位置，推荐文件顶部 |
| `/bash ...` | 执行 bash，失败则任务失败 | 任务开头 |
| `/webhook new ...` | 声明 Webhook 目标 | 文档任意位置，推荐文件顶部 |
| `/webhook name ...` | 按执行顺序推送 Webhook 消息 | 任务开头 |
| `/webhook use name...` | 授权当前任务中的 agent 自主推送 Webhook | 任务开头 |
| `/context #Heading` | 引入其他 Markdown section 的普通文档上下文 | Markdown task header |
| `/doc text` 或 `/doc` + fenced block | 写只给人看的说明，不进入 agent 上下文 | Markdown section 普通区域 |
| `/output [file]` | 保存文本输出或要求结构化 JSON 输出 | task header |
| `/db new ...` | 声明本地任务数据库 | scoped 声明块 |
| `/skill new name from path` | 声明本地 skill | scoped 声明块 |
| `/skill use/ignore ...` | 控制当前任务的 skill 视图 | 任务开头 |
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

占位符写法：

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

`/let name /bash ...` 和 `/let name /call ...` 只有在变量被 prompt、`/bash`、`/args`、`/cd`、`until`、`/return`、`/output` schema 或表达式实际读取时才执行。同一次任务调用内第一次读取后会缓存；未使用的绑定不会在普通执行中触发。静态 `check` 和 `check --plan` 会用 warning 标出 lazy bash 的潜在副作用和 lazy call 的渲染期依赖；plan flow 中显示为 `LazyBash(name)` 或 `LazyCall(def -> name)`。`check --plan --preview` 是显式例外：它可能执行 lazy bash，以及不需要运行 agent 就能返回的纯 lazy call。

独立 `/let` 块按 Markdown 词法作用域可见：根部 `/let` 对全文后续任务可见，heading 内 `/let` 只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 读取，也不能在声明前读取。Task header 里的 `/let` 只对当前 task block 及其 child-heading task 可见；继承到的 lazy provider 在 child task invocation 内解析并缓存，不和 parent 共享缓存；child section 中的同名 `/let` 会遮蔽父 task header 的值。

`agent.*` 不是普通 prompt 的全局变量；它只在 `/return` 中表示“当前 definition 调用已经产生的最近 assistant 消息”。如果要在普通 prompt 里使用它，先通过 `/let name /call ...` 接收返回值。

## 文档参数 `/flag`

`/flag` 用于把 atm 文件变成带参数的 CLI/API 入口。语法：

```txt
/flag <type> <name> <description...> [default:<value>]
```

支持类型：`string`、`int`、`number`、`bool`、`[]string`、`[]int`、`[]number`。没有默认值时为必填；`bool` 没有默认值时默认是 `false`。运行时传入的值会注入同名模板变量：

```txt
/flag string name 用户名
/flag []int shards 分片列表 default:1,2

/task
向 {{name}} 汇报分片 {{shards}} 的处理结果。
```

单文件运行可直接传文档 flag：

```sh
atm run api.todo.md -name Ada -shards 3 -shards 4
```

多个文件一起运行时不能传文档 flag，因为同名参数可能属于不同文件。

动态命令来自显式注册项；ATM 启动时不扫描 `$HOME` 或 `./.atm/flag`。默认写当前项目 `.atm/flag/index.json`，加 `-g` 写全局注册表。全局注册表使用 Go 的跨平台 `os.UserConfigDir()` 解析用户配置目录；如果系统没有提供配置目录，则回退到用户 home 下的 `.atm`。可以显式注册单个文件，也可以把 `./.atm/flag` 扫描一次写入注册表：

```sh
atm flag register workflows/review.todo.md --name review --description "运行审查任务"
atm flag register workflows/review.todo.md --name review -g
atm flag scan
atm flag scan -g
atm flag list
```

动态命令与内置命令或其他动态命令重名会报错。`atm <command> -h` 会展示该 atm 文件声明的 `/flag` 参数。

## 常用组合

### 任务工作区

```txt
/cd services/payments
在这个目录下实现任务。
```

`/cd path` 会在目录不存在时自动创建。需要要求目录必须已存在时，写 `/cd --must-exist path`。解析后的路径必须留在原始 atm 文件所在目录内；`/cd` 会影响 agent、`/bash`、`/let ... /bash` 和 本地表达式文件函数。

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

数字区间可以写成 `/for shard in range(1, 4)`。表达式 helper 支持 `range(stop)`、`range(start, stop)` 和 `range(start, stop, step)`；`step` 不能是 `0`。文件和目录枚举使用 `/for file in files()`、`/for file in walkFiles("src")` 和 `/for dir in dirs()`。动态序列为空时会输出运行时 warning 并跳过循环体。固定次数写成 `/for number`，例如 `/for 10`，并绑定小写 `n`。

### 条件分支

```txt
/if (json(open("gate.json")).passed)
继续。

/else
停止并说明原因。
```

`/if(...)` 使用本地表达式；`/if 自然语言条件` 使用 agent 结构化检查。`/if` 和 `/else` 是任务块级控制流，未选中的块会标记为 skipped。`/if` 可以在控制链中组合，例如 `/for 10 /if(n % 2 == 0) /go`；命令顺序决定控制流。`/if` 和 `/else` 不嵌套，复杂分支应通过 `/def` 封装。紧跟对应 `/if` 分支的空 `/else` 合法，表示 false 分支 no-op；`atm check` 会 warning，通常直接省略更清晰。

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

DB 任务工具包括 `atm_db_list`、`atm_db_get`、`atm_db_scan`、`atm_db_append`、`atm_db_set` 和 `atm_db_delete`。`scan` 支持 glob；`**` 可以跨 `/` 分段匹配。

`/db new` 按 Markdown 词法作用域可见：根部声明对全文后续任务可见，heading 内声明只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 使用，也不能在声明前使用。`scope:global` 只表示在这个可见范围内默认挂载；`scope:local` 表示只声明，任务仍要显式 `/db use name`。

### Skill

声明本地 skill，然后在任务中启用：

````txt
/skill new reviewer from .atm/skills/reviewer

/cd work
/skill use reviewer
执行需要 reviewer skill 的任务。
````

`/skill new` 按 Markdown 词法作用域可见：根部声明对全文后续任务可见，heading 内声明只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 读取，也不能在声明前读取。`/skill use` 要求源目录已存在且包含 `SKILL.md`；引用具名 skill 时名称必须在当前任务可见，直接写 path-like 值时按路径加载。


### Webhook 推送

声明：

```txt
/webhook new notify provider:<generic|feishu|dingtalk> url:<URL|env:VAR> [secret:<VALUE|env:VAR>] [keyword:<word>] [keywords:<a,b>]
```

执行：

```txt
/webhook notify 发布任务开始：{{version}}
```

也可以后接 JSON/YAML fence 作为完整 payload，payload 会先渲染模板：

````txt
/webhook notify
```json
{"message":"{{version}} 发布开始"}
```
````

ATM 不提供飞书或钉钉的默认地址；你需要在对应群里创建自定义机器人，复制生成的 Webhook URL，例如飞书的 `https://open.feishu.cn/open-apis/bot/v2/hook/...` 或钉钉的 `https://oapi.dingtalk.com/robot/send?access_token=...`。

推荐把凭据放在环境变量中：

```txt
/webhook new alarm provider:dingtalk url:env:DINGTALK_WEBHOOK secret:env:DINGTALK_SECRET keyword:监控报警
```

也支持在文档内直接填写：

```txt
/webhook new alarm provider:dingtalk url:https://oapi.dingtalk.com/robot/send?access_token=... secret:SEC... keyword:监控报警
```

内联 URL 和 secret 会明文保存在 todo 文档中，也可能进入执行副本或 工具配置产物；不要把包含真实凭据的文件提交到版本库。

默认文本 payload：

| provider | payload |
| --- | --- |
| `generic` | `{"message":"..."}`；如果有 secret，会同时发送 `X-ATM-Webhook-Secret` header |
| `feishu` | `{"msg_type":"text","content":{"text":"..."}}`；如果有 secret，会按飞书签名校验在 JSON body 中加入 `timestamp` 和 `sign` |
| `dingtalk` | `{"msgtype":"text","text":{"content":"..."}}`；如果有 secret，会按钉钉加签规则在 URL query 中加入 `timestamp` 和 `sign` |

钉钉支持两种安全方式：

- 自定义关键词：在声明中写 `keyword:监控报警`，或用 `keywords:监控报警,发布通知` 一次声明多个。最多 10 个。ATM 会在发送前检查 JSON payload 字符串中至少包含一个关键词；不包含时任务本地失败，不发请求。
- 加签：写 `secret:env:<VAR>` 或 `secret:SEC...`。ATM 会使用当前毫秒时间戳和 secret 计算 `HmacSHA256(timestamp + "\n" + secret)`，Base64 后作为 `sign`，并把 `timestamp` 和 URL-encoded `sign` 拼到钉钉 Webhook URL query 中。不能在文档中写固定 `sign`，因为它与当前时间戳绑定。

示例：

```txt
/webhook new alarm provider:dingtalk url:env:DINGTALK_WEBHOOK secret:env:DINGTALK_SECRET keyword:监控报警

/webhook alarm 监控报警：{{service}} 失败率过高
```

非 2xx 响应、缺少环境变量、payload 模板渲染失败或 JSON/YAML 解析失败都会使当前任务失败。

如果希望 agent 自主决定是否推送，不要写执行型 `/webhook notify ...`，而是在任务头挂载：

```txt
/webhook use notify
判断发布是否需要通知外部群；只有需要时才调用 webhook 工具。
```

ATM 会为每个授权目标提供一个 agent 可调用的通知工具。工具输入支持 `message`，也支持 `payload` object。`/webhook use` 表示该任务已授权这些工具发送消息；是否实际调用由 agent 决定。工具调用失败会使当前任务失败。

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
- 没有显式 `/wait` 就不等待剩余后台任务；`atm check` 会用 warning 提示未等待的后台任务，但不把它当作 error。`atm run` 在没有前台任务后退出，未汇合的后台 block 可能保持 `running`。
- `/output` 只能写在 task header 中，并且一个任务块最多一个；prompt 正文里的 `/output` 是错误。
- `/db ignore` 不带参数时不能和同一任务块的 `/db use` 或 `/db access` 混用。
- `/db access` 只能降低权限，不能超过声明时的最大 `access`。
- `/output` 和结构化 `/return` 的 schema fence 必须使用反引号，不能使用波浪线。
- `/return` 支持普通文本、bash、多行文本和结构化 JSON；definition 必须显式写 `/return`，不要依赖 `/output` fallback。`/return` 只允许写在 `/def` body 内。多行文本和多行 bash 也必须使用反引号 fenced 参数。`/def` body 里的 Markdown heading 是 prompt 文本，不是 definition 边界；`atm check` 会 warning。
- `/def` 按 Markdown 词法作用域可见：根部 definition 对全文后续任务可见，heading 内 definition 只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 调用，也不能在声明前调用。
- `/import` 只导入 definition，并按导入声明所在的 Markdown scope 对后续任务可见；同级 heading 不继承导入结果，也不会导入被导入文件里的 DB/skill/任务工具 资源声明。
