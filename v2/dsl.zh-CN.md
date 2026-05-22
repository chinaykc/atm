# ATM v2 DSL 设计草案

ATM DSL 是一门基于 Markdown 的自然语言任务编排语言。它的目标不是替代 shell、Makefile、CI 或通用工作流引擎，而是把“给编码代理的操作指南”变成可以执行、可以审阅、可以恢复的文档。

这份文档描述 v2 的语言层设计。工具层设计见 [工具设计草案](tool.zh-CN.md)。当前实现可以逐步向这里收敛，但任何新增语法都应先能放进这个模型里解释。

## 设计目标

- Markdown 原生：普通 Markdown 首先是给人看的文档，同时也是代理任务的提示词来源。
- 自然语言优先：复杂判断交给代理和条件检查，DSL 只表达少量编排意图。
- 小而可解释：每个任务都能 dry-run 成一棵执行计划树。
- 文档即操作指南：同一份文件可以被人审阅，也可以被 ATM 执行。
- 可恢复：执行状态必须能写回文档或旁路状态文件，并能在中断后继续。
- 工具中立：任务语义不绑定 Codex、Claude Code 或某个特定 CLI。

## 非目标

- 不做通用编程语言。
- 不提供任意表达式、算术、函数、条件分支或复杂数据结构。
- 不替代 shell 脚本、Makefile、GitHub Actions 或任务调度系统。
- 不在 DSL 内定义项目构建、依赖安装或环境管理规则。
- 不让 Markdown 变得只有机器能读。

## 核心模型

DSL 的核心单位是“自由文本目标”。斜杠命令只控制这个目标如何执行。

```md
/for 3 until tests pass
Run the test suite and fix failures.
```

可以理解为：

```txt
For(count=3, until="tests pass",
  Run("Run the test suite and fix failures."))
```

命令不是普通 prompt 的一部分。命令行只在任务正文开始前识别；正文里的 `/example` 是普通文本。

## Markdown 结构

Markdown 的结构用于组织任务和变量作用域。

推荐模型：

- 标题建立章节作用域。
- 段落、列表、代码块等 Markdown 内容组成任务提示词。
- 任务块由一组前置命令和紧随其后的自由文本组成。
- 空行分隔任务块；未来可以允许标题下的多个连续块形成顺序任务组。

示例：

```md
# Release

/let check go test ./...

## Fix Tests

/for 3 until tests pass
Run {{check}} and fix failures.

## Review Packages

/for dir /go
Review {{dir}} for release blockers.
```

## 命令是组合操作符

为了表达嵌套和并发，命令应从左到右解析为组合操作符，而不是简单的顺序步骤。

```txt
/a /b
Prompt
```

应解释为：

```txt
A(B(Run(Prompt)))
```

这意味着命令顺序有语义差异。

```txt
/for 3 /go
Review the code.
```

表示循环展开 3 个后台任务：

```txt
For(3, Go(Run(prompt)))
```

而：

```txt
/go /for 3
Review the code.
```

表示启动 1 个后台任务，在该后台任务内部顺序执行 3 次：

```txt
Go(For(3, Run(prompt)))
```

这个差异应保留，因为它能自然表达 fan-out 和后台子流程。

## 推荐 IR

解析 Markdown 后，应先生成中间表示，而不是边解析边执行。

建议 IR：

```txt
Document(nodes...)
Section(title, vars, nodes...)
Seq(nodes...)
Run(prompt)
Let(name, value)
WithArgs(args, body)
WithResume(body)
For(iterator, body)
Until(condition, body)
Go(body)
Wait()
```

执行器只执行 IR。这样可以提供：

- `atm plan`：输出执行计划。
- `atm format`：规范化可保留语义的部分。
- `atm check`：只解析和验证 DSL，不运行代理。
- 更清晰的错误信息和状态恢复。

## 变量和模板

变量使用 `/let` 定义，使用 `{{name}}` 渲染。

```txt
/let suite go test ./...

/for 3 until tests pass
Run {{suite}} and fix failures.
```

作用域规则建议：

- 文档级 `/let` 对后续所有章节生效。
- 标题下的 `/let` 对该标题及子标题生效。
- 任务块内的 `/let` 只对当前任务块生效。
- 内层变量遮蔽外层变量。
- `/name` 是变量插入命令：如果 `name` 已定义，则把变量内容插入到 prompt 前。

示例：

```txt
/let context Read README.md first.
/context
Review setup instructions.
```

等价于把 prompt 变成：

```txt
Read README.md first.
Review setup instructions.
```

变量不是 shell 环境变量。未来如果需要进程环境，应另设 `/env KEY=value`，不要复用 `/let`。

## 循环

`/for` 是唯一循环入口。它负责展开执行目标，并绑定循环变量。

固定次数：

```txt
/for 3
Review pass {{N}}.
```

目录：

```txt
/for dir
Review {{dir}}.
```

