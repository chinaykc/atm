# Command Reference

[中文](../zh/commands.md)

ATM means **Agent Task Markdown**. It is a Markdown-based DSL (domain-specific language) for scheduling agent tasks: normal Markdown carries context and notes, while `/task` and slash control commands define executable work.

Most ATM files only need plain prompts. Add command lines when you want a task to resume, pass tool arguments, run a flow more than once, stop on a condition, or run in the background.

Commands must appear at the start of a task block, before the prompt text.

## Task Boundaries

ATM uses one Markdown-native task boundary model for both `.txt` and Markdown files.

Plain text is parsed as document/background context. Executable work starts at `/task`, a task-start control command, or task header commands followed by prompt text. To run an ordinary prompt in a plain-text file, start it with `/task`:

```txt
/task
Prompt text sent to the runner.

/task
Next task block.
```

Blank lines are preserved inside the current prompt. They only help separate tasks when the next non-comment root-level line is a task-start/control command. Whole-line comments are ignored:

```txt
# ignored line
<!-- ignored HTML comment block -->
[//]: # (ignored reference comment)
[comment]: <> (ignored reference comment)
---
===
```

Standalone Markdown rule lines made only of three or more `-` or `=` characters are also ignored. Inline comments are not supported. A `#`, `---`, or `<!-- ... -->` later in a prompt line remains prompt text.

In Markdown task documents, headings define context and scope. They do not start tasks by themselves. A task starts with `/task`, a task-start control command, or task header commands followed by prompt text:

```md
# Context

This ordinary Markdown is documentation and is not executed.

## Verify

/for 2
Run go test ./... and fix failures.

/task
Run go vet ./... and fix actionable findings.

## Discuss

/task
This task sees the Discuss heading as context.

## Notes

This section is documentation again.
```

Ordinary Markdown before a task is preserved and passed as section context. A task continues until the next root-level task-start/control command after a blank line, the next same-or-higher heading, a report block, or the end of the document. Header-only blocks such as standalone `/let` declarations are scoped declarations when they have no prompt; the same `/let` line followed by prompt text is the current task's header. Deeper headings remain part of the task prompt.

If a deeper heading contains its own task-start command, ATM treats that command as a child-heading task. The child-heading task inherits the parent task's root prompt plus the ordinary Markdown in its own heading path. It does not inherit sibling child-heading text or sibling tasks:

```md
# Review

/task
Review backend.

### Scope 1

API and migrations.

/for 2
Fix tests {{n}}.

### Scope 2

Docs.

/task
Fix docs.
```

The Scope 1 task sees `Review backend.`, `Scope 1`, and `API and migrations.`. The Scope 2 task sees `Review backend.`, `Scope 2`, and `Docs.`. Child-heading tasks also inherit `/let` bindings from the parent task header, including lazy providers; a `/let` in the child section can shadow the inherited value. ATM runs pending child-heading tasks before the parent task. When the parent task finally runs, its prompt includes the completed child tasks' visible `> [!ATM]` report summaries under a `Completed child task reports` section. Skipped child tasks are not embedded.

Commands can be normalized with:

```sh
atm format todo.txt
```

Commands are only recognized before prompt text starts. A slash command written later in the prompt is an error unless it is a root-level sibling task after a blank line.

Task header commands can be written on one line or across multiple lines. Declaration commands such as `/task`, `/fork`, `/args`, `/output`, `/db use`, `/skill use`, and `/webhook use` are merged into the current task configuration. Flow commands such as `/cd`, `/bash`, `/call`, `/webhook name`, `/for`, `/go`, and `/wait` run in the order they appear.

An unquoted token that is itself a command starts the next header command. Quote such values when they are intended as data, for example `/bash printf "%s" "/task"`. A fenced `/output` schema or fenced `/webhook` payload must follow a header line containing only that payload command and its arguments.

## Commands

### `/task [name]`

Start a prompt task. A task name records the selected runner's session id after the task succeeds, so later tasks can resume that exact session.

```txt
/task review
Review the payment API and fix actionable findings.
```

Task names use the same identifier shape as variables: letters, digits, `_`, and `-`, with a non-digit first character.

### `/resume name`

Run the prompt through the runner's resume mode using the session recorded by `/task name`.

```txt
/task review
Review the payment API and fix actionable findings.

/resume review
Continue the review task session and verify the fixes.
```

ATM reads Codex and Claude session ids from structured runner output and stores them in the run state. `/resume name` requires a completed named task in the same managed run and the same selected tool adapter.

For Codex this means:

```sh
codex exec --json resume <thread-id> -
```

For Claude Code this means:

```sh
claude --resume <session-id> -p "prompt"
```

### `/fork name`

Fork the recorded session from `/task name` and run the current prompt from that session history.

```txt
/task base
Analyze the payment API and list the safest implementation plan.

/fork base
Try the fastest implementation path.

/task option_a /fork base
Try the minimal implementation path.

/task option_b /fork base
Try the compatibility-first implementation path.
```

Anonymous `/fork name` executes the branch without creating a reusable task session name. `/task new_name /fork name` records the new forked session under `new_name`. Claude Code uses `--resume <session-id> --fork-session`. For Codex, ATM materializes a new local rollout containing the parent session history, then executes the current prompt with `codex exec --json resume <new-thread-id> -`. Codex fork snapshots copy complete rollout records only; if the parent history is mid-turn, ATM marks that turn as interrupted in the fork snapshot.

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

The path is rendered with task variables, then resolved relative to the current task workdir or the original ATM file directory. The resolved path must stay inside the original ATM file directory. `/cd` affects Codex/Claude, `/bash`, `/let ... /bash`, natural-language `until` checks, and local expression file functions. It does not change where ATM writes result blocks, output artifacts, or DB files.

### `/let name value`

