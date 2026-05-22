# 支付台账切换运行手册

五月结算窗口会把延迟扣款对账从夜间台账批处理迁移到支付事件流。发布必须保证财务台账总额稳定、客服团队有明确的回滚和答复口径，并为事后复盘留下可追踪记录。

运行时使用独立产物目录：

```sh
atm run -file ops/payment-ledger-cutover.todo.md -output .atm/payment-ledger-cutover
```

## //workspace setup

/import workflows/ledger-shared.todo.md
/import audit from workflows/audit-controls.todo.md

/pool reviewer 4 12

/pool tester 2 6

/let release payment-ledger-cutover-2026-05-22
/let service payments-ledger
/let owner settlement-platform
/let validation go test ./... && go vet ./...
/let bypass_window false
/let branch /bash git rev-parse --abbrev-ref HEAD 2>/dev/null || printf unknown
/let diffstat /bash <<'SH'
git diff --stat -- . 2>/dev/null || true
SH

## /current operating picture
/args -c model_reasoning_effort="high"

/let service_map /call service_map {{service}}
/let commander_note /call incident_commander {{release}} {{owner}}

为 {{release}} 生成当前切换态势说明。当前分支：{{branch}}。

服务依赖图：
{{service_map}}

现场指挥说明：
{{commander_note}}

当前 diffstat：
{{diffstat}}

重点说明对账漂移、幂等性、队列积压、数据回填、客服准备度和回滚责任。

## /gate snapshot
/resume
/let rollback_signal /call audit.rollback_signal

评估 {{release}} 是否可以进入五月结算切换窗口。

当前回滚信号：{{rollback_signal}}

必须检查：

- 台账事件流可以重放且不会重复入账
- 延迟扣款对账保持幂等
- 结算总额可以与夜间批处理结果对比
- 客服有处理重复扣款疑问的用户口径
- 回滚可以关闭流写入器且不丢失已排队事件

返回结构化门禁结论。

/output gate-{{release}}
```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["passed", "reason", "open_issues", "rollback_signal"],
  "properties": {
    "passed": {"type": "boolean"},
    "reason": {"type": "string"},
    "open_issues": {"type": "array", "items": {"type": "string"}},
    "rollback_signal": {"type": "string"}
  }
}
```

## //gate routing

/if (existsOutput("gate-{{release}}.json"))

/if (jsonOutput("gate-{{release}}.json").passed)
准备 {{release}} 的发布经理交接说明。
包含结构化门禁原因、当前回滚信号和精确验证命令：
{{validation}}

/else
为 {{release}} 编写发布暂停说明。
使用 gate-{{release}}.json 中的门禁原因和 open issues，并把每个阻塞项分配给 payments、ledger、support 或 observability。

/else
在做任何发布决策前重新生成 gate snapshot。
说明缺少哪个产物，以及为什么发布经理不应该继续推进。

## //parallel slice review

/for area in [stream-writer ledger-posting support-playbook observability rollback] /go reviewer
/args -c model_reasoning_effort="high"
审查 {{release}} 的 {{area}} 切片。
使用分支 {{branch}} 和下面的 diffstat：
{{diffstat}}
返回具体缺陷、缺失测试、不安全前提和最小安全修复方向。不要扩大到 {{area}} 之外。

/go reviewer /for 2
从事故复盘视角反向挑战发布决策。
第 {{N}} 轮必须聚焦不同失败类别：
1. 用户可见的重复扣款或退款疑问
2. 财务对账漂移或不可逆台账写入

/wait reviewer

合并并行审查结果，形成优先级明确的切换风险清单。保留可用的精确文件路径和命令。

## /compliance matrix
/let matrix /call audit.compliance_matrix {{release}} {{service}}

使用返回的合规矩阵：

{{matrix}}

转换为 finance、support、SRE 和 settlement engineering 的签核清单。保留未解决的控制缺口。

## //validation and repair

/bash <<'SH'
mkdir -p artifacts
go test ./... 2>&1 | tee artifacts/go-test.log
go vet ./... 2>&1 | tee artifacts/go-vet.log
SH

/for 4 until(exists("artifacts/validation-summary.json") && json("artifacts/validation-summary.json").passed)
运行 {{validation}} 并修复阻塞 {{release}} 的失败。
每一轮结束后更新 artifacts/validation-summary.json：
- passed
- failed_command
- changed_files
- remaining_risk
保持修复最小，并说明每个改动为什么是本次切换必需的。

## //结算一致性演练

/for scenario in [idempotent-replay queue-lag rollback-freeze] /go tester
运行 {{release}} 的 {{scenario}} 验证演练。
使用 artifacts/go-test.log、artifacts/go-vet.log，以及 artifacts/ 下的台账比对文件。
返回精确的证据缺口、复现命令，以及财务是否可以签核该场景。

/wait tester

/for 3 until 财务一致性证据完整、可复现，并且已从 artifacts/validation-summary.json 链接
关闭 {{release}} 剩余的财务一致性证据缺口。
每一轮结束后用精确产物路径和签核负责人更新 artifacts/validation-summary.json。

## //manager acknowledgement

/if 发布经理已经确认回滚负责人和客服升级路径
向发布经理发送 {{release}} 的最终 go/no-go 摘要。
包含：
- 门禁结论
- 验证结果
- 回滚负责人
- 客服升级路径
- 仍然可接受的残余风险

/else
为 {{owner}} 起草升级请求。
要求在继续推进前明确确认回滚责任和客服升级路径。

## //runbook hardening

/let stream_writer_runbook /call runbook_patch stream-writer
/let support_runbook /call runbook_patch support-playbook
把运行手册更新合并到 docs/runbooks/payment-ledger-cutover.md。
流写入器更新：
{{stream_writer_runbook}}
客服手册更新：
{{support_runbook}}

/for doc in [docs/runbooks/payment-ledger-cutover.md docs/support/payment-ledger-faq.md docs/ops/settlement-dashboard.md] /go reviewer
审查 {{doc}} 是否与 {{release}} 一致。
重点检查操作动作、精确命令、用户口径和回滚时间。

/wait reviewer

应用能减少操作歧义的文档审查结果。

## /cutover record
为 {{release}} 编写最终切换记录。

记录必须适合归档到事故复盘材料中，并包含：

- 变更内容
- 门禁结论为什么可以接受
- 验证证据
- 回滚触发条件和负责人
- 客服升级路径
- 不阻塞当前结算窗口的后续工作

返回结构化切换记录。

/output cutover-record
```yaml
type: object
additionalProperties: false
required:
  - release
  - decision
  - validation
  - rollback
  - support
properties:
  release:
    type: string
  decision:
    type: string
  validation:
    type: string
  rollback:
    type: string
  support:
    type: string
```


