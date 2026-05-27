/import complex-shared.todo.md


/flag string name Name to echo default:human


/pool writer 2


/pool reader 1


/db new board scope:global persist:run access:admin


Complex run-local board. Writer tasks append fixed values; reader tasks read them.


/doc
```md
Complex smoke:

1. `atm check --plan examples/en/complex.todo.md` should show import, flag, two pools, one DB, one local if, structured output, def MCP, and joined fan-out.
2. A successful run should create `complex-answer.json`, write `complex/a` and `complex/b`, then read them back.
```

/let label /call tag base

/let pair_text /call pair left right

/bash <<'SH'
mkdir -p .atm-complex
printf '{"ok":true}\n' > .atm-complex/gate.json
SH

Print exactly:
name={{name}}
label={{label}}
pair={{pair_text}}


/if (exist(".atm-complex/gate.json") && json(open(".atm-complex/gate.json")).ok)

/output complex-answer
```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["ok", "label", "pair"],
  "properties": {
    "ok": {"type": "boolean"},
    "label": {"type": "string"},
    "pair": {"type": "string"}
  }
}
```

/mcp def use tag pair

Return structured output with ok=true, label="smoke:base", and pair="left=right".


/else

Print exactly: complex gate failed.


/for key in [a b]

/go writer

/db use board access:append

Write key `complex/{{key}}` to board with value `{{key}} ok`.


/wait writer


/for key in [a b]

/go reader

/db use board access:read

Read `complex/{{key}}` from board.
Print exactly: complex/{{key}}=<value>.


/wait reader


/db use board access:read

Read `complex/a` and `complex/b` from board.
Print the two values exactly as read.