Define a template variable. A standalone block containing only `/let` commands defines variables for later task blocks in the current Markdown scope. Root-level `/let` bindings are visible to later tasks in the whole document; heading-level `/let` bindings are visible only to later tasks in that heading and its child headings. Sibling headings do not inherit them, and `/let` is not hoisted before its declaration. Unused `/let` bindings are allowed, though comments are clearer for temporarily bypassing a task.

Inside a task header, `/let` is local to that task block and its child-heading tasks. Lazy task-header providers inherited by a child task are resolved in the child task invocation and do not share cache with the parent. A child section `/let` with the same name shadows the parent task header value.

```txt
/let suite go test ./...

/for 3 until tests pass
Run {{suite}} and fix failures.
```

Inside a task block, `/let` defines local variables. Render variables in prompt text with `{{name}}`:

```txt
/let context Read README.md first.
Review the setup instructions with this context:
{{context}}
```

In Markdown task documents, `/context #Heading` is a task header command for adding ordinary documentation from another section to the task context. It can share a header line with other task commands, and it does not render a variable named `context`.

`/let` can also capture bash stdout. `/let name /bash ...` and `/let name /call ...` are lazy providers: ATM records the provider in the task header, does not run it during `atm check --plan` or `atm check`, and executes it only when the variable is actually rendered or read by an expression. The first read in the same task invocation caches the value, so repeated `{{name}}` references reuse one result. To execute a definition or shell command multiple times, use separate bindings or a direct `/call` or `/bash` command.

Because lazy providers are render-time dependencies, `atm check` and static `atm check --plan` report them as warnings instead of executing them. Lazy bash providers are marked as possible side-effect points; lazy call providers are marked as definition dependencies. In the plan flow, they appear as `LazyBash(name)` or `LazyCall(def -> name)`.

Trailing newlines are removed before a bash value is rendered:

```txt
/let branch /bash git branch --show-current
Summarize release risk for {{branch}}.
```

### `/flag type name description [default:value]`

Declare a document parameter. Values passed by CLI or HTTP API are injected as template variables with the same name:

```txt
/flag string name user name
/flag []int shards shard list default:1,2

/task
Report {{name}} on shards {{shards}}.
```

Supported types are `string`, `int`, `number`, `bool`, `[]string`, `[]int`, and `[]number`. Flags without defaults are required. `bool` defaults to `false` when omitted.

Single-file runs can pass document flags directly:

```sh
atm run api.todo.md -name Ada -shards 3 -shards 4
```

Document flags are rejected for multi-file runs.

Dynamic commands are explicit registrations. Local registrations live in `.atm/flag/index.json`; global registrations use the OS user config directory, resolved with Go's cross-platform `os.UserConfigDir()` and falling back to the user home `.atm` directory. Register one file explicitly, or scan `./.atm/flag` once to populate a registry:

```sh
atm flag register workflows/review.todo.md --name review --description "Run the review workflow"
atm flag register workflows/review.todo.md --name review -g
atm flag scan
atm flag scan -g
atm flag list
```

Dynamic commands reuse the normal `run` flags and expose the target ATM file's `/flag` declarations in `atm <command> -h`. They execute a source copy and write artifacts under `.atm/commands/<command>/<timestamp>/`.

### Template Rendering

ATM renders prompts, `/bash` scripts, `until` conditions, `/args` values, and `/cd` paths with Go `text/template`.

Existing variable placeholders remain supported:

```txt
Review {{file}} on pass {{n}}.
```

That placeholder form is equivalent to:

```gotemplate
Review {{var "file"}} on pass {{var "n"}}.
```

Template data contains every current variable as a top-level key when the name is a valid Go template identifier, plus `.Vars` for map access:

```gotemplate
{{if .n}}Pass {{.n}}{{end}}
{{index .Vars "path"}}
{{var "path"}}
{{has "path"}}
```

Use `{{var "name"}}` or `{{index .Vars "name"}}` for names that contain `-`. Unknown placeholders such as `{{future}}` are preserved as text; invalid Go template syntax fails the task.

### System-Provided Template Values

ATM provides different template values in different render contexts.

| Context | Values |
| --- | --- |
| Normal prompts, `/bash`, `/args`, `/cd`, `until`, `/return`, and `/output` schemas | User variables from `/let`, lazy `/let ... /bash` and `/let ... /call` providers once read, `/for` loop variables, and definition parameters. |
| `/return` only | `{{agent.message}}`, `{{agent.last_message}}`, `{{agent.messages}}`, and `{{agent.messages_json}}` for the current definition call. |
| `/output` file names in background branches | `{{agent_index}}`, `{{agent}}`, and `{{agent_label}}`, plus normal variables. |

`agent.last_message` is not a global prompt variable. It exists when rendering `/return` because ATM has already run the definition body and collected recent assistant messages. To use an agent message in a later prompt, return it and bind it:

```txt
/let note /call reviewer api
Use this reviewer note:
{{note}}
```

### `/output [file]`

Save the task result into the run output directory. `/output` is a task header command: write it before prompt text begins, after any control/header commands it should belong to. A task block can contain at most one `/output`; writing `/output` inside the prompt body is an error.

Without a fenced schema block, `/output` saves the latest assistant message as text:

```txt
/output release-note

Summarize the release risk in one operator-readable note.
```

With a fenced schema block, `/output` requires structured JSON through a temporary structured tool. The optional same-line file name chooses where ATM saves the reported JSON inside the run output directory; `name` and `name.json` both save as `name.json`. If omitted, ATM creates a time-based JSON file name and reports it in the task log.

