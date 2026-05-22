# 支付服务 v2.4 发布协作文档

本文件用于协调支付服务 v2.4 发布前的工程检查、修复、验证和发布说明整理。团队在协作模式下逐步给可执行章节加 `/run`，agent 只处理已经授权的任务。

协作执行：

```sh
atm plan v2/release-collaboration.zh-CN.md
atm watch v2/release-collaboration.zh-CN.md
```

一次性执行当前已授权任务：

```sh
atm run v2/release-collaboration.zh-CN.md
```

/let service payments-service
/let branch release/v2.4
/let test go test ./...
/let frontend npm --prefix web/frontend test
/let build go build -buildvcs=false ./...
/let release_goal 在 {{branch}} 分支上完成 {{service}} 的低风险 v2.4 发布准备。
/let review_policy 优先做小而清晰、便于审查的修改。除非当前任务明确要求，不要改变公开 API。

## 发布目标

{{release_goal}}

全局约束：

- 所有修改必须保持 Go 后端、React 前端和文档一致。
- 不要把后续任务当作当前任务的替代品；当前任务需要独立完成。
- 如果任务需要验证，就在当前任务内完成验证。
- 详细执行结果写入 `.atm/reports/`，主文档只保留轻量协作报告。

## 环境预检

/run
/args --yolo
/for 2 until 工作区已经可以开始发布准备
/let local_note 修改文件前先确认工具链版本。
/local_note
确认工作区是否已经适合开始发布准备。

必须检查：

- 确认 Go、Node、npm 和 git 可用。
- 运行 `{{test}}`。
- 运行 `{{frontend}}`。
- 运行 `{{build}}`。
- 如果检查因为依赖缺失失败，只报告缺失前置条件，不要做无关修改。

<!-- atm:report v=2 id=environment-preflight-a1b2 prompt=sha256:preflight status=done report=.atm/reports/environment-preflight-a1b2.md -->
> [!NOTE]
> **ATM 协作报告**
> - 状态：已完成
> - 执行次数：1 / 2
> - 结果：工作区已经可以开始发布准备
> - 详情：[.atm/reports/environment-preflight-a1b2.md](.atm/reports/environment-preflight-a1b2.md)
<!-- /atm:report -->

## 后端包级发布检查

/run
/for dir /go /for 3 until {{dir}} 没有后端发布阻塞问题
/args --yolo
{{review_policy}}

审查后端包或顶层目录 `{{dir}}` 是否存在发布阻塞问题。

范围：

- 检查该包相关测试和明显的集成点。
- 只修复明确归属 `{{dir}}` 的问题。
- 如果该目录不是后端包，在报告中说明，不做修改。
- 运行与 `{{dir}}` 相关的最小 Go 测试命令。

报告要求：

- 总结 `{{dir}}` 是否已清理完成。
- 列出执行过的命令。
- 如果做了修改，说明为什么它属于发布关键修复。

<!-- atm:report v=2 id=backend-package-review-c3d4 prompt=sha256:backend-review status=running report=.atm/reports/backend-package-review-c3d4.md -->
> [!TIP]
> **ATM 协作报告**
> - 状态：运行中
> - 计划：`For(dir) > Go > For(N=1..3 until "{{dir}} 没有后端发布阻塞问题")`
> - 当前分支：`dir=api`、`dir=store`
> - 详情：[.atm/reports/backend-package-review-c3d4.md](.atm/reports/backend-package-review-c3d4.md)
<!-- /atm:report -->

## 前端重点区域审查

/run
/for area in [checkout dashboard settings]
/go
审查 React 前端区域 `{{area}}`。

约束：

- UI 修改要尽量小，并保持与现有设计系统一致。
- 不要添加落地页或营销内容。
- 优先做聚焦的组件修复或测试修复。
- 如果存在 `{{area}}` 相关前端测试，运行该测试；否则运行 `{{frontend}}`。

报告要求：

- `{{area}}` 状态。
- 检查过的文件。
- 运行过的测试。

<!-- atm:report v=2 id=frontend-area-review-e5f6 prompt=sha256:frontend-review status=done report=.atm/reports/frontend-area-review-e5f6.md -->
> [!NOTE]
> **ATM 协作报告**
> - 状态：已完成
> - 执行次数：3 / 3
> - 结果：没有前端发布阻塞问题
> - 详情：[.atm/reports/frontend-area-review-e5f6.md](.atm/reports/frontend-area-review-e5f6.md)
<!-- /atm:report -->

## 等待并行审查

/run
/wait

