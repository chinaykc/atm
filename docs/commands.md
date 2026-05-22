# Command Reference

[中文](commands.zh-CN.md)

ATM means **Agent Task Markdown**. It is a Markdown-based DSL (domain-specific language) for scheduling agent tasks: normal Markdown carries context and notes, while slash-heading sections and slash commands define executable work.

Most todo files only need plain prompts. Add command lines when you want a task to resume, pass tool arguments, run a flow more than once, stop on a condition, or run in the background.

Commands must appear at the start of a task block, before the prompt text.

## Task Boundaries

ATM has two parsing modes.

In legacy mode, a task is a text block:

```txt
/command
/another-command
Prompt text sent to the runner.

Next task block.
```

Blank lines separate tasks; any number of blank or whitespace-only lines is accepted. Whole-line comments are ignored:

```txt
# ignored line
<!-- ignored HTML comment block -->
[//]: # (ignored reference comment)
[comment]: <> (ignored reference comment)
---
===
```

Standalone Markdown rule lines made only of three or more `-` or `=` characters are also ignored. Inline comments are not supported. A `#`, `---`, or `<!-- ... -->` later in a prompt line remains prompt text.

In Markdown task mode, headings whose title starts with `/` define runnable sections:

```md
# Context

This ordinary Markdown is documentation and is not executed.

## //verify

Run go test ./... and fix failures.

Run go vet ./... and fix actionable findings.

## /discuss

This whole section is one prompt.

Blank lines stay inside the prompt.

## Notes

This section is documentation again.
```

If a file contains at least one slash heading (`#{1,6} /...` or `#{1,6} //...`), only slash-heading sections are parsed as runnable content. A runnable section ends at the next heading with the same or higher level.

- `# /name`: single-task section. The whole section body is one task prompt, and blank lines remain part of the prompt.
- `# //name`: task-list section. The section body uses legacy block rules, so blank lines split multiple tasks.

In a single-task `# /name` section, lower-level Markdown headings remain part of the prompt. In a task-list `# //name` section, the legacy comment and blank-line rules apply to each task block.

Commands can be normalized with:

```sh
atm format -file todo.txt
```

Commands are only recognized before prompt text starts. A slash command written later in the prompt is treated as normal prompt content.

## Commands

### `/resume`

Run the prompt through the runner's resume mode.

For Codex this means:

```sh
codex exec resume --last -
```

For Claude Code this means:

```sh
claude -c -p "prompt"
```

### `/args ...`

Append CLI arguments to the selected tool for every flow in the current block.

```txt
/args --yolo
Run the selected tool with an extra flag.
```

`/args` can also share a line with `/for`:

```txt
/args --yolo /for 3
Run three times with the extra flag.
```

### `/cd path`

Prepare and enter a task workspace. If the directory does not exist, ATM creates it with `mkdir -p` semantics before running later commands or the prompt.

```txt
/cd services/payments
Implement the payment service in this workspace.
```

Use `--must-exist` when the task should fail instead of creating a missing directory:

```txt
/cd --must-exist backend
Run the backend checks.
```

The path is rendered with task variables, then resolved relative to the current task workdir or the original todo file directory. The resolved path must stay inside the original todo file directory. `/cd` affects Codex/Claude, `/bash`, `/let ... /bash`, natural-language `until` checks, and local CEL file functions. It does not change where ATM writes result blocks, output artifacts, or DB files.

### `/let name value`

Define a template variable. A standalone block containing only `/let` commands defines globals for later task blocks. Unused `/let` bindings are allowed, though comments are clearer for temporarily bypassing a task.

```txt
/let suite go test ./...

/for 3 until tests pass
Run {{suite}} and fix failures.
```

Inside a task block, `/let` defines local variables. A slash command matching a variable name inserts that value before the prompt:

```txt
/let context Read README.md first.
/context
Review the setup instructions.
```

`/let` can also capture bash stdout. Trailing newlines are removed before the value is rendered:

```txt
/let branch /bash git branch --show-current
Summarize release risk for {{branch}}.
```

### Template Rendering

ATM renders prompts, `/bash` scripts, `until` conditions, `/args` values, and `/cd` paths with Go `text/template`.

Existing variable placeholders remain supported:

```txt
Review {{path}} on pass {{N}}.
```

That legacy form is equivalent to:

```gotemplate
Review {{var "path"}} on pass {{var "N"}}.
```

Template data contains every current variable as a top-level key when the name is a valid Go template identifier, plus `.Vars` for map access:

```gotemplate
{{if .N}}Pass {{.N}}{{end}}
{{index .Vars "path"}}
{{var "path"}}
{{has "path"}}
```

Use `{{var "name"}}` or `{{index .Vars "name"}}` for names that contain `-`. Unknown legacy placeholders such as `{{future}}` are preserved as text; invalid Go template syntax fails the task.

### System-Provided Template Values

ATM provides different template values in different render contexts.

| Context | Values |
| --- | --- |
| Normal prompts, `/bash`, `/args`, `/cd`, `until`, `/return`, and `/output` schemas | User variables from `/let`, `/let ... /call`, `/for` loop variables, and definition parameters. |
| `/return` only | `{{agent.message}}`, `{{agent.last_message}}`, `{{agent.messages}}`, and `{{agent.messages_json}}` for the current definition call. |
| `/output` file names in background branches | `{{agent_index}}`, `{{agent}}`, and `{{agent_label}}`, plus normal variables. |

`agent.last_message` is not a global prompt variable. It exists when rendering `/return` because ATM has already run the definition body and collected recent assistant messages. To use an agent message in a later prompt, return it and bind it:

```txt
/let note /call reviewer api
Use this reviewer note:
{{note}}
```

### `/output [file]`

Save the task result into the run output directory. `/output` applies to the nearest current task block, may appear anywhere inside that block, and can appear at most once in that block. It can be written before or after `/for`, `/go`, `/wait`, `/bash`, or the prompt text.

Without a fenced schema block, `/output` saves the latest assistant message as text:

```txt
Summarize the release risk in one operator-readable note.

/output release-note
```

With a fenced schema block, `/output` requires structured JSON through a temporary MCP tool. The optional same-line file name chooses where ATM saves the reported JSON inside the run output directory; `name` and `name.json` both save as `name.json`. If omitted, ATM creates a time-based JSON file name and reports it in the task log.

ATM follows Markdown fence length rules, so ```` can wrap schema text that contains ``` inside it. ATM accepts plain fences, `json`, `yaml`, and `yml` fences:

````txt
Explain whether the release is ready.

/output summary.json
```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["reason"],
  "properties": {
    "reason": {"type": "string", "description": "Detailed reason"}
  }
}
```
````

YAML fences use the same JSON Schema shape, written as YAML:

````txt
Report the current weather.

/output
```yaml
type: object
required:
  - weather
properties:
  weather:
    type: string
    description: Weather state