ATM follows Markdown fence length rules, so ```` can wrap schema text that contains ``` inside it. Schema fences must use backticks, not tildes. ATM accepts plain fences, `schema`, `json`, `yaml`, and `yml` fences:

````txt
/output summary.json

Explain whether the release is ready.
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
/output

Report the current weather.
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

Plain fences and `schema` fences can also use a compact field list. Each non-empty line is `name:type:description`; `type` is optional and defaults to `string`, so `weather:Weather state` and `weather::Weather state` are both valid:

````txt
/output result

Return the release gate result.
```
reason:string:Detailed reason
weather:Weather state
passed:boolean:Whether the release gate passed
```
````

For Codex and Claude, ATM attaches a temporary task tool service exposing `atm_report_output`. The tool's input schema is built from the `/output` schema block, so the agent reports structured data by calling that tool. If the tool is not called, the task fails instead of being marked done.

File names are rendered with the same template context as prompts. Loop variables such as `{{n}}`, `{{area}}`, `{{dir}}`, and `{{file}}` are available. Background branches also expose `{{agent_index}}`, `{{agent}}` (file-safe), and `{{agent_label}}` (human-readable). When `/output` runs inside a `/go` branch, ATM automatically appends a branch suffix to the file name to avoid collisions.

### `/db`

Declare lightweight task databases for agent memory or blackboard state. A database stores `map[string][]string` and is exposed to Codex and Claude through an task tool service. `/db new` is a scoped declaration block and does not run an agent:

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
| `scope:global` | Mounted by default for later task blocks where this declaration is visible. |
| `scope:local` | Declared but not mounted unless a visible task writes `/db use name`. |
| `persist:run` | Stored under the current run output directory, for example `.atm/20260521103000/db/name.json`. |
| `persist:project` | Stored under the project directory as `.atm/db/name.json`, so later runs can reuse it. |
| `access:read` | Read-only maximum access. |
| `access:append` | Read plus append-only writes. |
| `access:write` | Read, append, and replace keys, but no delete. |
| `access:admin` | Full read/write/delete access. |

The body after `/db new ...` is the usage description. ATM passes it to the DB task tool service, and `atm_db_list` returns it to the agent.

`/db new` follows Markdown lexical scope. A root-level declaration is visible to later tasks in the whole document; a declaration under a heading is visible only to later tasks in that heading and its child headings. Sibling headings do not inherit it, and declarations are not hoisted before their declaration line. The `scope:global` and `scope:local` options only decide default mounting inside that visible declaration set.

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

Access controls the DB task tool capabilities available to the agent:

| Access | task tool capabilities |
| --- | --- |
| `read` | `atm_db_list`, `atm_db_get`, `atm_db_scan` |
| `append` | `read` plus `atm_db_append` |
| `write` | `append` plus `atm_db_set` |
| `admin` | `write` plus `atm_db_delete` |

The task tools are:

| Tool | Arguments | Result |
| --- | --- | --- |
| `atm_db_list` | `{}` | Visible DBs, usage, access, and capabilities. |
| `atm_db_get` | `{"db":"name","key":"k"}` | One key and its string array. |
| `atm_db_scan` | `{"db":"name","pattern":"findings/**","limit":100,"cursor":""}` | Sorted matching keys with pagination. |
| `atm_db_append` | `{"db":"name","key":"k","values":["v"]}` | Appended key values. |
| `atm_db_set` | `{"db":"name","key":"k","values":["v"]}` | Replaced key values. |
| `atm_db_delete` | `{"db":"name","key":"k","values":["v"]}` or no `values` | Remaining values, or deleted key. |

`scan` accepts glob patterns. `*` matches within one slash-separated segment, and `**` can match across `/` segments. Writes are serialized with a DB lock file and written by temp-file rename so concurrent `append` calls do not overwrite each other.

Natural-language `until` and `/if` checks receive a read-only DB task tool service, even when the task itself has write access.

### `/skill`

Declare local skills, then opt task blocks into them:

````txt
/skill new reviewer from .atm/skills/reviewer

/cd work/release
/skill use reviewer
Prepare the release notes.
````

`/skill use` copies the selected skill directory into the current `/cd` workdir using the layout expected by the selected adapter: `.agents/skills/<name>` for Codex and `.claude/skills/<name>` for Claude. The source path must already exist and contain `SKILL.md`; ATM creates the target adapter directories as needed.

`/skill new` is a scoped declaration block. A root-level declaration is visible to later tasks in the whole document; a declaration under a heading is visible only to later tasks in that heading and its child headings. Sibling headings do not inherit it, and declarations are not hoisted before their declaration line. `/skill use` must name a declaration visible from the current task, unless it is given a path-like value.


### `/def`, `/call`, and `/return`

Define reusable task templates with `/def` and call them with `/call`. Definitions are not run by themselves; they run only at a call site.

`/def` is a definition block with an explicit `/return` boundary. The definition body can contain ordinary task blocks, and those internal tasks are not added to the outer plan:

```md
/def whereami

Identify the current city from the repository context or available environment.

/return {{agent.last_message}}

/def release_reviews area

/go reviewer
Review {{area}} implementation risks.

/go reviewer
Review {{area}} documentation risks.

/wait reviewer

