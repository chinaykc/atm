# ATM v2 工具设计草案

ATM v2 的工具层负责把 Markdown DSL 文档解析、解释、执行、记录和恢复。DSL 文档定义“用户如何表达任务”，工具设计定义“ATM 如何安全、可解释、可增量地执行这些任务”。

语言层设计见 [ATM v2 DSL 设计草案](dsl.zh-CN.md)。

## 目标

- 支持 Markdown 原生协作：主文档既是操作指南，也是执行入口和轻量看板。
- 支持 run 与 watch 两种使用方式：批处理执行和持续协作执行。
- 支持可解释执行：执行前能输出计划，执行后能追踪报告。
- 支持增量编辑：用户可以在 agent 工作时继续编辑文档。
- 支持可靠恢复：中断、失败、重启后能继续或给出明确状态。
- 支持工具中立：Codex、Claude Code 或未来适配器共享同一执行模型。

## 非目标

- 不实现长期运行的远程调度服务。
- 不提供数据库依赖。
- 不替代 Git、CI、issue tracker 或项目管理系统。
- 不把 agent 输出全文写回主文档。
- 不让工具层重新定义 DSL 语法；工具只执行 DSL 解析结果。

## 核心文件布局

推荐默认目录结构：

```txt
release.md
.atm/
  state.json
  lock
  reports/
    fix-tests-7f3a.md
  logs/
    fix-tests-7f3a-run-001.log
```

职责：

- `release.md`：用户主文档，包含任务、命令和轻量 report 摘要。
- `.atm/state.json`：机器可恢复状态，记录任务 id、prompt hash、执行计划路径、变量绑定和运行状态。
- `.atm/reports/*.md`：每个 task section 的详细协作报告。
- `.atm/logs/*.log`：每次工具执行的原始 stdout/stderr。
- `.atm/lock`：协调同一文档的本地并发写入。

主文档必须保持可读；复杂机器状态优先放到 `.atm/state.json`。

## 命令行界面

建议 v2 CLI：

```sh
atm run release.md
atm watch release.md
atm plan release.md
atm check release.md
atm report release.md
atm clean release.md
```

### `atm run`

一次性执行启动时文档快照。

规则：

- 启动时解析文档并冻结任务集合。
- 启动时判断是否存在 `/run`。
- 如果没有 `/run`，执行所有可执行 task section。
- 如果存在 `/run`，只执行启动快照中带 `/run` 的 task section。
- 运行中追加的新 section 不进入本次执行。
- 运行中用户编辑不会改变本次计划，只会影响 report 写回时的 hash 判断。

### `atm watch`

持续监听文档变化，适合用户边写边执行。

规则：

- 始终只执行带 `/run` 的 sealed task section。
- 没有 `/run` 的 section 是草稿或说明。
- 新增 `/run` section 可以被发现并排队。
- 已执行且 prompt hash 未变化的 section 不重复执行。
- watch 不长期锁定文档；只在写状态和 report 时短暂加锁。

### `atm plan`

只解析，不执行。

输出内容：

- 文档标题和 task section 列表。
- 每个 task 的执行计划树。
- 循环展开摘要。
- 并发 fan-out 位置。
- 变量来源和渲染结果预览。
- 将执行/跳过的原因。

`plan` 是 v2 的关键能力。任何复杂语法都必须能被 `plan` 解释清楚。

### `atm check`

校验文档和状态，不执行 agent。

检查内容：

- DSL 语法错误。
- 未定义变量。
- 重复 task id。
- report block 结构损坏。
- state.json 与主文档不一致。
- orphan report。
- 不推荐的命令组合，例如 `/for 3 until ... /go`。

### `atm report`

汇总当前文档的协作状态。

输出内容：

- Done / Running / Failed / Draft / Skipped 数量。
- 失败任务摘要。
- orphan report 列表。
- 最近运行日志位置。

### `atm clean`

清理 ATM 生成内容。

建议选项：

```sh
atm clean release.md --reports
atm clean release.md --state
atm clean release.md --logs
atm clean release.md --all
```

默认只清理安全可重建的内容，不删除用户正文。

## 执行管线

工具层应拆成明确阶段：

```txt
Read document
Parse Markdown
Extract DSL commands
Build AST
Resolve scopes and variables
Build IR
Plan execution
Reconcile with state/report
Execute plan
Write reports/state
```

### Markdown Parse

v2 不应继续依赖空行分隔 block 作为主模型。解析器应理解：

- heading 层级。
- fenced code block。
- HTML comment。
- blockquote/callout。
- list item。

命令只在普通 Markdown 块的行首识别，代码块中的 `/for` 不识别。

### AST

AST 保留文档结构：

```txt
Document
  Section(level=1, title="Release")
    Paragraph
    Section(level=2, title="Fix Tests")
      CommandBlock
      MarkdownBody
      ReportBlock
```

