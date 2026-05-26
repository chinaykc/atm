# WEB IDE Architecture Contract

本文定义 ATM WEB IDE 的架构、API、UX 和前端技术栈基线。它是后续实现、联调、测试和缺陷流转的工程合同，不是营销概要。

## 目标和非目标

WEB IDE 的目标是在浏览器中提供 ATM 项目管理、todo 文件编辑、plan 可视化、运行监控和产物浏览能力，同时保持 ATM 的核心心智模型：Markdown/txt todo 文件仍是任务源码和用户可见 API。

非目标：

- 不把任务迁移到数据库。
- 不引入常驻远程工作流系统。
- 不改变 DSL 语法、状态块格式或 CLI `run`/`plan` 行为。
- 不在首期实现多人协同编辑、远程队列、云端账号或插件市场。
- 不用 Web UI 绕过当前 `pkg/lang/compiler`、`pkg/lang/syntax`、`pkg/lang/ir`、`pkg/runtime/engine`、`pkg/runtime/store`、`pkg/integration/agent` 的职责边界。

## 为什么 todo 文件仍是源码

ATM 当前设计明确把 `todo.txt`、`todo.md` 或其他 Markdown/纯文本文件作为持久源码文件。执行完成、跳过、运行中状态通过 `> [!ATM]` 生成块写回同一文档；运行产物保存在 `.atm/YYYYMMDDHHMMSS[-N]` 或指定 output 目录。WEB IDE 必须延续这个模型，原因如下：

| 决策 | 说明 |
| --- | --- |
| todo 文件是源码 | 用户能用任意编辑器、Git diff、PR review、脚本和 CLI 直接查看、编辑和恢复任务。WEB IDE 只是一个更好的编辑和运行入口。 |
| 数据库只做索引和设置 | 项目 registry、近期运行、UI 设置可以放在 `.atm/web/*.json`。任务正文、DSL、运行状态不能只存在 DB 中。 |
| 文件模型兼容 CLI | `atm run -file todo.txt`、`atm plan -file todo.txt`、`append`、`format`、`untag` 必须继续工作；Web 运行失败后也能回到 CLI。 |
| block lease 依赖源码文本 | `pkg/runtime/store` 使用任务块索引和正文 hash 防止错位覆盖。迁移到 DB 会复制一套并发和恢复语义，增加不一致风险。 |
| active todo restore 已存在 | 运行时 `store.PrepareWorkspace` 会把源 todo 移到临时 active path，并用 marker 支持 append 和恢复。Web 后端应封装这个行为，而不是另建执行状态存储。 |
| 产物已经文件化 | JSONL、log、structured output、DB JSON、`result.md` 都在 `.atm` 下，天然适合 artifact browser。 |

后端可以缓存解析结果、plan JSON 和运行摘要，但缓存必须可丢弃。任何缓存都不得成为任务事实来源。

## 当前代码依据

| 区域 | 当前事实 | WEB IDE 约束 |
| --- | --- | --- |
| `main.go` | 根入口只调用 `cli.Run`。 | 新增 `web` 子命令仍从 `main.go` 进入，不扩大 main 职责。 |
| `pkg/app/cli` | 负责子命令解析，已有 `run`、`plan`、`append`、`format`、`untag`、`mcp`。 | `web` 只解析 server 参数并调用 Web 包；不在 CLI 层实现 HTTP 业务。 |
| `pkg/lang/compiler` | 负责源码编译、import、definition、scope 和静态校验。 | Web plan 服务复用 `CompileProgram`；React Flow 不重新解释 DSL。 |
| `pkg/lang/syntax` | 负责对外源码 AST。 | Web 编辑器、outline、lint UI 使用 `syntax.Document`，不直接消费 compiler-local AST。 |
| `pkg/lang/ir` | 负责执行模型。 | Web plan 和 run manager 传递 IR，不重新解析命令文本。 |
| `pkg/view/plan` | 负责 plan JSON 和 plan HTML。 | Web plan 服务复用现有 plan JSON 语义和图数据。 |
| `pkg/runtime/engine` | 负责 active workspace、调度、`/go`、`/wait`、输出 registry、状态块。 | Run manager 通过 engine API 执行，不 fork 一套调度器。需要为事件 broker 增加窄接口或 writer adapter。 |
| `pkg/runtime/store` | 负责锁、原子写回、active path、block lease。 | Todo 文件服务必须用 store 能力和路径 guard，不直接随意写文件。 |
| `pkg/integration/agent` | 负责 Codex/Claude/bash/MCP 适配，输出结构化事件。 | Web 只展示工具事件，不理解工具私有协议；解析继续留在 agent/engine 边界。 |
| plan HTML | 单文件 HTML 内嵌 plan data、CSS、JS，展示执行图、资源、任务、源文档。 | Web IDE 将其拆成 API data + React components；数据字段应尽量兼容现有 plan JSON 和 `buildPlanAppData` 的表达。 |
| `.atm/web/projects.json` | 已存在本地项目 registry 痕迹。 | 作为 registry 默认存储位置，但必须可重建、可校验、不能成为任务源码。 |

## 后端边界

建议新增包边界为 `pkg/web`，CLI 子命令只调用该包。具体命名可在实现阶段微调，但职责边界固定。

