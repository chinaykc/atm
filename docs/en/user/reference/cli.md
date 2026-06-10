# CLI Manual

[中文](../../../zh/user/reference/cli.md)

## Run

```sh
atm run todo.txt
atm todo.txt
atm run todo.txt rollout.md -jobs 4
atm run todo.txt -tool claude
atm run todo.txt -codex /path/to/codex
atm run todo.txt -output .atm/release-check
```

Direct `run` copies source and import files into a managed run directory, executes the working copy, and restores the source file unchanged when the run exits.

## Check And Plan

```sh
atm check todo.txt
atm check --plan todo.txt
atm check --plan --json todo.txt
atm check --plan --preview todo.txt
```

`check` validates the document. `--plan` prints the static execution plan. `--preview` may execute preview-safe lazy providers.

## Resume And Restore

```sh
atm resume <run-id>
atm resume --last
atm resume --restore-source
atm resume <run-id> --restore-source
```

`resume` continues a managed run. `--restore-source` restores a saved source copy to the original path or a selected target.

## Report

```sh
atm report
atm report <run-id>
atm report --source todo.txt
atm report -json
```

Reports summarize result documents, state, task reports, failures, orphan reports, and recent logs.

## Append

```sh
printf '/task\nAdd focused parser tests.\n' | atm append todo.txt
atm append todo.txt '/task
Review README.'
```

`append` accepts a source todo path. If that source is still running, append resolves to the active working file and can be picked up by live/rescan. After the run exits, it writes to the source file for the next run.

## Format And Clean

```sh
atm format todo.txt
atm clean result.todo.md
atm clean result.todo.md --reports --state --logs
atm clean --repair-ids result.todo.md
```

`format` writes composed task headers one command paragraph each, adds readable spacing between tasks while preserving flow order, and normalizes generated state layout. `clean` removes generated status/report blocks and optionally audit artifacts.

## Dynamic Commands

```sh
atm flag register workflows/review.todo.md --name review
atm flag register workflows/review.todo.md --name review -g
atm flag scan
atm flag list
atm review -h
```

Dynamic commands expose `/flag` declarations as CLI flags and execute source-file copies.

## Serve

```sh
atm serve workflows/create.todo.md --addr 127.0.0.1:8080
atm serve register workflows/create.todo.md --path /user/create
atm serve scan
atm serve --addr 127.0.0.1:8080
atm serve list
atm serve unregister /user/create
```

`serve` runs explicit ATM files or registered API files. `GET` runs synchronously; `POST` creates an async job.

## Global Options

Common options:

| Option | Meaning |
| --- | --- |
| `-tool codex|claude` | Select runner |
| `-codex PATH` | Codex executable |
| `-claude PATH` | Claude executable |
| `-danger` | Pass the selected runner's dangerous all-permissions flag to every agent invocation: `--dangerously-bypass-approvals-and-sandbox` for Codex, `--dangerously-skip-permissions` for Claude Code |
| `-jobs N` | Global background concurrency |
| `-retries N` | Retry transient agent failures; default `3`, `0` disables |
| `-output DIR`, `-o DIR` | Shared output artifact directory |

Use `atm <command> -h` for command-specific flags.
