# 结账可靠性发布

这个队列模拟一次真实的结账可靠性改造发布前检查。产品目标是减少重复扣款、保证订单创建幂等，并交付支持工程师能在灰度期间使用的说明。

需要可追查产物时，建议指定输出目录运行：

```sh
atm run -file examples/zh-CN/checkout-reliability-launch.todo.md -output .atm/checkout-reliability-launch
```

## //发布上下文

<!-- task-list section 里的 HTML 注释会被忽略。 -->

/let release checkout-reliability-2026-05-18

/let changed /bash <<'SH'
git diff --stat -- .
SH

/let validation go test ./... && go vet ./...

## /风险简报
/args -c model_reasoning_effort="high"

为 {{var "release"}} 准备一份发布风险简报。

变更文件概览：

{{index .Vars "changed"}}

重点关注支付幂等、订单状态流转、可观测性、回滚安全性，以及客服支持影响。

{{if .validation}}把这个验证命令作为发布门禁：{{.validation}}{{end}}

## //并行工程审查

/for area in [payments orders observability docs] /go
审查 {{release}} 的 {{area}} 分片。报告具体缺陷、缺失测试和会阻塞发布的歧义。不要做大范围重构。

/go /for 2
独立质疑结账灰度方案。第 {{N}} 轮应该寻找和上一轮不同类型的失败模式。

/wait

汇总并行审查发现，并只做最小安全修复。

## //验证闭环

/bash <<'SH'
go test ./... || true
go vet ./... || true
SH
/for 3 until {{validation}} 通过
运行 {{validation}}。直接修复失败，然后说明改了什么以及还剩哪些风险。

## /发布说明
/resume

编写 {{release}} 的最终内部发布说明。

需要包含：

- 变更内容
- 如何避免重复扣款
- 精确的验证结果
- 回滚步骤
- 发布后客服应该关注什么

不要隐藏未解决风险，要明确保留。

## 归档备注

这个普通 Markdown section 会作为说明保留，不会执行。生成的 `> [!ATM]` 结果块和输出目录里的 agent 原生 JSONL 流，可以在 `untag` 后继续追查本次运行。
