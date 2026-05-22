# Checkout Reliability Launch

This queue models a realistic pre-release pass for a checkout reliability change. The product goal is to reduce duplicate payment captures, keep order creation idempotent, and ship documentation that support engineers can use during rollout.

Run it with an explicit artifact directory when you want traceable output:

```sh
atm run -file examples/checkout-reliability-launch.todo.md -output .atm/checkout-reliability-launch
```

## //release context

<!-- Hidden notes for humans are ignored inside runnable task-list sections. -->

/let release checkout-reliability-2026-05-18

/let changed /bash <<'SH'
git diff --stat -- .
SH

/let validation go test ./... && go vet ./...

## /risk brief
/args -c model_reasoning_effort="high"

Prepare a release risk brief for {{var "release"}}.

Changed files:

{{index .Vars "changed"}}

Focus on payment idempotency, order state transitions, observability, rollback safety, and customer-support impact.

{{if .validation}}Use this validation command as the release gate: {{.validation}}{{end}}

## //parallel engineering review

/for area in [payments orders observability docs] /go
Review the {{area}} slice for {{release}}. Report concrete defects, missing tests, and release-blocking ambiguity. Do not make broad refactors.

/go /for 2
Independently challenge the checkout rollout plan. Pass {{N}} should look for a different class of failure than the previous pass.

/wait

Summarize the parallel review findings and apply only the smallest safe fixes.

## //validation loop

/bash <<'SH'
go test ./... || true
go vet ./... || true
SH
/for 3 until {{validation}} passes
Run {{validation}}. Fix failures directly, then explain what changed and what remains risky.

## /release note
/resume

Write the final internal release note for {{release}}.

Include:

- what changed
- how duplicate payment capture is prevented
- the exact validation result
- rollback steps
- what support should watch after launch

Preserve any unresolved risks instead of hiding them.

## Archive Notes

This ordinary Markdown section is kept as documentation and is not executed. Generated `> [!ATM]` result blocks and native agent JSONL streams in the output directory make the run auditable after `untag`.
