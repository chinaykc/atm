# 命令行手册

## 总览

```sh
atm [run] [flags] [files...]
atm plan -file todo.txt
atm append -file todo.txt "新任务"
atm format -file todo.txt
atm untag -file todo.txt
atm mcp check -result-file /tmp/atm/check.json
atm mcp output -result-file /tmp/atm/out.json -schema-file schema.json
atm mcp db -config-file /tmp/atm/db-config.json
```

## `atm run`

执行 pending 任务。`run` 是默认子命令：

```sh
atm -file todo.txt
atm run -file todo.txt
```

常用参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-file PATH` | 自动查找 `todo.txt`、`todo.md`、`toto.md` | todo 文件，可重复 |
| `-tool codex|claude|claude-code` | `codex` | 选择工具适配器 |
| `-codex PATH` | `codex` | Codex 可执行文件 |
| `-claude PATH` | `claude` | Claude Code 可执行文件 |
| `-messages N` | `1` | 每个分支保留最近 N 条 assistant 消息 |
| `-output DIR` | `.atm/YYYYMMDDHHMMSS[-N]` | 输出产物目录 |
| `-o DIR` | 同 `-output` | `-output` 简写 |
| `-jobs N` | `NumCPU` | 所有池共享的全局后台并发上限 |

多个文件按顺序排队：

```sh
atm run todo.txt rollout.md followup.md
atm run -file todo.txt -file rollout.md
```

同一个 `-output DIR` 搭配多个文件时，ATM 会为每个文件创建编号子目录，避免覆盖。

## `atm plan`

预览执行计划，不运行 agent、不执行 bash、不写状态：

```sh
atm plan -file todo.txt
atm plan -html plan.html -file todo.txt
atm plan -open -file todo.txt
atm plan -json -file todo.txt
atm plan dry-run -file todo.txt
```

适合检查 `/for /go` 顺序、条件控制块、定义调用、全局变量、DB/skill/MCP 声明和任务级挂载配置、runner 参数以及输出配置。`-html` 会保存单文件 HTML 流程图，`-open` 会生成临时 HTML 并用默认浏览器打开。

## `atm append`

向 todo 文件追加任务：

```sh
atm append -file todo.txt "运行测试并修复失败。"
```

从 stdin 读取：

```sh
printf '审查 README。' | atm append -file todo.txt
```

没有参数且 stdin 是终端时，ATM 会打开 `$VISUAL`、`$EDITOR` 或平台默认编辑器。

运行中的 ATM 会把 todo 文件移动到临时活跃路径；`append -file 原路径` 会自动解析并写入活跃文件。如果当前 `atm run` 仍有任务在执行，追加任务会在后续重新扫描时被当前 run 执行；如果 run 已经退出，则需要再次执行 `atm run`。

## `atm format`

整理生成状态块：

```sh
atm format -file todo.txt
```

## `atm untag`

移除生成状态：

```sh
atm untag -file todo.txt
atm untag -file todo.txt -done=false
atm untag -file todo.txt -running=false
```

参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-file PATH` | `todo.txt` | todo 文件 |
| `-done` | `true` | 是否移除 done 状态 |
| `-running` | `true` | 是否移除 running 状态 |

## `atm mcp`

`mcp` 子命令主要给 agent runtime 和测试使用。普通用户通常不直接调用。

### `atm mcp check`

运行 `until` 检查用的临时 stdio MCP server：

```sh
atm mcp check -result-file /tmp/atm/check-result.json
```

工具名：`atm_report_check`

输入：

```json
{"passed": true, "summary": "简短依据"}
```

### `atm mcp output`

运行结构化输出用的临时 stdio MCP server：

```sh
atm mcp output \
  -result-file /tmp/atm/output.json \
  -schema-file /tmp/atm/schema.json \
  -schema-format json
```

工具名：`atm_report_output`

输入 schema 来自 `/output` 的 fenced schema block。

### `atm mcp db`

运行 `/db` 使用的临时 stdio MCP server：

```sh
atm mcp db -config-file /tmp/atm/db-config.json
atm mcp db -config-file /tmp/atm/db-config.json -readonly
```

`-config-file` 是 ATM 运行时生成的 JSON 配置，普通用户通常不需要手写。配置包含当前任务可见的 DB 名称、数据文件路径、scope、persist、access 和 usage。

工具名：

| 工具 | 说明 |
| --- | --- |
| `atm_db_list` | 列出当前任务可见 DB |
| `atm_db_get` | 读取一个 key |
| `atm_db_scan` | 用 glob 遍历 key |
| `atm_db_append` | 追加字符串值 |
| `atm_db_set` | 替换 key 的字符串数组 |
| `atm_db_delete` | 删除 key 或 key 中指定值 |

`-readonly` 会把所有 DB 降为只读。ATM 在自然语言 `until` 和 `/if` 检查中使用这个模式。

### `atm mcp defs`

运行 `/mcp def use` 使用的临时 stdio MCP server：

```sh
atm mcp defs -config-file /tmp/atm/defs-config.json
```

`-config-file` 是 ATM 运行时生成的 JSON 配置，包含当前 todo 文件、允许暴露的 definition、当前 workdir、DB、skill、MCP 和 runner 配置。普通用户通常不需要手写。