等待所有后端包级审查和前端区域审查完成，然后再开始跨模块发布修复。

<!-- atm:report v=2 id=wait-parallel-reviews-7788 prompt=sha256:wait-parallel status=done report=.atm/reports/wait-parallel-reviews-7788.md -->
> [!NOTE]
> **ATM 协作报告**
> - 状态：已完成
> - 已等待：后端包级审查、前端区域审查
> - 详情：[.atm/reports/wait-parallel-reviews-7788.md](.atm/reports/wait-parallel-reviews-7788.md)
<!-- /atm:report -->

## 跨模块修复

/run
/resume /for 3 until 完整验证通过
/args --yolo
只把前面章节的报告当作历史上下文。当前任务必须独立完成。

修复前置报告中发现的跨模块发布阻塞问题。

本章节通过前必须完成的验证：

- `{{test}}`
- `{{frontend}}`
- `{{build}}`
- 文档中对 v2.4 的引用一致。

不要把验证推迟到发布说明任务。

<!-- atm:report v=2 id=cross-module-fixes-9911 prompt=sha256:cross-module status=failed report=.atm/reports/cross-module-fixes-9911.md -->
> [!WARNING]
> **ATM 协作报告**
> - 状态：失败
> - 执行次数：3 / 3
> - 最近错误：完整验证未通过；前端快照测试仍失败
> - 详情：[.atm/reports/cross-module-fixes-9911.md](.atm/reports/cross-module-fixes-9911.md)
<!-- /atm:report -->

## 发布说明草稿

这个章节还没有 `/run`，在 watch 模式中只是草稿。用户可以继续编辑，直到确认内容足够完整。

/let audience 运维同学和应用开发者

为{{audience}}起草发布说明。

需要包含：

- 升级说明。
- 已知限制。
- 验证证据。
- 详细 ATM 报告链接。

## 发布说明最终化

/run
/for 2 until 发布说明准确且完整
/let notes_target docs/releases/v2.4.md
最终整理 `{{notes_target}}`。

只把已完成报告作为证据。不要编造没有发生过的修改。

发布说明必须包含：

- 后端修复摘要。
- 前端审查摘要。
- 验证命令和结果。
- 如有未解决风险，明确列出。

<!-- atm:report v=2 id=release-notes-final-2233 prompt=sha256:release-notes status=running report=.atm/reports/release-notes-final-2233.md -->
> [!TIP]
> **ATM 协作报告**
> - 状态：运行中
> - 执行次数：1 / 2
> - 条件：发布说明准确且完整
> - 详情：[.atm/reports/release-notes-final-2233.md](.atm/reports/release-notes-final-2233.md)
<!-- /atm:report -->

## 状态文件结构

状态 JSON 不写入主文档。这里保留当前发布协作的状态快照，便于人工审阅。

```json
{
  "version": 2,
  "document": "v2/release-collaboration.zh-CN.md",
  "tasks": {
    "backend-package-review-c3d4": {
      "status": "running",
      "promptHash": "sha256:backend-review",
      "planHash": "sha256:plan-backend",
      "path": ["For:dir=api", "Go", "For:N=2"],
      "runs": 4,
      "report": ".atm/reports/backend-package-review-c3d4.md",
      "logs": [
        ".atm/logs/backend-package-review-c3d4-api-run-001.log",
        ".atm/logs/backend-package-review-c3d4-api-run-002.log"
      ]
    },
    "release-notes-final-2233": {
      "status": "running",
      "promptHash": "sha256:release-notes",
      "path": ["For:N=1"],
      "runs": 1,
      "report": ".atm/reports/release-notes-final-2233.md"
    }
  }
}
```

## 详细报告结构

详细报告写入 `.atm/reports/release-notes-final-2233.md`。这里保留当前任务的报告结构。

```md
# Agent 协作报告：发布说明最终化

- 来源：`v2/release-collaboration.zh-CN.md#发布说明最终化`
- 状态：运行中
- Prompt 哈希：`sha256:release-notes`
- 开始时间：`2026-05-09T10:12:00+08:00`

## 计划

循环执行最多 2 次，直到“发布说明准确且完整”。

## 执行

### 第 1 次执行

- 变量：`N=1`
- 日志：`../logs/release-notes-final-2233-run-001.log`
- 检查：失败
- 摘要：已补充验证证据，仍缺少未解决风险小节。
```

## 人工后续事项

这些内容没有 ATM 命令，因此不会被执行。

- 确认发布窗口。
- 通知支持团队。
- 发布后观察错误率和支付回调延迟。
