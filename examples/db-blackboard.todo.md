# DB Blackboard Example

Run with:

```sh
atm run -file examples/db-blackboard.todo.md -output .atm/db-blackboard
```

This example uses `/db` as a run-local blackboard. Parallel reviewers append findings, then a final task reads the board without write access.

## //db blackboard flow

/db new review_board scope:global persist:run access:append
Shared review blackboard for this run. Use keys findings/<area> for confirmed blockers and questions/<area> for unresolved questions. Append only; do not replace existing values.

/pool reviewer 3

/for area in [api docs tests] /go reviewer
Review the {{area}} slice for release-blocking issues.
Use the review_board database:
- Append confirmed blockers to findings/{{area}}.
- Append unresolved questions to questions/{{area}}.
- Do not edit source files in this review branch.

/wait reviewer

/db access review_board read
Read review_board with atm_db_scan patterns findings/** and questions/**.
Summarize the release risk as:
- blocking findings by area
- unresolved questions by area
- smallest next action

/db ignore
Write a short note explaining why the previous summary can be shared without exposing the raw blackboard data.