/return Reviews for {{area}} are complete.
```

Definitions must contain exactly one `/return`, and it must be the final definition block. Structured `/output` does not act as a return-value fallback. A `/return` outside a `/def` body is invalid; use ordinary Markdown text if a normal task should mention that word.

Markdown headings inside a `/def` body are treated as prompt text, not definition boundaries. `atm check` warns because headings inside definitions are easy to mistake for outer document structure.

Definitions follow Markdown lexical scope. A root-level `/def` is visible to later tasks in the whole document. A `/def` under a heading is visible only to later tasks in that heading and its child headings; sibling headings do not inherit it. To share a definition across sibling sections, put it before those sections in their common parent or at the document root. Definitions are not hoisted: a task cannot call a definition that appears later in the document.

Call arguments bind to definition parameters by position:

```txt
/call release_reviews checkout
```

When `/call` is a standalone task/header command, ATM executes the definition and ignores any return value unless it is assigned. Prompt text does not execute slash commands; to use a definition result in prompt text, bind it before the prompt with `/let name /call ...`:

```txt
/let city /call whereami
Get the weather for {{city}}.
```

Definitions return values with `/return`. `/return` may be single-line, bash-backed, multiline, or structured. Multiline text and multiline bash must use backtick fenced arguments; bare multiline text after `/return` is not valid:

````txt
/return {{city}}
/return /bash pwd

/return
```
City: {{city}}
Recent message: {{agent.last_message}}
```

/return /bash
```
git branch --show-current
```
````

Structured `/return` uses the same structured output mechanism as structured `/output`, but the JSON is returned to the caller instead of being primarily used as a file artifact. To avoid ambiguity with text returns, structured returns must use an explicit `json`, `yaml`, `yml`, or `schema` fence; an empty fence after `/return` is plain text:

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

Definitions must explicitly write `/return`. If a definition needs to return structured JSON, use structured `/return`; do not rely on structured `/output` as a fallback return value. `/output` is for file artifacts, while `/return` is for values consumed by callers.

Return templates can read recent assistant messages from the current definition call:

- `{{agent.last_message}}`: the most recent assistant message text.
- `{{agent.message}}`: alias for `{{agent.last_message}}`.
- `{{agent.messages}}`: the most recent N assistant messages joined as text.
- `{{agent.messages_json}}`: the same recent messages encoded as JSON.

N is the run's `-messages` value, which defaults to `1`.

Definitions may contain `/pool`, `/go`, and `/wait`. Pools declared inside a definition are local to that call, and all local or named pools still share the global `-jobs` concurrency limit. A definition does not implicitly insert `/wait` before `/return`; write `/wait` when the return value depends on background branches.

Import definitions from another file with `/import`:

```txt
/import workflows/location.todo.md
/import weather from workflows/weather.todo.md
```


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
go build ./...
SH
Summarize {{changes}} and the verification result.
```

### `/for`

Run a prompt through one or more flows. Commands on the same line combine into the same flow.

Counted loop:

```txt
/for 3
Review the final diff. This is pass {{n}}.
```

Counted loops bind lowercase `n` only. The values are zero-based, so `/for 3` runs with `n = 0`, `n = 1`, and `n = 2`; uppercase `N` is not generated.

Bounded retry with a completion condition:

```txt
/for 3 until tests pass
Fix the failing tests.
```

After each run, `atm` asks the selected tool to check the `until` condition. For Codex and Claude, `atm` attaches a temporary task tool service that exposes `atm_report_check`, and the agent must report the result through that tool.

Deterministic local retry with expressions:

```txt
/for 5 until(exist("result.json") && len(open("result.json")) > 0)
Generate result.json.
```

When `until` is followed by a parenthesized expression, ATM evaluates it locally with the local expression evaluator instead of asking an agent to judge natural language. The expression must return `bool`. Expression checks are read-only and support `exist(path)`, `open(path)`, `outputDir(path)`, `json(text)`, `yaml(text)`, `toml(text)`, `len(value)`, `range(...)`, `files([root])`, `dirs([root])`, `walkFiles([root])`, and `walkDirs([root])`. Relative paths are resolved under the current task workdir; `outputDir(path)` points at the run output directory even after `/cd`.

Unbounded expression retry is written without a count:

```txt
/for until(exist("result.json") && json(open("result.json")).passed)
Keep working until result.json reports passed=true.
```

This form keeps running until the expression is true or the process is interrupted. It is intentionally separate from natural-language `until`: `/for until tests pass` is invalid. File paths in expression functions must be quoted, for example `open("result.json")`; `open(result.json)` is parsed as variable access by the local expression evaluator.

### `/if` and `/else`

Choose one task block. `/if` is a task-block control command. `/else` is optional; when it is present, it must immediately follow the then task block. `/if` and `/else` do not nest. Use `/def` to package more complex branch bodies.

```txt
/if (exist("gate.json") && json(open("gate.json")).passed)
Continue the release.

/else
Write a release blocker summary from gate.json.
```

`/if(...)` and `/if (...)` use local expression, matching `until(...)`. `/if plain language condition` uses the selected tool's structured structured check, matching natural-language `until`:

```txt
/if release gate is open
Continue the release.
```

Long natural-language `/if` and `until` conditions can use a fenced text argument immediately after the command:

````txt
/if
```
release gate is open
and checks are green
```
Continue the release.

/for 3 until
```
tests pass
and lint passes
```
Fix the failures.
````

If the condition is true, ATM runs the `/if` branch and marks the `/else` branch skipped. If it is false, ATM marks the `/if` branch skipped and runs the `/else` branch when present. Without `/else`, a false inline `/if` block is only marked skipped. Skipped blocks use the generated state format:

```txt
> [!ATM]
> status: skipped
> time: 2026-05-22 10:30
> reason: if condition evaluated false
```

An empty `/else` immediately after its `/if` branch is legal and means false-branch no-op. It is rarely useful; `atm check` warns so the block can usually be removed.

`/if` can also appear inside a control chain. Command order defines the control flow: `/for /if /go`, `/for /go /if`, and `/if /go /for` are different. Multi-line control headers are equivalent to writing the same commands on one line, so prefer the multi-line form when it is easier to read:

```txt
/for 10
/if(n % 2 == 0)
/go

Review even shard {{n}}.

/wait
```

This filters the loop before dispatching background work. The following `/else` block can still provide a different prompt body for the same conditional task:

```txt
/for 3 /if(n == 1)
Review the selected shard {{n}}.

/else
Summarize why shard {{n}} is skipped.
```

