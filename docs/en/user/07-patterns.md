# 7. Patterns And Complete Examples

This chapter collects practical ATM patterns for everyday coding-agent workflows.

## Release Checklist

```txt
/task
Inspect the current branch and summarize release risk.

/for 3 until tests pass
Run go test ./... and fix failures.

/for area in [api docs tests] /go reviewer
Review {{area}} for release blockers.

/wait reviewer

/task
Summarize release status, risks, and manual follow-up.
```

## Planner And Workers

Use one definition to plan work, then fan out workers:

````txt
/pool reviewer 3

/def plan_shards
Split this release into independent review plans.

/return
```schema
plans:[]string:review plans
```

/for plan in(/call plan_shards)
/go reviewer
{{plan}}

/wait reviewer

/task
Merge the reviewer findings into a final risk summary.
````

## Gate With Structured Return

````txt
/def release_gate
Inspect tests, lint, and release notes.

/return
```schema
passed:boolean:whether release can proceed
reason:string:short reason
```

/let gate /call release_gate

/if (gate.passed)
Proceed with release: {{gate.reason}}

/else
Write a blocker summary: {{gate.reason}}
````

## Shared Review Blackboard

```txt
/db new review_board scope:global persist:run access:append
/pool reviewer 3

/for area in [api docs tests security] /go reviewer
/db use review_board access:append
Review {{area}} and append findings under findings/{{area}}.

/wait reviewer

/db use review_board access:read
Summarize all findings, group by severity, and propose next actions.
```

## Workspaces Per Task

```txt
/for service in [api billing worker] /go
/cd services/{{service}}
/bash go test ./...
Fix test failures in {{service}}.

/wait
```

`/cd` prepares the task workdir and runs agent, bash, lazy bash, and local file expressions from that directory.

## Append While Running

While a run is active:

```sh
printf '/task\nReview the generated result.todo.md for risk.\n' | atm append todo.txt
```

`append` targets the active working file for that source, so the live/rescan loop can pick up the new task. After the run exits, the same command appends to the source file for the next run.

## Dynamic Command

Turn an ATM file into a parameterized command:

```txt
/flag string service service name
/flag bool dry_run dry run default:false

/task
Review {{service}} with dry_run={{dry_run}}.
```

Register it:

```sh
atm flag register workflows/review.todo.md --name review-service
atm review-service -service api
```

Dynamic commands run source-file copies and write artifacts under `.atm/commands/<command>/<timestamp>/`.

## API Endpoint

Register an ATM file as an API route:

```sh
atm serve register workflows/create-user.todo.md --path /user/create
atm serve --addr 127.0.0.1:8080
```

`GET` runs synchronously. `POST` creates an async job. Both execute source copies and keep the registered source unchanged.

## Choosing The Right Primitive

| Need | Use |
| --- | --- |
| Repeat until a condition passes | `/for ... until ...` |
| Run independent work concurrently | `/go` + `/wait` |
| Limit concurrency | `/pool` |
| Share mutable structured notes | `/db` |
| Save final artifacts | `/output` |
| Reuse a workflow | `/def` + `/call` |
| Add human-only notes | `/doc` |
