/import complex-shared.todo.md

/pool reviewer 4 12
/pool tester 2 6

/db new review_board scope:global persist:run access:append
Run-local review blackboard. Parallel reviewers append findings; coordinator tasks read and merge them.

# Complex Cutover Runbook

## About

/doc
```md
This example keeps the richer workflow in one runnable file plus one import file. It covers imports, reusable definitions, pools, structured output, expression gates, DB blackboards, parallel review, and validation loops.

Run: `atm run -file examples/complex.todo.md -output .atm/complex-example`.
```

## setup

/let release complex-cutover
/let service payments-ledger
/let owner settlement-platform
/let validation go test ./... && go vet ./...
/let branch /bash git rev-parse --abbrev-ref HEAD 2>/dev/null || printf unknown
/let diffstat /bash <<'SH'
git diff --stat -- . 2>/dev/null || true
SH

## operating picture

/args -c model_reasoning_effort="high"
/let service_map /call service_map {{service}}
/let rollback_signal /call rollback_signal

Create the cutover operating picture for {{release}} on branch {{branch}}.

Service map:
{{service_map}}

Rollback signal:
{{rollback_signal}}

Current diffstat:
{{diffstat}}

Call out idempotency risk, queue lag risk, reconciliation drift, support readiness, and rollback ownership.

## gate snapshot

/resume
/output gate-{{release}}
```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["passed", "reason", "open_issues", "rollback_signal"],
  "properties": {
    "passed": {"type": "boolean"},
    "reason": {"type": "string"},
    "open_issues": {"type": "array", "items": {"type": "string"}},
    "rollback_signal": {"type": "string"}
  }
}
```

Evaluate whether {{release}} can proceed.

Required checks:

- event replay cannot double-post ledger entries
- settlement totals can be compared against the previous batch
- support has customer-facing language for duplicate capture questions
- rollback can disable the writer without losing queued events

Return the structured gate decision.

## gate routing

/if (exist(outputDir("gate-{{release}}.json")) && json(open(outputDir("gate-{{release}}.json"))).passed)
Prepare the release manager handoff for {{release}}. Include the gate reason, rollback signal, and validation command: {{validation}}

/else
Write a release hold note for {{release}}. Assign every open issue to engineering, support, finance, or observability.

## review blackboard

/for area in [stream-writer ledger-posting support-playbook observability rollback] /go reviewer
/db use review_board access:append
Review the {{area}} slice for {{release}}.
Append one concrete risk and one smallest safe mitigation to `review_board`.

/go reviewer /for 2
/db use review_board access:append
Challenge the rollout decision from an incident-review perspective. Pass {{n}} must focus on a different failure class and append the finding to `review_board`.

/wait reviewer

/db use review_board access:read
Merge the `review_board` findings into one prioritized cutover risk list. Keep exact file paths and commands when available.

## validation and repair

/bash <<'SH'
mkdir -p artifacts
go test ./... > artifacts/go-test.log 2>&1 || true
go vet ./... > artifacts/go-vet.log 2>&1 || true
SH

/for 4 until(exist("artifacts/validation-summary.json") && json(open("artifacts/validation-summary.json")).passed)
Run {{validation}} and repair failures that block {{release}}.
After each pass, update artifacts/validation-summary.json with passed, failed_command, changed_files, and remaining_risk.

## parity drills

/for scenario in [idempotent-replay queue-lag rollback-freeze] /go tester
Run the {{scenario}} validation drill for {{release}}. Use artifacts/go-test.log and artifacts/go-vet.log as evidence inputs. Return the exact evidence gap and the command to reproduce it.

/wait tester

Close remaining finance parity evidence gaps and link exact artifact paths from artifacts/validation-summary.json.

## runbook hardening

/let stream_writer_runbook /call runbook_patch stream-writer
/let support_runbook /call runbook_patch support-playbook

Fold these updates into the release runbook.

Stream writer:
{{stream_writer_runbook}}

Support:
{{support_runbook}}

## cutover record

/task
/output cutover-record
```yaml
type: object
additionalProperties: false
required:
  - release
  - decision
  - validation
  - rollback
  - support
properties:
  release:
    type: string
  decision:
    type: string
  validation:
    type: string
  rollback:
    type: string
  support:
    type: string
```

Write the final cutover record for {{release}}.

Include what changed, why the gate decision is acceptable, validation evidence, rollback trigger and owner, support escalation path, and follow-up work that must not block the current window.
