# Audit Control Workflows

Definitions used by the payment ledger cutover queue.

## /def rollback_signal

/return /bash if [ -f rollback.lock ]; then printf locked; else printf clear; fi

## /def gate_snapshot release

/output gate-{{release}}
```
passed:boolean:Whether the cutover can proceed
reason:string:Decision summary for the release manager
open_issues:array:Release blockers or follow-up issues
rollback_signal:string:Current rollback signal
```

Assess the release gate for {{release}}.

Use repository state, changed files, validation artifacts, and support-readiness evidence. Return only the structured gate decision.

## /def compliance_matrix release service

/output compliance-matrix-{{release}}
```yaml
type: object
additionalProperties: false
required:
  - finance
  - support
  - sre
  - settlement
properties:
  finance:
    type: string
  support:
    type: string
  sre:
    type: string
  settlement:
    type: string
```

Prepare the compliance matrix for {{release}} on {{service}}.

Cover finance reconciliation evidence, customer support readiness, SRE observability, and settlement engineering rollback controls. Return the structured matrix.
