# ATM 演示：午夜天文台开场

这是一份可运行的 ATM DSL 巡览。它故意带一点舞台感：所选 agent 帮一座小天文台开启午夜观星秀，舞台助手并行工作，数据库充当共享黑板，结构化产物决定后续路由。

先看 dry-run 执行计划：

```sh
cd demo
../atm plan -file demo.zh-CN.md
```

带产物目录运行：

```sh
cd demo
../atm run -file demo.zh-CN.md -output .atm/demo-launch-zh-CN
```

像本节这样的普通 Markdown 只是说明文档，不会执行。只有标题名以 `/` 或 `//` 开头的 section 会成为可运行内容。单斜杠标题表示一个完整任务；双斜杠标题表示任务列表，列表里空行会拆分任务块。

## //bootstrap

/import workflows/ledger-shared.todo.md
/import audit from workflows/audit-controls.todo.md

/pool stagehand 3 8
/pool critic 1

/let show midnight-observatory
/let audience curious-builders
/let signal-word luminous
/let name-with-dash comet-tail
/let briefing 把每个 prompt 当作一条小型操作提示。宁可输出一行有用信息，也不要写长篇说明。

/let repo_hint /bash <<'SH'
printf 'ATM demo rooted at %s' "${PWD##*/}"
SH

/db new show_board scope:global persist:run access:append
本次 run 内共享的舞台黑板。请把提示追加到 cues/<area>，把问题追加到 questions/<area>。条目要短，方便现场扫描。

/db new souvenir_book scope:local persist:run access:write
单个任务可挂载的本地纪念册。只有写了 `/db use souvenir_book` 的任务才能看到它。

/db new long_term_archive scope:local persist:project access:read
可选的项目持久化归档语法示例。这个可运行 demo 不会挂载或写入这个只读 local 数据库。

## /def call_sign show

/return {{show}} 的天文台呼号：{{signal-word}}-beam。

## /def shell_stamp

/return /bash printf 'shell-stamp:%s' "$(date +%H%M%S)"

## /def spark area

为 {{area}} 站点写一句短小、有画面感的舞台提示。
不要使用列表。

/return
{{area}} 的火花提示：
{{agent.last_message}}

## /def gate_plans show

为 {{show}} 准备正好三个紧凑的舞台助手计划。
每个计划都要命名一个站点，并要求该助手回复一条短提示。

/return
```
plans:[]string:三个舞台助手计划
```

## /def structured_gate show

返回 {{show}} 的迷你门禁结果。
把 `passed` 设为 true，`reason` 写成一条适合操作员阅读的短句，并在 `acts` 中返回正好两个 cue 名称。

/return
```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["passed", "reason", "acts"],
  "properties": {
    "passed": {"type": "boolean"},
    "reason": {"type": "string"},
    "acts": {"type": "array", "items": {"type": "string"}}
  }
}
```

## //def two_lens_review area

/pool lens 2 4

/go lens
以冷静操作员的视角审查 {{area}}。回复一个风险和一个稳定现场的 cue。

/go lens
以急切观众的视角审查 {{area}}。回复一个问题和一个 cue。

/wait lens

/return
{{area}} 的双视角审查：
{{agent.messages}}

## /opening beacon

/let intro /call call_sign {{show}}
/let stamp /call shell_stamp
/let imported_signal /call audit.rollback_signal
/let spark_note /call spark dome
/let task_briefing 把每个 prompt 当作一条小型操作提示。宁可输出一行有用信息，也不要写长篇说明。
/task_briefing
/bash <<'SH'
mkdir -p .atm/demo
printf '{"ready":true,"source":"bash"}\n' > .atm/demo/beacon.json
SH
为 {{audience}} 写一条紧凑的开场说明。

使用这些已绑定的定义结果：
- {{intro}}
- {{stamp}}
- 来自命名空间导入定义的 rollback signal：{{imported_signal}}

全局 bash 绑定捕获的仓库提示：
{{repo_hint}}

带短横线的变量通过 `var` 渲染：{{var "name-with-dash"}}。
普通 Go template 数据仍能判断 show 是否存在：{{if has "show"}}yes{{else}}no{{end}}。