AST 负责定位、作用域和写回。AST 不直接执行。

### IR

IR 是执行模型：

```txt
Run(prompt)
For(iterator, body)
Until(condition, body)
Go(body)
Wait(scope)
WithArgs(args, body)
WithResume(body)
Seq(nodes...)
```

IR 不关心 Markdown 原始排版，只保留可执行语义和 source span。

### Plan

Plan 是 IR 加上运行时上下文：

```txt
TaskPlan {
  id
  title
  sourceSpan
  promptHash
  modeDecision
  rootNode
  variables
  reportRef
}
```

Plan 是 `run/watch/plan` 共享的数据结构。

## Task Section

v2 的主要任务单位是 Markdown section。

规则：

- 一个 heading 开始一个 section。
- section 结束于下一个同级或更高级 heading。
- section 中的命令组控制该 section 的自由文本。
- report block 是该 section 的附属内容，不进入 prompt。
- 子 section 如果没有自己的可执行命令，可以作为父 section prompt 的一部分。
- 子 section 如果有自己的可执行命令，则成为独立 task section。

无标题文档可以退化为 paragraph task，但文档型协作应推荐 heading section。

## Task 身份

不能依赖 section 在文档中的位置。v2 需要稳定身份。

建议生成：

```txt
id = slug(title) + "-" + shortHash(initialPrompt)
```

第一次写 report 时固定 id：

```md
<!-- atm:report v=2 id=fix-tests-7f3a prompt=sha256:abc123 status=running report=.atm/reports/fix-tests-7f3a.md -->
> [!TIP]
> **ATM report**
> - Status: Running
> - Detail: [.atm/reports/fix-tests-7f3a.md](.atm/reports/fix-tests-7f3a.md)
<!-- /atm:report -->
```

后续定位优先级：

1. report id。
2. section heading + report adjacency。
3. prompt hash。

如果 id 重复，停止执行相关 section，并提示 `atm check` 或未来 `atm repair-ids`。

## Report 设计

主文档只放轻量引用块。

示例：

```md
<!-- atm:report v=2 id=fix-tests-7f3a prompt=sha256:abc123 status=done report=.atm/reports/fix-tests-7f3a.md -->
> [!NOTE]
> **ATM report**
> - Status: Done
> - Runs: 2 / 3
> - Duration: 4m20s
> - Result: tests pass
> - Detail: [.atm/reports/fix-tests-7f3a.md](.atm/reports/fix-tests-7f3a.md)
<!-- /atm:report -->
```

失败：

```md
<!-- atm:report v=2 id=fix-tests-7f3a prompt=sha256:abc123 status=failed report=.atm/reports/fix-tests-7f3a.md -->
> [!WARNING]
> **ATM report**
> - Status: Failed
> - Runs: 3 / 3
> - Last error: condition not satisfied
> - Detail: [.atm/reports/fix-tests-7f3a.md](.atm/reports/fix-tests-7f3a.md)
<!-- /atm:report -->
```

主文档 report 规则：

- 必须有 HTML comment 边界。
- 可见内容使用 Markdown blockquote/callout。
- ATM 可以整体替换 report block。
- report block 不进入 prompt。
- 用户删除 report 等价于请求重新评估该 section。

详细报告放 `.atm/reports/`。

建议内容：

```md
# Agent Report: Fix Tests

- Source: `release.md#fix-tests`
- Status: Done
- Prompt hash: `sha256:abc123`
- Started: ...
- Ended: ...

## Plan

For N in [1 2 3] until "tests pass"

## Runs

### Run 1

- Variables: `N=1`
- Args: `--yolo`
- Log: `../logs/fix-tests-7f3a-run-001.log`
- Check: failed

### Run 2

- Variables: `N=2`
- Log: `../logs/fix-tests-7f3a-run-002.log`
- Check: passed
```

## State 设计

`.atm/state.json` 是恢复真相来源之一，但不应取代主文档。

建议结构：

```json
{
  "version": 2,
  "document": "release.md",
  "documentHash": "sha256:...",
  "tasks": {
    "fix-tests-7f3a": {
      "status": "running",
      "promptHash": "sha256:abc123",
      "planHash": "sha256:def456",
      "startedAt": "2026-05-09T10:12:00+08:00",
      "updatedAt": "2026-05-09T10:14:00+08:00",
      "path": ["For:N=2"],
      "runs": 1,
      "report": ".atm/reports/fix-tests-7f3a.md",
      "logs": [".atm/logs/fix-tests-7f3a-run-001.log"]
    }
  }
}
```

状态写入规则：

- 每次 run 开始前写 running。
- 每次 run 完成后更新 path/runs/log。
- condition pass 后写 done。
- 失败后写 failed。
- 写主文档 report 和 state 应尽量同一锁内完成；如果不能原子跨文件，也要可通过 `atm check` 修复。

## 增量编辑策略

### 用户编辑当前任务

执行使用启动时 section 快照。

完成写回时：

- 如果当前 prompt hash 未变：正常写 done/failed report。
- 如果当前 prompt hash 已变：写 report 表明“旧快照已完成”，但不能把当前内容标记为 done。
- 详细报告仍保留旧 prompt hash。

### 用户编辑前文

不应影响正在运行任务。ATM 写回前重新扫描全文，通过 report id 或 section id 找到目标。

### 用户移动 section

如果 report 跟随 section 移动，id 不变，可以继续写回。

### 用户删除 section

任务继续完成，但不写主文档。详细报告写入 `.atm/reports/`，命令行提示 orphan report。

### 用户复制 section

如果复制了 report id，形成 duplicate id。ATM 应跳过相关任务，并提示修复。

## run/watch 执行策略

### run

run 是快照执行：

```txt
snapshot = read document at startup
hasRun = snapshot contains /run
tasks = planned tasks from snapshot
execute(tasks filtered by hasRun)
```

启动后 append 的 task 不进入本次 run。

### watch

watch 是持续执行：

```txt
loop:
  read document
  parse sealed /run sections
  reconcile with state/report
  enqueue new runnable tasks
  write reports as tasks change
