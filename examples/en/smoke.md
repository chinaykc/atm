/flag string who Name to echo default:human

/pool smoke 2

/db new board scope:global persist:run access:admin
Smoke run-local board. Tasks only write and read fixed values.

/def tag name
/return smoke:{{name}}

/doc
```md
Fast manual check:

1. `atm check examples/en/smoke.md` should pass with only the lazy call warning.
2. `atm check --plan examples/en/smoke.md` should show:
   - one flag: who
   - one pool: smoke
   - one DB: board
   - one local if
   - one joined fan-out: For(key in [a b])
   - output=answer, def-mcp=tag
```

/let label /call tag base
/task base
/bash <<'SH'
mkdir -p .atm-smoke
printf '{"ok":true}\n' > .atm-smoke/gate.json
SH
Print exactly:
label={{label}}
who={{who}}

/if (exist(".atm-smoke/gate.json") && json(open(".atm-smoke/gate.json")).ok)
/output answer
```
ok:boolean:true when gate passed
note:string:write gate ok
```
/mcp def use tag
Return structured output with ok=true and note="gate ok".

/else
Print exactly: gate failed.

/for key in [a b] /go smoke
/db use board access:append
Write key `smoke/{{key}}` to board with value `{{key}} ok`.

/wait smoke

/db use board access:read
Read `smoke/a` and `smoke/b` from board.
Print the two values exactly as read.
