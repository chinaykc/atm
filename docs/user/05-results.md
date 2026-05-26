# 5. 结构化输出、状态块与产物目录

ATM 的输出分几类：写回 todo 的状态块、保存在 output 目录的事件流和结果文档、`/output` 生成的结构化 JSON、`persist:run` 的 `/db` 数据文件，以及原 todo 所在目录下的 `.atm/state.json`、`.atm/reports/` 和 `.atm/logs/` 审计文件。

运行时控制台会从 Codex/Claude 的结构化事件流中展示当前任务行号范围、工具调用名和 assistant 消息；原始 JSONL 仍会完整保存到 output 目录。ATM 自己提供的 MCP 工具会显示为 `[output]`、`[check]` 或 `[db]`，而不是混进普通 agent tool 日志。

## 状态块

执行完成后，ATM 会在任务后追加 Markdown 引用块：

```txt
运行测试并修复失败。
<!-- atm:report v=2 id=run-tests-and-fix-failures-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=.atm/reports/run-tests-and-fix-failures-6f2d9c8a41.md status=done -->
> [!ATM]
> status: done
> started: 2026-05-21 10:00
> finished: 2026-05-21 10:03
> duration: 3m
> runs: 1x
> id: run-tests-and-fix-failures-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: .atm/reports/run-tests-and-fix-failures-6f2d9c8a41.md
>
> messages:
> - assistant (codex):
>   测试已经通过。
<!-- /atm:report -->
```

运行中断或循环未完成时，会写入 running 状态：

```txt
<!-- atm:report v=2 id=run-tests-and-fix-failures-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=.atm/reports/run-tests-and-fix-failures-6f2d9c8a41.md status=running -->
> [!ATM]
> status: running
> started: 2026-05-21 10:00
> step: 1
> step-runs: 1x
> total-runs: 1x
> id: run-tests-and-fix-failures-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: .atm/reports/run-tests-and-fix-failures-6f2d9c8a41.md
<!-- /atm:report -->
```

任务失败时，会写入 failed 状态：

```txt
<!-- atm:report v=2 id=run-tests-and-fix-failures-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=.atm/reports/run-tests-and-fix-failures-6f2d9c8a41.md status=failed -->
> [!ATM]
> status: failed
> started: 2026-05-21 10:00
> finished: 2026-05-21 10:01
> duration: 1m
> runs: 0x
> error: task 1 run failed: simulated failure
> id: run-tests-and-fix-failures-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: .atm/reports/run-tests-and-fix-failures-6f2d9c8a41.md
<!-- /atm:report -->
```

`failed` 是终端生成状态。后续 `atm run` 不会把它当作待执行任务静默重跑；确认原因并修改任务后，用 `atm untag` 清掉生成状态再重新执行。

ATM 使用 block lease 避免错位覆盖：如果任务正文被用户编辑，旧任务返回时会被视为 obsolete，调度器重新扫描。当前生成的状态块由 HTML comment 边界包住，并写入 `id`、`source`、`rendered` 和 `report`：`id` 是任务/report 的稳定身份，`source` 是写入状态时任务源文本的 `sha256`，`rendered` 是实际发送给 agent 的 prompt hash，`report` 指向 `.atm/reports/` 下的详细报告。如果同一文档里出现重复 `id`，`atm check`、`atm plan` 和执行前解析会报错；复制了带 report block 的任务后，可以用 `atm repair-ids todo.txt` 给重复项重新生成唯一身份。

主文档只保留轻量摘要。任务完成后，ATM 会在原 todo 文件所在目录写入 `.atm/reports/<id>.md`，记录状态、source hash、rendered prompt hash、plan hash、运行次数、output 目录和最近 assistant 消息，便于后续审计。

## 最近消息

默认每个执行分支保留最近 1 条 assistant 消息。调整：

```sh
atm run -file todo.txt -messages 3
```

对于 `/for ... /go`，每个分支都会保留自己的最近消息：

```txt
> - assistant (codex) [area=api]:
>   API 审查完成。
> - assistant (codex) [area=docs]:
>   文档审查完成。
```

## 结构化输出 `/output`

用带 schema 的 `/output` 要求 agent 返回结构化 JSON，而不是在普通文本里猜：

````txt
/output release-gate
```
passed:boolean:发布是否可继续
reason:string:原因
```

判断当前发布是否可以继续。
````

输出文件会写到本次 output 目录：

```txt
.atm/20260521103000/release-gate.json
```

如果没有写文件名，ATM 会生成带时间的文件名，并在日志中报告。

## 任务数据库 `/db`

`persist:run` 的数据库会写入本次 output 目录：

```txt
.atm/20260521103000/db/review_board.json
```

`persist:project` 的数据库会写入项目目录：

```txt
.atm/db/release_memory.json
```

DB 文件是普通 JSON，形状是 `map[string][]string`。它们不是 `> [!ATM]` 状态块的一部分；任务完成状态只记录到 todo 文档，DB 内容由 MCP 工具单独读写。

