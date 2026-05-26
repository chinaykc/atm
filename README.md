# ATM

[中文文档](README.zh-CN.md)

**ATM** means **Agent Task Markdown**: a Markdown-based DSL (domain-specific language) for scheduling and coordinating agent tasks.

Use it when you have a few coding-agent jobs and want them to run in a clear order without setting up a database, daemon, web UI, or workflow system. Write prompts and slash commands in a Markdown or plain-text task file, preview the execution plan with `atm plan`, and let `atm` mark finished work in the same file.

For a guided manual with examples and diagrams, see [docs/user/README.md](docs/user/README.md).

## Quick Start

Build the CLI:

```sh
go build -buildvcs=false -o atm ./cmd/atm
```

Create `todo.txt`:

```txt
Run the test suite and fix any failures.

/for 3 until tests pass
Make the repository ready for release.

/go
Review the README for unclear setup steps.

/wait
```

Run it:

```sh
./atm
```

With no file argument, `atm` uses the first existing default file from `todo.txt`, `todo.md`, or `toto.md`.

The explicit subcommand is equivalent:

```sh
./atm run -file todo.txt
```

`run` is the live collaboration mode: it keeps rescanning the active todo file, so tasks appended while the command is still running can be picked up by that same `run`. Use `exec` for one-shot snapshot execution:

```sh
./atm exec todo.txt
```

`exec` freezes the task block set at startup while keeping the same tool flags, output directory, state, and report behavior as `run`. Tasks appended later remain in the document but are left for a later `run` or `exec`.

On Windows PowerShell:

```powershell
.\atm.exe -file todo.txt
```

By default, `atm` runs tasks through Codex:

```sh
./atm -tool codex -file todo.txt
```

To use Claude Code instead:

```sh
./atm -tool claude -file todo.txt
```

`-tool claude-code` is also accepted. If the executable is not on `PATH`, pass `-codex /path/to/codex` or `-claude /path/to/claude`.

Multiple todo files can be queued in one run:

```sh
./atm run todo.txt rollout.md followup.md
./atm run -file todo.txt -file rollout.md
```

## Common Uses

- Run a release checklist from top to bottom.
- Ask the selected tool to keep trying until a condition passes with `/for 3 until tests pass`.
- Start independent reviews with `/go`, then join them with `/wait`.
- Preview execution order without running anything with `atm plan`.
- Keep task state in a file you can edit, review, commit, or throw away.

## Todo Format

ATM supports plain text task files and Markdown task documents. In Markdown, headings are context and scope, not executable task declarations. A task starts at `/task`, a task-start control command such as `/for`, `/go`, `/call`, `/bash`, `/wait`, `/if`, or `/else`, or a task header command such as `/let`, `/args`, `/cd`, `/output`, `/db use`, `/skill use`, or `/mcp use` followed by prompt text. Use `/task` when the task has no header/control command and is just ordinary prompt text.

Ordinary Markdown before a task is preserved in the file and provided as context to tasks in that section. Prompt text starts after the task header; slash commands inside prompt text are rejected unless they start a new root-level task after a blank line.

Use `/context #Heading` in a Markdown task header to add ordinary documentation from another section to the task context. Use `/doc text` or `/doc` followed by a fenced block for human-only notes that stay out of agent context.

Whole-line comments are ignored in legacy text task blocks: lines whose first non-space character is `#`, HTML comment blocks such as `<!-- ... -->`, and Markdown reference comments such as `[//]: # (...)`. Standalone Markdown rule lines made only of three or more `-` or `=` characters are ignored in runnable content.

```txt
First prompt sent to the selected tool.

/for 2
Run this prompt twice.

/resume /for 3
Continue the previous tool session.
```

Markdown task document example:

```md
# Release notes

This is documentation and is not executed.

## Verify

/for 2
Run go test ./... and fix failures.

/task
Run go vet ./... and fix actionable findings.

## discuss

/task
This whole section is one prompt.

Blank lines stay inside the prompt.
```

Commands go at the top of a block, before the prompt text. The supported commands are:

- `/resume`: continue the selected tool's most recent session.
- `/args ...`: append CLI arguments to the selected tool, for example `/args --yolo`.
- `/cd path`: prepare and enter a task workspace; missing directories are created by default. Use `/cd --must-exist path` to require an existing directory.
- `/let name value`: define a template variable; standalone `/let` blocks are visible to later tasks in the current Markdown scope.
- `/let name /bash script`: define a lazy variable from bash stdout; it runs only when the variable is rendered or read.
- `/let name /call def [args...]`: lazily call a reusable definition and bind its return value.
- `/bash script`: run a bash script before the prompt; heredoc form is supported for multi-line scripts.
- `/context #Heading`: add another Markdown section's ordinary documentation to the current task context.
- `/doc text` or `/doc` plus a fenced block: keep human-only notes out of agent context.
- `/output [file]`: require structured JSON through a temporary MCP tool and save it as an output artifact.
- `/db new/use/access/ignore ...`: declare task databases and control per-block MCP access.
- `/skill new/use/ignore ...`: declare local skills in the current Markdown scope and mount selected skills into a `/cd` workdir.
- `/mcp new/use/ignore ...` and `/mcp def use ...`: declare scoped temporary MCP servers and expose selected definitions as MCP tools.
- `/def name [params...]` plus `/return`: define reusable task templates.
- `/call name [args...]`: execute a definition as a task/header command; use `/let name /call ...` before prompt text when the return value must be rendered.
- `/return ...`: return text, bash output, or a multiline template from a definition.
- `/import [namespace from] path`: import definitions from another todo file.
- `/pool name max [buffer]`: declare a named background worker pool.
- `/for 3 [until condition]`: run up to `3` times; `{{n}}` renders as the zero-based loop index (`0`, `1`, `2`).
- `/for until(expr)`: run until a local expression is true. Parenthesized `until(...)` is deterministic local control flow; plain-language `until condition` still uses the tool-backed MCP check.
- `/if (expr)` or `/if natural language`: choose one branch with local expression or the MCP check tool; `/if` and `/else` do not nest.
- `/for name in files()`, `/for name in walkFiles("src")`, `/for name in dirs()`, or `/for name in [a b]`: run once per file, directory, or listed value. File and directory traversal is only expressed through the `files()`, `dirs()`, `walkFiles()`, and `walkDirs()` expression helpers; legacy `/for file`, `/for dir`, and `/for path` headers are invalid.
- `/go [pool]`: start this task in the background, optionally in a named pool.
- `/wait [pool]`: wait for earlier `/go` tasks, optionally only one named pool.

Prompts, `/bash` scripts, `until` conditions, `/args` values, and `/cd` paths are rendered with Go `text/template`. Placeholders like `{{n}}`, `{{file}}`, and `{{branch}}` are available when those variables are in scope. New templates can also use `{{.n}}`, `{{index .Vars "file"}}`, `{{var "file"}}`, and control actions such as `{{if .n}}...{{end}}`.

For `until`, `atm` attaches a structured temporary MCP check tool named `atm_report_check`; the check agent must report through that tool. Finished and running state is written as a generated Markdown quote block starting with `> [!ATM]`. Codex and Claude output is read from structured streams, so the console can show the current task line range, tool call names, and assistant messages while the run is active. The most recent assistant message is also shown in the quote block by default.

`/db` attaches a temporary MCP server for task databases. Use `scope` to control which task blocks see a database, `persist` to choose run-local or project-local storage, and `access` to grant read, append, write, or delete capability per task block.

Background branches run under a global concurrency limit. It defaults to `NumCPU` and can be changed with `-jobs N`. Named `/pool` declarations add per-pool limits while still sharing that global limit.

Each run also writes artifacts under `.atm/YYYYMMDDHHMMSS[-N]` by default. Use `-output DIR` or `-o DIR` to choose a directory. Artifacts include per-run native agent JSONL event streams, run-local DB files under `db/`, and `result.md`, a copy of the final todo document before generated state is removed with `clean` or `untag`.

## Handy Commands

Append work while `atm` is still running. If the current run has already exited, run `atm` again to execute the appended task:

```sh
./atm append -file todo.txt "Add focused tests for the parser."
```

Format a todo file:

```sh
./atm format -file todo.txt
```

Remove generated state blocks:

```sh
./atm untag -file todo.txt
```

Summarize current task reports and audit state:

```sh
./atm report todo.txt
```

Clean generated document state while keeping audit artifacts:

```sh
./atm clean todo.txt
```

Clean selected audit artifacts explicitly:

```sh
./atm clean todo.txt --reports --state --logs
```

Repair duplicate generated report ids after copying task blocks:

```sh
./atm repair-ids todo.txt
```

Preview the execution plan without running bash or the selected tool:

```sh
./atm plan todo.txt
```

Preview lazy bash provider values explicitly:

```sh
./atm plan --preview todo.txt
```

Write a browser-friendly HTML flowchart:

```sh
./atm plan -file todo.txt -html plan.html
```

Open the flowchart in the default browser:

```sh
./atm plan -file todo.txt -open
```

For tooling, render the same plan as JSON:

```sh
./atm plan -json -file todo.txt
```

## More

- Full command reference: [docs/commands.md](docs/commands.md)
- Ready-to-edit examples: [quick start](examples/quick-start.todo.md), [simple](examples/simple.todo.md), and [complex](examples/complex.todo.md)
- Design notes: [docs/design.md](docs/design.md)
- Security policy: [SECURITY.md](SECURITY.md)

`atm` is written in Go, and supports Linux, macOS, and Windows.

## License

MIT. See [LICENSE](LICENSE).