路径：

```txt
/for path
Review {{path}}.
```

显式列表：

```txt
/for area in [api docs tests]
Review {{area}}.
```

`until` 是 `/for` 的参数，表示每次执行后检查完成条件：

```txt
/for 3 until tests pass
Fix tests.
```

建议语义：

```txt
for each iteration:
  run prompt
  if condition passes:
    stop this For
if condition never passes:
  fail this For
```

`until` 使用完成条件，而不是继续条件。用户应写 `tests pass`，而不是 `tests fail`。

## 嵌套

嵌套通过命令组合表达。

```txt
/for dir /for 3
Review {{dir}}, pass {{N}}.
```

解释为：

```txt
For(dir,
  For(N=1..3,
    Run(prompt)))
```

并发嵌套：

```txt
/for dir /go /for 3 until {{dir}} is clean
Work only in {{dir}}.
```

解释为：

```txt
For(dir,
  Go(
    For(N=1..3, until="{{dir}} is clean",
      Run(prompt))))
```

这表示每个目录启动一个后台流程，每个后台流程内部最多重试 3 次。

更大的 fan-out：

```txt
/for dir /for 3 /go
Review {{dir}}, pass {{N}}.
```

解释为每个目录的每个 pass 都是独立后台任务。

## 并发

`/go` 把其内部 body 放入后台执行。

```txt
/go
Review docs.
```

```txt
/for 3 /go
Review in parallel.
```

```txt
/go /for 3
Review three times in one background flow.
```

`/wait` 等待当前作用域内此前启动的后台任务。建议后续明确作用域：

- 文档级 wait：等待此前所有后台任务。
- 标题级 wait：等待该标题作用域内此前启动的后台任务。
- 任务块级 wait：只作为当前块执行前屏障。

初期可以保持简单：`/wait` 等待此前所有未收集后台任务。

## 条件检查

`Until(condition, body)` 的条件检查应是只读任务。

检查 prompt 应明确要求：

- 不修改文件。
- 不创建或删除文件。
- 只检查当前工作区。
- 最后一行输出机器可解析结果。

`until` 和并发组合时，不应试图取消已经启动的后台任务。

因此：

```txt
/for 3 until tests pass /go
Fix tests.
```

不推荐。更清晰的写法是：

```txt
/go /for 3 until tests pass
Fix tests.
```

前者会产生“先展开后台任务还是先检查条件”的心智负担。DSL 可以允许它，但文档应推荐后者。

## 参数

`/args` 为所选工具附加 CLI 参数。

```txt
/args --yolo /for 3
Fix tests.
```

参数属于运行选项，不属于 prompt。变量应可渲染到参数中：

```txt
/for model in [fast deep]
/args --model {{model}}
Review the implementation.
```

参数顺序应保持用户写入顺序。适配器负责把通用运行选项映射到具体工具命令。

## 状态恢复

状态应记录执行计划中的位置，而不是只记录文本块索引。

嵌套后，状态至少需要：

- 任务块身份。
- IR 节点路径，例如 `For(dir=api) > Go > For(N=2)`。
- 当前循环变量绑定。
- 当前 step run count 和 total run count。
- started time、last updated time。

旧的单层 marker：

```txt
[running|20260508-14:30|step=1|step-runs=1x|total=1x]
```

不足以描述嵌套。后续可以考虑：

```txt
[running|20260508-14:30|path=1.0.2|vars=dir:api,N:2|total=4x]
```

或者把复杂状态放入旁路 JSON 文件，Markdown 里只放一个简短 marker。

建议原则：

- 用户文档保持可读。
- 状态足够恢复。
- 状态格式可以向后读取。

## 错误模型

建议错误规则：

- `Run` 失败：当前 flow 失败。
- `Until` 达到上限仍未通过：当前 `For` 失败。
- 后台任务失败：`/wait` 或最终自动 wait 返回首个错误。
- 变量未定义：解析期错误。
- 循环展开为空：默认跳过，或解析期警告；发布版应先选择一种固定语义。
- 条件检查缺少机器结果：执行期错误。

错误信息应包含 Markdown 位置、任务标题、命令行和 IR 节点路径。

## 可解释性

DSL 必须支持 dry-run。

示例：

```sh
atm plan -file release.md
```

输出可以是：

```txt
Release > Fix Tests
  For N in [1 2 3] until "tests pass"
    Run "Run go test ./... and fix failures."

Release > Review Packages
  For dir in [api docs web]
    Go
      Run "Review {{dir}} for release blockers."
```

这能让用户在真正启动代理前确认嵌套、并发和变量展开是否符合预期。

## 语法边界

为了保持 Markdown 可读，建议限制：

- 命令只在行首识别。
- 命令只在任务正文开始前识别。
- 不引入缩进块或 `/end`。
- 不支持任意表达式。
- 显式列表只支持简单 token，复杂文本用 `/let` 命名。
- 条件是自然语言字符串，不是布尔表达式。

