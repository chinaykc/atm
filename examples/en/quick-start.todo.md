/doc
```md
Quick start smoke:

1. `atm check --plan examples/en/quick-start.todo.md` should show one output task and one fixed list loop.
2. `atm run examples/en/quick-start.todo.md` should create `quick-answer.json` and run two fixed echo prompts.
```

/task answer
/output quick-answer
```
ok:boolean:true when this task ran
text:string:write quick ok
```
Return structured output with ok=true and text="quick ok".

/for item in [one two]
Print exactly: quick item {{item}}.