If a branch needs several tasks or nested choices, define that workflow with `/def` and call it from the branch.

Directory and file loops:

```txt
/for dir in dirs()
Review {{dir}}.

/for file in files()
Review {{file}}.
```

Explicit list loop:

```txt
/for area in [api docs tests]
Review {{area}}.
```

Dynamic expression list loop:

```txt
/for plan in(/call plan_shards)
{{plan}}
```

Numeric expression range loop:

```txt
/for shard in range(1, 4)
Review shard {{shard}}.
```

`in expr`, `in(expr)`, and `in (expr)` are accepted for dynamic fan-out. The expression is evaluated at runtime. It may be local expression returning a list, or a parenthesized `/call name` returning a list, such as `in(/call plan_shards)`. If `/call` returns an object with a `plans` array, ATM expands that array. Scalar items render with `{{plan}}`; object items can be accessed with fields such as `{{area.name}}`. `range(stop)`, `range(start, stop)`, and `range(start, stop, step)` are available for Python-style integer ranges; `step` cannot be `0`. Use `files()` and `dirs()` for one-level entries under the task workdir, or `walkFiles()` and `walkDirs()` for recursive traversal; each accepts an optional root such as `walkFiles("src")` or `walkFiles(outputDir("reports"))`. Generated and dependency-heavy directories such as `.git`, `node_modules`, `vendor`, `dist`, and `build` are skipped. File and directory traversal uses the expression form. If the dynamic sequence is empty, ATM emits a runtime warning and skips the loop body.

The loop variable renders with `{{n}}`, `{{dir}}`, `{{file}}`, `{{area}}`, or fields from a dynamic object item. Fixed-count `/for number`, such as `/for 10`, remains supported and only binds lowercase `n`.

Command order matters when `/for` and `/go` share a line:

```txt
/for 3 /go
Review shard {{n}}.
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
Review shard {{n}}.
```

Both forms still write one generated `> [!ATM]` result block for the todo block. For `/for ... /go`, each loop item is labelled as a separate agent branch such as `n=0` or `area=api`; `-messages N` keeps the most recent N structured assistant messages for each branch, so the default `-messages 1` still shows one message from every parallel branch.

### `/pool name max [buffer]`

Declare a named background worker pool. Pool declarations are scoped declaration blocks: a root-level `/pool` is visible to later tasks in the whole document, while a heading-level `/pool` is visible only to later tasks in that heading and its child headings. Sibling headings do not inherit it, and `/pool` is not hoisted before its declaration. A pool declaration configures later `/go poolName` commands and does not run an agent.

```txt
/pool tester 5
```

The second argument is the maximum number of concurrently running branches in that named pool. By default, queued work is unlimited and submitting more branches does not block the flow. Add a third argument to cap the queue:

```txt
/pool tester 5 10
```

Named pools are also constrained by the global background limit. The global limit defaults to `NumCPU` and can be changed with `atm run -jobs N`.

### `/go`

Start the task in the background and continue scanning the ATM file.

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

`/go` does not imply a join. If the document contains background work with no later matching `/wait`, `atm check` reports a warning but still accepts the file. `atm run` exits when no foreground work remains; unmatched background blocks may remain marked `running` and are reported as left without `/wait`. This is useful for intentional monitor-style background agents, but most fan-out workflows should add an explicit `/wait`.

### `/wait`

Wait for all previously started `/go` tasks.

Standalone wait block:

```txt
/wait
```

`/wait` with no prompt is a pure join. `/wait` with prompt is a wait coordinator task: ATM starts the prompt while matching background work may still be running, gives it a coordination context with the wait scope, pending task block numbers, pool names, current visible ATM report/status, log paths, and the current cancellation capability. The current runtime reports cancellation as unavailable; the coordinator should still say when cancellation would be appropriate. ATM then waits for those tasks before marking the wait task done.

```txt
/wait
Watch the background reviews, summarize failures and follow-up work, and report whether manual intervention is needed.
```

Use `/wait poolName` to wait only for earlier work submitted to that named pool:

```txt
/wait tester
Summarize tester findings while other pools may still be running.
```

`atm check` warns when background work has no later matching `/wait`; write `/wait` when the workflow depends on those results.

### `/webhook new` and `/webhook name`

Declare a webhook target:

```txt
/webhook new notify provider:<generic|feishu|dingtalk> url:<URL|env:VAR> [secret:<VALUE|env:VAR>] [keyword:<word>] [keywords:<a,b>]
```

Send a webhook at a specific point in the flow:

```txt
/webhook notify Release {{version}} started.
```

You can also provide a complete JSON or YAML payload in a following fence. The payload is rendered as a template before it is parsed:

````txt
/webhook notify
```json
{"message":"Release {{version}} started"}
```
````

ATM has no built-in Feishu or DingTalk webhook URL. Create a custom bot in the target chat, then either reference environment variables:

```txt
/webhook new alarm provider:dingtalk url:env:DINGTALK_WEBHOOK secret:env:DINGTALK_SECRET keyword:monitor-alert
```

or provide credentials inline:

```txt
/webhook new alarm provider:dingtalk url:https://oapi.dingtalk.com/robot/send?access_token=... secret:SEC... keyword:monitor-alert
```

Environment variables are recommended. Inline URL and secret values remain in the document and may be copied into runtime artifacts or tool configuration files; do not commit documents containing real credentials.

Default text payloads:

| Provider | Payload and signing |
| --- | --- |
| `generic` | `{"message":"..."}`. If `secret:env:` is set, ATM sends it as `X-ATM-Webhook-Secret`. |
| `feishu` | `{"msg_type":"text","content":{"text":"..."}}`. If `secret:env:` is set, ATM adds Feishu `timestamp` and `sign` fields to the JSON body. |
| `dingtalk` | `{"msgtype":"text","text":{"content":"..."}}`. If `secret:env:` is set, ATM adds DingTalk `timestamp` and `sign` query parameters to the request URL. |