这些限制让 DSL 更像“可执行操作手册”，而不是脚本语言。

## 与 v1 实现的关系

当前实现已经具备这些基础：

- 任务块。
- `/resume`、`/args`、`/go`、`/wait`。
- `/for 3 until ...`、`/for dir`、`/for path`、`/for name in [...]`。
- `/let` 和 `{{name}}` 渲染。
- done/running marker。

还没有完全实现的目标模型：

- Markdown 标题作用域。
- 命令作为通用组合操作符的完整 IR。
- 真正的嵌套执行树。
- `atm plan` dry-run。
- 嵌套状态恢复格式。

后续演进应优先补 IR 和 plan，再扩展嵌套执行。否则直接在现有执行器上继续叠语法，会很快让状态恢复和错误信息变得不可控。

## Run 与 Watch 模式

DSL 需要区分一次性批处理和协作式持续执行。

### Run 模式

`atm run file.md` 表示执行启动时文档快照中的任务。

启动时先扫描整篇文档：

```txt
initialHasRunMarkers = document contains /run outside code blocks
```

然后本次 run 的策略被冻结：

- 如果启动快照中没有任何 `/run`，执行启动快照里所有可执行 task section。
- 如果启动快照中至少有一个 `/run`，只执行启动快照里带 `/run` 的 task section。
- 启动后的编辑、插入或 append 不改变本次 run 的执行策略。
- 启动后的新 task section 不进入本次 run 队列；它们属于下一次 run 或 watch。

这保证 run 模式是可预测的批处理：它跑的是“启动时这份文档”的计划，而不是运行过程中不断变化的计划。

### Watch 模式

`atm watch file.md` 表示用户和 agent 持续协作。

watch 模式始终采用显式门控：

- 默认不执行任何 task section。
- 只有带 `/run` 的 sealed task section 才进入执行队列。
- 新增或 append 的 `/run` section 可以被持续发现并执行。
- 没有 `/run` 的 section 视为草稿、说明或待办想法。

watch 模式适合用户边写文档、边让 agent 执行已经确认的部分。

## `/run` 门控

`/run` 表示“这个 task section 可以被执行”。它是门控元数据，不是流程控制操作符，不参与命令组合树。

```md
## Fix Tests

/run
/for 3 until tests pass
Run tests and fix failures.
```

等价于：

```txt
Gate(run=true)
For(3, until="tests pass",
  Run(prompt))
```

不应解释为：

```txt
Run(For(...))
```

因此：

```txt
/run /for 3 /go
```

只表示当前 section 被授权执行，计划仍是：

```txt
For(3, Go(Run(prompt)))
```

### `/run` 对文档的影响

一旦文档中出现 `/run`，说明作者选择了显式门控语义。

在 run 模式中：

```txt
if document has any /run at startup:
  execute only /run sections from startup snapshot
else:
  execute all executable sections from startup snapshot
```

在 watch 模式中：

```txt
always execute only /run sections
```

### `/run` 与 report

执行完成后不应自动删除 `/run`。是否再次执行由 report、prompt hash 和用户显式操作共同决定。

建议规则：

- `/run` + no report：执行。
- `/run` + running report：跳过。
- `/run` + done report + same prompt hash：跳过。
- `/run` + failed report + same prompt hash：默认跳过，除非用户清理 report 或使用未来的 `/rerun`。
- `/run` + done report + changed prompt hash：默认不自动重跑，提示用户确认；未来可用 `/rerun` 表达新版本执行。

这样 `/run` 是授权标记，不是每次扫描都重复执行的触发器。

## 增量编辑

协作式文档必须允许用户在 agent 工作时编辑。

建议规则：

- ATM 只拥有 `atm:report` 报告块和旁路 `.atm/` 状态/报告文件。
- 用户正文归用户所有，ATM 不应重排或改写用户正文。
- 执行任务时使用启动该任务时的 section 快照。
- 写回 report 前重新读取文档，通过 report id、section id 或标题锚点定位。
- 如果当前 section 内容 hash 与启动快照不同，报告应标记为“针对旧快照完成”，不能把新内容标为 done。
- 如果 section 被删除，详细报告写入 `.atm/reports/`，主文档不再写回，命令行提示 orphan report。

run 模式下，增量编辑不影响本次任务队列。watch 模式下，增量编辑可以新增未来任务，但仍需 `/run` 门控。

## 设计检查清单

新增 DSL 特性前，应回答：

1. 它能否表示成 IR 节点或已有节点参数？
2. 它是否仍然能 dry-run 成用户可读的计划？
3. 它是否需要新的状态恢复字段？
4. 它是否会让 Markdown 正文变得不自然？
5. 它是否能用自然语言 prompt 或外部脚本解决？
6. 它是否绑定某个具体工具？

如果答案不清楚，就先不要加语法。
