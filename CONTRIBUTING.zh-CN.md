# 贡献指南

[English](CONTRIBUTING.md)

ATM 刻意保持小而清晰。贡献应保持 todo 文件格式稳定，保留零额外运行时依赖，并避免加入工作流引擎级功能。

## 本地检查

提交变更前运行：

```sh
go test ./...
go vet ./...
go build -buildvcs=false ./...
```

测试套件使用假的工具可执行文件，因此不需要真实 Codex 或 Claude Code CLI。

`-buildvcs=false` 可以让源码快照或没有有效 VCS 元数据的目录也能通过本地检查。如果发布构建来自干净的 git checkout，并且希望写入 VCS 信息，可以省略它。

## 变更准则

- 保持任务语义显式写在 todo 文件中。
- 优先使用标准库 API。
- 保持 Linux、macOS、Windows 的跨平台行为。
- 对解析器、标签、存储和编排变更添加聚焦测试。
- 在 `toolRunner` 契约能支持新工具且不改变 todo 语言前，不增加新的工具适配器。

## 发布检查清单

1. 运行 `go test ./...`。
2. 运行 `go vet ./...`。
3. 运行 `go build -buildvcs=false ./...`。
4. 检查 `README.md`、`docs/commands.md` 和 `docs/design.md` 是否与行为一致。
5. 确认 `examples/` 下示例仍然描述当前行为。