加入这条火花提示：
{{spark_note}}

/output opening-note
> [!ATM]
> status: done
> started: 2026-05-22 03:30
> finished: 2026-05-22 03:31
> duration: 37s
> runs: 1x
>
> messages:
> - assistant (codex) [call=spark]:
>   穹顶下灯影缓缓升起，观众像走进一场悬在夜空里的呼吸。
> - assistant (codex):
>   curious-builders，luminous-beam 已在 demo 点亮：shell-stamp:033047，rollback signal=clear，comet-tail 就绪，show=yes，穹顶下灯影缓缓升起。

## //安全展示 runner 专属语法

/if (false)
/resume
/args --illustrative-runner-flag
这个分支会被刻意跳过。它把 `/resume` 和任意 runner `/args` 留在 demo 里，但不会真的恢复会话，也不会把未知参数传给所选工具。
> [!ATM]
> status: skipped
> time: 2026-05-22 03:31
> reason: if condition evaluated false

## //循环画廊

/for 2
请严格回复：counted orbit {{N}} for {{show}}.
> [!ATM]
> status: done
> started: 2026-05-22 03:31
> finished: 2026-05-22 03:31
> duration: 33s
> runs: 2x
>
> messages:
> - assistant (codex) [N=1]:
>   counted orbit 1 for midnight-observatory.
> - assistant (codex) [N=2]:
>   counted orbit 2 for midnight-observatory.

/bash <<'SH'
mkdir -p .atm/demo
printf '{"passed":true,"loop":"cel"}\n' > .atm/demo/cel-loop.json
SH
/for until(exists(".atm/demo/cel-loop.json") && json(".atm/demo/cel-loop.json").passed)
请严格回复：local CEL loop is ready.
> [!ATM]
> status: done
> started: 2026-05-22 03:31
> finished: 2026-05-22 03:32
> duration: 17s
> runs: 1x
>
> messages:
> - assistant (codex) [N=1]:
>   local CEL loop is ready.

/for 1 until 文件 .atm/demo/beacon.json 存在，并且其中的 JSON ready 字段为 true
请严格回复：natural-language retry check can see the beacon.
> [!ATM]
> status: done
> started: 2026-05-22 03:32
> finished: 2026-05-22 03:32
> duration: 44s
> runs: 1x
>
> messages:
> - assistant (codex) [N=1]:
>   natural-language retry check can see the beacon.

/for 1 until (exists(".atm/demo/beacon.json") && json(".atm/demo/beacon.json").ready)
请严格回复：bounded CEL retry check can see the beacon.
> [!ATM]
> status: done
> started: 2026-05-22 03:32
> finished: 2026-05-22 03:33
> duration: 16s
> runs: 1x
>
> messages:
> - assistant (codex) [N=1]:
>   bounded CEL retry check can see the beacon.

/if (false)
/for dir
可选的全目录循环语法。回复目录 {{dir}}。
> [!ATM]
> status: skipped
> time: 2026-05-22 03:33
> reason: if condition evaluated false

/if (false)
/for path
可选的全路径循环语法。回复路径 {{path}}。
> [!ATM]
> status: skipped
> time: 2026-05-22 03:33
> reason: if condition evaluated false

## /gate artifact

为 {{show}} 创建公开门禁产物。
把 `passed` 设为 true，`reason` 保持短句，`acts` 设置为两个符合天文台主题的短 act 名称。

/output public-gate
```yaml
type: object
additionalProperties: false
required:
  - passed
  - reason
  - acts
properties:
  passed:
    type: boolean
  reason:
    type: string
  acts:
    type: array
    items:
      type: string
```
> [!ATM]
> status: done
> started: 2026-05-22 03:33
> finished: 2026-05-22 03:33
> duration: 20s
> runs: 1x

## //gate routing

/if (existsOutput("public-gate.json"))
> [!ATM]
> status: done
> started: 2026-05-22 03:33
> finished: 2026-05-22 03:33
> duration: 0s
> runs: 0x