For DingTalk, ATM supports both official security modes:

- Custom keywords: declare `keyword:monitor-alert` or `keywords:monitor-alert,release-notice`. Up to 10 keywords are accepted. ATM checks that the rendered JSON payload contains at least one keyword before sending.
- Signing: declare `secret:env:<VAR>` or `secret:SEC...`. ATM computes `HmacSHA256(timestamp + "\n" + secret)`, Base64-encodes it, and sends URL-encoded `timestamp` and `sign` query parameters. A fixed `sign` value cannot be declared because it is tied to the current timestamp.

Missing env vars, payload rendering/parsing failures, and non-2xx HTTP responses fail the current task.

To let the agent decide whether to notify, authorize webhook targets for the current task:

```txt
/webhook use notify
Decide whether the release needs an external notification. Call the webhook tool only if needed.
```

Each authorized target becomes an agent-callable notification tool accepting either `message` or a full `payload` object. `/webhook use` explicitly authorizes those notification tools for the task; the agent still chooses whether to call one. Failed tool calls fail the task.

## Check Plan


```sh
atm check --plan todo.txt
atm check --plan todo.txt --preview
```

Use `--preview` when you explicitly want provider values in the plan. Preview mode may execute lazy `/let name /bash ...` providers and pure lazy `/let name /call ...` providers whose definitions can return without running an agent. It prints values in text or JSON output. It keeps agents, main-document reports, and `.atm/state.json` untouched. A lazy call provider whose definition needs runtime execution is listed as not executed.

Use `-html FILE` to save a browser-friendly flowchart, or `-open` to generate one and open it in the default browser. The HTML view shows parent/child task links, WaitAgent coordinator tasks, explicit `/wait` joins, and unjoined background work separately; it does not display an implicit final wait.

```sh
atm check --plan todo.txt -html plan.html
atm check --plan todo.txt -open
```

Use `-json` when another tool should consume the IR plan:

```sh
atm check --plan todo.txt -json
```

The JSON format is intended as a tool-facing contract:

- `source`, `document`, `globals`, `tasks`, `tasks[].block`, `tasks[].prompt`, and `tasks[].flow` are stable fields. `document.title` is the first level-1 heading when present; `document.sections` is the nested Markdown section tree with heading line, level, title, and path.
- `tasks[].context` summarizes default Markdown context that will be prepended to the task prompt. It reports line count, character count, and a preview rather than duplicating the full context text.
- `tasks[].decision` gives the planned task action and reason. It distinguishes foreground execution, `/go` background dispatch, pure `/wait` joins, `/wait` coordinator tasks, conditional execution, parent/child dependencies, and skipped/no-op branch reasons.
- `loops` summarizes every `/for` node with task/block, variable name, static values/count when known, dynamic source expression or call, `until` condition, and run options. Static loops expose their values; dynamic loops expose the source without executing it.
- `conditions` lists conditional tasks with their condition kind/text and the static branch outcome: true executes then and skips else when present; false skips then and either executes else or no-ops when no else branch exists.
- `tasks[].variables` lists `{{name}}` references found in the prompt or output config. Each item marks the source as `global-let`, `global-lazy-bash`, `task-let`, `task-lazy-bash`, `task-lazy-call`, `loop`, or `unresolved`; literal values are included when known without executing a provider.
- `tasks[].runtime` summarizes runtime environment inputs: `resume`, runner `args`, `workdirs` from `/cd`, pre-agent `bash` commands, and lazy providers. The same data remains available in `tasks[].flow`; `runtime` is the convenient tool-facing view.
- `async.background` lists tasks that dispatch background work, `async.joins` lists explicit `/wait` joins, and `async.unjoined` lists background work that has no later matching `/wait`; `fanout` describes the loop or single-branch source behind each background dispatch.
- `tasks[].flow.kind` and nested `children`/`elseChildren` describe control order; new flow kinds may be added in future minor versions.
- Existing flow fields are additive-compatible. Consumers should ignore unknown fields.
- Block numbers are 1-based and refer to the current source file snapshot.

## Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | Command succeeded; for `run`, all planned work for that invocation completed. |
| `1` | Execution failure, including agent, bash, filesystem, or tool-adapter errors. |
| `2` | CLI/DSL validation failure, including task syntax, expressions, duplicate report ids, unknown pool/db/skill references, or error-level `check` diagnostics. |
| `3` | Hard state inconsistency; inspect or repair the relationship between `.atm/state.json`, main-document report blocks, and task detail reports. |
| `130` | User interrupt. On POSIX, other terminating signals return `128 + signal`. |

`atm check` reports audit artifact mismatches as warnings by default and still exits `0`, because a missing detail report or orphan report often needs human review but does not necessarily make the DSL invalid. Exit code `3` is reserved for hard state inconsistency errors.

## Result Blocks

Result blocks are generated state in managed result documents. Direct `run` restores source files unchanged when the run exits.

### Done

```txt
Task prompt
<!-- atm:report v=2 id=task-prompt-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md status=done -->
> [!ATM]
> status: done
> started: 2026-05-08 14:30
> finished: 2026-05-08 14:32
> duration: 2m
> runs: 1x
> id: task-prompt-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md
>
> messages:
> - assistant (codex):
>   Fixed the parser tests.
<!-- /atm:report -->
```

Fields are status, start time, finish time, duration, total run count, stable report id, task source hash, rendered prompt hash, detail report path, and recent assistant messages. Codex is run with structured JSON events, and Claude Code is run with stream JSON, so `atm` can extract assistant messages without scraping terminal text.

