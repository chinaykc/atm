# ATM

[中文文档](README.zh-CN.md)

<p align="center">
  <img src="docs/assets/atm-logo-usd.svg" alt="ATM" width="420">
</p>

**ATM** stands for **Agent Task Markdown**: a Markdown runbook format for coding agents.

It is for the space between one-off prompts and full workflow systems. When a project needs several agent tasks, such as running checks, reviewing docs, validating examples, and applying fixes, a chat thread quickly becomes hard to audit and repeat. ATM lets you write that work down as a readable `todo.md`: project context stays as normal Markdown, and a small set of slash commands describes how the work should run.

ATM turns the file into an agent execution plan. Tasks can run in order, retry until a condition passes, branch into parallel checks, wait for each other, and produce structured results. You can preview the plan before any agent starts, run it through Codex or Claude Code, and reuse workflows as local commands or HTTP APIs.

## When To Use It

- Ask AI to draft and refine ATM files, then let the workflow evolve with the project instead of living only in chat history.
- Turn repeated prompt work into reusable runbooks, so agents can spend more time executing and less time being re-instructed.
- Try different collaboration patterns, from sequential review to parallel checks and multi-agent style handoffs, without building a workflow service first.
- Capture complex project procedures as Markdown that both humans and agents can read, review, run, and improve.
- Expose recurring workflows as local commands or small HTTP APIs when they become part of everyday project operations.

## Requirements

- Codex on `PATH` for the default runner, or Claude Code on `PATH` when using `--tool claude`.
- Linux, macOS, or Windows.
- Go 1.25 or newer only when building from source.

If a runner is not on `PATH`, pass its executable path with `--codex /path/to/codex` or `--claude /path/to/claude`.

## Install

Download the archive for your platform from GitHub Releases, unzip it, and put the `atm` binary on `PATH`.

Check the installed binary:

```sh
atm --version
```

To build from source instead:

```sh
go build -o atm ./cmd/atm
```

## Quick Start

Create `todo.md`:

```txt
/for 3 until tests pass
Run the project test suite and fix any failures.

/go
Check the setup documentation for steps that are missing, stale, or unclear.
Write findings to `checks/setup.md`.

/go
Check examples, scripts, and configuration files for commands that no longer work.
Write findings to `checks/commands.md`.

/wait

/task
Read `checks/setup.md` and `checks/commands.md`, then fix the confirmed project issues.
```

Preview the execution plan. `check` does not start an agent:

```sh
./atm check --plan todo.md
```

Open the plan flowchart in a browser:

```sh
./atm check --open todo.md
```

Run it:

```sh
./atm run todo.md
```

The default command is also `run`, so this is equivalent:

```sh
./atm todo.md
```

On Windows PowerShell:

```powershell
.\atm.exe run todo.md
```

Select a runner explicitly:

```sh
./atm run --tool codex todo.md
./atm run --tool claude todo.md
```

`--tool claude-code` is also accepted. Multiple ATM files can be queued in one run:

```sh
./atm run --jobs 4 todo.md rollout.md followup.md
```

## ATM File Format

In Markdown ATM files, headings provide context and scope. They do not start tasks by themselves. Executable work starts at `/task`, a control command such as `/for`, `/go`, `/wait`, `/if`, or `/else`, or a task header command followed by prompt text.

```md
# Project context

This ordinary Markdown is context for tasks in this section.

## Documentation

/task docs
Review the docs with the section context above.
```

Task command quick reference:

| Command | Use |
| --- | --- |
| `/task [name]` | Start a prompt task and optionally name its agent session. |
| `/resume name` | Continue a named session from an earlier task. |
| `/fork name` | Fork a named session and run the current task from that history. |
| `/args ...` | Append CLI arguments to the selected runner for this task. |
| `/cd path` | Prepare and enter a task workspace. |
| `/let name value` | Define a template variable, or a lazy value from `/bash` or `/call`. |
| `/bash script` | Run a shell script before the prompt. |
| `/output [file]` | Save task output; with a schema fence, require structured JSON. |
| `/db ...` | Attach a local JSON task database as memory or a blackboard. |
| `/skill ...` | Declare or mount local skills for the task workspace. |
| `/mcp ...` | Declare MCP servers, expose definitions, or grant task access to MCP tools. |
| `/webhook ...` | Declare webhook targets or send webhook notifications. |
| `/def` + `/call` | Define and reuse task templates. |
| `/return ...` | Return text, bash output, or a multiline template from a definition. |
| `/import ...` | Import definitions from another ATM file. |
| `/pool name max [buffer]` | Declare a named background worker pool. |
| `/if condition` | Choose a branch with a local expression or structured check. |
| `/else` | Provide the alternate branch for `/if`. |
| `/for ...` | Retry, loop, or iterate over files, directories, ranges, or lists. |
| `/go [pool]` | Start a task in the background. |
| `/wait [pool]` | Wait for earlier background tasks. |
| `/flag ...` | Declare parameters for `atm run`, dynamic commands, and `serve` APIs. |
| `/context #Heading` | Add another Markdown section's ordinary documentation to task context. |
| `/doc ...` | Keep human-only notes out of agent context. |

For the complete format, see the [user manual](docs/en/user/README.md) and [command reference](docs/en/commands.md).

## Reuse Workflows

Any ATM file can be run directly, registered as a project-local command, or exposed through `serve`. Add `/flag` when the reused workflow should accept CLI or API parameters.

