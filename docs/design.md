# Design Notes

[中文](design.zh-CN.md)

ATM means **Agent Task Markdown**. It is intentionally smaller than a workflow engine: it is a Markdown-based DSL (domain-specific language) for scheduling agent tasks, but it does not own project state, scheduling policy, retries, or tool-specific configuration outside the task document.

The current language model is Markdown-native: headings provide documentation, context, and lexical scope; executable work starts at `/task`, at a task-start slash control command, or at task header commands followed by prompt text. The command reference in [commands.md](commands.md) is the authoritative syntax document for this model.

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
- The DSL frontend parses Markdown sections, task blocks, scoped declarations, commands, variables, lazy providers, bash captures, and ordered flow operations into parser AST types.
- The frontend lowers AST into a small IR in `ir.go`.
- `atm plan` renders that IR as text, JSON, or HTML and does not execute tools or bash by default. `plan --preview` may evaluate preview-safe lazy providers.
- The execution backend runs the plan, manages `/go` branches and `/wait`, and writes generated state blocks.
- `toolRunner` is the adapter boundary. Codex and Claude Code adapters share the same backend behavior.
- Output from each task is streamed to the console and to a temp log file. Codex and Claude Code adapters use structured output modes so recent assistant messages can be rendered into the generated `> [!ATM]` block and detail report.
- Background work is keyed by block index and cleaned body so unchanged blocks are not duplicated.
- `/db` declarations are parsed by the DSL, resolved by the engine into task-visible DB configs, and exposed by tool adapters as a temporary MCP server. DB files are local JSON `map[string][]string` documents.

## Frontend And Backend

The frontend/backend split is represented by real Go packages:

- **Compiler package**: `pkg/lang/compiler` owns source compilation, command parsing, imports, definitions, scope, and validation.
- **Syntax package**: `pkg/lang/syntax` owns the external source AST used by IDEs, linters, and tooling.
- **IR package**: `pkg/lang/ir` owns the execution model consumed by runtime, plan views, and integrations.
- **Document package**: `pkg/lang/document` owns task document block discovery and Markdown heading helpers.
- **Marker package**: `pkg/lang/marker` owns generated ATM status/report blocks.
- **Format package**: `pkg/lang/format` owns task document and flow formatting helpers.
- **Plan view package**: `pkg/view/plan` owns dry-run plan text, JSON, and HTML rendering.
- **CLI package**: `pkg/app/cli` owns argument parsing and subcommand orchestration.
- **Engine package**: `pkg/runtime/engine` owns ordered IR interpretation, `/go` branches, `/wait`, execution state, and output reporting.
- **Store package**: `pkg/runtime/store` owns task documents, block leases, locks, active temp files, and atomic writeback.
- **Agent adapter package**: `pkg/integration/agent` owns Codex, Claude, bash, and condition-check adapters.
- **MCP integration package**: `pkg/integration/mcp` owns local MCP tool definitions and servers.
- **Task document package**: `pkg/workspace/taskdoc` owns file-level format, untag, append, and repair helpers.
- **Public package**: `atm.go` is the importable integration facade for compiling, planning, running, and editing todo files.
- **Command entrypoint**: `cmd/atm/main.go` only calls the public CLI facade.

The frontend can explain a task without executing it. External tools can read a source AST through `compiler.ParseSyntax`, which returns `syntax.Document`. The engine executes `ir.Task` values and does not reinterpret textual command syntax. Runtime code receives exported IR types such as `ir.Task`, `ir.FlowNode`, `ir.FlatOp`, and `ir.For`.

## Package Layout

The project uses a small command plus layered public implementation packages:

```txt
atm.go          public integration facade
cmd/atm/        standard command entrypoint
pkg/app/cli/             argument parsing and subcommand orchestration
pkg/lang/compiler/       source compilation, command parsing, imports, definitions, scope, and validation
pkg/lang/document/       task block discovery and Markdown heading helpers
pkg/lang/expr/           local expression evaluator used by /if, /for, and output helpers
pkg/lang/format/         task document and flow formatting helpers
pkg/lang/ir/             execution model shared by runtime, plan, and integrations
pkg/lang/marker/         generated ATM status/report blocks
pkg/lang/syntax/         external source AST and diagnostics
pkg/runtime/engine/      ordered IR interpreter, async branches, waits, and reporting
pkg/runtime/store/       task document, block lease, lock, and active-file persistence
pkg/integration/agent/   Codex, Claude, bash, and check adapters
pkg/integration/mcp/     local MCP tool definitions and servers
pkg/view/plan/           dry-run plan text, JSON, and HTML rendering
pkg/workspace/taskdoc/   file-level format, untag, append, and repair helpers
docs/           user-facing command and design documentation
examples/       editable todo files
```

`pkg/lang/compiler` is the language compiler package:

- `ast.go` owns compiler-local parser AST types.
- `ir.go` lowers compiler-local AST into the exported IR model.
- `blocks.go` and `markdown_parser.go` own Markdown/legacy task block discovery.
- `command_*.go`, `*_parser.go`, and `task_command_scan.go` own command parsing and task AST construction.
- `program.go` owns program compilation from source text into IR.
- `scope.go` and `symbols.go` own Markdown lexical visibility and symbol resolution.
- `validate.go` owns static validation that `atm check` and program compilation share.
- `syntax.go` maps parsed documents into the public `syntax.Document` AST.

New code should first fit one of these packages. Add a new package only when a responsibility is clear and stable.

External integrations should prefer the root `atm` package unless they need a lower-level package contract. The `pkg/*` packages remain usable implementation layers, but the root facade is the stable place for embedding the CLI, compiling todo content, rendering plans, running the engine, and applying file-level todo maintenance helpers.

## Tool Adapter Boundary

Adapters implement `agent.Runner`:

- `Execute` receives the active todo path, rendered prompt, run options, and output writers.
- `Check` receives the active todo path, rendered prompt, rendered condition, run options, and output writers, then returns a boolean result.

The todo language is intentionally tool-neutral. Adapter-specific flags should stay outside task blocks unless the task prompt itself needs them.

## Portability

Runtime code uses the Go standard library only. Platform-specific behavior is isolated in `platform_*.go`:

- restore signals: POSIX listens for interrupt and terminate, Windows listens for interrupt.
- cross-device rename fallback: `os.Rename` is tried first, then copy-and-replace when the platform reports a cross-device move.

## File Safety

Todo rewrites use a temp file in the target directory followed by `os.Rename`. A short-lived `.atm/lock` file next to the source todo serializes local readers and writers from this process family.

During a run, the original todo file is moved to a temp active path. On normal exit or handled interrupt, the active file is moved back.

Human-readable task logs live under `.atm/logs/` next to the source todo file so state and reports can point at stable audit paths. Native agent event streams, structured outputs, run-local DB files, and `result.md` live under the selected run output directory.

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
