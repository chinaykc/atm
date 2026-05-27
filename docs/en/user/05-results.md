# 5. Structured Output, Status Blocks, And Artifacts

ATM keeps the source ATM file clean during direct runs and writes the status snapshot to `~/.atm/runs/<run-id>/result.todo.md`.

## Status Blocks

Completed tasks get a generated ATM report block:

```txt
<!-- atm:report v=2 id=run-tests-6f2d9c8a41 source=sha256:... rendered=sha256:... report=/home/me/.atm/runs/<run-id>/tasks/run-tests-6f2d9c8a41/report.md status=done -->
> [!ATM]
> status: done
> started: 2026-05-27 10:00
> finished: 2026-05-27 10:03
> duration: 3m
> runs: 1x
> id: run-tests-6f2d9c8a41
> source: sha256:...
> rendered: sha256:...
> report: /home/me/.atm/runs/<run-id>/tasks/run-tests-6f2d9c8a41/report.md
<!-- /atm:report -->
```

The visible quote block is for humans and agents. The HTML boundary and identity fields let ATM replace state safely after edits.

Statuses include `done`, `running`, `failed`, and `skipped`. Skipped blocks are written when local or agent-evaluated conditions choose another branch.

## Detailed Reports

Each task writes a detailed report:

```txt
~/.atm/runs/<run-id>/tasks/<task-id>/report.md
```

The report records status, hashes, plan hash, run count, output directory, log paths, and recent assistant messages. Result documents stay lightweight while reports keep audit detail.

## Structured `/output`

Save output as text:

```txt
/output summary.md
Summarize the release.
```

Require structured JSON:

````txt
/output gate

Decide whether release can proceed.

```schema
passed:boolean:whether the gate passed
reason:string:short reason
```
````

ATM exposes a temporary structured output tool to the agent and validates the returned JSON against the schema. Output artifacts are written under the run output directory.

Background tasks can use template variables in output names:

```txt
/for area in [api docs tests] /go
/output {{area}}-review.json
Review {{area}}.
```

## Artifact Layout

Direct run directory:

```txt
~/.atm/runs/<run-id>/
  manifest.json
  sources/
  work/
  result.todo.md
  tasks/
    <task-id>/
      report.md
      logs/
      task-NNN-run-NNN-TOOL.jsonl
  outputs/
```

`manifest.json` records the run id, source paths, source/import copies, status, and restore commands. `sources/` contains captured source and import files. `work/` contains the files ATM executes. `result.todo.md` is the final status document.

`-output DIR` changes the shared output artifact directory. Source backups, task directories, manifest, and `result.todo.md` remain in the managed run directory.

## Reports And Cleanup

Summarize a run:

```sh
atm report
atm report <run-id>
atm report --source todo.txt
atm report -json
```

Clean generated state from a result document:

```sh
atm clean result.todo.md
atm clean result.todo.md --reports --state --logs
atm clean --repair-ids result.todo.md
```

Use `--repair-ids` after copying generated report blocks and creating duplicate task ids.

## Orphans

If a task block is deleted or edited while a leased execution is still running, ATM does not write the completed result into the changed block. It writes the task report, marks the state entry as orphaned, and prints an orphan report notice.
