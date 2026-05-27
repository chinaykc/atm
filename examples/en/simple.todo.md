/flag string name Name to echo default:human

/pool smoke 2

/db new board scope:global persist:run access:admin
Simple run-local board. Tasks write fixed values and the final task reads them.

/doc
```md
Simple smoke:

1. `atm check --plan examples/en/simple.todo.md` should show one flag, one pool, one DB, one local if, one output, and one joined fan-out.
2. A successful run should write `simple-answer.json`, write `item/a` and `item/b` to `board`, then read both values back.
```

/bash <<'SH'
mkdir -p .atm-simple
printf '{"ok":true}\n' > .atm-simple/gate.json
SH
Print exactly: hello {{name}}.

/if (exist(".atm-simple/gate.json") && json(open(".atm-simple/gate.json")).ok)
/output simple-answer
```
ok:boolean:true when gate passed
text:string:write simple ok
```
Return structured output with ok=true and text="simple ok".

/else
Print exactly: simple gate failed.

/for key in [a b] /go smoke
/db use board access:append
Write key `item/{{key}}` to board with value `{{key}} ok`.

/wait smoke

/db use board access:read
Read `item/a` and `item/b` from board.
Print the two values exactly as read.
