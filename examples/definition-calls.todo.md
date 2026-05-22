# Definition Calls Example

Run it with:

```sh
atm run -file examples/definition-calls.todo.md -output .atm/definition-calls
```

## /def whereami

Infer the user's current city from the repository context and environment. Answer with only the city name.

/return {{agent.last_message}}

## /def release_gate area

Assess whether {{area}} is ready for release. Return the gate decision.

/return
```json
{
  "type": "object",
  "required": ["passed", "reason"],
  "properties": {
    "passed": {"type": "boolean"},
    "reason": {"type": "string"}
  }
}
```

## //def area_review area

/pool reviewer 2

/go reviewer
Review {{area}} for implementation risks.

/go reviewer
Review {{area}} for documentation risks.

/wait reviewer

/return
Completed implementation and documentation review for {{area}}.
Most recent reviewer note:
{{agent.last_message}}

## /plan_weather_check

Prepare a release-day operations note for
/call whereami
using the latest checkout reliability context.

## /run_reviews

/let checkout_review /call area_review checkout
/let checkout_gate /call release_gate checkout
Summarize the reusable review result:
{{checkout_review}}

Gate reason:
{{checkout_gate.reason}}

Create a short follow-up checklist for the checkout launch.

/output follow-up-note
