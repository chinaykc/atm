# Design Notes

[中文](design.zh-CN.md)

ATM means **Agent Task Markdown**. It is intentionally smaller than a workflow engine: it is a Markdown-based DSL (domain-specific language) for scheduling agent tasks, but it does not own project state, scheduling policy, retries, or tool-specific configuration outside the task document.

## Architecture

ATM is shaped like a small Markdown DSL (domain-specific language) implementation:

```txt
Markdown task file
  -> DSL frontend
  -> AST
  -> IR
  -> dry-run plan or execution plan
  -> execution backend
  -> tool adapter
  -> generated result blocks and logs
```

- `todo.txt`, `todo.md`, or another Markdown/plain-text task file is the durable source file.
- The DSL frontend parses blocks, commands, variables, bash captures, and ordered flow operations into parser AST types.
- The frontend lowers AST into a small IR in `ir.go`.
- `atm plan` renders that IR as text or JSON and does not execute tools or bash.
- The execution backend runs the plan, manages `/go` branches and `/wait`, and writes generated state blocks.
- `toolRunner` is the adapter boundary. Codex and Claude Code adapters share the same backend behavior.
- Output from each task is streamed to the console and to a temp log file. Codex and Claude Code adapters use structured output modes so recent assistant messages can be rendered into the generated `> [!ATM]` block.
- Background work is keyed by block index and cleaned body so unchanged blocks are not duplicated.
- `/db` declarations are parsed by the DSL, resolved by the engine into task-visible DB configs, and exposed by tool adapters as a temporary MCP server. DB files are local JSON `map[string][]string` documents.

## Frontend And Backend

The frontend/backend split is represented by real Go packages:

- **DSL package**: `pkg/dsl` owns block parsing, command parsing, AST, IR, generated result blocks, template rendering, and dry-run plan output.
- **CLI package**: `pkg/cli` owns argument parsing and subcommand orchestration.
- **Engine package**: `pkg/engine` owns ordered IR interpretation, `/go` branches, `/wait`, execution state, and output reporting.
- **Store package**: `pkg/store` owns todo documents, block leases, locks, active temp files, and atomic writeback.
- **Tools package**: `pkg/tools` owns Codex, Claude, bash, and condition-check adapters.
- **Command entrypoint**: `cmd/atm/main.go` only calls `cli.Run`.

The frontend can explain a task without executing it. The engine executes `dsl.Task` IR values and does not reinterpret textual command syntax. Parser AST types stay inside `pkg/dsl`; runtime code receives exported IR types such as `dsl.Task`, `dsl.Op`, and `dsl.For`.

## Package Layout

The project uses a small command plus layered public implementation packages:

```txt
cmd/atm/        standard command entrypoint
pkg/dsl/        language frontend, AST, IR, result blocks, and plan rendering
pkg/engine/     ordered IR interpreter, async branches, waits, and reporting
pkg/store/      todo document, block lease, lock, and active-file persistence
pkg/tools/      Codex, Claude, bash, and check adapters
pkg/cli/        argument parsing and subcommand orchestration
docs/           user-facing command and design documentation
examples/       editable todo files
v2/             next-generation DSL and tool design drafts
```

`pkg/dsl` is the language package:

- `ast.go` owns parser AST types.
- `ir.go` owns the frontend output types: plan, task, operations, and execution cursor.
- `parser.go` owns command parsing and AST construction.
- `program.go` owns program compilation from source text into IR.
- `plan.go` owns dry-run plan rendering, including `-json`.
- `markers.go` owns generated state blocks.
- `types.go` owns exported language data types.

New code should first fit one of these packages. Add a new package only when a responsibility is clear and stable.

## Tool Adapter Boundary

Adapters implement `tools.Runner`:

- `Execute` receives the active todo path, rendered prompt, run options, and output writers.
- `Check` receives the active todo path, rendered prompt, rendered condition, run options, and output writers, then returns a boolean result.

The todo language is intentionally tool-neutral. Adapter-specific flags should stay outside task blocks unless the task prompt itself needs them.

## Portability

Runtime code uses the Go standard library only. Platform-specific behavior is isolated in `platform_*.go`:

- restore signals: POSIX listens for interrupt and terminate, Windows listens for interrupt.
- cross-device rename fallback: `os.Rename` is tried first, then copy-and-replace when the platform reports a cross-device move.

## File Safety

Todo rewrites use a temp file in the target directory followed by `os.Rename`. A short-lived lock file serializes local readers and writers from this process family.

During a run, the original todo file is moved to a temp active path. On normal exit or handled interrupt, the active file is moved back.

Generated output logs live under the system temp directory. They are diagnostic artifacts, not durable project state.

Project-persistent `/db persist:project` files live under `.atm/db` in the source todo file's project directory. Run-local `/db persist:run` files live under the selected run output directory. DB writes use a sidecar lock file plus temp-file rename to serialize local MCP tool writes.

## Non-Goals

- No daemon or background service.
- No external database service or daemon. `/db` uses local JSON files for task-scoped agent state.
- No remote queue.
- No built-in project templates.
- No project-specific tool adapters in the core binary.

## Compatibility Contract

- Todo files are the user-facing API. Existing commands and generated state blocks should remain readable by future versions.
- Adapter additions should not change task block syntax.
- Runtime behavior should stay portable across Linux, macOS, and Windows.
- The binary should remain small and standard-library-only unless a dependency removes substantial complexity.