```md
/flag string area project area to review
/flag bool fix apply safe fixes default:false

/task
Review {{area}}. If {{fix}} is true, apply safe fixes.
```

Register it as a local command:

```sh
./atm flag register workflows/review.md --name review
./atm review -area docs -fix
```

Serve an ATM file as an HTTP API:

```sh
./atm serve workflows/review.md --addr 127.0.0.1:8080
```

For reusable project APIs, register routes:

```sh
./atm serve register workflows/review.md --path /review
./atm serve --addr 127.0.0.1:8080
```

`GET /review` runs synchronously. `POST /review` creates an async job, and `GET /jobs/{id}` reads job state. `GET /openapi.json` returns generated OpenAPI metadata.

## Results

`atm run` works from a managed live copy and restores the original source file when the run exits.

| Item | Default location |
| --- | --- |
| Run workspace | `~/.atm/runs/<run-id>/` |
| Result document | `~/.atm/runs/<run-id>/result.todo.md` |
| Task reports, logs, events, and outputs | `~/.atm/runs/<run-id>/tasks/<task-id>/` |
| Project-local dynamic command artifacts | `.atm/commands/<command>/<timestamp>/` |
| Project-local API artifacts | `.atm/api/runs/...` and `.atm/api/jobs/...` |

Use `ATM_HOME` to choose a different ATM home. `--output DIR` or `-o DIR` redirects the shared output directory, but source backups, task directories, and `result.todo.md` remain in the managed run workspace.

Unfinished runs can be continued with:

```sh
./atm resume <run-id>
./atm resume --last
```

If a run is interrupted while a source file is hidden behind a placeholder, recover the latest saved source copy with:

```sh
./atm resume --restore-source
```

## How It Works

ATM compiles the ATM file into a static plan before execution. `atm check --plan` prints that plan without running bash scripts or agents, so loops, branches, background work, pools, and task dependencies can be reviewed first.

During `atm run`, ATM copies the source ATM file and imports into a managed run workspace, executes the live working copy, and restores the original source file when the run exits. Status blocks, reports, runner event streams, structured outputs, and logs are written under the run workspace for auditability.

Natural-language checks such as `/for 3 until tests pass` are implemented as structured tool calls. After each attempt, ATM exposes a scoped MCP check tool to the runner, currently through a local loopback streamable HTTP endpoint by default, and asks the model to report whether the condition is satisfied with machine-readable fields rather than free-form prose. Local expression forms such as `/for until(expr)` skip the model and are evaluated deterministically.

ATM uses scoped MCP tools for structured-output boundaries because modern coding agents are trained to use tools and MCP-style schemas reliably. Whenever ATM needs a structured decision or result, such as an `until` decision, schema-backed `/output`, or definition-backed helper output, it exposes a narrowly scoped local MCP endpoint and reads the tool result instead of scraping assistant text. Features such as `/db` and `/webhook` also use scoped tools where the agent needs controlled access to local state or external notifications.

## Handy Commands

CLI command quick reference:

| Command | Use |
| --- | --- |
| `atm --version` | Print the CLI version, commit, and build time when available. |
| `atm run [files...]` | Run pending prompt blocks; also the default command. |
| `atm resume ...` | Resume an unfinished managed run or restore saved source files; supports `--last` and `--restore-source`. |
| `atm flag register/scan/unregister/list` | Manage ATM files registered as dynamic CLI commands. |
| `atm append <file> [prompt...]` | Append a formatted task to a source file or active run. |
| `atm check [files...]` | Validate ATM files without running agents. |
| `atm report ...` | Summarize task reports and audit state. |
| `atm clean ...` | Remove generated state/report blocks or audit artifacts. |
| `atm format <file>` | Normalize task headers and generated state layout. |
| `atm serve [file]` | Serve one ATM file as an HTTP API. |
| `atm serve register/scan/unregister/list` | Manage registered API ATM files. |

Common examples:

| Command | Use |
| --- | --- |
| `./atm check todo.md` | Validate an ATM file without running it. |
| `./atm check --plan todo.md` | Print a dry-run execution plan. |
| `./atm check --plan --preview todo.md` | Include previewable lazy provider values in the plan. |
| `./atm check --open todo.md` | Open a temporary plan flowchart in a browser. |
| `./atm check --plan todo.md --html plan.html` | Write the same flowchart to a file. |
| `./atm append todo.md 'Review README.'` | Append a formatted task to a source or active run. |
| `./atm format todo.md` | Normalize task headers and generated state layout. |
| `./atm report` | Summarize the latest run for the current project. |
| `./atm clean result.todo.md` | Remove generated status blocks while keeping audit artifacts. |
| `./atm clean --repair-ids result.todo.md` | Repair duplicate generated report ids. |

## More

- User manual: [docs/en/user/README.md](docs/en/user/README.md)
- CLI manual: [docs/en/user/reference/cli.md](docs/en/user/reference/cli.md)
- Command reference: [docs/en/commands.md](docs/en/commands.md)
- Ready-to-edit examples: [quick start](examples/en/quick-start.todo.md), [simple](examples/en/simple.todo.md), and [complex](examples/en/complex.todo.md)
- Design notes: [docs/en/design.md](docs/en/design.md)
- Security policy: [SECURITY.md](SECURITY.md)

## License

MIT. See [LICENSE](LICENSE).