| 边界 | 职责 | 不负责 |
| --- | --- | --- |
| CLI `web` 子命令 | 当前实现解析 `-addr`、`-project`、`-tool`、`-codex`、`-claude`、`-jobs`、`-messages` 并创建 server；后续 release/frontend 阶段再补 `-open`、`-registry`、`-dev-assets` 等便捷参数。 | 不解析 DSL、不处理 HTTP route、不运行 agent。 |
| HTTP server | 路由、JSON 编解码、错误格式、静态资源、SSE、request id、日志、CORS 策略。 | 不保存任务源码、不绕过服务层。 |
| Project registry | 维护可打开项目列表：id、name、root、created、lastOpened。默认 `.atm/web/projects.json`；`lastTodoFile` 属于后续 UX 偏好字段，不能和当前 DTO 混用。 | 不存任务内容，不存运行状态事实。 |
| Todo file service | 安全列出、读取、保存、格式化、untag、append todo 文件；处理 active todo 映射。 | 不解释执行计划，不直接访问 registry 外路径。 |
| Plan service | 对 todo 内容调用 `dsl.CompileProgram`，返回 plan JSON、graph data、diagnostics。 | 不运行 bash/agent，不修改 todo 文件。 |
| Run manager | 创建、取消、查询 run；封装 `engine.Run`，绑定 output dir 和 context cancel。 | 不实现 DSL 调度，不修改 engine 运行语义。 |
| SSE event broker | 按 run/project 分发结构化事件；维护 replay buffer 和 last event id。 | 不持久化完整日志，不替代 artifact 文件。 |
| Artifact service | 枚举、读取、下载 `.atm` output 下的 log、JSONL、JSON、DB、`result.md`，支持范围和大小限制。 | 不执行任意路径读取，不解析大文件为内存无限对象。 |
| Settings service | 管理 UI 和本地工具设置：默认 tool、messages、jobs、theme、editor prefs。 | 不保存密钥明文，不覆盖 shell 环境策略。 |
| 静态资源嵌入 | release build 用 `go:embed` 提供 Vite `dist`；dev 可代理到 Vite server。 | 不要求用户安装 Node 才能使用 release binary。 |

### 后端数据原则

- 所有路径都以 project root 为能力边界，project root 必须是绝对路径。
- API 输入中的 file path 使用 project 相对路径，返回也使用相对路径；只有 registry 和 debug 字段可返回绝对 root。
- 路径解析必须 `filepath.Clean` 后确认仍在 project root 内。拒绝绝对 todo path、`..` 逃逸、symlink 逃逸和 Windows drive 逃逸。
- 写文件必须使用 store 的锁和原子写回策略，保留文件权限。
- 运行 output dir 默认在项目 `.atm/web-runs/<runId>`，并可配置到 project root 内的其他目录。
- Web run 必须支持 cancel。Cancel 只取消当前 run context，并等待 engine 恢复 active todo 或报告恢复失败。

### CLI `web` 参数合同

| 参数 | 状态 | 说明 |
| --- | --- | --- |
| `-addr` | 已实现 | 监听地址，默认 `127.0.0.1:0`。 |
| `-project` | 已实现 | 预注册项目 root，可重复。 |
| `-tool`、`-codex`、`-claude` | 已实现 | 作为后续 Web run 默认工具配置暴露到 `/api/config`。 |
| `-jobs`、`-messages` | 已实现 | 作为后续 Web run 默认执行参数暴露到 `/api/config`。 |
| `-open` | 计划 | release/frontend 阶段自动打开浏览器。 |
| `-registry` | 计划 | 显式 registry path；当前通过 `web.Options.RegistryPath` 支持测试和嵌入调用，CLI 尚未暴露。 |
| `-dev-assets` | 计划 | 开发阶段代理或读取 Vite assets；release binary 仍应使用 embedded static。 |
| `-host`、`-root` | 不采用为当前 CLI 主形态 | 分别由 `-addr` 和重复 `-project` 覆盖，除非后续 UX 需要别名。 |

## 前端边界

前端是一个本地 IDE，不是 landing page。首屏就是项目工作台。

| 边界 | 职责 | 不负责 |
| --- | --- | --- |
| Project shell | 顶层布局、activity bar、项目标题、全局 loading/error、route。 | 不直接调用低层 fetch，不保存业务数据。 |
| Explorer | 项目 todo 文件树、近期文件、dirty 标记、active todo 提示、产物目录入口。 | 不编辑文件内容，不解析 DSL。 |
| Editor workbench | Monaco editor、tabs、dirty buffer、保存、format、untag、append、diagnostics。 | 不实现文本编辑器核心能力。 |
| Plan graph | React Flow 渲染任务、条件、fanout、wait、资源和定义关系；联动 editor/inspector。 | 不自行解析 Markdown/DSL。 |
| Run monitor | 启动/取消 run、SSE timeline、stdout/stderr、assistant/tool events、任务状态。 | 不读取任意本地日志路径。 |
| Artifact browser | 列出 run output、预览 text/json/jsonl、下载或打开 result.md。 | 不加载超限大文件到主线程。 |
| Settings | 工具路径、默认 tool、messages、jobs、theme、editor 选项。 | 不做系统级安装或密钥管理。 |
| Command palette | 快速打开文件、run plan、start/cancel run、format、untag、open artifact。 | 不绕过权限和 dirty state 检查。 |
| Shared API client | typed fetch、错误归一化、SSE client、query keys。 | 不包含 React 组件逻辑。 |

## 前端技术栈基线

这些库是默认基线。替换必须满足“功能更完整、维护成本更低、bundle 风险可控、团队能测试”的条件，并在 PR 中更新本文。