## 产物目录

默认目录：

```txt
.atm/YYYYMMDDHHMMSS[-N]
```

指定目录：

```sh
atm run -file todo.txt -output .atm/checkout-launch
```

目录结构：

```txt
.atm/checkout-launch/
  task-001-run-001-codex.jsonl
  task-002-run-001-codex-area-api.jsonl
  db/
    review_board.json
  release-gate.json
  result.md
.atm/
  lock
  state.json
  logs/
    task-001-1676374790.log
  reports/
    run-tests-and-fix-failures-6f2d9c8a41.md
```

| 文件 | 用途 |
| --- | --- |
| `task-NNN-run-NNN-TOOL[-BRANCH].jsonl` | agent 自己产生的原生结构化事件流 |
| `*.json` | `/output` 结构化结果 |
| `db/*.json` | `persist:run` 的 `/db` 数据 |
| `result.md` | 执行结束时 todo 文档快照 |
| `.atm/state.json` | 按 task id 记录 status、source/rendered prompt hash、plan hash、report、日志路径和运行次数 |
| `.atm/reports/*.md` | 每个任务的详细 Markdown 报告 |
| `.atm/logs/task-NNN-*.log` | 人类可读 stdout/stderr 合并日志 |
| `.atm/lock` | 短生命周期本地写锁，用于串行化同一文档的本地写入 |

`state.json`、`reports/` 和 `logs/` 总是写在原 todo 文件所在目录下，不受 `-output DIR` 改变。`-output DIR` 只控制本次 run 的 JSONL、结构化输出、run-local DB 和 `result.md`。

`sourcePromptHash` 基于任务源文本和会进入 prompt 的可见 Markdown 上下文，用来判断任务来源是否变化；显式 `/context` 引用会计入，`/doc` 排除的文本和 child task 运行结果不会计入。`renderedPromptHash` 基于实际发给 agent 的 prompt，包含变量、lazy provider 展开结果和可见 child task report 摘要；`planHash` 基于任务控制流、输出和资源配置形状。三者分开记录，方便区分“文档改了”“渲染输入变了”和“执行计划变了”。

如果任务运行期间对应 task block 被用户删除或改到 lease 失效，ATM 不会把完成结果写回主文档，但会继续写 `.atm/reports/<id>.md`，在 `.atm/state.json` 中记录 `"orphan": true`，并在命令行输出 orphan 提示。这样 agent 已完成的结果不会丢失，主文档也不会被错误标记为完成。

`atm check` 会把这些审计文件纳入 warning 诊断：主文档 report 指向的 detail report 不存在、state 中的 task id 在主文档里找不到、主文档与 state 的 status/report 路径/source hash/rendered prompt hash 不一致、`.atm/reports/` 下存在没有主文档或 state 引用的 orphan detail report，都会显示 warning。warning 不阻止 `check` 通过，但提示需要人工确认或清理。

需要快速查看当前协作状态时，可以使用：

```sh
atm report -file todo.txt
```

`atm report` 不运行 agent，也不会修改文件。它会汇总主文档状态块、`.atm/state.json` 和 `.atm/reports/*.md`，显示 `done`、`running`、`failed`、`skipped`、`draft` 数量，并列出失败任务、orphan report 和最近日志路径。加 `-json` 可以得到适合工具消费的结构化摘要。

```mermaid
flowchart LR
  A[Agent stream] --> B[task-run jsonl]
  A --> C[recent messages]
  C --> D[todo > [!ATM]]
  E[/output MCP] --> F[structured json]
  H[/db MCP] --> I[db json]
  D --> G[result.md]
```

## 清理状态

只清理主文档里的生成状态块，保留 `.atm/state.json`、`.atm/reports/` 和 `.atm/logs/` 审计文件：

```sh
atm clean todo.txt
```

显式清理审计产物：

```sh
atm clean todo.txt --reports
atm clean todo.txt --state
atm clean todo.txt --logs
atm clean todo.txt --all
```

`--all` 会同时移除主文档生成状态块、`.atm/reports/`、`.atm/state.json` 和 `.atm/logs/`。这些命令不删除用户手写正文。

修复重复 report id 但保留审计文件：

```sh
atm repair-ids todo.txt
```

`repair-ids` 只改主文档中的重复 report identity。它不会删除 state 或 detail report；修复后继续用 `atm check` 或 `atm report` 查看是否还有 stale state、缺失 detail report 或 orphan report warning。

旧的 `untag` 命令仍可用于只移除 done/running 状态：

```sh
atm untag -file todo.txt
```

只移除 running：

```sh
atm untag -file todo.txt -done=false
```

只移除 done：

```sh
atm untag -file todo.txt -running=false
```

建议：先保留 output 目录，确认 `result.md` 和 JSONL 事件流足够追查，再执行 `untag`。
