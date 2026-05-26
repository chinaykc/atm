# 3. 执行流程：循环、并行与工作池

ATM 的执行器按 IR 顺序解释命令。理解 `/for`、`/go`、`/wait` 的顺序，是写清晰工作流的关键。

## 顺序任务

```txt
运行测试并修复失败。

运行 go vet ./...，修复可操作问题。

总结修改。
```

ATM 从上到下执行，每个任务完成后才进入下一个。

## 循环与重试

固定次数：

```txt
/for 3
第 {{n}} 次审查最终 diff。
```

带条件的重试：

```txt
/for 3 until tests pass
修复测试失败。
```

每次执行后，ATM 会让所选工具报告条件是否满足；这部分交互由 ATM 自动处理。

如果 `until` 后面是括号表达式，ATM 会把它当成 本地表达式，在本地做确定性判断：

```txt
/for 5 until(exist("result.json") && len(open("result.json")) > 0)
生成 result.json。
```

也可以不写次数，让循环一直运行到 本地表达式条件满足：

```txt
/for until(exist("result.json") && json(open("result.json")).passed)
持续修复，直到 result.json 中 passed=true。
```

无界形式只支持 本地表达式，不支持自然语言判断。文件路径要写成字符串，例如 `open("result.json")`；读取 JSON 文件要写成 `json(open("result.json"))`。

## 条件分支

`/if` 和可选的 `/else` 用 本地表达式在本地选择任务块：

```txt
/if (exist("gate.json") && json(open("gate.json")).passed)
继续发布。

/else
写发布阻塞说明。
```

未选中的块会被写成 `> [!ATM] status: skipped`，所以后续扫描不会再执行它。`/if` 是任务块级控制流，不是 prompt 内的模板语法。

`/if(...)` 使用本地表达式；`/if 自然语言条件` 会走 agent 的 MCP check：

```txt
/if 发布门禁已经打开
继续发布。
```

`/if` 和 `/else` 不嵌套。需要复杂分支时，把复杂流程写成 `/def`，再在分支里 `/call`。空 `/else` 表示 false 分支 no-op，但 `atm check` 会 warning，通常省略更清晰。`/if` 可以和 `/for`、`/go` 组成控制链；顺序决定含义：

```txt
/for 10
/if(n % 2 == 0)
/go

审查偶数分片 {{n}}。

/wait
```

紧跟的 `/else` block 会并入同一个 conditional task，可以给另一个分支单独写 prompt：

```txt
/for 3 /if(n == 1)
审查被选中的分片 {{n}}。

/else
说明为什么跳过分片 {{n}}。
```

## 文件、目录和列表循环

```txt
/for dir in dirs()
审查目录 {{dir}}。

/for file in files()
审查文件 {{file}}。

/for area in [api docs tests]
审查 {{area}}。
```

## 后台任务

```txt
/go
审查 README。

/go
审查 docs/commands.md。

/wait

汇总两个审查结果。
```

如果 `/wait` 后面紧跟 prompt，它本身就是一个协调任务，而不是“等完后再启动普通任务”：

```txt
/wait
观察两个后台审查，汇总失败、风险和后续人工处理项。
```

这种写法会让协调 prompt 带上等待范围、待等待后台任务列表、当前可见 ATM report/status、日志路径和当前取消能力说明；ATM 随后等待这些后台任务完成，再写入该 `/wait` 任务的结果。

如果没有写 `/wait`，ATM 不会在退出前替你汇合后台任务；没有前台任务后进程会结束，未汇合的后台 block 可能保持 `running`。确实需要结果时必须显式写 `/wait`。

图示：

```mermaid
gantt
  title /go 和 /wait
  dateFormat X
  axisFormat %s
  README review :a, 0, 4
  commands review :b, 0, 5
  wait :milestone, 5, 0
  summary :c, 5, 2
```

## `/for /go` 与 `/go /for`

命令顺序有语义差异。

```txt
/for area in [api docs tests] /go
审查 {{area}}。
```

这会启动多个后台分支，每个循环项一个 agent 分支。

也可以从 planner 返回的数组动态展开。最简单模式是不落盘：`/for item in(/call planner)` 直接调用 planner，并展开返回对象里的 `plans` 数组：

```md
/def plan_shards

规划本次发布需要并行审查的工作项。
每个计划包含审查人、负责人、重点问题和相对于 ./result 的写目录。

/return
```schema
plans:[]string:计划
```

## parallel review

/for plan in(/call plan_shards)
/go reviewer
{{plan}}

/wait reviewer
```

动态 `/for` 在运行期读取表达式结果，因此适合 “planner 先返回数组，worker 再 fan-out” 的模式。需要审计或跨任务复查时，再把 planner 结果用 `/output` 保存到文件并通过 `json(open(outputDir("plan.json")))` 读取。数字区间可以用 `range(stop)`、`range(start, stop)` 或 `range(start, stop, step)`，例如 `/for shard in range(1, 4)`；文件和目录枚举用 `files()`、`dirs()`、`walkFiles()`、`walkDirs()`，例如 `/for file in walkFiles("src")`。这是跨版本稳定规则：旧的 `/for file`、`/for dir`、`/for path` 控制头无效。`step` 不能是 `0`。动态序列为空时会输出运行时 warning 并跳过循环体。固定次数 `/for number` 仍然支持，例如 `/for 10`，并绑定小写 `n`。`in expr`、`in(expr)` 和 `in (expr)` 都支持；推荐控制流换行写，简单 `/let name value` 或 `/let name /call def` 仍用单行。

```txt
/go /for 3
审查第 {{n}} 轮。
```

这会启动一个后台分支，循环留在该分支内部顺序执行。

## 工作池 `/pool`

声明具名工作池：

```txt
/pool reviewer 3
```

把后台任务提交到池：

```txt
/for area in [api docs tests ux] /go reviewer
审查 {{area}}。

/wait reviewer
```

`/pool reviewer 3` 表示 reviewer 池最多同时运行 3 个后台分支。默认队列无限。限制额外排队容量：

`/pool` 按 Markdown 词法作用域可见。根部 pool 对全文后续任务可见；heading 内 pool 只对该 heading 的后续任务和子 heading 可见，不能被同级 heading 使用，也不能在声明前使用。

```txt
/pool reviewer 3 10
```

所有池都受全局并发限制 `-jobs` 约束：

```sh
atm run -file todo.txt -jobs 8
```

```mermaid
flowchart TB
  G["全局 -jobs 8"] --> P1["reviewer max=3"]
  G --> P2["tester max=4"]
  G --> P3["默认池"]
  P1 --> A["/go reviewer"]
  P2 --> B["/go tester"]
  P3 --> C["/go"]
```

## 推荐模式

发布检查示例：

```txt
/pool reviewer 3

/bash go test ./...
确认测试结果，修复失败。

/for area in [api docs observability] /go reviewer
审查 {{area}} 的发布风险。

/wait reviewer

/for 2 until release notes are accurate
更新发布说明，直到准确。
```

经验规则：

- 会修改同一批文件的任务尽量顺序执行。
- 只读审查、文档检查、独立模块分析适合 `/go`。
- 并发数量优先用 `/pool` 和 `-jobs` 限制，不要让 agent 数量失控。
