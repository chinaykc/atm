# 3. Workflows: Loops, Parallelism, And Pools

ATM interprets commands in IR order. Clear workflows come from understanding `/for`, `/go`, `/wait`, and `/pool`.

## Sequential Tasks

```txt
/task
Run tests and fix failures.

/task
Run go vet ./... and fix actionable issues.

/task
Summarize the changes.
```

ATM executes from top to bottom. Each foreground task finishes before the next foreground task starts.

## Loops And Retries

Fixed count:

```txt
/for 3
Review the final diff on pass {{n}}.
```

Retry with an agent-checked condition:

```txt
/for 3 until tests pass
Fix failing tests.
```

After each attempt, ATM asks the selected tool to report whether the condition passed through a structured structured check.

Local expression conditions are deterministic and run locally:

```txt
/for 5 until(exist("result.json") && len(open("result.json")) > 0)
Generate result.json.
```

Unbounded loops require a local expression:

```txt
/for until(exist("result.json") && json(open("result.json")).passed)
Keep fixing until result.json says passed=true.
```

## Conditional Branches

Use `/if` and optional `/else` to choose task blocks:

```txt
/if (exist("gate.json") && json(open("gate.json")).passed)
Continue the release.

/else
Write the release blocker summary.
```

Skipped branches are written with `> [!ATM] status: skipped`. `/if` is task-block control flow, not prompt template syntax.

`/if(...)` uses local expressions. Plain-language `/if condition` uses the agent-backed structured check:

```txt
/if release gate is open
Continue the release.
```

`/if` and `/else` do not nest. Put complex branch bodies in `/def` blocks and call them from the branch. `/if` can combine with `/for` and `/go`; command order defines control flow:

```txt
/for 10
/if(n % 2 == 0)
/go

Review even shard {{n}}.

/wait
```

## Files, Directories, And Lists

```txt
/for dir in dirs()
Review directory {{dir}}.

/for file in files()
Review file {{file}}.

/for area in [api docs tests]
Review {{area}}.
```

Use `range(stop)`, `range(start, stop)`, and `range(start, stop, step)` for numeric ranges. Use `files()`, `dirs()`, `walkFiles()`, and `walkDirs()` for filesystem traversal.

## Background Tasks

```txt
/go
Review README.

/go
Review docs/en/commands.md.

/wait

/task
Summarize both reviews.
```

If `/wait` has prompt text, ATM waits first, then runs that prompt with a small wait result context:

```txt
/wait
Watch both background reviews and summarize failures, risks, and manual follow-up.
```

ATM gives the prompt the wait scope plus the matched background task ids, block numbers, pool names, final statuses, log paths, errors, and visible reports.

Background work is joined only by explicit `/wait`. When no foreground work remains, unmatched background branches may stay marked `running`.

## `/for /go` And `/go /for`

Command order matters.

```txt
/for area in [api docs tests] /go
Review {{area}}.
```

This starts one background branch per loop item.

```txt
/go /for 3
Review pass {{n}}.
```

This starts one background branch and runs the loop sequentially inside that branch.

Dynamic fan-out can call a planner:

````md
/def plan_shards

Plan parallel review items for this release.

/return
```schema
plans:[]string:plans
```

## parallel review

/for plan in(/call plan_shards)
/go reviewer
{{plan}}

/wait reviewer
````

## Worker Pools

Declare a named pool:

```txt
/pool reviewer 3
```

Submit background work to it:

```txt
/for area in [api docs tests ux] /go reviewer
Review {{area}}.

/wait reviewer
```

`/pool reviewer 3` limits the `reviewer` pool to three concurrent branches. An optional buffer limits queued branches:

```txt
/pool reviewer 3 10
```

Pools follow Markdown lexical scope. A root pool is visible to later tasks in the whole document. A pool under a heading is visible only to later tasks in that heading and its child headings.

All pools also share the global concurrency limit:

```sh
atm run todo.txt -jobs 8
```
