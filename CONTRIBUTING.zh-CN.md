# 贡献指南

[English](CONTRIBUTING.md)

ATM 刻意保持小而清晰。贡献应保持 atm 文件格式稳定，保留零额外运行时依赖，并避免加入工作流引擎级功能。

## 本地检查

提交变更前运行：

```sh
go test ./...
go vet ./...
go build ./...
```

测试套件使用假的工具可执行文件，因此不需要真实 Codex 或 Claude Code CLI。

## 变更准则

- 保持任务语义显式写在 atm 文件中。
- 优先使用标准库 API。
- 保持 Linux、macOS、Windows 的跨平台行为。
- 对解析器、标签、存储和编排变更添加聚焦测试。
- 在 `toolRunner` 契约能支持新工具且不改变 todo 语言前，不增加新的工具适配器。

## 文档检查

- CLI 参数或子命令变化时，用 `go run ./cmd/atm -h` 和相关 `go run ./cmd/atm <command> -h` 输出对照文档。
- DSL 命令变化时，在同一变更中更新 `README.md`、`README.zh-CN.md`、`docs/commands.md`、`docs/commands.zh-CN.md` 和 `docs/user/reference/commands.md`。
- 用 `go run ./cmd/atm check examples/*.todo.md` 校验可运行示例。
