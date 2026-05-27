/import complex-shared.todo.md

/flag string name 要输出的名字 default:human

/pool writer 2
/pool reader 1

/db new board scope:global persist:run access:admin
复杂运行内黑板。writer 任务追加固定值，reader 任务读取这些值。

/doc
```md
复杂 smoke：

1. `atm check --plan examples/zh/complex.todo.md` 应该看到 import、flag、两个 pool、一个 DB、一个本地 if、结构化 output、def MCP 和已 join 的 fan-out。
2. 成功运行后应生成 `complex-answer.json`，写入 `complex/a` 和 `complex/b`，再读回它们。
```

/let label /call tag base
/let pair_text /call pair left right
/bash <<'SH'
mkdir -p .atm-complex
printf '{"ok":true}\n' > .atm-complex/gate.json
SH
只输出：
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
返回结构化输出：ok=true，label="smoke:base"，pair="left=right"。

/else
只输出：complex gate failed。

/for key in [a b] /go writer
/db use board access:append
向 board 写入 key `complex/{{key}}`，value 为 `{{key}} ok`。

/wait writer

/for key in [a b] /go reader
/db use board access:read
从 board 读取 `complex/{{key}}`。
只输出：complex/{{key}}=<value>。

/wait reader

/db use board access:read
读取 board 中的 `complex/a` 和 `complex/b`。
原样输出读到的两个值。
