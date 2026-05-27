# 5. 结构化输出、状态块与产物目录

ATM 的输出分几类：托管运行目录里的状态结果文档、每个任务自己的产物目录、`/output` 生成的文本或结构化 JSON 产物、`persist:run` 的 `/db` 数据文件，以及工作副本旁的 `.atm/state.json`。直接 `run` 默认不把状态写回源文件。

运行时控制台会从 Codex/Claude 的结构化事件流中展示当前任务行号范围、工具调用名和 assistant 消息；原始 JSONL 仍会完整保存到 output 目录。ATM 自己提供的 任务工具会显示为 `[output]`、`[check]` 或 `[db]`，而不是混进普通 agent tool 日志。

## 状态块

执行完成后，ATM 会在 `~/.atm/runs/<run-id>/result.todo.md` 的任务后追加 Markdown 引用块：

```txt
运行测试并修复失败。
<!-- atm:report v=2 id=run-tests-and-fix-failures-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/run-tests-and-fix-failures-6f2d9c8a41/report.md status=done -->
> [!ATM]
> status: done
> started: 2026-05-21 10:00
> finished: 2026-05-21 10:03
> duration: 3m
> runs: 1x
> id: run-tests-and-fix-failures-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/run-tests-and-fix-failures-6f2d9c8a41/report.md
>
> messages:
> - assistant (codex):
>   测试已经通过。
<!-- /atm:report -->
```

运行中断或循环未完成时，会写入 running 状态：

```txt
<!-- atm:report v=2 id=run-tests-and-fix-failures-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/run-tests-and-fix-failures-6f2d9c8a41/report.md status=running -->
> [!ATM]
> status: running
> started: 2026-05-21 10:00
> step: 1
> step-runs: 1x
> total-runs: 1x
> id: run-tests-and-fix-failures-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/run-tests-and-fix-failures-6f2d9c8a41/report.md
<!-- /atm:report -->
```

任务失败时，会写入 failed 状态：

```txt
<!-- atm:report v=2 id=run-tests-and-fix-failures-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/run-tests-and-fix-failures-6f2d9c8a41/report.md status=failed -->
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
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/run-tests-and-fix-failures-6f2d9c8a41/report.md
<!-- /atm:report -->
```

`failed` 是该运行目录里的终端生成状态。可以用 `atm resume <run-id>` 继续该工作副本，或修改保持不变的源文件后重新执行 `atm run`。

ATM 使用 block lease 避免错位覆盖：如果工作副本中的任务正文被编辑，返回结果会被视为 obsolete，调度器重新扫描。生成的状态块由 HTML comment 边界包住，并写入 `id`、`source`、`rendered` 和 `report`：`id` 是任务/report 的稳定身份，`source` 是写入状态时任务源文本的 `sha256`，`rendered` 是实际发送给 agent 的 prompt hash，`report` 指向任务目录里的详细报告。如果同一文档里出现重复 `id`，`atm check` 和执行前解析会报错；复制了带 report block 的任务后，可以用 `atm clean --repair-ids result.todo.md` 给重复项重新生成唯一身份。

结果文档只保留轻量摘要。任务完成后，ATM 会在 `~/.atm/runs/<run-id>/tasks/<task-id>/report.md` 写入详细报告，记录状态、source hash、rendered prompt hash、plan hash、运行次数、output 目录和最近 assistant 消息，便于后续审计。

## 最近消息

默认每个执行分支保留最近 1 条 assistant 消息。调整：

```sh
atm run todo.txt -messages 3
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
~/.atm/runs/<run-id>/outputs/release-gate.json
```

如果没有写文件名，ATM 会生成带时间的文件名，并在日志中报告。

## 任务数据库 `/db`

`persist:run` 的数据库会写入本次 output 目录：

```txt
~/.atm/runs/<run-id>/outputs/db/review_board.json
```

`persist:project` 的数据库会写入项目目录：

```txt
~/.atm/runs/<run-id>/work/.atm/db/release_memory.json
```

DB 文件是普通 JSON，形状是 `map[string][]string`。它们不是 `> [!ATM]` 状态块的一部分；任务完成状态记录到结果文档，DB 内容由 任务工具单独读写。

## 产物目录

默认目录：

```txt
~/.atm/runs/<run-id>/
```

指定目录：

```sh
atm run todo.txt -output .atm/checkout-launch
```

目录结构：