| 库 | 使用位置 | 不造轮子的边界 | 替代条件 |
| --- | --- | --- | --- |
| Vite | 前端构建、dev server、HMR、静态资源产物。 | 不手写 bundler、dev proxy 或 TS 构建链。 | 只有当 Go embed/release 流程无法满足或需要框架级 SSR 时才评估替换。 |
| React | UI 组件模型和状态渲染。 | 不使用手写 DOM app 复刻现有 plan HTML。 | 只有团队整体迁移到其他前端框架时替换。 |
| TypeScript | API 类型、组件 props、状态模型。 | 不用裸 JS 维护复杂 API 合同。 | 不替换；生成类型可由 OpenAPI/Go schema 补强。 |
| TanStack Query | server state：projects、files、plan、runs、artifacts、settings。 | 不手写缓存、重试、失效和 loading/error 状态。 | 只有应用改为全局同步数据层且能覆盖 Query 能力时替换。 |
| Zustand | client state：layout、tabs、selection、command palette、panel visibility、unsaved buffers metadata。 | 不把 server state 放进 Zustand；不把 transient UI 状态塞到 Query。 | 若状态复杂到需要事件溯源或协同编辑再评估。 |
| Monaco Editor | todo 文件编辑、diagnostics、diff、只读 result.md、JSON preview。 | 不自研编辑器、语法高亮、undo stack、worker。 | 只有 Monaco worker/bundle 成本不可接受且功能降级被接受时换 CodeMirror。 |
| React Flow | plan graph、节点选择、边、fit view、minimap、layout 扩展。 | 不用 SVG/Canvas 手写流程图交互。 | 只有图规模或布局需求超出 React Flow 时替换。 |
| xterm.js | run monitor 中 stdout/stderr/agent stream 的终端式输出。 | 不自研 ANSI、scrollback、selection、copy。 | 如果只保留结构化 timeline 且不显示终端输出，可移除。 |
| Radix/shadcn + Tailwind | 可访问 dialog、menu、popover、tabs、tooltip、command、toast 和基础样式。 | 不自写复杂可访问交互组件；不做一套独立设计系统。 | 若项目已有成熟设计系统且覆盖 Radix 能力，可替换。 |
| lucide-react | activity bar、toolbar、文件、run、cancel、settings 等图标。 | 不手绘常见图标。 | 仅当统一品牌 icon set 可覆盖全部通用图标时替换。 |
| React Hook Form | settings dialog、run form、project add form。 | 不手写表单 dirty/validation/touched 逻辑。 | 表单数量极少且无复杂校验时可局部不用。 |
| Zod | API payload runtime validation、表单 schema、settings schema。 | 不只依赖 TypeScript 静态类型验证网络数据。 | 若后续生成 OpenAPI validator，可替换或组合。 |
| Vitest | 前端单元测试、API client、stores、纯函数。 | 不用 Jest 复制 Vite 环境。 | 只有 monorepo 统一测试平台要求时替换。 |
| Testing Library | React component 行为测试。 | 不做脆弱 DOM 结构快照作为主测试。 | 不替换，除非迁移非 React 框架。 |
| Playwright | e2e、布局、Monaco worker、SSE、run monitor、artifact preview。 | 不用手动截图验收替代自动化。 | 不替换；可补充 Go HTTP tests。 |

## HTTP API 合同

统一规则：

- Base path: 当前后端实现 `/api`，并保留 `/api/v1` 兼容别名。
- Content-Type: request/response JSON 使用 `application/json; charset=utf-8`。
- 时间使用 RFC3339。
- 成功响应外层不强制包 `data`，按资源返回清晰对象。
- 错误响应统一：

```json
{
  "code": "path_escape",
  "message": "path escapes project root",
  "detail": {
    "path": "../secret.txt"
  }
}
```

常用错误码：

| Code | HTTP | 阶段 | UI 处理 |
| --- | --- | --- | --- |
| `bad_request` | 400 | 已实现 | 显示字段/请求错误，保留当前表单或编辑内容。 |
| `not_found` | 404 | 已实现 | 显示资源不存在并提供返回项目/文件列表操作。 |
| `conflict` | 409 | 已实现 | 打开 diff/resolve 流程，使用响应中的 current version 重新加载。 |
| `path_escape` | 400 | 已实现 | 阻止操作，显示 project root 能力边界提示。 |
| `parse_error` | 400 | 已实现 | 显示 todo 操作或 DSL 解析诊断，不清空编辑器。 |
| `active_todo` | 409 | 已实现 | 表示当前 todo 正被 run active workspace 占用；禁止 Web 写入，提示等待 run 结束或使用恢复路径。 |
| `method_not_allowed` | 405 | 已实现 | API client 使用 `Allow` 头修正调用；用户界面通常不直接展示。 |
| `unsupported` | 400 | 已实现/保留 | 用于已识别资源下暂不支持的操作，后续可细分。 |
| `active_todo_restore_failed` | 409/500 | 计划 | Run/cancel 恢复失败，显示 active path、marker 和手动恢复证据。 |
| `run_not_cancelable` | 409 | 计划 | 终态 run 不再可取消，刷新 run detail。 |
| `run_already_active` | 409 | 计划 | 一项目一 active run；禁用重复 start 并跳转当前 run。 |
| `payload_too_large` | 413 | 计划 | 提供范围预览、下载或缩小请求。 |
| `internal` | 500 | 已实现 | 保留 request context，展示可复制诊断。 |

不支持的 HTTP method 必须返回 `405 method_not_allowed` 并设置 `Allow` header；这不是业务校验失败，不能归为 `bad_request`。

