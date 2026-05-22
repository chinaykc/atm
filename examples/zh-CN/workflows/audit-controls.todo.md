# 审计控制工作流

支付台账切换队列使用的定义。

## /def rollback_signal

/return /bash if [ -f rollback.lock ]; then printf locked; else printf clear; fi

## /def gate_snapshot release

/output gate-{{release}}
```
passed:boolean:切换是否可以继续
reason:string:给发布经理的决策摘要
open_issues:array:发布阻塞项或后续问题
rollback_signal:string:当前回滚信号
```

评估 {{release}} 的发布门禁。

使用仓库状态、变更文件、验证产物和客服准备度证据。只返回结构化门禁结论。

## /def compliance_matrix release service

/output compliance-matrix-{{release}}
```yaml
type: object
additionalProperties: false
required:
  - finance
  - support
  - sre
  - settlement
properties:
  finance:
    type: string
  support:
    type: string
  sre:
    type: string
  settlement:
    type: string
```

为 {{service}} 上的 {{release}} 准备合规矩阵。

覆盖财务对账证据、客服准备度、SRE 可观测性和结算工程回滚控制。返回结构化矩阵。
