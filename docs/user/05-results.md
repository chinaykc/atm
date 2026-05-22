# 5. 结构化输出、状态块与产物目录

ATM 的输出分几类：写回 todo 的状态块、保存在 output 目录的事件流和结果文档、`/output` 生成的结构化 JSON，以及 `persist:run` 的 `/db` 数据文件。

运行时控制台会从 Codex/Claude 的结构化事件流中展示当前任务行号范围、工具调用名和 assistant 消息；原始 JSONL 仍会完整保存到 output 目录。ATM 自己提供的 MCP 工具会显示为 `[output]`、`[check]` 或 `[db]`，而不是混进普通 agent tool 日志。

## 状态块

执行完成后，ATM 会在任务后追加 Markdown 引用块：

```txt
运行测试并修复失败。
> [!ATM]
> status: done
> started: 2026-05-21 10:00
> finished: 2026-05-21 10:03
> duration: 3m
> runs: 1x
>
> messages:
> - assistant (codex):
>   测试已经通过。
```

运行中断或循环未完成时，会写入 running 状态：

```txt
> [!ATM]
> status: running
> started: 2026-05-21 10:00
> step: 1
> step-runs: 1x
> total-runs: 1x
```

ATM 使用 block lease 避免错位覆盖：如果任务正文被用户编辑，旧任务返回时会被视为 obsolete，调度器重新扫描。

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
判断当前发布是否可以继续。

/output release-gate
```
passed:boolean:发布是否可继续
reason:string:原因
```
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
  task-001-1676374790.log
  task-001-run-001-codex.jsonl
  task-002-run-001-codex-area-api.jsonl
  db/
    review_board.json
  release-gate.json
  result.md
```

| 文件 | 用途 |
| --- | --- |
| `task-NNN-*.log` | 人类可读 stdout/stderr 合并日志 |
| `task-NNN-run-NNN-TOOL[-BRANCH].jsonl` | agent 自己产生的原生结构化事件流 |
| `*.json` | `/output` 结构化结果 |
| `db/*.json` | `persist:run` 的 `/db` 数据 |
| `result.md` | 执行结束时 todo 文档快照 |

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

移除 done/running 状态：

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