### Phase-02 已实现 API

当前后端基础阶段已实现以下本地 JSON API。所有响应使用 `application/json; charset=utf-8`，错误响应为 `{code,message,detail}`。路径错误返回 `path_escape`，保存版本冲突返回 `conflict`，active todo 写入返回 `active_todo`，空 prompt 或无法解析的 todo 操作返回 `parse_error`，不支持的 HTTP method 返回 `405 method_not_allowed` 和 `Allow` header。

| Method | Path | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 返回 `{"ok":true}`。 |
| GET | `/api/config` | 返回监听配置、默认 tool、codex/claude path、jobs、messages、registryPath，并暴露 `dangerousRun.enabled=false`、`requiresConfirmation=true` 作为后续运行确认设计位。 |
| GET | `/api/projects` | 返回 registry 中的项目列表。 |
| POST | `/api/projects` | 请求 `{"name":"atm","path":"/abs/project"}` 或 `root`，添加/打开项目；同一真实 root 去重并更新 `lastOpened`。 |
| GET | `/api/projects/{projectId}` | 读取项目并更新 `lastOpened`。 |
| PATCH | `/api/projects/{projectId}` | 当前支持更新 `name`，并刷新 `lastOpened`。 |
| DELETE | `/api/projects/{projectId}` | 从 registry 删除项目，不删除磁盘文件。 |
| GET | `/api/projects/{projectId}/files?kind=todo` | 发现 root 下 `todo.txt`、`todo.md`、`toto.md`，以及合理深度内的 `*.todo.md`、`*.todo.txt`；跳过 `.git` 和根 `.atm`。 |
| GET | `/api/projects/{projectId}/files/{path}` | 读取 URL escaped 的项目相对 todo 路径；若源 todo 被 `atm run` 移到 active path，会通过 `store.ResolveActiveTodoPath` 读取 active 内容并返回 `isActive/activePath`。 |
| PUT | `/api/projects/{projectId}/files/{path}` | 请求 `{"content":"...","baseVersion":"sha256:..."}` 保存文件；限制写入 project root 内，保留原权限并原子替换；版本不匹配返回 `409 conflict`。active todo 在项目外时拒绝保存，避免 Web 写入临时运行文件。 |
| POST | `/api/projects/{projectId}/files/{path}:format` | 复用 CLI format 逻辑格式化 todo 状态标记，返回最新文档。 |
| POST | `/api/projects/{projectId}/files/{path}:untag` | 请求 `{"done":true,"running":true}`，复用 CLI untag 逻辑清理生成状态，返回最新文档。 |
| POST | `/api/projects/{projectId}/files/{path}:append` | 追加格式化后的 prompt。当前 Web 端要求目标非 active，以保持所有 Web 写入都在 project root 内。 |

Phase-02 尚未实现 run/plan/artifact/SSE/settings 持久 API。浏览器触发 agent run 的入口在本阶段故意不执行，只在 config 中保留危险操作确认设计位。

### Project API

| Method | Path | 请求 | 响应 |
| --- | --- | --- | --- |
| GET | `/projects` | none | `{"projects":[Project]}` |
| POST | `/projects` | `{"name":"atm","path":"/abs/project"}` 或 `{"root":"/abs/project"}` | `Project` |
| GET | `/projects/{projectId}` | none | `Project`，同时刷新 `lastOpened` |
| PATCH | `/projects/{projectId}` | 当前：`{"name":"atm"}`；计划：`lastTodoFile` 进入 settings/偏好 API | `Project` |
| DELETE | `/projects/{projectId}` | none | `{"deleted":true}` |

Project DTO 以当前实现为准，前端 Zod schema 不应同时猜测 `path/createdAt/updatedAt` 和 `root/created/lastOpened` 两套形状。若后续改名，必须通过 DTO migration 和 QA gate 统一调整。

```json
{
  "id": "p_4d1cca97f277",
  "name": "atm",
  "root": "/path/to/atm",
  "created": "2026-05-22T19:09:08Z",
  "lastOpened": "2026-05-22T19:09:08Z"
}
```

### Todo File API

| Method | Path | 请求 | 响应 |
| --- | --- | --- | --- |
| GET | `/projects/{projectId}/files?kind=todo` | none | `{"files":[TodoFile]}` |
| GET | `/projects/{projectId}/files/{path}` | path is URL-escaped relative path | `TodoDocument` |
| PUT | `/projects/{projectId}/files/{path}` | `{"content":"...","baseVersion":"sha256:..."}` | `TodoDocument` |
| POST | `/projects/{projectId}/files/{path}:append` | `{"prompt":"..."}` | `TodoDocument` |
| POST | `/projects/{projectId}/files/{path}:format` | `{}` | `TodoDocument` |
| POST | `/projects/{projectId}/files/{path}:untag` | `{"done":true,"running":true}` | `TodoDocument` |

```json
{
  "path": "todo.md",
  "content": "## /verify\nRun tests.\n",
  "version": "sha256:8b1a...",
  "activePath": "/tmp/atm/todo-atm.txt",
  "isActive": false,
  "mtime": "2026-05-22T20:00:00+08:00",
  "size": 28
}
```

Conflict rule: `PUT` with stale `baseVersion` returns `409 conflict` and includes current `version`. The client must show a diff/resolve flow.

### Plan API

