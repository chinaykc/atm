# Web 黑盒漏洞扫描队列

这个队列只能用于已授权的 Web 目标。运行前先编辑 scope 变量；默认值故意是占位符。

先看执行计划：

```sh
atm plan -file examples/zh-CN/blackbox-web-security-scan.todo.md
```

再用独立产物目录运行：

```sh
atm run -file examples/zh-CN/blackbox-web-security-scan.todo.md -output .atm/blackbox-web-security
```

这个示例把黑盒安全测试拆成 scope 门禁、低影响攻击面清点、并行 surface 检查、证据分诊、最小复核和最终报告。它不会给未知目标授予扫描权限。

## //scope setup

/let target_base_url https://staging.example.test
/let allowed_hosts staging.example.test api.staging.example.test
/let account_rule 只使用本次评估提供的测试账号。没有账号时只做未登录路径。
/let rate_rule 先按人工节奏低频请求。除非书面 scope 明确允许，否则不要跑高并发爬取、暴力枚举或口令猜测。
/let forbidden_actions 不改动类生产数据，不触发真实支付或通知，不探测内网地址，不越过 host allowlist，不上传有害文件。

/pool surface 3 8
/pool verify 2 6

/db new scan_board scope:global persist:run access:append
本次 run 的安全测试黑板。把精简证据追加到 inventory/<kind>、coverage/<focus>、hypotheses/<focus>、findings/<focus>、verification/confirmed 和 verification/rejected。证据要可复现，但不要存储 secret。

## /def testing_guardrails target

/return
已授权黑盒目标：{{target}}。
允许 host：{{allowed_hosts}}。
账号规则：{{account_rule}}
速率规则：{{rate_rule}}
禁止动作：{{forbidden_actions}}
除非评估负责人书面调整 scope，否则源代码、构建文件、基础设施控制台、日志和数据库都不在范围内。
优先使用能说明观察结果的最小请求。没有可复现证据和影响论证时，不要把一个现象定性为漏洞。

## /scope gate

/let guardrails /call testing_guardrails {{target_base_url}}
使用这些 guardrails：

{{guardrails}}

在任何主动测试之前，先判断本次评估是否 ready：

- 确认目标不再是占位符，并且允许的 host 范围清楚。
- 检查目标 origin 可达性时，最多做低影响 baseline 请求。
- 列出缺失的测试账号、角色、测试数据、速率限制和停止条件。
- 如果 scope 还不完整，把 `ready` 设为 false，并说明阻塞点。

/output scope-card
```yaml
type: object
additionalProperties: false
required:
  - ready
  - target
  - in_scope_hosts
  - allowed_modes
  - blocked_questions
  - baseline_observations
properties:
  ready:
    type: boolean
  target:
    type: string
  in_scope_hosts:
    type: array
    items:
      type: string
  allowed_modes:
    type: array
    items:
      type: string
  blocked_questions:
    type: array
    items:
      type: string
  baseline_observations:
    type: array
    items:
      type: string
```

## //authorized black-box gate

/if (existsOutput("scope-card.json") && jsonOutput("scope-card.json").ready)

## /surface inventory

/let guardrails /call testing_guardrails {{target_base_url}}
为 {{target_base_url}} 建立低影响黑盒攻击面清单。

使用这些 guardrails：

{{guardrails}}

只基于外部可观察行为工作：

- 记录 landing page、跳转、公开 robots 或 sitemap 线索、公开 API 入口、表单、上传边界、会改变状态的动作、浏览器存储信号、cookie、安全 header 和认证入口。
- 先用正常浏览和低频请求。如果已安装扫描器且 scope 允许，只使用 allowlist 加限速的 passive 或 low-risk 模式，并记录精确模式。
- 把精简 inventory 证据和 coverage 备注追加到 `scan_board`。
- 已知时把计划中的检查映射到 OWASP WSTG 分类或 ID；不要编造 ID。

/output surface-map
```yaml
type: object
additionalProperties: false
required:
  - target
  - entry_points
  - auth_contexts
  - data_boundaries
  - planned_focus
  - inventory_gaps
properties:
  target:
    type: string
  entry_points:
    type: array
    items:
      type: string
  auth_contexts:
    type: array
    items:
      type: string
  data_boundaries:
    type: array
    items:
      type: string
  planned_focus:
    type: array
    items:
      type: string
  inventory_gaps:
    type: array
    items:
      type: string
```

## /parallel surface checks

/for focus in [information-exposure auth-session access-control input-boundaries upload-browser api-misconfiguration] /go surface
/let guardrails /call testing_guardrails {{target_base_url}}
基于已有黑盒 inventory，测试 {{target_base_url}} 的 {{focus}} surface。

使用这些 guardrails：

{{guardrails}}

对这个 surface：

- 读取 `surface-map.json`，只使用观察到的 entry point 或明确 allowlist 内的 host。
- 优先使用安全 marker、角色对比、header 与 cookie 观察、正常导航和单请求边界探测。
- 做授权检查时，只比较本次评估提供的测试账号和角色；不要访问真实用户数据。
- 扫描器告警或单纯响应差异不能直接算 confirmed issue。记录最小请求、观察到的响应、前置条件、影响假设和置信度。
- 把覆盖情况追加到 `coverage/{{focus}}`，把待证假设追加到 `hypotheses/{{focus}}`，把有证据的候选发现追加到 `findings/{{focus}}`。

## /surface triage

/wait surface

/db access scan_board read
读取 `surface-map.json`，并用 `coverage/**`、`hypotheses/**`、`findings/**` 扫描 `scan_board`。

分诊黑盒证据：

- 把看起来可确认的候选项和未验证假设分开。
- 只有能用 least-harm 复现步骤复核的候选项才能进入 `retest_queue`。
- 当 scope、账号或目标行为挡住检查时，保留 coverage gap。

/output triage
```yaml
type: object
additionalProperties: false
required:
  - retest_queue
  - likely_findings
  - hypotheses
  - coverage_gaps
  - stop_conditions
properties:
  retest_queue:
    type: array
    items:
      type: string
  likely_findings:
    type: array
    items:
      type: string
  hypotheses:
    type: array
    items:
      type: string
  coverage_gaps:
    type: array
    items:
      type: string
  stop_conditions:
    type: array
    items:
      type: string
```

## /minimal candidate retests

/for candidate in(jsonOutput("triage.json").retest_queue) /go verify
/let guardrails /call testing_guardrails {{target_base_url}}
用仍在 scope 内的最小黑盒证明复核这个 candidate 一次：

{{candidate}}

使用这些 guardrails：

{{guardrails}}

把精简结论追加到 `scan_board` 的 `verification/confirmed` 或 `verification/rejected`。写清前置条件、endpoint、安全证明步骤、观察结果，以及这个结果为什么改变置信度。

## /black-box report

/wait verify

/db access scan_board read
为 {{target_base_url}} 准备最终黑盒安全测试报告。

使用 `scope-card.json`、`surface-map.json`、`triage.json` 和 `scan_board` 中的 verification 记录。

报告必须包含：

- 实际使用的 scope 与 guardrails
- 方法和主要覆盖面
- 已确认发现的最小复现证据与影响
- 未验证假设，以及为什么仍未验证
- coverage gap、停止条件和残余风险
- 下一步修复或复测动作

不要隐藏不确定性。不要写出超出 least-harm 证据范围的利用链。

/output blackbox-report

## /scope blocked branch

/else
为评估负责人写一条简短的 scope-blocked 说明。

使用 `scope-card.json` 指出 agent 继续前还缺哪些 scope、账号、目标、速率限制或停止条件答案。

/output scope-blocked
