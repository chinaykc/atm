/flag string who 名字 default:human


/pool smoke 2


/db new board scope:global persist:run access:admin


Smoke 运行内黑板。任务只写入和读取固定值。


/def tag name
/return smoke:{{name}}

/doc
```md
快速人工验证：

1. `atm check examples/zh/smoke.md` 应该通过，且只有 lazy call warning。
2. `atm check --plan examples/zh/smoke.md` 应该一眼看到：
   - 1 个 flag：who
   - 1 个 pool：smoke
   - 1 个 db：board
   - 1 个 if 条件
   - 1 个并行 fan-out：For(key in [a b])
   - output=answer
```

/let label /call tag base

/task base

/bash <<'SH'
mkdir -p .atm-smoke
printf '{"ok":true}\n' > .atm-smoke/gate.json
SH

你的代号是 {{label}}。
名字是 {{who}}。
告诉我你是谁。


/if (exist(".atm-smoke/gate.json") && json(open(".atm-smoke/gate.json")).ok)

/output answer
```
ok:boolean:是否通过
note:string:一句话说明
```

/mcp def use tag

gate.json 已通过。
返回结构化 answer：ok=true，note 写“gate ok”。


/else

gate.json 不存在或 ok=false。
返回一句话说明 gate 未通过。


/for key in [a b]

/go smoke

/db use board access:append

向 board 写入 key `smoke/{{key}}`，value 为 `{{key}} ok`。


/wait smoke


/task read

/db use board access:read

读取 board 中的 `smoke/a` 和 `smoke/b`。
原样输出读到的两个值。
