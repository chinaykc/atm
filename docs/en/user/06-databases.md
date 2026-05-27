# 6. Task Databases: Memory, Blackboard, And Permissions

`/db` exposes local JSON databases to agents through a temporary task tool service. Use it for run memory, review blackboards, checklists, and structured handoff between tasks.

## Declare A Database

```txt
/db new review_board scope:global persist:run access:append
```

Common options:

| Option | Meaning |
| --- | --- |
| `scope:global` | Visible to later tasks in the whole document unless hidden |
| `scope:local` | Visible only in the current Markdown scope |
| `persist:run` | Stored in the managed run directory |
| `persist:project` | Stored under the source project `.atm/db` |
| `access:read` | Read-only |
| `access:append` | Append entries |
| `access:write` | Set or replace entries |
| `access:delete` | Delete entries |

## Use, Access, And Ignore

Mount a visible DB in a task:

```txt
/db use review_board
Review API and append findings under findings/api.
```

Adjust permissions for the current task block:

```txt
/db access review_board read
Summarize findings without changing them.
```

Hide databases:

```txt
/db ignore review_board
/db ignore
```

`/db ignore` hides all visible DBs for the current task.

## Blackboard Pattern

```txt
/db new review_board scope:global persist:run access:append

/for area in [api docs tests] /go
/db use review_board access:append
Review {{area}} and append findings under findings/{{area}}.

/wait

/db use review_board access:read
Summarize all findings.
```

Each background reviewer appends to a shared blackboard. The summary task reads it without write access.

## Project Memory

Use project persistence for long-lived memory:

```txt
/db new release_notes scope:global persist:project access:append
```

Project DB files live under `.atm/db` next to the source todo project. Run-local DBs live under the selected run directory. DB writes use sidecar locks and atomic rename.

## Natural-Language Checks

Natural-language `until` and `/if` checks receive a read-only DB task tool service, even when the task itself has write access. This lets checks inspect shared state without mutating it.

## Practical Rules

Use DBs for data that agents need to inspect or update through tools. Use `/output` for final artifacts, structured reports, and outputs that should be part of the run result. Use `/let` for small values that only need to render into one task.
