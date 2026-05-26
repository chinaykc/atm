# Complex Shared Definitions

## About

/doc Shared definitions imported by the complex examples. This file intentionally contains definitions only.

## Definitions

/def service_map service

Build a concise dependency map for {{service}}.

Include upstream inputs, downstream consumers, dashboards, queues, feature flags, and rollback controls.

/return {{agent.last_message}}

/def rollback_signal

/return /bash if [ -f rollback.lock ]; then printf locked; else printf clear; fi

/def runbook_patch area

/pool runbook 2 4

/go runbook
Review the current {{area}} runbook for operator ambiguity, missing commands, stale alert names, and rollback timing gaps.

/go runbook
Review the current {{area}} runbook from the support escalation perspective. Identify customer-facing wording that could cause confusion.

/wait runbook

/return
```
Runbook patch for {{area}}:
{{agent.messages}}
```
