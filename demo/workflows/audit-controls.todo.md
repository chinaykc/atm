# Demo Audit Control Workflows

Definitions used by the self-contained ATM demo.

## /def rollback_signal

/return /bash if [ -f rollback.lock ]; then printf locked; else printf clear; fi

## /def gate_snapshot release

/output gate-{{release}}
```
passed:boolean:Whether the demo can proceed
reason:string:Decision summary for the operator
open_issues:array:Blockers or follow-up issues
rollback_signal:string:Current rollback signal
```

Assess the demo gate for {{release}}.

Use repository state, generated artifacts, and support-readiness evidence.
Return only the structured gate decision.

## /def compliance_matrix release service

/output compliance-matrix-{{release}}
```yaml
type: object
additionalProperties: false
required:
  - operator
  - support
  - observability
  - rollback
properties:
  operator:
    type: string
  support:
    type: string
  observability:
    type: string
  rollback:
    type: string
```

Prepare the compliance matrix for {{release}} on {{service}}.

Cover operator readiness, support wording, observability, and rollback controls.
Return the structured matrix.
