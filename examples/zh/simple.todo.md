/flag string name 要输出的名字 default:human

/pool smoke 2

/db new board scope:global persist:run access:admin
简单运行内黑板。任务写入固定值，最后一个任务读取。

/doc
```md
简单 smoke：

1. `atm check --plan examples/zh/simple.todo.md` 应该看到一个 flag、一个 pool、一个 DB、一个本地 if、一个 output 和一个已 join 的并行 fan-out。
2. 成功运行后应生成 `simple-answer.json`，向 `board` 写入 `item/a` 和 `item/b`，最后读回两个值。
```

/bash <<'SH'
mkdir -p .atm-simple
printf '{"ok":true}\n' > .atm-simple/gate.json
SH
只输出：hello {{name}}。

/if (exist(".atm-simple/gate.json") && json(open(".atm-simple/gate.json")).ok)
/output simple-answer
```
ok:boolean:gate 通过时为 true
text:string:写 simple ok
```
返回结构化输出：ok=true，text="simple ok"。

/else
只输出：simple gate failed。

/for key in [a b] /go smoke
/db use board access:append
向 board 写入 key `item/{{key}}`，value 为 `{{key}} ok`。

/wait smoke

/db use board access:read
读取 board 中的 `item/a` 和 `item/b`。
原样输出读到的两个值。
