# Command Manual

[中文](../../../zh/user/reference/commands.md)

This page is the user-facing command quick reference. The full low-level syntax reference is [../../commands.md](../../commands.md).

## Task Commands

| Command | Purpose | Common position |
| --- | --- | --- |
| `/task [name]` | Start a prompt task; a name records the agent session | Task start |
| `/resume name` | Continue the recorded session from a named task | Task start |
| `/fork name` | Fork the recorded session from a named task | Task start |
| `/args ...` | Add Codex/Claude arguments | Task start |
| `/cd path` | Prepare and enter a task workdir | Task start |
| `/let name value` | Define a variable | Task header or scoped declaration |
| `/let name /bash ...` | Lazily capture bash stdout | Task header or scoped declaration |
| `/let name /call ...` | Lazily call a definition and bind the return value | Task header |
| `/flag type name ...` | Declare CLI/API parameters | Document level |
| `/bash ...` | Run bash before the prompt | Task header |
| `/webhook new ...` | Declare a webhook target | Scoped declaration |
| `/webhook name ...` | Send a webhook message | Task header |
| `/webhook use name...` | Allow agent-controlled webhook notifications | Task header |
| `/context #Heading` | Include another Markdown section's ordinary docs | Markdown task header |
| `/doc text` or `/doc` plus fence | Human-only notes outside agent context | Markdown body |
| `/output [file]` | Save text or structured JSON output | Task header |
| `/db new/use/access/ignore ...` | Declare or control task databases | Declaration or task header |
| `/skill new/use/ignore ...` | Declare or mount local skills | Declaration or task header |
| `/def name ...` | Define a reusable task template | Definition block |
| `/call name ...` | Call a definition | Task/header command |
| `/return ...` | Return from a definition | Inside definition |
| `/import ...` | Import definitions | Scoped declaration |
| `/for ...` | Loop, retry, or iterate | Task start |
| `/pool name max [buffer]` | Declare a worker pool | Scoped declaration |
| `/go [pool]` | Run the following task suffix in the background | Task start |
| `/wait [pool]` | Wait for background tasks | Task start |

## Templates

ATM renders prompts, `/bash`, `/args`, `/cd`, `until`, `/return`, and `/output` schemas with Go `text/template`:

```txt
Review {{file}} on pass {{n}}.
{{var "name-with-dash"}}
{{index .Vars "path"}}
{{has "path"}}
```

Lazy `/let ... /bash` and `/let ... /call` providers run only when read. Static `check` and `check --plan` report lazy providers as warnings; `check --plan --preview` may execute preview-safe providers.

## Document Flags

```txt
/flag string name user name
/flag []int shards shard list default:1,2

/task
Report {{name}} on shards {{shards}}.
```

Single-file runs can pass flags directly:

```sh
atm run api.todo.md -name Ada -shards 3 -shards 4
```

Register a dynamic command:

```sh
atm flag register workflows/review.todo.md --name review
atm flag scan
atm flag list
```

## Common Combinations

Retry until a condition passes:

```txt
/for 3 until tests pass
Run tests and fix failures.
```

Local expression condition:

```txt
/for until(exist("result.json") && json(open("result.json")).passed)
Keep fixing result.json.
```

Parallel review:

```txt
/pool reviewer 3

/for area in [api docs tests] /go reviewer
Review {{area}}.

/wait reviewer
```

Structured output:

````txt
/output gate
Decide whether release can proceed.

```schema
passed:boolean:whether it passed
reason:string:short reason
```
````

For details, use [../../commands.md](../../commands.md).
