# Black-box Web Security Scan Queue

Use this queue only for an authorized web target. Edit the scope variables
before running it; the defaults are placeholders on purpose.

Start with a plan:

```sh
atm plan -file examples/blackbox-web-security-scan.todo.md
```

Run with a separate artifact directory:

```sh
atm run -file examples/blackbox-web-security-scan.todo.md -output .atm/blackbox-web-security
```

This example guides a security-testing agent through a black-box workflow:
scope gate, low-impact inventory, parallel surface checks, evidence triage,
minimal retests, and an operator-readable report. It does not grant permission
to scan an unknown host.

## //scope setup

/let target_base_url https://staging.example.test
/let allowed_hosts staging.example.test api.staging.example.test
/let account_rule Use only test accounts provided for this assessment. If no accounts are available, stay unauthenticated.
/let rate_rule Start at human-paced requests. Do not run high-concurrency crawling, brute force, or password guessing unless the written scope explicitly allows it.
/let forbidden_actions Do not mutate production-like data, trigger real payments or notifications, probe internal addresses, leave the host allowlist, or upload harmful files.

/pool surface 3 8
/pool verify 2 6

/db new scan_board scope:global persist:run access:append
Run-local security testing blackboard. Append concise evidence to
inventory/<kind>, coverage/<focus>, hypotheses/<focus>, findings/<focus>,
verification/confirmed, and verification/rejected. Keep request and response
evidence reproducible without storing secrets.

## /def testing_guardrails target

/return
Authorized black-box target: {{target}}.
Allowed hosts: {{allowed_hosts}}.
Account rule: {{account_rule}}
Rate rule: {{rate_rule}}
Forbidden actions: {{forbidden_actions}}
Treat source code, build files, infrastructure consoles, logs, and databases as
out of scope unless the assessment owner changes scope in writing.
Prefer the smallest request that demonstrates an observation. Do not call a
behavior a vulnerability without reproducible evidence and an impact argument.

## /scope gate

/let guardrails /call testing_guardrails {{target_base_url}}
Use these guardrails:

{{guardrails}}

Before any active testing, decide whether the assessment is ready:

- Confirm the target is not still a placeholder and the allowed hosts are clear.
- Use at most a low-impact baseline request to the target origin when checking
  reachability.
- List required test accounts, roles, test data, rate limits, and stop
  conditions that are missing.
- If scope is not ready, set `ready` to false and explain what blocks testing.

/output scope-card
```yaml
type: object
additionalProperties: false
required:
  - ready
  - target
  - in_scope_hosts
  - allowed_modes
  - blocked_questions
  - baseline_observations
properties:
  ready:
    type: boolean
  target:
    type: string
  in_scope_hosts:
    type: array
    items:
      type: string
  allowed_modes:
    type: array
    items:
      type: string
  blocked_questions:
    type: array
    items:
      type: string
  baseline_observations:
    type: array
    items:
      type: string
```

## //authorized black-box gate

/if (existsOutput("scope-card.json") && jsonOutput("scope-card.json").ready)

## /surface inventory

/let guardrails /call testing_guardrails {{target_base_url}}
Build a low-impact black-box inventory for {{target_base_url}}.

Use these guardrails:

{{guardrails}}

Work from externally observable behavior only:

- Record landing pages, redirects, robots or sitemap hints if exposed, public
  API entry points, forms, upload boundaries, state-changing actions, browser
  storage signals, cookies, security headers, and authentication entry points.
- Use normal browsing and low-rate requests first. If a scanner is already
  installed and scope allows it, keep it allowlisted and throttled; record the
  exact passive or low-risk mode used.
- Append concise inventory evidence and coverage notes to `scan_board`.
- Map planned checks to relevant OWASP WSTG categories or IDs when known; do
  not invent IDs.

/output surface-map
```yaml
type: object
additionalProperties: false
required:
  - target
  - entry_points
  - auth_contexts
  - data_boundaries
  - planned_focus
  - inventory_gaps
properties:
  target:
    type: string
  entry_points:
    type: array
    items:
      type: string
  auth_contexts:
    type: array
    items:
      type: string
  data_boundaries:
    type: array
    items:
      type: string
  planned_focus:
    type: array
    items:
      type: string
  inventory_gaps:
    type: array
    items:
      type: string
```

## /parallel surface checks

/for focus in [information-exposure auth-session access-control input-boundaries upload-browser api-misconfiguration] /go surface
/let guardrails /call testing_guardrails {{target_base_url}}
Test the {{focus}} surface of {{target_base_url}} from the existing black-box
inventory.

Use these guardrails:

{{guardrails}}

For this surface:

- Read `surface-map.json` and use only observed entry points or explicitly
  allowlisted hosts.
- Prefer safe markers, role comparisons, header and cookie observation, normal
  navigation, and single-request boundary probes.
- For authorization checks, compare only assessment test accounts and roles.
  Do not access another user's real data.
- Do not treat a scanner alert or a response difference alone as a confirmed
  issue. Capture the minimal request, observed response, precondition, impact
  hypothesis, and confidence.
- Append coverage to `coverage/{{focus}}`, tentative leads to
  `hypotheses/{{focus}}`, and evidence-backed candidates to
  `findings/{{focus}}` in `scan_board`.

## /surface triage

/wait surface

/db access scan_board read
Read `surface-map.json` and scan `scan_board` with patterns `coverage/**`,
`hypotheses/**`, and `findings/**`.

Triage the black-box evidence:

- Keep confirmed-looking candidates separate from unverified hypotheses.
- Put only candidates that can be retested with a least-harm reproduction into
  `retest_queue`.
- Preserve coverage gaps when scope, accounts, or target behavior prevented a
  check.

/output triage
```yaml
type: object
additionalProperties: false
required:
  - retest_queue
  - likely_findings
  - hypotheses
  - coverage_gaps
  - stop_conditions
properties:
  retest_queue:
    type: array
    items:
      type: string
  likely_findings:
    type: array
    items:
      type: string
  hypotheses:
    type: array
    items:
      type: string
  coverage_gaps:
    type: array
    items:
      type: string
  stop_conditions:
    type: array
    items:
      type: string
```

## /minimal candidate retests

/for candidate in(jsonOutput("triage.json").retest_queue) /go verify
/let guardrails /call testing_guardrails {{target_base_url}}
Retest this candidate once with the smallest black-box proof that is still
within scope:

{{candidate}}

Use these guardrails:

{{guardrails}}

Append a concise outcome to `verification/confirmed` or
`verification/rejected` in `scan_board`. Include the precondition, endpoint,
safe proof step, observed result, and why the outcome changes confidence.

## /black-box report

/wait verify

/db access scan_board read
Prepare the final black-box security test report for {{target_base_url}}.

Use `scope-card.json`, `surface-map.json`, `triage.json`, and the verification
entries in `scan_board`.

The report must include:

- Scope and guardrails actually used
- Method and major coverage areas
- Confirmed findings with minimal reproduction evidence and impact
- Unverified hypotheses and why they remain unverified
- Coverage gaps, stop conditions, and residual risk
- Next remediation or retest actions

Do not hide uncertainty. Do not include exploit chains beyond the
least-harm evidence needed for the assessment owner to reproduce a finding.

/output blackbox-report

## /scope blocked branch

/else
Write a short scope-blocked note for the assessment owner.

Use `scope-card.json` to state which scope, account, target, rate-limit, or
stop-condition answers are required before an agent should continue.

/output scope-blocked
