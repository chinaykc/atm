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

ATM supports two todo styles. If a document contains no slash heading, it uses the legacy style: each task is one block, and any number of blank or whitespace-only lines can separate blocks.

If a Markdown heading starts with a slash command, the document enters Markdown task mode. Only slash-heading sections run; ordinary Markdown sections are preserved as documentation. A single-slash heading such as `## /discuss` is one task whose prompt is the whole section, so Markdown paragraph blank lines are preserved. A double-slash heading such as `# //verify` is a task-list section whose contents are split into task blocks with the legacy rules.

Whole-line comments are ignored in legacy task blocks and `//` task-list sections: lines whose first non-space character is `#`, HTML comment blocks such as `<!-- ... -->`, and Markdown reference comments such as `[//]: # (...)`. In a single `/` Markdown section, lower-level headings remain prompt content. Standalone Markdown rule lines made only of three or more `-` or `=` characters are ignored in runnable content.

```txt
First prompt sent to the selected tool.

/for 2
Run this prompt twice.

/resume /for 3
Continue the previous tool session.
```

Markdown task mode example:

```md
# Release notes

This is documentation and is not executed.

## //verify

Run go test ./... and fix failures.

Run go vet ./... and fix actionable findings.

## /discuss

This whole section is one prompt.

Blank lines stay inside the prompt.
```

Commands go at the top of a block, before the prompt text. The supported commands are:

- `/resume`: continue the selected tool's most recent session.
- `/args ...`: append CLI arguments to the selected tool, for example `/args --yolo`.
- `/cd path`: prepare and enter a task workspace; missing directories are created by default. Use `/cd --must-exist path` to require an existing directory.
- `/let name value`: define a template variable; standalone `/let` blocks define globals.
- `/let name /bash script`: define a variable from bash stdout.
- `/let name /call def [args...]`: call a reusable definition and bind its return value.
- `/bash script`: run a bash script before the prompt; heredoc form is supported for multi-line scripts.
- `/output [file]`: require structured JSON through a temporary MCP tool and save it as an output artifact.
- `/db new/use/access/ignore ...`: declare task databases and control per-block MCP access.
- `/skill new/use/ignore ...`: declare local skills and mount selected skills into a `/cd` workdir.
- `/mcp new/use/ignore ...` and `/mcp def use ...`: inject temporary MCP servers and expose selected definitions as MCP tools.
- `/def name [params...]` and `//def name [params...]`: define reusable task templates.
- `/call name [args...]`: execute a definition; as a prompt line, inline its return text.
- `/return ...`: return text, bash output, or a multiline template from a definition.
- `/import [namespace from] path`: import definitions from another todo file.
- `/pool name max [buffer]`: declare a named background worker pool.
- `/for 3 [until condition]`: run up to `3` times; `{{N}}` renders as the run number.
- `/for until(expr)`: run until a local CEL expression is true. Parenthesized `until(...)` is deterministic local control flow; plain-language `until condition` still uses the tool-backed MCP check.
- `/if (expr)` or `/if natural language`: choose one branch with local CEL or the MCP check tool; nested header-only `/if` branches require a matching `/else`.
- `/for dir`, `/for path`, or `/for name in [a b]`: run once per directory, file path, or listed value.
- `/go [pool]`: start this task in the background, optionally in a named pool.
- `/wait [pool]`: wait for earlier `/go` tasks, optionally only one named pool.

Prompts, `/bash` scripts, `until` conditions, `/args` values, and `/cd` paths are rendered with Go `text/template`. Existing placeholders like `{{N}}`, `{{path}}`, and `{{branch}}` continue to work. New templates can also use `{{.N}}`, `{{index .Vars "path"}}`, `{{var "path"}}`, and control actions such as `{{if .N}}...{{end}}`.

For `until`, `atm` attaches a structured temporary MCP check tool named `atm_report_check`; the check agent must report through that tool. Finished and running state is written as a generated Markdown quote block starting with `> [!ATM]`. Codex and Claude output is read from structured streams, so the console can show the current task line range, tool call names, and assistant messages while the run is active. The most recent assistant message is also shown in the quote block by default.

`/db` attaches a temporary MCP server for task databases. Use `scope` to control which task blocks see a database, `persist` to choose run-local or project-local storage, and `access` to grant read, append, write, or delete capability per task block.

Background branches run under a global concurrency limit. It defaults to `NumCPU` and can be changed with `-jobs N`. Named `/pool` declarations add per-pool limits while still sharing that global limit.

Each run also writes artifacts under `.atm/YYYYMMDDHHMMSS[-N]` by default. Use `-output DIR` or `-o DIR` to choose a directory. Artifacts include per-run native agent JSONL event streams, run-local DB files under `db/`, and `result.md`, a copy of the final todo document before generated state is removed with `untag`.

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

Preview the execution plan without running bash or the selected tool:

```sh
./atm plan -file todo.txt
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
- Ready-to-edit examples: [examples](examples)
- Design notes: [docs/design.md](docs/design.md)
- Security policy: [SECURITY.md](SECURITY.md)

`atm` is written in Go, and supports Linux, macOS, and Windows.

## License

MIT. See [LICENSE](LICENSE).