/if (jsonOutput("public-gate.json").passed)
读取 `public-gate.json`，并用其中的 gate reason 回复一条很短的交接语。
> [!ATM]
> status: done
> started: 2026-05-22 03:33
> finished: 2026-05-22 03:34
> duration: 41s
> runs: 1x
>
> messages:
> - assistant (codex):
>   公开门禁已创建，可以交接。

/else
如果 gate 文件存在但没有通过，回复一条很短的 hold 说明。
> [!ATM]
> status: skipped
> time: 2026-05-22 03:33
> reason: if condition evaluated true

/else
回复一条短句，说明必须先重建 `public-gate.json`。
> [!ATM]
> status: skipped
> time: 2026-05-22 03:33
> reason: if condition evaluated true

/if 文件 .atm/demo/beacon.json 报告 ready 为 true
请严格回复：natural-language if routed to launch.
> [!ATM]
> status: done
> started: 2026-05-22 03:34
> finished: 2026-05-22 03:34
> duration: 15s
> runs: 1x
>
> messages:
> - assistant (codex):
>   natural-language if routed to launch.

/else
请严格回复：natural-language if routed to hold.
> [!ATM]
> status: skipped
> time: 2026-05-22 03:34
> reason: if condition evaluated true

## //黑板 fanout

/for area in [lanterns mirrors cocoa] /go stagehand
/output cue-{{agent_index}}-{{area}}
```
area:string:站点名称
cue:string:给操作员看的短提示
```
使用 `show_board` ATM DB。
向 key `cues/{{area}}` 追加一条愉快的 cue，然后为 {{area}} 站点提交结构化 cue 产物。
> [!ATM]
> status: done
> started: 2026-05-22 03:34
> finished: 2026-05-22 03:35
> duration: 34s
> runs: 3x
>
> messages:
> - assistant (codex) [area=lanterns]:
>   已追加并提交结构化 cue。
> - assistant (codex) [area=mirrors]:
>   完成。
> - assistant (codex) [area=cocoa]:
>   已完成。

/go critic /for 2
为 pass {{N}} 回复一个很小的观众挑战。
> [!ATM]
> status: done
> started: 2026-05-22 03:34
> finished: 2026-05-22 03:35
> duration: 1m2s
> runs: 2x
>
> messages:
> - assistant (codex) [agent=1, N=1]:
>   Pass 1: 如果星光突然变暗，谁先发出信号？
> - assistant (codex) [agent=1, N=2]:
>   已为 `pass 2` 追加到 `questions/audience`：
>   
>   `Pass 2: 观众挑战：用一个手势把星光叫回来。`

/go
请严格回复：unpooled background cue launched.
> [!ATM]
> status: done
> started: 2026-05-22 03:34
> finished: 2026-05-22 03:35
> duration: 18s
> runs: 1x
>
> messages:
> - assistant (codex) [agent=1]:
>   unpooled background cue launched.

