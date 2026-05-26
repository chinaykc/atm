# Simple Release Check

## About

/doc
```md
This example shows variables, bash capture, parallel branches, `/wait`, and an `until` retry loop without importing other files.

Run: `atm run -file examples/simple.todo.md -output .atm/simple-example`.
```

## setup

/let release simple-release
/let validation go test ./... && go vet ./...
/let diffstat /bash <<'SH'
git diff --stat -- . 2>/dev/null || true
SH

## release brief

/task
Prepare a short release brief for {{release}}.

Changed files:

{{diffstat}}

Use this validation command as the gate: {{validation}}

## parallel review

/for area in [code tests docs] /go
Review the {{area}} slice for {{release}}. Report concrete defects, missing tests, and release-blocking ambiguity. Keep the suggested fix small.

/go /for 2
Challenge the release plan. Pass {{n}} should focus on a different failure class.

/wait

Summarize the parallel findings and apply only the smallest safe fixes.

## validation loop

/bash <<'SH'
go test ./... || true
go vet ./... || true
SH
/for 3 until {{validation}} passes
Run {{validation}}. Fix failures directly, then explain what changed and what remains risky.

## release note

/resume
Write the final internal release note for {{release}}.

Include:

- what changed
- validation result
- rollback steps
- unresolved risks