```txt
~/.atm/runs/<run-id>/
  manifest.json
  sources/
    todo.txt
    imported-file.todo.md
  result.todo.md
  work/
    todo.txt
    imported-file.todo.md
  outputs/
    release-gate.json
    db/
      review_board.json
    result.md
  tasks/
    run-tests-and-fix-failures-6f2d9c8a41/
      report.md
      task-001-run-001-codex.jsonl
      logs/
        task-001-1676374790.log
  work/.atm/
    state.json
```

| 文件 | 用途 |
| --- | --- |
| `tasks/<task-id>/task-NNN-run-NNN-TOOL[-BRANCH].jsonl` | agent 自己产生的原生结构化事件流 |
| `*.json` | `/output` 结构化结果 |
| `db/*.json` | `persist:run` 的 `/db` 数据 |
| `sources/...` | 执行开始前捕获的源文件和 `/import` 文件副本 |
| `work/...` | ATM 实际执行的工作副本，import 路径已改写到运行目录内 |
| `result.todo.md` | 执行结束时 todo 文档快照 |
| `work/.atm/state.json` | 按 task id 记录 status、source/rendered prompt hash、plan hash、report、日志路径和运行次数 |
| `tasks/<task-id>/report.md` | 每个任务的详细 Markdown 报告 |
| `tasks/<task-id>/logs/task-NNN-*.log` | 人类可读 stdout/stderr 合并日志 |
| `.atm/lock` | 短生命周期本地写锁，用于串行化同一文档的本地写入 |

`state.json` 写在托管工作副本旁；每个任务的报告、日志和原生事件流写入 `tasks/<task-id>/`。`-output DIR` 只控制结构化输出、run-local DB 和 output `result.md`；源备份、import 备份、任务目录、manifest 和 `result.todo.md` 仍保存在 `~/.atm/runs/<run-id>/`。

`sourcePromptHash` 基于任务源文本和会进入 prompt 的可见 Markdown 上下文，用来判断任务来源是否变化；显式 `/context` 引用会计入，`/doc` 排除的文本和 child task 运行结果不会计入。`renderedPromptHash` 基于实际发给 agent 的 prompt，包含变量、lazy provider 展开结果和可见 child task report 摘要；`planHash` 基于任务控制流、输出和资源配置形状。三者分开记录，方便区分“文档改了”“渲染输入变了”和“执行计划变了”。

如果任务运行期间工作副本中的对应 task block 被删除或改到 lease 失效，ATM 不会把完成结果写回该 block，但会继续写任务目录中的 `report.md`，在 `.atm/state.json` 中记录 `"orphan": true`，并在命令行输出 orphan 提示。这样 agent 已完成的结果不会丢失，源文件也不会被错误标记为完成。

`atm check` 会把这些审计文件纳入 warning 诊断：主文档 report 指向的 detail report 不存在、state 中的 task id 在主文档里找不到、主文档与 state 的 status/report 路径/source hash/rendered prompt hash 不一致、任务目录下存在没有主文档或 state 引用的 orphan detail report，都会显示 warning。warning 不阻止 `check` 通过，但提示需要人工确认或清理。

需要快速查看当前协作状态时，可以使用：

```sh
atm report
```

`atm report` 不运行 agent，也不会修改文件。默认读取当前项目最新 run，也可以传 run id、`--project` 或 `--source`。它会汇总结果文档状态块、`.atm/state.json` 和任务详细报告，显示 `done`、`running`、`failed`、`skipped`、`draft` 数量，并列出失败任务、orphan report 和最近日志路径。加 `-json` 可以得到适合工具消费的结构化摘要。

```mermaid
flowchart LR
  A[Agent stream] --> B[task-run jsonl]
  A --> C[recent messages]
  C --> D[todo > [!ATM]]
  E[/output 任务工具] --> F[structured json]
  H[/db 任务工具] --> I[db json]
  D --> G[result.todo.md]
```

## 清理状态

只清理结果文档里的生成状态块，保留运行目录里的审计文件：

```sh
atm clean result.todo.md
```

显式清理审计产物：

```sh
atm clean result.todo.md --reports
atm clean result.todo.md --state
atm clean result.todo.md --logs
atm clean result.todo.md --all
```

`--all` 会同时移除目标文档生成状态块、`.atm/reports/`、`.atm/state.json` 和 `.atm/logs/`。这些命令不删除用户手写正文。

修复重复 report id 但保留审计文件：

```sh
atm clean --repair-ids result.todo.md
```

`--repair-ids` 只改目标文档中的重复 report identity。它不会删除 state 或 detail report；修复后继续用 `atm check` 或 `atm report` 查看是否还有 stale state、缺失 detail report 或 orphan report warning。
