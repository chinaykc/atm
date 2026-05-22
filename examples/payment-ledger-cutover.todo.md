# Payment Ledger Cutover Runbook

The May settlement window moves delayed capture reconciliation from the nightly ledger batch into the payment event stream. The release must keep ledger totals stable for finance, keep customer support staffed with clear rollback criteria, and leave an auditable trail for the incident review.

Run with a traceable artifact directory:

```sh
atm run -file ops/payment-ledger-cutover.todo.md -output .atm/payment-ledger-cutover
```

## //workspace setup

/import workflows/ledger-shared.todo.md
/import audit from workflows/audit-controls.todo.md

/pool reviewer 4 12

/pool tester 2 6

/let release payment-ledger-cutover-2026-05-22
/let service payments-ledger
/let owner settlement-platform
/let validation go test ./... && go vet ./...
/let bypass_window false
/let branch /bash git rev-parse --abbrev-ref HEAD 2>/dev/null || printf unknown
/let diffstat /bash <<'SH'
git diff --stat -- . 2>/dev/null || true
SH

## /current operating picture
/args -c model_reasoning_effort="high"

/let service_map /call service_map {{service}}
/let commander_note /call incident_commander {{release}} {{owner}}

Create the cutover operating picture for {{release}} on branch {{branch}}.

Service dependency map:
{{service_map}}

Incident commander note:
{{commander_note}}

Current diffstat:
{{diffstat}}

Call out reconciliation drift risk, idempotency risk, queue lag risk, data backfill risk, support readiness, and rollback ownership.

## /gate snapshot
/resume
/let rollback_signal /call audit.rollback_signal

Evaluate whether {{release}} can enter the May settlement cutover.

Use rollback signal: {{rollback_signal}}

Required checks:

- ledger event stream can replay without double posting
- delayed capture reconciliation remains idempotent
- settlement totals can be compared against the nightly batch
- support has customer-facing language for duplicate capture questions
- rollback can disable the stream writer without losing queued events

Return the structured gate decision.

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

## //gate routing

/if (existsOutput("gate-{{release}}.json"))

/if (jsonOutput("gate-{{release}}.json").passed)
Prepare the release manager handoff for {{release}}.
Include the structured gate reason, the current rollback signal, and the exact validation command:
{{validation}}

/else
Write the release hold note for {{release}}.
Use the gate reason and open issues from gate-{{release}}.json, then assign each blocker to payments, ledger, support, or observability.

/else
Rebuild the gate snapshot before any rollout decision is made.
Explain which artifact is missing and why the release manager should not proceed.

## //parallel slice review

/for area in [stream-writer ledger-posting support-playbook observability rollback] /go reviewer
/args -c model_reasoning_effort="high"
Review the {{area}} slice for {{release}}.
Use branch {{branch}} and the diffstat below:
{{diffstat}}
Return concrete defects, missing tests, unsafe assumptions, and the smallest safe patch direction. Do not broaden scope beyond the {{area}} slice.

/go reviewer /for 2
Challenge the rollout decision from an adversarial incident-review perspective.
Pass {{N}} must focus on a different failure class:
1. customer-visible duplicate capture or refund confusion
2. finance reconciliation drift or irreversible ledger mutation

/wait reviewer

Merge the parallel review findings into one prioritized cutover risk list. Keep exact file paths and commands when they are available.

## /compliance matrix
/let matrix /call audit.compliance_matrix {{release}} {{service}}

Use the returned compliance matrix:

{{matrix}}

Convert it into a sign-off checklist for finance, support, SRE, and settlement engineering. Preserve unresolved control gaps.

## //validation and repair

/bash <<'SH'
mkdir -p artifacts
go test ./... 2>&1 | tee artifacts/go-test.log
go vet ./... 2>&1 | tee artifacts/go-vet.log
SH

/for 4 until(exists("artifacts/validation-summary.json") && json("artifacts/validation-summary.json").passed)
Run {{validation}} and repair failures that block {{release}}.
After each pass, update artifacts/validation-summary.json with:
- passed
- failed_command
- changed_files
- remaining_risk
Keep fixes minimal and explain why each one is necessary for the cutover.

## //settlement parity drill

/for scenario in [idempotent-replay queue-lag rollback-freeze] /go tester
Run the {{scenario}} validation drill for {{release}}.
Use artifacts/go-test.log, artifacts/go-vet.log, and any ledger comparison files under artifacts/.
Return the exact evidence gap, the command to reproduce it, and whether finance can sign off this scenario.

/wait tester

/for 3 until finance parity evidence is complete, reproducible, and linked from artifacts/validation-summary.json
Close the remaining finance parity evidence gaps for {{release}}.
Update artifacts/validation-summary.json with exact artifact paths and the sign-off owner after each pass.

## //manager acknowledgement

/if release manager has acknowledged the rollback owner and support escalation path
Send the release manager a final go/no-go summary for {{release}}.
Include:
- gate decision
- validation result
- rollback owner
- support escalation path
- residual risks that remain acceptable

/else
Draft the escalation request for {{owner}}.
Ask for explicit acknowledgement of rollback ownership and support escalation before continuing.

## //runbook hardening

/let stream_writer_runbook /call runbook_patch stream-writer
/let support_runbook /call runbook_patch support-playbook
Fold the runbook updates into docs/runbooks/payment-ledger-cutover.md.
Stream writer update:
{{stream_writer_runbook}}
Support update:
{{support_runbook}}

/for doc in [docs/runbooks/payment-ledger-cutover.md docs/support/payment-ledger-faq.md docs/ops/settlement-dashboard.md] /go reviewer
Review {{doc}} for accuracy against {{release}}.
Focus on operator actions, exact commands, customer-facing language, and rollback timing.

/wait reviewer

Apply the documentation review findings that reduce operator ambiguity.

## /cutover record
Write the final cutover record for {{release}}.

It must be suitable for the incident review archive and include:

- what changed
- why the gate decision is acceptable
- validation evidence
- rollback trigger and owner
- support escalation path
- follow-up work that must not block the current settlement window

Return the structured cutover record.

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
