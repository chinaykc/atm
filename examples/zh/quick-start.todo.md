/doc
```md
快速开始 smoke：

1. `atm check --plan examples/zh/quick-start.todo.md` 应该看到一个 output 任务和一个固定列表循环。
2. `atm run examples/zh/quick-start.todo.md` 应该生成 `quick-answer.json`，并执行两次固定 echo prompt。
```

/task answer

/output quick-answer
```
ok:boolean:任务运行后为 true
text:string:写 quick ok
```

返回结构化输出：ok=true，text="quick ok"。


/for item in [one two]

只输出：quick item {{item}}。