```
````

Plain fences can also use a compact field list. Each non-empty line is `name:type:description`; `type` is optional and defaults to `string`, so `weather:Weather state` and `weather::Weather state` are both valid:

````txt
Return the release gate result.

/output result
```
reason:string:Detailed reason
weather:Weather state
passed:boolean:Whether the release gate passed
```
````

For Codex and Claude, ATM attaches a temporary MCP server exposing `atm_report_output`. The tool's input schema is built from the `/output` schema block, so the agent reports structured data by calling that tool. If the tool is not called, the task fails instead of being marked done.

File names are rendered with the same template context as prompts. Loop variables such as `{{N}}`, `{{area}}`, `{{dir}}`, and `{{path}}` are available. Background branches also expose `{{agent_index}}`, `{{agent}}` (file-safe), and `{{agent_label}}` (human-readable). When `/output` runs inside a `/go` branch, ATM automatically appends a branch suffix to the file name to avoid collisions.

### `/db`

Declare lightweight task databases for agent memory or blackboard state. A database stores `map[string][]string` and is exposed to Codex and Claude through an MCP server. `/db new` is a standalone global declaration block and does not run an agent:

```txt
/db new decisions scope:global persist:project access:write
Use for durable release decisions. Keys should use decisions/<topic>.
```

Declaration syntax:

```txt
/db new name [scope:local|global] [persist:run|project] [access:read|append|write|admin]
usage description
```

Defaults are `scope:global`, `persist:run`, and `access:admin`.

| Option | Meaning |
| --- | --- |
| `scope:global` | Visible to later task blocks by default. |
| `scope:local` | Declared but not mounted unless a task writes `/db use name`. |
| `persist:run` | Stored under the current run output directory, for example `.atm/20260521103000/db/name.json`. |
| `persist:project` | Stored under the project directory as `.atm/db/name.json`, so later runs can reuse it. |
| `access:read` | Read-only maximum access. |
| `access:append` | Read plus append-only writes. |
| `access:write` | Read, append, and replace keys, but no delete. |
| `access:admin` | Full read/write/delete access. |

The body after `/db new ...` is the usage description. ATM passes it to the DB MCP server, and `atm_db_list` returns it to the agent.

Task blocks can adjust their own DB view:

```txt
/db use scratch access:append
/db access decisions read
/db ignore obsolete
Review and update the shared notes.
```

Task-level syntax:

| Command | Meaning |
| --- | --- |
| `/db use name name2... [access:level]` | Mount local DBs or override visible DBs for this task. |
| `/db access name name2... level` | Set task-local access for named visible DBs. |
| `/db access * level` | Set task-local access for all currently visible DBs. |
| `/db ignore name name2...` | Hide named DBs from this task. |
| `/db ignore` | Hide all DBs from this task. |

Task-level access can only reduce or select access within the declaration's maximum access; it cannot elevate. `/db ignore` with no names cannot be combined with `/db use` or `/db access` in the same task block.

Access controls the DB MCP capabilities available to the agent:

| Access | MCP capabilities |
| --- | --- |
| `read` | `atm_db_list`, `atm_db_get`, `atm_db_scan` |
| `append` | `read` plus `atm_db_append` |
| `write` | `append` plus `atm_db_set` |
| `admin` | `write` plus `atm_db_delete` |

The MCP tools are:

| Tool | Arguments | Result |
| --- | --- | --- |
| `atm_db_list` | `{}` | Visible DBs, usage, access, and capabilities. |
| `atm_db_get` | `{"db":"name","key":"k"}` | One key and its string array. |
| `atm_db_scan` | `{"db":"name","pattern":"findings/**","limit":100,"cursor":""}` | Sorted matching keys with pagination. |
| `atm_db_append` | `{"db":"name","key":"k","values":["v"]}` | Appended key values. |
| `atm_db_set` | `{"db":"name","key":"k","values":["v"]}` | Replaced key values. |
| `atm_db_delete` | `{"db":"name","key":"k","values":["v"]}` or no `values` | Remaining values, or deleted key. |

`scan` accepts glob patterns. `*` matches within one slash-separated segment, and `**` can match across `/` segments. Writes are serialized with a DB lock file and written by temp-file rename so concurrent `append` calls do not overwrite each other.

Natural-language `until` and `/if` checks receive a read-only DB MCP server, even when the task itself has write access.

### `/skill` and `/mcp`

Declare local skills and temporary MCP servers, then opt task blocks into them:

````txt
/skill new reviewer from .atm/skills/reviewer

/mcp new helper
```json
{"command":"helper-mcp","args":["--stdio"]}
```

/cd work/release
/skill use reviewer
/mcp use helper
/mcp def use release_gate
Prepare the release notes.
````

`/skill use` copies the selected skill directory into the current `/cd` workdir using the layout expected by the selected adapter: `.agents/skills/<name>` for Codex and `.claude/skills/<name>` for Claude. The source path must already exist and contain `SKILL.md`; ATM creates the target adapter directories as needed.

`/mcp use` injects named MCP servers through temporary runner configuration. It does not write `.mcp.json` or persistent Codex config. `/mcp def use name...` exposes selected definitions to the agent as MCP tools named like `atm_def_<name>`, with one string argument per definition parameter. A def invoked this way inherits the task workdir, DBs, skills, and MCPs, but nested def-MCP exposure is disabled by default to avoid recursive agent self-dispatch.

### `/def`, `//def`, `/call`, and `/return`

Define reusable task templates with `/def` and call them with `/call`. Definitions are not run by themselves; they run only at a call site.

In Markdown task mode, `/def` and `//def` mirror the normal `/` and `//` task-heading rules:

