# 定义调用示例

运行方式：

```sh
atm run -file examples/zh-CN/definition-calls.todo.md -output .atm/definition-calls
```

## /def whereami

根据仓库上下文和可用环境推断用户当前城市。只回答城市名。

/return {{agent.last_message}}

## /def release_gate area

判断 {{area}} 是否已经可以发布。返回门禁结论。

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
审查 {{area}} 的实现风险。

/go reviewer
审查 {{area}} 的文档风险。

/wait reviewer

/return
已完成 {{area}} 的实现和文档审查。
最近一条审查消息：
{{agent.last_message}}

## /plan_weather_check

为
/call whereami
的发布日运营准备一份简短说明。

## /run_reviews

/let checkout_review /call area_review checkout
/let checkout_gate /call release_gate checkout
总结可复用审查结果：
{{checkout_review}}

门禁原因：
{{checkout_gate.reason}}

为 checkout 发布整理一个后续检查清单。

/output follow-up-note
