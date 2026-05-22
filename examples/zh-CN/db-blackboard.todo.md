# DB 黑板示例

运行：

```sh
atm run -file examples/zh-CN/db-blackboard.todo.md -output .atm/db-blackboard
```

这个示例把 `/db` 用作本次 run 内的共享黑板。并行 reviewer 追加发现，最后一个任务以只读权限汇总黑板。

## //db blackboard flow

/db new review_board scope:global persist:run access:append
本次 run 的共享审查黑板。确认的阻塞项写入 findings/<area>，待确认问题写入 questions/<area>。只追加，不替换已有值。

/pool reviewer 3

/for area in [api docs tests] /go reviewer
审查 {{area}} 这部分是否有发布阻塞问题。
使用 review_board 数据库：
- 把确认的阻塞项追加到 findings/{{area}}。
- 把待确认问题追加到 questions/{{area}}。
- 这个审查分支不要修改源码文件。

/wait reviewer

/db access review_board read
用 atm_db_scan 读取 review_board，pattern 使用 findings/** 和 questions/**。
按下面结构汇总发布风险：
- 各 area 的阻塞发现
- 各 area 的待确认问题
- 最小下一步行动

/db ignore
写一段简短说明：为什么上一段汇总可以对外共享，而不需要暴露原始黑板数据。