| Method | Path | 请求 | 响应 |
| --- | --- | --- | --- |
| GET | `/projects/{projectId}/plan?file=todo.md` | none | `PlanResponse` |
| POST | `/projects/{projectId}/plan` | `{"file":"todo.md","content":"optional unsaved buffer"}` | `PlanResponse` |

`POST` supports unsaved editor buffers without writing the todo file.

```json
{
  "source": "todo.md",
  "version": "sha256:8b1a...",
  "stats": {"tasks": 3, "branches": 1, "fanouts": 1, "joins": 1},
  "plan": {
    "tasks": [],
    "controls": [],
    "definitions": []
  },
  "graph": {
    "nodes": [],
    "edges": []
  },
  "diagnostics": []
}
```

`plan` should be compatible with current `atm plan -json`; `graph` is React Flow-oriented derived data. Diagnostics use `{severity,message,line,column,endLine,endColumn,code}`.

### Run API

| Method | Path | 请求 | 响应 |
| --- | --- | --- | --- |
| GET | `/projects/{projectId}/runs` | none | `{"runs":[RunSummary]}` |
| POST | `/projects/{projectId}/runs` | `RunCreateRequest` | `RunSummary` |
| GET | `/projects/{projectId}/runs/{runId}` | none | `RunDetail` |
| POST | `/projects/{projectId}/runs/{runId}:cancel` | `{"reason":"user"}` | `RunSummary` |

```json
{
  "file": "todo.md",
  "tool": "codex",
  "messages": 1,
  "jobs": 4,
  "outputDir": ".atm/web-runs/20260522200001-cb2301b057c0",
  "args": []
}
```

```json
{
  "id": "run_20260522200001_cb2301b057c0",
  "projectId": "p_4d1cca97f277",
  "file": "todo.md",
  "status": "running",
  "tool": "codex",
  "startedAt": "2026-05-22T20:00:01+08:00",
  "finishedAt": null,
  "outputDir": ".atm/web-runs/20260522200001-cb2301b057c0",
  "activeTodo": "/tmp/atm/todo-atm.txt",
  "cancelable": true
}
```

Run status values: `queued`、`running`、`canceling`、`succeeded`、`failed`、`canceled`、`restore_failed`。

Concurrency rule: MVP allows one active run per project by default. Starting another run returns `409 run_already_active`. Multi-run support must define file-level leases and UI conflict handling before enabling.

Run state transitions:

| From | Event | To | 要求 |
| --- | --- | --- | --- |
| `queued` | worker acquired | `running` | 创建 output dir，记录 source file、tool、activeTodo 和 run-started event。 |
| `running` | user cancel | `canceling` | 取消 run context，UI 禁止重复 start/cancel，后台等待 engine defer 和 active todo restore。 |
| `canceling` | restore succeeded | `canceled` | 写入 run-canceled event，刷新 todo、artifacts 和 active marker 状态。 |
| `canceling` | restore timeout/failure | `restore_failed` | 写入 `active_todo_restore_failed` 诊断，暴露 active path、marker、output dir 和可恢复证据。 |
| `running` | engine success | `succeeded` | 写入 run-finished event，刷新 result.md、todo 和 artifact list。 |
| `running` | engine/tool error | `failed` | 保留 stdout/stderr、tool events 和 result/partial artifacts；若恢复失败则转 `restore_failed`。 |
| terminal | cancel request | terminal | 返回 `409 run_not_cancelable`；不会重启或改变已终止 run。 |

服务重启后若发现未恢复 active marker，Run detail 必须能呈现 `restore_failed` 或只读恢复提示；在恢复状态明确前不能启动同项目新 run。

### Event API

| Method | Path | 请求 | 响应 |
| --- | --- | --- | --- |
| GET | `/projects/{projectId}/runs/{runId}/events` | SSE, optional `Last-Event-ID` | `text/event-stream` |
| GET | `/projects/{projectId}/runs/{runId}/events?since={id}` | fallback polling | `{"events":[RunEvent]}` |

SSE frame:

```txt
id: 42
event: stdout
data: {"runId":"run_...","seq":42,"type":"stdout","time":"2026-05-22T20:00:02+08:00","text":"..."}
```

### Artifact API

| Method | Path | 请求 | 响应 |
| --- | --- | --- | --- |
| GET | `/projects/{projectId}/runs/{runId}/artifacts` | none | `{"artifacts":[Artifact]}` |
| GET | `/projects/{projectId}/runs/{runId}/artifacts/{artifactId}` | optional `?offset=&limit=` | content or JSON wrapper |
| GET | `/projects/{projectId}/artifacts?dir=.atm/...` | project-relative dir | `{"artifacts":[Artifact]}` |

```json
{
  "id": "art_task_001_run_001_codex_jsonl",
  "path": ".atm/web-runs/20260522200001-cb2301b057c0/task-001-run-001-codex.jsonl",
  "kind": "jsonl",
  "size": 120034,
  "mtime": "2026-05-22T20:02:00+08:00",
  "previewable": true,
  "truncated": false
}
```

MVP limits: text preview defaults to 256 KiB; larger files require range reads or download. JSONL preview may stream first N events.

Artifact safety contract:

- `artifactId` is server-issued from a normalized, project-relative artifact path scoped to one run output dir; clients never pass arbitrary absolute paths as artifact IDs.
- `dir` query values must be project-relative, cleaned, and contained under an allowed output root such as `.atm/` or `.atm/web-runs/`; absolute paths, `..`, Windows drive paths, UNC paths, and NUL bytes return `path_escape`.
- Every listed or opened artifact path must be checked with symlink evaluation at read time. Symlinks that resolve outside the project root or outside the selected output dir are omitted from listings and rejected on direct open.
- Preview reads use `offset` and `limit` with a hard server maximum. Download uses streaming response and must set safe `Content-Disposition` from basename only.
- Artifact metadata is returned before content. UI cannot request unbounded full-file JSON/string payloads for large files.

