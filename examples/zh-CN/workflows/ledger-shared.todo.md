# 台账共享工作流

用于结算平台发布工作的可复用任务定义。

## /def service_map service

为 {{service}} 生成简洁依赖图。

包含上游事件生产者、下游台账消费者、仪表盘、队列、功能开关和回滚面。

/return {{agent.last_message}}

## /def incident_commander release owner

为 {{owner}} 负责的 {{release}} 编写现场指挥 briefing。

说明主要决策人、回滚负责人、客服联络人、财务联络人，以及重复扣款告警升高时最先执行的三个检查。

/return {{agent.last_message}}

## //def runbook_patch area

/pool runbook 2 4

/go runbook
审查当前 {{area}} 运行手册，找出操作歧义、缺失命令、过期告警名和回滚时机缺口。

/go runbook
从客服升级视角审查当前 {{area}} 运行手册。找出结算窗口期间可能让用户困惑的对外措辞。

/wait runbook

/return
{{area}} 运行手册补丁：
{{agent.last_message}}

## //def reviewer_pack area severity

/pool reviewer 2 4

/for lens in [code-tests observability docs rollback] /go reviewer
从 {{lens}} 视角审查 {{area}}，严重级别 {{severity}}。

返回一个具体发布风险和一个最小安全缓解措施。

/wait reviewer

/return
{{area}} 的 {{severity}} 级审查包：
{{agent.messages}}
