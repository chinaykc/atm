/import complex-shared.todo.md

/pool reviewer 4 12
/pool tester 2 6

/db new review_board scope:global persist:run access:append
本次运行内的审查黑板。并行 reviewer 追加发现；协调任务读取并合并它们。

# 复杂切换运行手册

## 说明

/doc
```md
这个例子把较完整的工作流保留在一个可运行文件里，并额外 import 一个共享定义文件。它覆盖 import、可复用定义、pool、结构化输出、表达式门禁、DB 黑板、并行审查和验证循环。

运行：`atm run -file examples/complex.zh-CN.todo.md -output .atm/complex-example`。
```

## 准备

/let release complex-cutover
/let service payments-ledger
/let owner settlement-platform
/let validation go test ./... && go vet ./...
/let branch /bash git rev-parse --abbrev-ref HEAD 2>/dev/null || printf unknown
/let diffstat /bash <<'SH'
git diff --stat -- . 2>/dev/null || true
SH

## 运行态势

/args -c model_reasoning_effort="high"
/let service_map /call service_map {{service}}
/let rollback_signal /call rollback_signal

为分支 {{branch}} 上的 {{release}} 创建切换态势说明。

服务依赖图：
{{service_map}}

回滚信号：
{{rollback_signal}}

当前 diffstat：
{{diffstat}}

指出幂等风险、队列延迟风险、对账漂移、客服准备度和回滚负责人。

## 门禁快照

/resume
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

评估 {{release}} 是否可以继续。

必须检查：

- 事件重放不会重复入账
- 结算总额可以和上一批次对比
- 客服已有处理重复扣款问题的对外话术
- 回滚可以关闭 writer 且不丢失排队事件

返回结构化门禁决策。

## 门禁分流

/if (exist(outputDir("gate-{{release}}.json")) && json(open(outputDir("gate-{{release}}.json"))).passed)
为 {{release}} 准备发布经理交接说明。包含门禁原因、回滚信号和验证命令：{{validation}}

/else
为 {{release}} 写发布暂停说明。把每个 open issue 分配给 engineering、support、finance 或 observability。

## 审查黑板

/for area in [stream-writer ledger-posting support-playbook observability rollback] /go reviewer
/db use review_board access:append
审查 {{release}} 的 {{area}} 部分。
向 `review_board` 追加一个具体风险和一个最小安全缓解措施。

/go reviewer /for 2
/db use review_board access:append
从事故复盘视角挑战发布决策。第 {{n}} 轮必须关注不同失败类型，并把发现追加到 `review_board`。

/wait reviewer

/db use review_board access:read
把 `review_board` 发现合并为一份按优先级排序的切换风险列表。可用时保留精确文件路径和命令。

## 验证与修复

/bash <<'SH'
mkdir -p artifacts
go test ./... > artifacts/go-test.log 2>&1 || true
go vet ./... > artifacts/go-vet.log 2>&1 || true
SH

/for 4 until(exist("artifacts/validation-summary.json") && json(open("artifacts/validation-summary.json")).passed)
运行 {{validation}} 并修复阻塞 {{release}} 的失败。
每轮之后更新 artifacts/validation-summary.json，包含 passed、failed_command、changed_files 和 remaining_risk。

## 对账演练

/for scenario in [idempotent-replay queue-lag rollback-freeze] /go tester
运行 {{release}} 的 {{scenario}} 验证演练。使用 artifacts/go-test.log 和 artifacts/go-vet.log 作为证据输入。返回精确证据缺口和复现命令。

/wait tester

关闭剩余财务对账证据缺口，并从 artifacts/validation-summary.json 链接精确产物路径。

## 运行手册加固

/let stream_writer_runbook /call runbook_patch stream-writer
/let support_runbook /call runbook_patch support-playbook

把这些更新合入发布运行手册。

Stream writer：
{{stream_writer_runbook}}

Support：
{{support_runbook}}

## 切换记录

/task
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

为 {{release}} 写最终切换记录。

包含变更内容、为什么门禁决策可接受、验证证据、回滚触发条件和负责人、客服升级路径，以及不能阻塞当前窗口的后续工作。