### Settings API

| Method | Path | 请求 | 响应 |
| --- | --- | --- | --- |
| GET | `/settings` | none | `Settings` |
| PATCH | `/settings` | partial settings | `Settings` |
| GET | `/projects/{projectId}/settings` | none | merged settings |
| PATCH | `/projects/{projectId}/settings` | partial project settings | merged settings |

```json
{
  "theme": "system",
  "defaultTool": "codex",
  "codexPath": "codex",
  "claudePath": "claude",
  "messages": 1,
  "jobs": 0,
  "editor": {
    "fontSize": 13,
    "wordWrap": "on"
  }
}
```

## 事件模型

所有事件共享字段：

```json
{
  "id": "42",
  "runId": "run_20260522200001_cb2301b057c0",
  "projectId": "p_4d1cca97f277",
  "type": "task-started",
  "time": "2026-05-22T20:00:02+08:00",
  "task": {"index": 0, "block": 1, "lineStart": 3, "lineEnd": 5}
}
```

| Event | 必要 payload | UI 消费 |
| --- | --- | --- |
| `run-started` | `file`、`tool`、`outputDir`、`activeTodo` | Run monitor 切到 running，Explorer 标记 active todo。 |
| `task-started` | `task.index`、`task.block`、`lineStart`、`lineEnd`、`step`、`agent` | 高亮 editor 和 plan node。 |
| `stdout` | `text` | xterm stdout stream。 |
| `stderr` | `text` | xterm stderr stream，错误样式。 |
| `assistant-message` | `role`、`tool`、`agent`、`text` | Timeline 消息卡，状态块预览。 |
| `tool-call` | `tool`、`name`、`phase`、`argumentsPreview` | Timeline tool chip。 |
| `artifact-created` | `artifact` | Artifact browser 增量刷新。 |
| `task-finished` | `task`、`runs`、`durationMs`、`messages` | Plan node done，editor gutter 更新。 |
| `run-finished` | `status:"succeeded"`、`durationMs`、`resultPath` | 解锁 run controls，刷新 todo/result/artifacts。 |
| `run-error` | `error`、`restoreStatus` | Error panel，保留 recovery 操作。 |
| `run-canceled` | `reason`、`restoreStatus` | 标记 canceled，刷新 active todo 状态。 |

SSE broker 要求：

- 每个 run 至少保留最近 500 个事件用于重连 replay。
- 支持 `Last-Event-ID`，重连后按 seq 继续。
- stdout/stderr 可以合并小块，但必须保持顺序。
- 每个 run 必须设置最大内存事件字节数；超过 replay buffer 时插入 `events-dropped` 或等价 marker，并提示从 artifact log 补全。
- stdout/stderr 合并策略要保留时间顺序和 stream 类型，单个 chunk 不能超过 server 配置上限。
- SSE 连接要发送 heartbeat；客户端重连时如果 `Last-Event-ID` 早于 buffer 最小 seq，必须返回可检测的 gap 信息。
- run 终态后 broker 可以关闭连接，但 run detail、artifact log 和 fallback polling 仍要能恢复关键上下文。
- engine 现有 writer 输出需要通过 tee writer 转成 stdout/stderr 事件。
- tools 解析出的 assistant/tool display event 应通过窄接口进入 broker；首期不能解析时至少保留 stdout/stderr 和 artifact/run/task 事件。

## UI 信息架构

最小但完整的布局：

```txt
ProjectShell
  ActivityBar
    ExplorerIcon
    PlanIcon
    RunIcon
    ArtifactsIcon
    SettingsIcon
  Sidebar
    Explorer
      ProjectSwitcher
      TodoFileTree
      RecentRuns
      ArtifactFolders
  Main
    MainTabs
      EditorTab(todo.md)
      PlanTab(todo.md)
      RunTab(run_...)
      ArtifactTab(result.md)
    EditorWorkbench
      MonacoEditor
      EditorToolbar(save, format, untag, plan, run)
      DiagnosticsGutter
    PlanGraph
      ReactFlowCanvas
      GraphToolbar(fit, zoom, layout)
      NodeInspectorLink
  Inspector
    SelectionDetails
    TaskOps
    ResourceRefs
    Diagnostics
  BottomPanel
    Tabs(Problems, Terminal, Events, Artifacts)
    XtermRunOutput
    EventTimeline
    ArtifactList
  CommandBar
    CommandPalette
  SettingsDialog
```

### Activity bar

固定左侧窄栏，只放图标按钮和 tooltip。图标来自 `lucide-react`。活动项：Explorer、Plan、Run、Artifacts、Settings。不要用大块文本按钮。

### Explorer

显示当前项目、todo 文件、近期 run 和 artifact output 目录。文件节点状态包括 dirty、active、running、error。空项目显示添加项目 CTA；无 todo 文件时显示创建 `todo.md` 操作。

### Main tabs

支持 editor、plan、run、artifact 多 tab。Tab title 使用文件名或 run 短 id。关闭 dirty tab 时必须确认。

### Inspector

根据 selection 显示 task prompt preview、ops、DB/skill/MCP、definition links、diagnostics、artifact metadata。没有 selection 时显示当前文件摘要。

