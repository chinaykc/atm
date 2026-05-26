# ATM 用户手册

ATM 是 **Agent Task Markdown**：一种基于 Markdown 的 Agent 任务调度 DSL（领域专用语言）。普通 Markdown 用来承载背景、计划和说明，`/task`、task header 命令与斜杠控制命令用来声明可执行任务、循环、并行、条件、结构化输出和复用定义。

这是一份面向日常使用者的 ATM 手册。它从第一个 `todo.txt` 开始，逐步介绍任务边界、并行、循环、结构化输出、可复用定义、数据库黑板、产物追踪和发布协作 runbook，最后给出命令和 CLI 参考。

## 目录

1. [快速开始](01-getting-started.md)
2. [Todo 文件与任务边界](02-todo-format.md)
3. [执行流程：循环、并行与工作池](03-workflows.md)
4. [复用任务：定义、调用、返回值与导入](04-reuse.md)
5. [结构化输出、状态块与产物目录](05-results.md)
6. [任务数据库：记忆、黑板与权限](06-databases.md)
7. [设计模式与完整例子](07-patterns.md)
8. [命令手册](reference/commands.md)
9. [命令行手册](reference/cli.md)
10. [环境变量](reference/environment.md)

## ATM 适合什么

ATM 是一个“Markdown 文件即执行队列”的 Agent 任务调度工具。你把任务写成普通文本或 Markdown，ATM 按顺序交给 Codex/Claude 执行，并把完成状态写回同一个文件。

```mermaid
flowchart LR
  A[todo.txt / todo.md] --> B[atm run]
  B --> C{解析任务}
  C --> D[Codex / Claude]
  D --> E[写回 > [!ATM] 状态块]
  D --> F[保存 JSONL 事件流和 result.md]
```

## 最小心智模型

| 概念 | 你需要记住的规则 |
| --- | --- |
| 任务块 | `/task`、任务启动控制命令，或带 prompt 的 task header 命令开始的一段 prompt |
| 命令 | 写在任务开头，影响这个任务怎么执行 |
| `/go` | 后台启动任务，继续扫描后续任务 |
| `/wait` | 等待此前启动的后台任务 |
| `/for` | 循环、重试，或通过 `files()` / `dirs()` / 列表逐项执行 |
| `/output` | 保存文本结果或结构化 JSON |
| `/db` | 给 agent 暴露本地 JSON 数据库，作为记忆或黑板 |
| `/def` + `/call` | 定义可复用任务模板，并在需要时调用 |

建议先读 [快速开始](01-getting-started.md)，再按你的使用场景跳到对应章节。