```md
## /def whereami

Identify the current city from the repository context or available environment.

/return {{agent.last_message}}

## //def release_reviews area

/go reviewer
Review {{area}} implementation risks.

/go reviewer
Review {{area}} documentation risks.

/wait reviewer

/return Reviews for {{area}} are complete.
```

`/def` treats the whole section as one task template and preserves Markdown paragraph blank lines. `//def` treats the section as a list of task blocks, so each block runs in order when called. In legacy todo files, `/def name [params...]` defines a single task block.

Call arguments bind to definition parameters by position:

```txt
/call release_reviews checkout
```

When `/call` is a standalone task block, ATM simply executes the definition and ignores any return value. When `/call` appears as an independent line inside prompt text, ATM executes the definition first, replaces that line with the return text, then runs the surrounding task:

```txt
Get the weather for
/call whereami
today.
```

`/let name /call ...` executes the definition synchronously and binds its return value for later templates:

```txt
/let city /call whereami
Get the weather for {{city}}.
```

Definitions return values with `/return`. `/return` may be single-line, bash-backed, multiline, or structured:

```txt
/return {{city}}
/return /bash pwd

/return
City: {{city}}
Recent message: {{agent.last_message}}
```

Structured `/return` uses the same MCP output mechanism as structured `/output`, but the JSON is returned to the caller instead of being primarily used as a file artifact:

````txt
Assess the release gate.

/return
```json
{
  "type": "object",
  "required": ["passed", "reason"],
  "properties": {
    "passed": {"type": "boolean"},
    "reason": {"type": "string"}
  }
}
```
````

If a definition has both `/return` and `/output`, `/return` has priority as the call return value. If a definition has no `/return` but has structured `/output`, the output JSON becomes the fallback return value. If it has neither, it returns nothing. A call site that needs a value, such as `/let name /call ...` or an inline prompt `/call`, fails when the definition returns nothing.

Return templates can read recent assistant messages from the current definition call:

- `{{agent.last_message}}`: the most recent assistant message text.
- `{{agent.message}}`: alias for `{{agent.last_message}}`.
- `{{agent.messages}}`: the most recent N assistant messages joined as text.
- `{{agent.messages_json}}`: the same recent messages encoded as JSON.

N is the run's `-messages` value, which defaults to `1`.

Definitions may contain `/pool`, `/go`, and `/wait`. Pools declared inside a definition are local to that call, and all local or named pools still share the global `-jobs` concurrency limit. A definition call waits for its own remaining background branches before it returns.

Import definitions from another file with `/import`:

```txt
/import workflows/location.todo.md
/import weather from workflows/weather.todo.md
```

Imports load definitions only; ordinary runnable tasks in imported files are not executed. Paths are relative to the importing todo file. Namespaced imports are called as `weather.lookup`. ATM detects recursive definition calls, including cycles across imported files, and fails at plan/parse time.

### `/bash script`

Run a bash script before the prompt. The command inherits `ATM_TODO_FILE` and uses the current `/cd` task workdir when set; a non-zero exit status fails the task.

```txt
/bash go test ./...
Summarize the test result and fix failures if needed.
```

For longer scripts, use heredoc syntax:

```txt
/let changes /bash <<EOF
git status --short
git diff --stat
EOF

/bash <<'SH'
go test ./...
go build -buildvcs=false ./...
SH
Summarize {{changes}} and the verification result.
```

### `/for`

Run a prompt through one or more flows. Commands on the same line combine into the same flow.

Counted loop:

```txt
/for 3
Review the final diff. This is pass {{N}}.
```

Bounded retry with a completion condition:

```txt
/for 3 until tests pass
Fix the failing tests.
```

After each run, `atm` asks the selected tool to check the `until` condition. For Codex and Claude, `atm` attaches a temporary MCP server that exposes `atm_report_check`, and the agent must report the result through that tool.

Deterministic local retry with CEL:

```txt
/for 5 until(exists("result.json") && len(read("result.json")) > 0)
Generate result.json.
```

When `until` is followed by a parenthesized expression, ATM evaluates it locally with CEL instead of asking an agent to judge natural language. The expression must return `bool`. CEL checks are read-only and support `exists(path)`, `read(path)`, `json(path)`, `existsOutput(path)`, `readOutput(path)`, `jsonOutput(path)`, and `len(value)`. Relative paths are resolved under the original todo file directory; `*Output` functions read under the run output directory.