```

watch 不执行没有 `/run` 的 section。

## append 行为

`atm append` 在 v2 中只负责修改文档，不直接改变当前 run 快照。

规则：

- run 模式中 append 的内容不会进入本次 run。
- watch 模式中 append 的 `/run` section 可以被发现并执行。
- append 应尽量插入到文档末尾或指定 heading 下。
- append 不应直接写 report。

建议命令：

```sh
atm append release.md --section "Fix Tests" <<'EOF'
/run
/for 3 until tests pass
Run tests and fix failures.
EOF
```

## 锁与并发写入

ATM 应使用短锁。

锁保护：

- `.atm/state.json` 写入。
- 主文档 report block 替换。
- report/log 文件路径分配。

锁不保护：

- agent 长时间运行。
- 用户编辑器打开文档。
- 文档读取和 plan 生成。

写回策略：

1. 读取最新文档。
2. 定位 report block。
3. 验证 id/hash。
4. 替换 ATM-owned 区域。
5. 原子写回。

如果验证失败，不覆盖用户正文。

## 工具适配器

工具适配器只负责执行渲染后的 prompt 和只读条件检查。

接口概念：

```txt
Execute(TaskInvocation) -> RunResult
Check(CheckInvocation) -> CheckResult
```

`TaskInvocation` 包含：

- active document path。
- rendered prompt。
- run options：resume、args、environment。
- task id。
- variables。
- output writers。

适配器不应理解 DSL。DSL 已经在工具层解析成 plan。

## Prompt 组装

执行 prompt 不应只包含当前自由文本，也不应默认包含整篇文档。

建议默认上下文：

- 文档标题和最近父级说明。
- 当前 task section 完整正文。
- 相关变量展开结果。
- 已完成前置任务的 report 摘要。
- 当前任务边界说明。

默认不包含未来任务正文，避免 agent 把当前责任推给未来任务。

如果需要未来上下文，可由 DSL 或工具选项显式请求，例如未来的 `/context plan`。

## 错误与退出码

建议：

- 0：所有计划任务完成。
- 1：执行失败。
- 2：DSL 解析或校验失败。
- 3：状态不一致，需要 `atm check`。
- 130：用户中断。

错误输出应包含：

- 文档路径。
- task id 和标题。
- 源位置。
- IR path。
- 报告和日志路径。

## 与 v1 实现的关系

v1 已有能力：

- 纯文本 task block。
- Codex/Claude 适配器。
- `/for`、`until`、`/args`、`/let` 的部分语义。
- 后台 `/go` 和 `/wait`。
- done/running tag。
- 临时 active todo 文件和本地锁。

v2 需要新增或替换：

- Markdown section parser。
- AST/IR/Plan 分层。
- `run/watch/plan/check/report/clean` 命令。
- `/run` 门控和 run 快照策略。
- `atm:report` 引用块。
- `.atm/state.json`、`.atm/reports/`、`.atm/logs/`。
- 稳定 task id 和 prompt hash。
- 嵌套执行树。
- 更细的 prompt 上下文策略。

迁移应先实现只读能力：

1. Markdown parser。
2. IR builder。
3. `atm plan`。
4. `atm check`。

确认计划输出稳定后，再替换执行器和状态格式。

## 设计检查清单

工具层新增能力前，应回答：

1. 是否能通过 `atm plan` 解释？
2. 是否会写用户正文？
3. 写入内容是否限制在 ATM-owned 区域？
4. 是否能在用户编辑前文后正确定位？
5. 是否需要更新 state schema？
6. 是否影响 run/watch 的执行策略？
7. 是否会让 agent 看到不该看到的未来任务？
8. 是否能在中断后恢复或明确失败？
