# ATM Demo: Midnight Observatory Launch

This is a runnable tour of ATM's DSL. It is deliberately theatrical: the
selected agent helps a tiny observatory open a midnight sky show, stagehands
work in parallel, databases act as a blackboard, and structured artifacts drive
the routing decisions.

Run a dry plan first:

```sh
cd demo
../atm plan -file demo.md
```

Run the demo with an artifact directory:

```sh
cd demo
../atm run -file demo.md -output .atm/demo-launch
```

Ordinary Markdown sections like this one are documentation. Only headings whose
names start with `/` or `//` become runnable sections. A single slash heading is
one task; a double slash heading is a task list where blank lines split blocks.

## //bootstrap

<!-- Imports can be plain or namespaced. Imported runnable tasks do not run. -->
/import workflows/ledger-shared.todo.md
/import audit from workflows/audit-controls.todo.md

<!-- Task-list comments and Markdown rules are ignored before command parsing. -->
---
/pool stagehand 3 8
/pool critic 1

/let show midnight-observatory
/let audience curious-builders
/let signal-word luminous
/let name-with-dash comet-tail
/let briefing Read every prompt as a tiny operational cue. Prefer one useful line over a long speech.

/let repo_hint /bash <<'SH'
printf 'ATM demo rooted at %s' "${PWD##*/}"
SH

/db new show_board scope:global persist:run access:append
Run-local stagehand blackboard. Append cues under cues/<area> and questions/<area>. Keep entries short enough to scan during a live show.

/db new souvenir_book scope:local persist:run access:write
Local notebook for a single task. It is mounted only by tasks that write `/db use souvenir_book`.

/db new long_term_archive scope:local persist:project access:read
Optional project-persistent archive syntax. The runnable demo does not mount or write this read-only local database.

## /def call_sign show

/return Observatory call sign for {{show}}: {{signal-word}}-beam.

## /def shell_stamp

/return /bash printf 'shell-stamp:%s' "$(date +%H%M%S)"

## /def spark area

Reply with one short imaginative cue for the {{area}} station.
Do not use a list.

/return
Spark for {{area}}:
{{agent.last_message}}

## /def gate_plans show

Prepare exactly three compact stagehand plans for {{show}}.
Each plan should name a station and tell that stagehand to reply with a short cue.

/return
```
plans:[]string:Three stagehand plans
```

## /def structured_gate show

Return a miniature gate for {{show}}.
Set `passed` to true, set `reason` to a short operator-readable line, and
return exactly two cue names in `acts`.

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
Review {{area}} as the calm operator. Reply with one risk and one calming cue.

/go lens
Review {{area}} as the impatient audience. Reply with one question and one cue.

/wait lens

/return
Two-lens review for {{area}}:
{{agent.messages}}

## /opening beacon

/let intro /call call_sign {{show}}
/let stamp /call shell_stamp
/let imported_signal /call audit.rollback_signal
/let spark_note /call spark dome
/let task_briefing Read every prompt as a tiny operational cue. Prefer one useful line over a long speech.
/task_briefing
/bash <<'SH'
mkdir -p .atm/demo
printf '{"ready":true,"source":"bash"}\n' > .atm/demo/beacon.json
SH
Write a compact opening note for {{audience}}.

Use the bound definition results:
- {{intro}}
- {{stamp}}
- rollback signal from an imported namespaced definition: {{imported_signal}}

Repository hint captured by a global bash binding:
{{repo_hint}}

The dash-named variable renders through `var`: {{var "name-with-dash"}}.
The normal Go template data still knows whether show exists: {{if has "show"}}yes{{else}}no{{end}}.

Include this spark:
{{spark_note}}

/output opening-note

## //runner-specific syntax kept safe

/if (false)
/resume
/args --illustrative-runner-flag
This branch is intentionally skipped. It keeps `/resume` and arbitrary runner
`/args` visible in the demo without resuming a real session or passing an
unknown flag to the selected tool.

## //loop gallery

/for 2
Reply with exactly: counted orbit {{N}} for {{show}}.