### Bottom panel

Problems 显示 plan diagnostics；Terminal 使用 xterm.js 显示 stdout/stderr；Events 显示结构化事件；Artifacts 显示当前 run 产物。可折叠，默认在 run started 时打开 Terminal。

### Command bar

命令面板支持搜索：open file、save、format、untag、preview plan、start run、cancel run、open settings、open artifact。命令必须尊重 dirty state 和 run concurrency。

命令可用性规则：

| 命令 | 可用条件 | 禁用/失败状态 |
| --- | --- | --- |
| save | 当前 tab 是 dirty todo buffer，且文件未处于项目外 active todo。 | active todo 返回 `active_todo`；stale `baseVersion` 显示 diff/resolve。 |
| format/untag/append | 当前文件在 project root 内且无 active todo 冲突。 | run active/canceling 时禁止会改写同一 todo 的操作。 |
| preview plan | 有 todo 文件；dirty buffer 可通过 `POST /plan` 预览，不强制保存。 | parse error 显示 diagnostics，不覆盖 editor 内容。 |
| start run | 项目无 active run，SSE 未处于 reconnecting，且危险操作确认通过。 | dirty buffer 未保存时必须明确选择保存后运行或放弃改动；不能把 unsaved buffer 隐式作为 run 源。 |
| cancel run | run status 为 `queued` 或 `running`。 | `canceling`、终态或 SSE reconnecting 期间不重复发 cancel。 |
| close tab | 非 dirty 或用户确认丢弃/保存。 | dirty tab 关闭必须确认。 |
| open artifact | artifact metadata 已加载且 preview/download 权限通过路径 guard。 | 超限 preview 显示 metadata、range preview 和下载/open 操作。 |

### Settings dialog

用 Radix/shadcn dialog + React Hook Form + Zod。分组：General、Tools、Run Defaults、Editor、Advanced。保存后 invalidate settings query。

### Empty/Error/Loading states

| 状态 | 行为 |
| --- | --- |
| Loading project | 保留 shell skeleton，不闪烁全屏。 |
| No project | 显示 project picker 和 add project。 |
| No todo file | Explorer 提供创建 `todo.md`，Main 显示空 editor。 |
| Plan parse error | Editor diagnostics + Problems + Plan tab error state。 |
| Run error | Run monitor 保留日志、错误、restore 状态和 artifacts。 |
| SSE reconnecting | Timeline 顶部显示 reconnecting，禁止重复 start。 |
| Artifact too large | 显示 metadata、range preview 和下载/open 操作。 |

## 状态管理边界

| 状态 | Owner | 说明 |
| --- | --- | --- |
| projects | TanStack Query | 来自 `/projects`，mutation 后 invalidate。 |
| todo document | TanStack Query + editor buffer | Query 保存 server 版本；Monaco/Zustand 保存 dirty buffer 和 baseVersion。 |
| unsaved content | Zustand | 按 `{projectId,path}` 保存 content、dirty、baseVersion、tab id。 |
| plan | TanStack Query | Query key 包含 projectId、file、content hash。unsaved buffer 用 POST plan。 |
| run summary/detail | TanStack Query | POST start 后写入 cache；SSE 事件增量更新。 |
| SSE events | Zustand ring buffer + Query detail | 高频事件进入 run store；关键结束事件 invalidate runs/artifacts/files。 |
| layout/tabs/selection | Zustand | 不发到后端，可持久化到 localStorage。 |
| settings | TanStack Query | 表单局部状态由 React Hook Form 管理。 |
| artifact preview | TanStack Query | key 包含 artifactId、offset、limit。 |

边界规则：

- 不把文件正文的 server copy 和 dirty copy 混在一个 store 字段。
- 不把高频 stdout 每行都写入 TanStack Query；xterm 通过事件订阅 append。
- Plan graph selection 存 ID，不存完整 node payload。
- 所有 API response 用 Zod 在 client 边界验证，验证失败显示 `api_contract_mismatch`。

## 开发 agent 与测试 agent 协作合同

每个阶段目录位于 `.atm/web-ide-qa/phase-NN-name/`。本阶段目录为 `.atm/web-ide-qa/phase-01-architecture/`。

协作对象：

| 对象 | 说明 | 本阶段值 |
| --- | --- | --- |
| `phase_iteration` | 一次开发 agent 交付和测试 agent 复审的闭环。每轮包含 handoff、review/defects、fix、retest 和 acceptance 更新。 | `phase-01-architecture.iteration-1`，若同一缺陷复开则递增 `repairRound`，最多 3 轮。 |
| `acceptance_gate` | 阶段进入下一阶段前必须满足的门禁集合。每个 gate 必须能追溯到文档、测试计划、缺陷状态和验收结论。 | `contract` 为 Phase 01 主 gate；`integration`、`UX`、`regression`、`recovery`、`evidence` 作为后续继承 gate。 |
| `defects` | 测试 agent 记录的缺陷状态机和证据索引。缺陷关闭前不能只修改 acceptance 结论。 | `.atm/web-ide-qa/phase-01-architecture/defects.md`。 |
| `blocked` | 同一 phase gate 存在 blocker 缺陷、必需证据缺失、或同一缺陷第 3 轮修复后仍失败。blocked 阶段不得标记 accepted，除非产品负责人明确记录 accepted risk。 | Phase 01 当前不得有 open blocker；后续实现 gate 按同一规则继承。 |

