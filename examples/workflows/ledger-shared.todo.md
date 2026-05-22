# Ledger Shared Workflows

Reusable task definitions for settlement platform release work.

## /def service_map service

Build a concise dependency map for {{service}}.

Include upstream event producers, downstream ledger consumers, dashboards, queues, feature flags, and the rollback surface.

/return {{agent.last_message}}

## /def incident_commander release owner

Write the incident commander briefing note for {{release}} owned by {{owner}}.

The note must identify the primary decision maker, rollback owner, support liaison, finance liaison, and the first three checks to run if duplicate capture alerts rise.

/return {{agent.last_message}}

## //def runbook_patch area

/pool runbook 2 4

/go runbook
Review the current {{area}} runbook for operator ambiguity, missing commands, stale alert names, and rollback timing gaps.

/go runbook
Review the current {{area}} runbook from the support escalation perspective. Identify customer-facing wording that could cause confusion during the settlement window.

/wait runbook

/return
Runbook patch for {{area}}:
{{agent.last_message}}

## //def reviewer_pack area severity

/pool reviewer 2 4

/for lens in [code-tests observability docs rollback] /go reviewer
Review {{area}} with severity {{severity}} through the {{lens}} lens.

Return one concrete release risk and one smallest safe mitigation.

/wait reviewer

/return
Reviewer pack for {{area}} at severity {{severity}}:
{{agent.messages}}