/bash <<'SH'
mkdir -p .atm/demo
printf '{"passed":true,"loop":"cel"}\n' > .atm/demo/cel-loop.json
SH
/for until(exists(".atm/demo/cel-loop.json") && json(".atm/demo/cel-loop.json").passed)
Reply with exactly: local CEL loop is ready.

/for 1 until the file .atm/demo/beacon.json exists and its JSON ready field is true
Reply with exactly: natural-language retry check can see the beacon.

/for 1 until (exists(".atm/demo/beacon.json") && json(".atm/demo/beacon.json").ready)
Reply with exactly: bounded CEL retry check can see the beacon.

/if (false)
/for dir
Optional full-directory loop syntax. Reply with directory {{dir}}.

/if (false)
/for path
Optional full-path loop syntax. Reply with path {{path}}.

## /gate artifact

Create the public gate artifact for {{show}}.
Set `passed` to true, keep `reason` short, and set `acts` to two short act
names that fit the observatory theme.

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

## //gate routing

/if (existsOutput("public-gate.json"))

/if (jsonOutput("public-gate.json").passed)
Read `public-gate.json` and reply with one short handoff line using its gate
reason.

/else
Reply with one short hold line for a gate that exists but did not pass.

/else
Reply with one short line explaining that `public-gate.json` must be rebuilt.

/if the file .atm/demo/beacon.json reports ready true
Reply with exactly: natural-language if routed to launch.

/else
Reply with exactly: natural-language if routed to hold.

## //blackboard fanout

/for area in [lanterns mirrors cocoa] /go stagehand
/output cue-{{agent_index}}-{{area}}
```
area:string:Station name
cue:string:Short cue shown to the operator
```
Use the `show_board` ATM DB.
Append one cheerful cue to key `cues/{{area}}`, then submit the structured cue
artifact for the {{area}} station.

/go critic /for 2
Reply with one tiny audience challenge for pass {{N}}.

/go
Reply with exactly: unpooled background cue launched.

/wait stagehand
/db access * read
/db access show_board read
Scan `show_board` cues/** with the ATM DB tools.
Summarize the stagehand cues in three short bullets while the critic branch may
still be running.

/wait critic
Reply with exactly: all critic passes joined.

/wait
Reply with exactly: all remaining background work joined.

/db use souvenir_book access:write
Use the ATM DB tools to set `souvenir_book` key `notes/opening` to one short
string that mentions {{show}}. Then reply with the stored string.

/db use souvenir_book access:read
/db ignore show_board
Read `souvenir_book` key `notes/opening`.
Reply with one sentence that proves the local DB can be mounted without exposing
the shared show board.

/db ignore
Reply with exactly: no databases mounted for this cue.

## //definition and dynamic fanout

/call two_lens_review telescope

/let bound_review /call two_lens_review soundtrack
/let gate /call structured_gate {{show}}
Summarize the bound review in one line:
{{bound_review}}
Structured return fields are available to later template rendering in the same
task: passed={{gate.passed}}, reason={{gate.reason}}.

/for plan in(/call gate_plans {{show}})
/go stagehand
Execute this dynamic `/call` plan and reply with one line:
{{plan}}

/wait stagehand
Reply with exactly: dynamic call plans joined.

/for act in(jsonOutput("public-gate.json").acts)
Reply with one operator cue for dynamic CEL act {{act}}.

## /imported plain definition

/let map /call service_map demo-stage
Turn this plain imported definition result into a whimsical one-line dependency
map for the observatory demo:

{{map}}

## /final constellation

Prepare the final operator note for {{show}}.

Inline calls are replaced before this surrounding prompt runs:
/call call_sign {{show}}

Mention these ATM features in a compact line:
Markdown task mode, imports, definitions, bash capture, structured output,
conditions, loops, pools, DB blackboards, and output artifacts.

/output final-note

## Notes after the run

This is ordinary Markdown again. Inspect the selected output directory for:

- `opening-note.txt` and `final-note.txt` from text `/output`
- `public-gate.json` and per-stagehand cue JSON from structured `/output`
- native JSONL streams, including `[output]`, `[check]`, and `[db]` MCP calls
- `result.md` with generated ATM state blocks

The skipped loop blocks show `/for dir` and `/for path` without forcing a demo
run to send one prompt per directory or file in the current project.