| 文件 | Owner | 内容 |
| --- | --- | --- |
| `dev-handoff.md` | 开发 agent | 范围、变更文件、架构决策、待测重点、已知风险。 |
| `test-plan.md` | 测试 agent 或开发 agent 草案 | 需要审查/执行的检查项、方法、证据要求。 |
| `defects.md` | 测试 agent | 缺陷列表，含 severity、状态、复现/证据、owner。 |
| `retest-report.md` | 测试 agent | 每轮修复后的复测结果。phase-01 若无缺陷可省略，若有缺陷必须创建。 |
| `acceptance.md` | 测试 agent | 验收结论、覆盖范围、残余风险、是否可进入下一阶段。 |

缺陷状态流转：

```txt
open -> acknowledged -> fixed -> retest_passed -> closed
open -> acknowledged -> wont_fix -> accepted_risk
open -> duplicate
open -> not_reproducible
```

最多 3 轮修复规则：

1. 测试 agent 提交 `open` defects。
2. 开发 agent 修复并把状态改为 `fixed`，说明变更文件。
3. 测试 agent 复测，`retest_passed` 后 `closed`；失败则重新 `open` 并记录轮次。
4. 同一缺陷最多 3 轮修复。第 3 轮仍失败时，必须升级为 phase gate blocker，不能静默进入下一阶段。

Phase-01 特例：本阶段主要是架构合同。测试 agent 的任务是合同完整性审查：检查本文是否覆盖指定边界、API、事件、UX、技术栈、协作合同、验收 gate 和 MVP 风险。

## 分阶段验证策略和验收 gate

| Gate | 目标 | 验收证据 |
| --- | --- | --- |
| contract | 架构/API/UX/技术栈合同完整。 | `docs/web-ide.md`、phase QA 文件、测试 agent acceptance。 |
| integration | Go server、CLI `web`、API client、静态资源嵌入可联通。 | Go tests、frontend tests、HTTP smoke、Vite build、embed build。 |
| UX | IDE 首屏、编辑、plan、run、artifact、settings 可用且状态完整。 | Playwright desktop/mobile screenshots、loading/error/empty 状态覆盖。 |
| regression | CLI 现有行为不回退。 | `go test ./...`、现有 CLI tests、手动 smoke：`atm plan`、`atm run` 小样例。 |
| recovery | cancel、active todo restore、SSE reconnect、artifact large file 路径安全可靠。 | 故障注入测试、cancel test、path escape tests、reconnect e2e。 |
| evidence | 每阶段产物可追溯。 | `.atm/web-ide-qa/phase-*` 中 handoff、test-plan、defects、retest、acceptance。 |

阶段建议：

| Phase | 范围 | Gate |
| --- | --- | --- |
| 01 architecture | 本文和 QA 合同。 | contract |
| 02 server skeleton | `web` 子命令、HTTP server、registry、settings、static embed 占位。 | integration |
| 03 todo + plan | 文件 API、Monaco shell、plan API、React Flow read-only graph。 | UX + regression |
| 04 run monitor | run manager、SSE broker、cancel、xterm、event timeline。 | recovery |
| 05 artifacts | artifact API/browser、large file preview、result.md/diff。 | recovery + UX |
| 06 hardening | path security, cross-platform tests, bundle budget, Playwright suite. | regression + evidence |

## MVP 风险

| 风险 | 影响 | MVP 处理 |
| --- | --- | --- |
| active todo restore | cancel/crash 后源 todo 可能仍在 `/tmp/atm`。 | Run detail 暴露 `activeTodo` 和 `restoreStatus`；启动前检测 marker；提供只读恢复提示，自动恢复必须走 store。 |
| cancel 清理 | context cancel 后 agent 子进程、active file、output registry 可能未完全收尾。 | Run manager 等待 engine defer 完成；状态使用 `canceling`；超时后标 `restore_failed`。 |
| 路径逃逸 | 任意文件读写或 artifact 下载。 | project root guard、symlink eval、相对路径 API、path escape tests。 |
| 跨平台路径 | Windows drive、separator、rename 行为不同。 | 使用 `filepath`，避免 URL path 直接当 OS path；补 Windows path 单测。 |
| SSE 重连 | 页面刷新或网络闪断丢事件。 | seq + Last-Event-ID + replay buffer；结束后可从 artifact/log 补全。 |
| 前端 bundle size | Monaco、React Flow、xterm 增大首包。 | route/lazy load Monaco/graph/xterm；bundle analyzer gate；release 首屏预算。 |
| Monaco worker | go embed 路径、CSP、worker 加载失败。 | Vite worker 配置单测/e2e；worker URL 从 asset manifest 读取。 |
| 并发 run | 同项目多 run 改同一 todo 文件导致冲突。 | MVP 一项目一 active run；后续按 file lease 扩展。 |
| artifact 大文件 | 浏览器卡死或 server 内存暴涨。 | metadata first、range reads、preview limit、下载流式传输。 |

## 实现 TODO 草案

这些是后续实现入口，不代表本阶段要写代码：

- `pkg/app/cli`: 增加 `web` 子命令解析并调用 `pkg/web`.
- `pkg/web`: 新增 server、routes、JSON error、path guard、registry、settings。
- `pkg/web/todo`: 封装 store read/write/append/format/untag。
- `pkg/web/plan`: 封装 `dsl.CompileProgram` 和 graph DTO。
- `pkg/web/run`: run manager、event broker、engine adapter、cancel。
- `pkg/web/artifact`: output dir scanner、range reader、preview limits。
- `web/`: Vite React app，release build 输出供 Go embed。
