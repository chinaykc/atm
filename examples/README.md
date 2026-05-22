# Examples

[中文示例](zh-CN/README.md)

These files are ready-to-edit todo queues. Copy one into your project as `todo.txt`, adjust the prompts, then run `atm -file todo.txt`.

## Files

- [basic.todo.txt](basic.todo.txt): sequential tasks, resume, and counted loops.
- [loops.todo.txt](loops.todo.txt): `/for`, `until`, and counted loops.
- [parallel.todo.txt](parallel.todo.txt): background review tasks with `/go` and `/wait`.
- [planner-fanout.todo.md](planner-fanout.todo.md): one planner writes a structured array, then dynamic `/for ... in (...) /go` expands it into parallel reviewer branches.
- [db-blackboard.todo.md](db-blackboard.todo.md): run-local `/db` blackboard with append-only parallel reviewers and a read-only summary task.
- [blackbox-web-security-scan.todo.md](blackbox-web-security-scan.todo.md): authorized black-box web security test queue with scope gating, low-impact inventory, parallel surface checks, evidence triage, retests, and a final report.
- [def-mcp-skill.todo.md](def-mcp-skill.todo.md): mount a local skill and expose a reusable `/def` as an MCP tool the agent can call.
- [release-readiness.todo.txt](release-readiness.todo.txt): a compact release checklist.
- [checkout-reliability-launch.todo.md](checkout-reliability-launch.todo.md): a realistic Markdown task-mode release queue using variables, bash capture, Go templates, parallel branches, `until`, `resume`, and output artifacts.
- [definition-calls.todo.md](definition-calls.todo.md): reusable `/def` and `//def` blocks, `/call` inline embedding, `/return`, and def-local pools.
- [payment-ledger-cutover.todo.md](payment-ledger-cutover.todo.md): a full Agent Task Markdown runbook using cross-file imports, pools, nested `if`/`else`, natural-language checks, CEL gates, structured output, reusable definitions, bash capture, parallel review, validation loops, and run artifacts.
- [workflows/](workflows/): imported definitions used by the payment ledger cutover runbook.

## Recommended Flow

1. Start with `basic.todo.txt`.
2. Add `/go` only for independent review or inspection prompts.
3. Use `/for N until condition` when completion can be checked in plain language.
4. Use `/db` when independent tasks need shared memory, and prefer append-only writes for parallel branches.
5. Use `/mcp def use ...` when the agent should decide when to call a reusable task during a larger prompt.
6. Keep prompts small enough that the generated `> [!ATM]` result block remains easy to scan.

## Skill and Def-MCP Smoke Test

Preview the skill + definition-MCP example without starting an agent:

```sh
go run ./cmd/atm plan -file examples/def-mcp-skill.todo.md
```

Run it with the selected adapter after Codex or Claude is authenticated:

```sh
go run ./cmd/atm run -file examples/def-mcp-skill.todo.md -tool codex
go run ./cmd/atm run -file examples/def-mcp-skill.todo.md -tool claude
```
