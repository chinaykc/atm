# 简单发布检查

## 说明

/doc
```md
这个例子展示变量、bash 捕获、并行分支、`/wait` 和 `until` 重试循环，不依赖其他文件。

运行：`atm run -file examples/simple.zh-CN.todo.md -output .atm/simple-example`。
```

## 准备

/let release simple-release
/let validation go test ./... && go vet ./...
/let diffstat /bash <<'SH'
git diff --stat -- . 2>/dev/null || true
SH

## 发布简报

/task
为 {{release}} 准备一份简短发布简报。

变更文件：

{{diffstat}}

把这个验证命令作为发布门禁：{{validation}}

## 并行审查

/for area in [code tests docs] /go
审查 {{release}} 的 {{area}} 部分。报告具体缺陷、缺失测试和会阻塞发布的模糊点。建议的修复要保持小范围。

/go /for 2
挑战发布计划。第 {{n}} 轮关注不同类型的失败模式。

/wait

汇总并行发现，并只应用最小安全修复。

## 验证循环

/bash <<'SH'
go test ./... || true
go vet ./... || true
SH
/for 3 until {{validation}} passes
运行 {{validation}}。直接修复失败，然后说明改了什么以及还剩什么风险。

## 发布说明

/resume
为 {{release}} 写最终内部发布说明。

包含：

- 变更内容
- 验证结果
- 回滚步骤
- 未解决风险
