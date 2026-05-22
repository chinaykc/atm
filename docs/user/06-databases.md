# 6. 任务数据库：记忆、黑板与权限

`/db` 给任务声明本地 JSON 数据库，并通过 MCP 工具暴露给 Codex/Claude。它适合保存跨任务共享的事实、并行分支的发现、发布决策、待确认问题和审计记录。

数据库的数据模型固定为：

```json
{
  "key": ["string value", "another value"]
}
```

也就是 `map[string][]string`。Key 通常用 `/` 分段，例如 `findings/api`、`decision/rollback`、`questions/support`。

## 声明数据库

声明块写成独立任务块，不会运行 agent：

```txt
/db new decisions scope:global persist:project access:write
记录发布决策、已接受风险和后续待办。Key 使用 decisions/<topic>。
```

字段含义：

| 字段 | 可选值 | 默认值 | 含义 |
| --- | --- | --- | --- |
| `scope` | `global`、`local` | `global` | 是否默认对后续任务块可见 |
| `persist` | `run`、`project` | `run` | 数据保存到本次 run 目录，还是项目 `.atm/db` |
| `access` | `read`、`append`、`write`、`admin` | `admin` | 声明允许的最大权限 |

声明行后面的正文是 usage description。ATM 会把它放进 MCP 工具返回结果里，让 agent 知道这个数据库应该怎么用。

## 可见范围和持久化是两件事

`scope` 只控制“哪些任务块默认能看到 DB”：

- `scope:global`：声明点之后的任务块默认挂载这个 DB。
- `scope:local`：不会默认挂载；任务块必须写 `/db use name`。

`persist` 只控制“数据文件保存多久”：

- `persist:run`：保存到当前 run 的 output 目录，例如 `.atm/20260521103000/db/scratch.json`。
- `persist:project`：保存到项目目录 `.atm/db/name.json`，下次 run 还能继续读取。

常见组合：

| 目标 | 推荐声明 |
| --- | --- |
| 本次运行的并行黑板 | `scope:global persist:run access:append` |
| 项目长期记忆 | `scope:global persist:project access:write` |
| 敏感库，只给少数任务使用 | `scope:local persist:project access:read` |

## 任务块权限控制

任务块可以缩小或启用自己的 DB 视图：

```txt
/db access decisions read
根据已记录的发布决策写对外说明。不要修改数据库。
```

```txt
/db use scratch access:append
把新发现追加到 scratch。不要覆盖已有 key。
```

```txt
/db ignore decisions scratch
执行不需要上下文数据库的独立检查。
```

```txt
/db ignore
完全禁用当前任务块的所有数据库。
```

权限等级：

| 权限 | 允许操作 |
| --- | --- |
| `read` | `list`、`get`、`scan` |
| `append` | `read` + 追加值 |
| `write` | `append` + 替换整个 key |
| `admin` | `write` + 删除 key 或删除 key 中的指定值 |

任务块只能降权，不能超过声明时的 `access`。例如声明为 `access:append` 的 DB，任务块不能提升到 `write`。

## MCP 工具

ATM 给 agent 挂载这些 MCP 工具：

| 工具 | 作用 |
| --- | --- |
| `atm_db_list` | 列出当前任务可见 DB、usage、access 和 capabilities |
| `atm_db_get` | 精确读取一个 key |
| `atm_db_scan` | 用 glob 遍历 key |
| `atm_db_append` | 向 key 追加字符串数组 |
| `atm_db_set` | 替换 key 的整个字符串数组 |
| `atm_db_delete` | 删除整个 key，或删除 key 中指定值 |

`atm_db_scan` 支持 glob。`*` 匹配单段，`**` 可以跨 `/` 分段：

| Pattern | 示例匹配 |
| --- | --- |
| `findings/*` | `findings/api` |
| `findings/**` | `findings/api/auth` |
| `decisions/release-*` | `decisions/release-2026-05` |

自然语言 `until` 和 `/if` 检查使用只读 DB MCP server，即使任务本身有写权限，检查 agent 也只能 list/get/scan。

## 并行黑板示例

```txt
/db new review_board scope:global persist:run access:append
并行 reviewer 追加发现。Key 使用 findings/<area> 和 questions/<area>。

/pool reviewer 3

/for area in [api docs tests] /go reviewer
审查 {{area}}。把阻塞发现追加到 review_board 的 findings/{{area}}。

/wait reviewer

/db access review_board read
读取 review_board 中 findings/** 和 questions/**，汇总最终发布风险。
```

这个模式用 `append` 避免并行分支互相覆盖。同一个 key 的每次追加都会在文件锁内原子完成。

## 项目记忆示例

```txt
/db new release_memory scope:global persist:project access:write
记录长期有效的发布知识。Key 使用 memory/<topic>。

根据当前仓库和 release_memory，补充或更新 memory/rollback 和 memory/support。

/db access release_memory read
基于 release_memory 写本次发布 checklist，不要修改记忆库。
```

`persist:project` 会写到 `.atm/db/release_memory.json`。如果项目要把这类记忆纳入版本控制，可以显式提交；如果只想本机保留，把 `.atm/db/` 加入忽略规则。

## 设计建议

- 并行任务优先用 `access:append`，汇总任务用 `read`。
- 只有维护类任务使用 `write` 或 `admin`。
- usage description 写清 key 命名约定，比让 agent 自由发明 key 更稳定。
- 对需要审计的结论，用 `/output` 保存结构化结果；`/db` 更适合跨任务共享状态。
