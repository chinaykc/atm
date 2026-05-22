# 环境变量

ATM 的环境变量接口很小。分成两类：ATM 设置给子进程的变量，以及 ATM 自己读取的变量。

## ATM 设置的变量

| 变量 | 可见范围 | 含义 |
| --- | --- | --- |
| `ATM_TODO_FILE` | `/bash`、`/let ... /bash`、Codex、Claude、临时 MCP server（check/output/db） | 当前运行处理的 todo 文件路径 |

示例：

```txt
/bash printf 'current todo: %s\n' "$ATM_TODO_FILE"
总结当前 todo 文件路径。
```

注意：运行期间 ATM 会把原 todo 文件移动到系统临时目录作为活跃文件。因此 `ATM_TODO_FILE` 可能指向临时活跃路径，而不是原始 `-file` 路径。

如果任务使用 `/cd path`，`/bash`、`/let ... /bash`、Codex、Claude 和检查 agent 会在该任务工作区中运行，但 `ATM_TODO_FILE` 仍指向当前活跃 todo 文件。

## ATM 读取的变量

| 变量 | 使用场景 | 含义 |
| --- | --- | --- |
| `ATM_MCP_CHECK_LOG` | `/for N until ...` 的临时 MCP 检查 | 可选调试日志路径 |
| `VISUAL` | `atm append` 交互输入 | 首选编辑器 |
| `EDITOR` | `atm append` 交互输入 | 备用编辑器 |

## `ATM_MCP_CHECK_LOG`

调试 `until` 判断时可以设置：

```sh
ATM_MCP_CHECK_LOG=/tmp/atm-check.log atm run -file todo.txt
```

当 check agent 调用 `atm_report_check` 后，临时 MCP server 会向该文件追加类似记录：

```txt
2026-05-21T10:00:00+08:00 PASS tests passed
```

正常使用不需要设置这个变量。

## 编辑器变量

当你运行：

```sh
atm append -file todo.txt
```

并且没有直接提供 prompt，stdin 也是终端时，ATM 会按顺序尝试：

1. `VISUAL`
2. `EDITOR`
3. 平台默认小编辑器

示例：

```sh
VISUAL=vim atm append -file todo.txt
EDITOR=nano atm append -file todo.txt
```

## 普通系统环境

ATM 会继承父进程环境。常见影响包括：

| 变量 | 影响 |
| --- | --- |
| `PATH` | 查找 `codex`、`claude`、shell 命令和编辑器 |
| 语言/区域变量 | 影响 shell 命令输出格式 |
| 认证变量 | 如果 Codex/Claude 或脚本依赖这些变量，它们会随父环境传入 |

ATM 不会把 `/let` 自动导出为 shell 环境变量。`/let` 是模板变量，只在 ATM 渲染 prompt、bash 脚本、`/args`、`/cd`、`/output` schema 和 `/return` 时使用。

`/db` 声明也不会导出环境变量。数据库路径和权限通过临时 MCP 配置传给 agent runtime；普通任务通过 `atm_db_*` 工具访问 DB。