Unbounded CEL retry is written without a count:

```txt
/for until(exists("result.json") && json("result.json").passed)
Keep working until result.json reports passed=true.
```

This form keeps running until the CEL expression is true or the process is interrupted. It is intentionally separate from natural-language `until`: `/for until tests pass` is invalid. File paths in CEL functions must be quoted, for example `read("result.json")`; `read(result.json)` is parsed as variable access by CEL.

### `/if` and `/else`

Choose one task block. `/if` must be the first command in its task block. `/else` is optional for inline branches and required for header-only nested branches.

```txt
/if (exists("gate.json") && json("gate.json").passed)
Continue the release.

/else
Write a release blocker summary from gate.json.
```

`/if(...)` and `/if (...)` use local CEL, matching `until(...)`. `/if plain language condition` uses the selected tool's structured MCP check, matching natural-language `until`:

```txt
/if release gate is open
Continue the release.
```

If the condition is true, ATM runs the `/if` branch and marks the `/else` branch skipped. If it is false, ATM marks the `/if` branch skipped and runs the `/else` branch when present. Without `/else`, a false inline `/if` block is only marked skipped. Skipped blocks use the generated state format:

```txt
> [!ATM]
> status: skipped
> time: 2026-05-22 10:30
> reason: if condition evaluated false
```

Nested branches use header-only `/if` and `/else` blocks. Because ATM does not use indentation, pairing is structural: each `/else` belongs to the nearest unmatched header-only `/if`; header-only nested `/if` blocks must have a matching `/else`.

```txt
/if (exists("gate.json"))

/if (json("gate.json").passed)

Publish.

/else
Write the gate failure summary.

/else
Generate gate.json first.
```

In this example, the first `/else` belongs to the inner `/if`; the second `/else` belongs to the outer `/if`.

Directory and path loops:

```txt
/for dir
Review {{dir}}.

/for path
Review {{path}}.
```

Explicit list loop:

```txt
/for area in [api docs tests]
Review {{area}}.
```

Dynamic CEL list loop:

```txt
/for plan in(/call plan_shards)
{{plan}}
```

`in (...)` and `in(...)` are both accepted. The parenthesized expression is evaluated at runtime. It may be CEL returning a list, or `/call name` returning a list. If `/call` returns an object with a `plans` array, ATM expands that array. Scalar items render with `{{plan}}`; object items can be accessed with fields such as `{{area.name}}`.

The loop variable renders with `{{N}}`, `{{dir}}`, `{{path}}`, `{{area}}`, or fields from a dynamic object item.

Command order matters when `/for` and `/go` share a line:

```txt
/for 3 /go
Review shard {{N}}.
```

This starts one background branch per loop item. For readability, the same flow can be split across lines:

```txt
/for plan in(/call plan_shards)
/go reviewer
{{plan}}
```

The reverse order keeps the loop inside a single background task:

```txt
/go /for 3
Review shard {{N}}.
```

Both forms still write one generated `> [!ATM]` result block for the todo block. For `/for ... /go`, each loop item is labelled as a separate agent branch such as `N=1` or `area=api`; `-messages N` keeps the most recent N structured assistant messages for each branch, so the default `-messages 1` still shows one message from every parallel branch.

### `/pool name max [buffer]`

Declare a named background worker pool. Pool declarations are global blocks; they configure later `/go poolName` commands and do not run an agent.

```txt
/pool tester 5
```

The second argument is the maximum number of concurrently running branches in that named pool. By default, queued work is unlimited and submitting more branches does not block the flow. Add a third argument to cap the queue:

```txt
/pool tester 5 10
```

Named pools are also constrained by the global background limit. The global limit defaults to `NumCPU` and can be changed with `atm run -jobs N`.

### `/go`

Start the task in the background and continue scanning the todo file.

```txt
/go
Review documentation.
```

Use `/go poolName` to submit the background branch to a named pool:

```txt
/pool tester 2

/for area in [api docs tests] /go tester
Review {{area}}.
```

`atm` will not start the same unchanged block twice while it is in flight.

### `/wait`

Wait for all previously started `/go` tasks.