Result blocks are wrapped by `<!-- atm:report ... -->` and `<!-- /atm:report -->`; ATM uses that boundary when replacing generated state. The inner `> [!ATM]` quote block is the visible summary for humans and agents. `id` is the stable identity for this task/report. `source` is the `sha256` of the task source at the time ATM writes the result block. `rendered` is the `sha256` of the prompt actually sent to the agent after variables and lazy providers resolve. `report` points to the task's detailed Markdown report under `~/.atm/runs/<run-id>/tasks/<task-id>/report.md`. Together they let ATM associate reports with tasks after document edits instead of relying only on document position. Duplicate `id` values in one document are errors in `atm check` and execution-time parsing; repair the duplicate identities before continuing.

### Running

```txt
Task prompt
<!-- atm:report v=2 id=task-prompt-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md status=running -->
> [!ATM]
> status: running
> started: 2026-05-08 14:30
> step: 1
> step-runs: 1x
> total-runs: 1x
> id: task-prompt-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md
<!-- /atm:report -->
```

Running blocks let interrupted or failed loops resume from the remaining work instead of starting from zero. `atm` only replaces a trailing generated `> [!ATM]` block while holding the block lease; edits to the task body make the lease obsolete and force a rescan. `id`, `source`, `rendered`, and `report` are written on running, done, and skipped states for later deduplication and report association. When a task finishes, ATM writes `report.md` in that task's run artifact directory; the main document keeps only the lightweight summary.

### Failed

```txt
Task prompt
<!-- atm:report v=2 id=task-prompt-6f2d9c8a41 source=sha256:6f2d9c8a41e3c2e2c... rendered=sha256:54c0ffee... report=/home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md status=failed -->
> [!ATM]
> status: failed
> started: 2026-05-08 14:30
> finished: 2026-05-08 14:31
> duration: 1m
> runs: 0x
> error: task 1 run failed: simulated failure
> id: task-prompt-6f2d9c8a41
> source: sha256:6f2d9c8a41e3c2e2c...
> rendered: sha256:54c0ffee...
> report: /home/me/.atm/runs/run-20260527-abc123/tasks/task-prompt-6f2d9c8a41/report.md
<!-- /atm:report -->
```

Failed tasks write a `failed` result block, a task-local `report.md` detail report, and `.atm/state.json` state in the managed run workspace. Use `atm resume <run-id>` to continue that workspace, or edit the unchanged source file and start a new run.

## Subcommands

### `run`

Run pending prompt blocks. This is the default command, so these are equivalent:

```sh
atm todo.txt
atm run todo.txt
```

Direct execution requires one or more positional ATM files.

Use `-tool`, `-codex`, and `-claude` with either form. Use `-messages N` to choose how many recent structured assistant messages are kept per execution branch in each generated result block; the default is `1`. Use `-jobs N` to set the global maximum number of concurrently running background branches across all pools; by default it is `NumCPU`.

Multiple positional files are queued and executed in order:

```sh
atm run todo.txt rollout.md followup.md
atm todo.txt rollout.md followup.md -jobs 4
```

Direct executions create a managed workspace under `~/.atm/runs/<run-id>/`. ATM copies the source file and recursive `/import` files into that directory, hides the original paths behind short placeholder files during execution, and restores the originals unchanged on exit. Use `ATM_HOME` to change the base directory. The run directory contains:

- `manifest.json`: run id, source path, copied sources/imports, status, and recovery commands.
- `sources/...`: source and import file copies captured before execution.
- `work/...`: the files actually executed by ATM, with import paths rewritten inside the run directory.
- `result.todo.md`: the final todo document snapshot, including generated `> [!ATM]` result blocks.
- `tasks/<task-id>/report.md`: the detailed Markdown report for one task.
- `tasks/<task-id>/logs/task-NNN-*.log`: human-readable stdout/stderr for that task.
- `tasks/<task-id>/task-NNN-run-NNN-TOOL[-BRANCH].jsonl`: the native structured event stream emitted by the agent for that execution.

ATM also maintains `.atm/state.json` and a short-lived `.atm/lock` next to the working file inside the run workspace. `state.json` is machine-recoverable state keyed by stable task/report id; it records status, source/rendered prompt hash, plan hash, report path, log paths, and run count. `sourcePromptHash` is based on the task source plus visible Markdown context that enters the prompt, including explicit `/context` sections and excluding `/doc` text. It does not include child task result reports. `renderedPromptHash` is based on the actual prompt sent to the agent, including variable/lazy-provider expansion and visible child task report summaries. `planHash` is based on the control-flow, output, and resource configuration shape. `-output DIR` changes only the output artifact directory; source/import backups, task directories, manifest, and `result.todo.md` remain in the managed run directory.

If a task block is deleted or edited enough to obsolete its lease while the task is running, ATM writes that task's `report.md`, marks `"orphan": true` in `.atm/state.json`, and prints an orphan report notice on the command line.

`atm check` validates obvious consistency issues across main-document report blocks, `.atm/state.json`, and task detail reports. It warns about missing detail reports, task ids present in state but absent from the main document, document/state status, report-path, source-hash, or rendered-prompt-hash mismatches, and orphan detail reports.

When one `-output DIR` is used with multiple input files, `atm` creates one numbered output subdirectory per file so native event streams do not overwrite each other.

Continue or recover managed runs:

```sh
atm resume <run-id>
atm resume --last
atm resume --restore-source
atm resume <run-id> --restore-source
```

`atm resume <run-id>` continues the managed run described by `~/.atm/runs/<run-id>/manifest.json`. `--last` selects the latest unfinished run from `~/.atm/runs/index.json`; use `--project` or `--source` to filter when several projects have unfinished runs. `--restore-source` copies the saved source back to the original path or an optional target path. Without a run id, restore mode selects the latest run copy for the current working directory, including successful and unfinished runs. Existing non-placeholder targets require confirmation, or `--force` in non-interactive use.

