# 示例

[English examples](../README.md)

这些文件是可以直接编辑的 todo 队列。把其中一个复制到项目中作为 `todo.txt`，调整提示词，然后运行 `atm -file todo.txt`。

## 文件

- [basic.todo.txt](basic.todo.txt)：顺序任务、resume 和固定次数循环。
- [loops.todo.txt](loops.todo.txt)：`/for`、`until` 和固定次数循环。
- [parallel.todo.txt](parallel.todo.txt)：用 `/go` 和 `/wait` 并行审查。
- [db-blackboard.todo.md](db-blackboard.todo.md)：本次 run 内的 `/db` 黑板，并行 reviewer 只追加，汇总任务只读。
- [blackbox-web-security-scan.todo.md](blackbox-web-security-scan.todo.md)：已授权 Web 黑盒安全测试队列，覆盖 scope 门禁、低影响攻击面清点、并行 surface 检查、证据分诊、复核和最终报告。
- [def-mcp-skill.todo.md](def-mcp-skill.todo.md)：挂载本地 skill，并把可复用 `/def` 暴露成 agent 可调用的 MCP 工具。
- [release-readiness.todo.txt](release-readiness.todo.txt)：紧凑的发布检查清单。
- [checkout-reliability-launch.todo.md](checkout-reliability-launch.todo.md)：一个真实感更强的 Markdown task mode 发布队列，覆盖变量、bash 捕获、Go template、并行分支、`until`、`resume` 和输出产物。
- [definition-calls.todo.md](definition-calls.todo.md)：可复用 `/def` 和 `//def`、`/call` 内联嵌入、`/return` 以及定义内局部 pool。
- [payment-ledger-cutover.todo.md](payment-ledger-cutover.todo.md)：完整的 Agent Task Markdown 运行手册，覆盖跨文件 import、pool、嵌套 `if`/`else`、自然语言判断、CEL 门禁、结构化输出、可复用定义、bash 捕获、并行审查、验证循环和产物归档。
- [workflows/](workflows/)：支付台账切换运行手册导入的定义文件。

## 推荐流程

1. 从 `basic.todo.txt` 开始。
2. 只把相互独立的审查或检查任务标记为 `/go`。
3. 当完成状态能用自然语言描述时，使用 `/for N until condition`。
4. 当独立任务需要共享记忆时使用 `/db`，并行分支优先使用 append-only 写入。
5. 当希望 agent 在大任务中自行决定何时调用可复用任务时，使用 `/mcp def use ...`。
6. 保持提示词足够小，让生成的 `> [!ATM]` 结果块仍然容易扫描。

## Skill 和 Def-MCP 冒烟验证

不启动 agent，只预览 skill + definition-MCP 示例：

```sh
go run ./cmd/atm plan -file examples/zh-CN/def-mcp-skill.todo.md
```

确认 Codex 或 Claude 已登录后运行：

```sh
go run ./cmd/atm run -file examples/zh-CN/def-mcp-skill.todo.md -tool codex
go run ./cmd/atm run -file examples/zh-CN/def-mcp-skill.todo.md -tool claude
```