Standalone wait block:

```txt
/wait
```

Embedded wait before another prompt:

```txt
/wait
Run after background tasks finish.
```

Use `/wait poolName` to wait only for earlier work submitted to that named pool:

```txt
/wait tester
Summarize tester findings while other pools may still be running.
```

Before the process exits, `atm` waits for all remaining background work.

## Plan Dry-Run

Use `atm plan` to inspect how command order will be interpreted. It parses the todo file and prints global declarations, conditional control blocks, the flow for each runnable block, task-level DB/skill/MCP configuration, and runner arguments, but does not run bash, does not call the selected tool, and does not write result blocks.

```sh
atm plan -file todo.txt
```

`atm plan dry-run -file todo.txt` is accepted as an explicit alias.

Use `-html FILE` to save a browser-friendly flowchart, or `-open` to generate one and open it in the default browser:

```sh
atm plan -file todo.txt -html plan.html
atm plan -file todo.txt -open
```

Use `-json` when another tool should consume the IR plan:

```sh
atm plan -json -file todo.txt
```

The JSON format is intended as a tool-facing contract:

- `source`, `globals`, `tasks`, `tasks[].block`, `tasks[].prompt`, and `tasks[].ops` are stable fields.
- `imports`, `dbs`, `skills`, `mcps`, and `controls` expose parsed declaration or control blocks when present; `tasks[].db`, `tasks[].skill`, and `tasks[].mcp` expose task-local tool configuration when present.
- `ops[].kind` is stable; new operation kinds may be added in future minor versions.
- Existing operation fields are additive-compatible. Consumers should ignore unknown fields.
- Block numbers are 1-based and refer to the current source file snapshot.

## Result Blocks

Result blocks are generated state. They can be removed with `atm untag`.

### Done

```txt
Task prompt
> [!ATM]
> status: done
> started: 2026-05-08 14:30
> finished: 2026-05-08 14:32
> duration: 2m
> runs: 1x
>
> messages:
> - assistant (codex):
>   Fixed the parser tests.
```

Fields are status, start time, finish time, duration, total run count, and recent assistant messages. Codex is run with structured JSON events, and Claude Code is run with stream JSON, so `atm` can extract assistant messages without scraping terminal text.

### Running

```txt
Task prompt
> [!ATM]
> status: running
> started: 2026-05-08 14:30
> step: 1
> step-runs: 1x
> total-runs: 1x
```

Running blocks let interrupted or failed loops resume from the remaining work instead of starting from zero. `atm` only replaces a trailing generated `> [!ATM]` block while holding the block lease; edits to the task body make the lease obsolete and force a rescan.

## Subcommands

### `run`

Run pending prompt blocks. This is the default command, so these are equivalent:

```sh
atm -file todo.txt
atm run -file todo.txt
```

With no file argument, `atm run` uses the first existing default file from `todo.txt`, `todo.md`, or `toto.md`.

Use `-tool`, `-codex`, and `-claude` with either form. Use `-messages N` to choose how many recent structured assistant messages are kept per execution branch in each generated result block; the default is `1`. Use `-jobs N` to set the global maximum number of concurrently running background branches across all pools; by default it is `NumCPU`.

Multiple files are queued and executed in order. Pass them as positional arguments or repeat `-file`:

```sh
atm run todo.txt rollout.md followup.md
atm run -file todo.txt -file rollout.md
```

Execution artifacts are written to an output directory. By default, `atm` creates `.atm/YYYYMMDDHHMMSS`, adding `-1`, `-2`, and so on if a directory with the same timestamp already exists. Use `-output DIR` or `-o DIR` to choose a specific directory. The directory contains:

- `task-NNN-*.log`: combined human-readable stdout/stderr for a task block.
- `task-NNN-run-NNN-TOOL[-BRANCH].jsonl`: the native structured event stream emitted by the agent for that execution.
- `result.md`: the final todo document snapshot, including generated `> [!ATM]` result blocks, saved before later `untag` cleanup.

When one `-output DIR` is used with multiple input files, `atm` creates one numbered subdirectory per file under `DIR` so result documents and native event streams do not overwrite each other.

### `append`

Append one or more formatted blocks to a todo file:

```sh
atm append -file todo.txt "Run go test ./... and fix failures."
```

When the main runner has moved `todo.txt` to its active temp path, `append` resolves that active file automatically. If the current `atm run` still has work in progress, the appended task is picked up by a later scan in that same run. If the run has already exited, `append` only writes the todo file; run `atm run` again to execute the appended work.

With no prompt argument, `append` reads stdin. If stdin is a terminal, it opens `$VISUAL`, `$EDITOR`, or a small platform default editor.

### `format`

Rewrite generated state into a tidy block layout:

```sh
atm format -file todo.txt
```

Legacy generated markers are moved to their own line when needed. Current generated `> [!ATM]` result blocks are already block-formatted. Combined command lines such as `/resume /for 3` are preserved because commands on the same line share one flow.

### `untag`

Remove generated state:

```sh
atm untag -file todo.txt
atm untag -file todo.txt -done=false
atm untag -file todo.txt -running=false
```

### `mcp check`

Run the temporary stdio MCP server used by structured `until` checks:

```sh
atm mcp check -result-file /tmp/atm/check-result.json
```

This exposes one tool, `atm_report_check`, whose input is:

```json
{"passed": true, "summary": "brief evidence"}
```

The command is primarily for agent runtimes and tests. Normal users should keep using `/for N until condition`; Codex and Claude checks receive this server through temporary command-line MCP configuration.

### `mcp output`

Run the temporary stdio MCP server used by structured `/output` and structured `/return`:

```sh
atm mcp output \
  -result-file /tmp/atm/output.json \
  -schema-file /tmp/atm/schema.json \
  -schema-format json
```

This exposes `atm_report_output`. The schema file contains the JSON Schema generated from the task's fenced `/output` block.

### `mcp db`

Run the temporary stdio MCP server used by `/db`:

```sh
atm mcp db -config-file /tmp/atm/db-config.json
atm mcp db -config-file /tmp/atm/db-config.json -readonly
```

The config file is generated by ATM during a run and lists the DBs visible to the current task. `-readonly` forces all configured DBs to read-only access; ATM uses this for natural-language check agents.

The server exposes `atm_db_list`, `atm_db_get`, `atm_db_scan`, `atm_db_append`, `atm_db_set`, and `atm_db_delete`.

## Environment Variables

ATM keeps its environment surface deliberately small. These are the environment variables ATM sets or reads.

### Set By ATM

| Variable | Visible to | Meaning |
| --- | --- | --- |
| `ATM_TODO_FILE` | `/bash`, `/let ... /bash`, Codex, Claude, and temporary MCP tool processes | Absolute or active-path todo file for the current run. Use this when a script or agent needs to locate the todo document being processed. |

`ATM_TODO_FILE` is added to the child process environment in addition to the parent process environment. It may point at ATM's active temporary todo file while a run is in progress, not necessarily the original path passed with `-file`. When a task uses `/cd`, `/bash`, Codex, Claude, and check agents run in that task workdir, but `ATM_TODO_FILE` still points to the active todo file.

### Read By ATM

| Variable | Used by | Meaning |
| --- | --- | --- |
| `ATM_MCP_CHECK_LOG` | temporary `atm_report_check` MCP server | Optional debug log path for `until` checks. When set, ATM passes it into the check MCP server and the server appends check decisions there. |
| `VISUAL` | `atm append` | Preferred editor when `append` needs interactive input and stdin is a terminal. |
| `EDITOR` | `atm append` | Fallback editor when `VISUAL` is unset. |

ATM also inherits ordinary operating-system environment variables such as `PATH`, which affects how `codex`, `claude`, shell commands, and editors are found.

## Editing While Running

When `atm` runs, it temporarily moves the original todo file to the system temp directory and prints the active path. This prevents the runner from editing the same file path that is being restored on exit.

Use `atm append -file todo.txt ...` during a run. The command resolves the active temp file automatically.

## Cross-Platform Notes

- Paths passed to `-file` and `-codex` use the current shell's normal path syntax.
- Paths passed to `-claude` follow the same rule.
- Output logs are written under the operating system temp directory.
- On handled interrupts, the active todo file is restored before the process exits.
- On POSIX systems, interrupt and terminate signals are handled. On Windows, console interrupt is handled.