/wait stagehand
/db access * read
/db access show_board read
用 ATM DB 工具扫描 `show_board` 的 cues/**。
在 critic 分支可能仍在运行时，用三条短 bullet 汇总舞台助手 cue。
> [!ATM]
> status: done
> started: 2026-05-22 03:34
> finished: 2026-05-22 03:35
> duration: 56s
> runs: 1x
>
> messages:
> - assistant (codex):
>   - `cues/cocoa`：热可可香气上线，笑容加糖。
>   - `cues/lanterns`：灯影亮起来，笑声跟上。
>   - `cues/mirrors`：镜面轻转，今天的光也笑起来。

/wait critic
请严格回复：all critic passes joined.
> [!ATM]
> status: done
> started: 2026-05-22 03:35
> finished: 2026-05-22 03:36
> duration: 23s
> runs: 1x
>
> messages:
> - assistant (codex):
>   all critic passes joined.

/wait
请严格回复：all remaining background work joined.
> [!ATM]
> status: done
> started: 2026-05-22 03:36
> finished: 2026-05-22 03:36
> duration: 18s
> runs: 1x
>
> messages:
> - assistant (codex):
>   all remaining background work joined.

/db use souvenir_book access:write
使用 ATM DB 工具，把 `souvenir_book` 的 key `notes/opening` 设置为一个提到 {{show}} 的短字符串。然后回复这个已存储的字符串。
> [!ATM]
> status: done
> started: 2026-05-22 03:36
> finished: 2026-05-22 03:36
> duration: 23s
> runs: 1x
>
> messages:
> - assistant (codex):
>   midnight-observatory welcomes the first quiet note.

/db use souvenir_book access:read
/db ignore show_board
读取 `souvenir_book` 的 key `notes/opening`。
用一句话证明 local DB 可以被挂载，同时不暴露共享 show board。
> [!ATM]
> status: done
> started: 2026-05-22 03:36
> finished: 2026-05-22 03:37
> duration: 23s
> runs: 1x
>
> messages:
> - assistant (codex):
>   已读取：`midnight-observatory welcomes the first quiet note.`
>   
>   这证明本地 DB `souvenir_book` 已成功挂载并可读取指定 key，且未暴露任何共享 show board 内容。

/db ignore
请严格回复：no databases mounted for this cue.
> [!ATM]
> status: done
> started: 2026-05-22 03:37
> finished: 2026-05-22 03:37
> duration: 16s
> runs: 1x
>
> messages:
> - assistant (codex):
>   no databases mounted for this cue.

## //定义与动态 fanout

/call two_lens_review telescope
> [!ATM]
> status: done
> started: 2026-05-22 03:37
> finished: 2026-05-22 03:38
> duration: 37s
> runs: 0x
>
> messages:
> - assistant (codex) [call=two_lens_review]:
>   风险：望远镜若被临场触碰或过快转向，焦点会漂移，观众看到的星点可能短暂丢失。
>   
>   稳定现场 cue：望远镜方位锁定，星点稳定入框。

/let bound_review /call two_lens_review soundtrack
/let gate /call structured_gate {{show}}
用一行总结已绑定的审查：
{{bound_review}}
结构化 return 字段可以在同一 task 的后续模板里使用：passed={{gate.passed}}, reason={{gate.reason}}.
> [!ATM]
> status: done
> started: 2026-05-22 03:38
> finished: 2026-05-22 03:39
> duration: 1m21s
> runs: 1x
>
> messages:
> - assistant (codex) [call=two_lens_review]:
>   风险：`soundtrack` 的主旋律如果只跟灯光半拍走、未等望远镜锁定确认，入场会显得抢。
>   
>   稳定现场 cue：灯光抬起前半拍进低鼓点，望远镜锁定时主旋律露头并稳住。
> - assistant (codex) [call=structured_gate]:
>   这里只有一个运行内黑板。我会扫描相关 key，找出这个区域已经记录的 cue。
> - assistant (codex):
>   soundtrack 审查已绑定：主旋律需等望远镜锁定后入场，低鼓点在灯光抬起前半拍铺底，passed=true。

/for plan in(/call gate_plans {{show}})
/go stagehand
执行这个动态 `/call` 计划，并回复一行：
{{plan}}
> [!ATM]
> status: done
> started: 2026-05-22 03:39
> finished: 2026-05-22 03:40
> duration: 47s
> runs: 3x
>
> messages:
> - assistant (codex) [plan=站点：Glass Meridian。助手回复短提示："雾幕开启，信号稳定。"]:
>   雾幕开启，信号稳定。
> - assistant (codex) [plan=站点：Lunar Rail。助手回复短提示："轨道清空，静候入场。"]:
>   轨道清空，静候入场。
> - assistant (codex) [plan=站点：North Dome。助手回复短提示："调低主光，星图就绪。"]:
>   调低主光，星图就绪。

/wait stagehand
请严格回复：dynamic call plans joined.
> [!ATM]
> status: done
> started: 2026-05-22 03:39
> finished: 2026-05-22 03:40
> duration: 45s
> runs: 1x
>
> messages:
> - assistant (codex):
>   dynamic call plans joined.

/for act in(jsonOutput("public-gate.json").acts)
为动态 CEL act {{act}} 回复一条操作员 cue。
> [!ATM]
> status: done
> started: 2026-05-22 03:40
> finished: 2026-05-22 03:41
> duration: 59s
> runs: 2x
>
> messages:
> - assistant (codex) [act=观星入场]:
>   动态 CEL act 观星入场：星幕先开一线，主星点亮即放行。
> - assistant (codex) [act=星图巡检]:
>   已追加到 `show_board` 的 `cues/cel`：
>   
>   动态 CEL act 星图巡检：外环先扫，主星锁稳后放行下一段。

## /导入的普通定义

/let map /call service_map demo-stage
把这个 plain imported definition 的返回值改写成一条有趣的一行天文台依赖图：

{{map}}
> [!ATM]
> status: done
> started: 2026-05-22 03:41
> finished: 2026-05-22 03:43
> duration: 1m45s
> runs: 1x
>
> messages:
> - assistant (codex) [call=service_map]:
>   **demo-stage Dependency Map**
>   
>   Sources: [demo.md](/home/test/testtask/atm/demo/demo.md:37) seeds `show`, audience, signal word, repo hint, bash stamp, `.atm/demo/beacon.json`, imported `audit.rollback_signal`, `public-gate.json`, and `show_board` DB cues. The reusable map task is [service_map](/home/test/testtask/atm/demo/workflows/ledger-shared.todo.md:6).
>   
>   Queues: `stagehand` pool `3/8`, `critic` pool `1`, fanout over `lanterns`, `mirrors`, `cocoa`, dynamic `/call gate_plans`, then explicit `/wait stagehand`, `/wait critic`, `/wait` joins.
>   
>   Downstream notes: `opening-note.txt`, `final-note.txt`, per-stagehand `cue-*.json`, `public-gate.json`, `souvenir_book notes/opening`, and operator cue lines from `public-gate.json.acts`.
>   
>   Dashboards: output artifact directory `.atm/demo-launch`, `result.md`, native JSONL streams `[output]`, `[check]`, `[db]`, `public-gate.json`, and `show_board cues/**`.
>   
>   Feature flags / gates: `existsOutput("public-gate.json")`, `jsonOutput("public-gate.json").passed`, `.atm/demo/beacon.json.ready`, skipped `/if false` demo branches, and illustrative runner `/args`.
>   
>   Rollback surface: [audit.rollback_signal](/home/test/testtask/atm/demo/workflows/audit-controls.todo.md:5) checks `rollback.lock`; gate snapshots carry `rollback_signal`; compliance matrix has a rollback field; gate routing falls back to hold/rebuild when `public-gate.json` is missing or failed.
>   
>   I also appended a short dependency-map cue to the run-local `show_board` blackboard.
> - assistant (codex):
>   `demo-stage` 观测链路：`demo.md` 星源、beacon、`audit.rollback_signal`、`public-gate.json` 与 `show_board` -> `stagehand`/`critic` 星队列和 `gate_plans` 合流 -> notes、`cue-*.json`、dashboards 与 CEL cues 点亮 -> gates 放行，`rollback.lock` 一暗就切回 hold/rebuild。

## /final constellation

为 {{show}} 准备最终操作员说明。

独立行 inline call 会在外围 prompt 执行前被替换：
/call call_sign {{show}}

用一条紧凑的话提到这些 ATM 能力：
Markdown task mode、imports、definitions、bash capture、structured output、conditions、loops、pools、DB blackboards 和 output artifacts。

/output final-note
> [!ATM]
> status: running
> started: 2026-05-22 03:43
> step: 1
> step-runs: 0x
> total-runs: 0x

## 运行后查看

这里重新变回普通 Markdown。运行后请查看所选 output 目录：

- `opening-note.txt` 和 `final-note.txt` 来自文本 `/output`
- `public-gate.json` 和每个 stagehand cue JSON 来自结构化 `/output`
- 原生 JSONL stream，里面会包含 `[output]`、`[check]` 和 `[db]` MCP 调用
- 带生成状态块的 `result.md`

被跳过的循环块展示了 `/for dir` 和 `/for path`，但不会强迫 demo 对当前项目里的每个目录或文件都发起一次 prompt。
