# Demo Shared Workflows

Reusable definitions kept next to the demo so `cd demo && ../atm -file demo.md`
works without depending on paths outside this directory.

## /def service_map service

Build a concise dependency map for {{service}}.

Include upstream signal sources, downstream operator notes, dashboards, queues,
feature flags, and the rollback surface.

/return {{agent.last_message}}

## /def incident_commander release owner

Write the incident commander briefing note for {{release}} owned by {{owner}}.

The note must identify the primary decision maker, rollback owner, support
liaison, finance liaison, and the first three checks to run if alerts rise.

/return {{agent.last_message}}

## //def runbook_patch area

/pool runbook 2 4

/go runbook
Review the current {{area}} runbook for operator ambiguity, missing commands,
stale alert names, and rollback timing gaps.

/go runbook
Review the current {{area}} runbook from the support escalation perspective.
Identify wording that could confuse people during a live demo.

/wait runbook

/return
Runbook patch for {{area}}:
{{agent.last_message}}

## //def reviewer_pack area severity

/pool reviewer 2 4

/for lens in [code-tests observability docs rollback] /go reviewer
Review {{area}} with severity {{severity}} through the {{lens}} lens.

Return one concrete demo risk and one smallest safe mitigation.

/wait reviewer

/return
Reviewer pack for {{area}} at severity {{severity}}:
{{agent.messages}}