### `serve`

Serve either one explicitly supplied ATM file or the project's registered API files:

```sh
atm serve workflows/create.todo.md --addr 127.0.0.1:8080
atm serve register workflows/create.todo.md --path /user/create
atm serve scan
atm serve --addr 127.0.0.1:8080
atm serve list
atm serve unregister /user/create
```

Registrations are stored in `.atm/api/index.json`. Without a file argument, `serve` reads only that registry. `atm serve scan` is an explicit one-time import from `./.atm/api` into the current project's registry, skipping generated `runs/` and `jobs/`. Each route has both forms, such as `/user/create` and `/user/create.todo.md`. `GET` runs synchronously and `POST` creates an asynchronous job; both execute a source copy rather than write status into the registered source document. GET artifacts live under `.atm/api/runs/<route>/<timestamp>/`, POST job state and artifacts live under `.atm/api/jobs/<jobId>/`, and generated artifact paths are kept separate from API registrations.

Use `-g` with `serve register`, `serve scan`, `serve list`, or `serve unregister` to target the global API registry instead of the local project registry.

### `report`

Summarize task reports and ATM audit state without running agents, bash, or checks:

```sh
atm report
atm report <run-id>
atm report --source todo.txt
atm report -json
```

With no positional argument, `report` selects the latest run copy for the current project. A positional value may be a run id, a run directory, a manifest path, or a todo/result file. `--last`, `--project`, and `--source` select the latest matching run from `~/.atm/runs/index.json`. `report` reads the managed result document, `.atm/state.json`, and task detail reports. It prints counts for `done`, `running`, `failed`, `skipped`, and `draft`, then lists failed tasks, orphan reports, and recent log paths. `draft` means task blocks that still compile as pending executable work. Use `-json` when another tool or CI should consume the summary; task entries include available `id`, `status`, `report`, `source`, and `rendered` fields.

### `clean`

Remove generated ATM state without deleting user-authored document text:

```sh
atm clean todo.txt
atm clean todo.txt --reports
atm clean todo.txt --state
atm clean todo.txt --logs
atm clean todo.txt --all
atm clean --repair-ids result.todo.md
```

With no cleanup option, `clean` removes only generated report/status blocks from the target document. Because direct `run` leaves source files unchanged, this is mainly for result documents or manually copied generated blocks. Explicit options remove audit artifacts next to the target file: `--reports` deletes `.atm/reports/`, `--state` deletes `.atm/state.json`, `--logs` deletes `.atm/logs/`, and `--all` cleans the document blocks plus those `.atm` artifacts. `--repair-ids` repairs duplicate ATM report identities after generated blocks have been copied.

### `append`

Append one or more formatted blocks to an ATM file:

```sh
printf '/task\nRun go test ./... and fix failures.\n' | atm append todo.txt
```

`append` accepts the source todo path, then resolves the active file for that source. If the source is still being executed, the appended task is written to the current active working file and can be picked up by the live/rescan loop. If the run has already exited, `append` writes to the source file and the task runs next time.

The appended input must contain at least one task block such as `/task` followed by prompt text. With no prompt argument, `append` reads stdin. If stdin is a terminal, it opens `$VISUAL`, `$EDITOR`, or a small platform default editor.

### `format`

Rewrite generated state into a tidy block layout:

```sh
atm format todo.txt
```

Generated markers are moved to their own line when needed. Generated `> [!ATM]` result blocks use block formatting. Composed task headers are normalized to one command per line while preserving their merged configuration and flow order.

## Environment Variables

ATM keeps its environment surface deliberately small. These are the environment variables ATM sets or reads.

### Set By ATM

| Variable | Visible to | Meaning |
| --- | --- | --- |
| `ATM_TODO_FILE` | `/bash`, `/let ... /bash`, Codex, Claude, and temporary structured tool processes | Managed working ATM file for the current run. Use this when a script or agent needs to locate the todo document being processed. |

`ATM_TODO_FILE` is added to the child process environment in addition to the parent process environment. During direct runs it points at the managed working copy under `~/.atm/runs/<run-id>/`, not the original positional source path. When a task uses `/cd`, `/bash`, Codex, Claude, and check agents run in that task workdir, but `ATM_TODO_FILE` still points to the working copy.

### Read By ATM

| Variable | Used by | Meaning |
| --- | --- | --- |
| `ATM_HOME` | direct `run`, `resume` | ATM home directory; defaults to `.atm` under the current OS user's home directory. |
| `VISUAL` | `atm append` | Preferred editor when `append` needs interactive input and stdin is a terminal. |
| `EDITOR` | `atm append` | Fallback editor when `VISUAL` is unset. |

ATM also inherits ordinary operating-system environment variables such as `PATH`, which affects how `codex`, `claude`, shell commands, and editors are found.

## Editing While Running

When `atm run` starts, it creates a managed run workspace under `~/.atm/runs/<run-id>/`, copies the source and imported files there, and writes short placeholders at the original paths while the agent is running. The placeholders do not expose the run directory path; they only tell the agent to ignore that file.

`atm append todo.txt ...` resolves the active file for `todo.txt`. During an active run it writes to the current working file so the live/rescan loop can execute the appended task. After the run exits it writes to `todo.txt`, so the task is picked up by the next run.

## Cross-Platform Notes

- Positional todo paths and paths passed to `-codex` use the current shell's normal path syntax.
- Paths passed to `-claude` follow the same rule.
- Human-readable task logs are written under `~/.atm/runs/<run-id>/tasks/<task-id>/logs/`.
- On handled interrupts, the active ATM file is restored before the process exits.
- On POSIX systems, interrupt and terminate signals are handled. On Windows, console interrupt is handled.
