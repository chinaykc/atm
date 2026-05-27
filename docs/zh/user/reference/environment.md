# 环境变量

ATM 的环境变量接口很小。分成两类：ATM 设置给子进程的变量，以及 ATM 自己读取的变量。

## ATM 设置的变量

| 变量 | 可见范围 | 含义 |
| --- | --- | --- |
| `ATM_TODO_FILE` | `/bash`、`/let ... /bash`、Codex、Claude、临时工具进程 | 当前运行处理的托管工作副本路径 |

示例：

```txt
/bash printf 'current todo: %s\n' "$ATM_TODO_FILE"
总结当前 atm 文件路径。
```

注意：直接 `run` 期间，`ATM_TODO_FILE` 指向 `~/.atm/runs/<run-id>/work/...` 下的工作副本，而不是原始位置参数对应的源文件。原始源文件和 import 文件会被占位文件暂时隐藏，退出时恢复不变。

如果任务使用 `/cd path`，`/bash`、`/let ... /bash`、Codex、Claude 和检查 agent 会在该任务工作区中运行，但 `ATM_TODO_FILE` 仍指向托管工作副本。

## ATM 读取的变量

| 变量 | 使用场景 | 含义 |
| --- | --- | --- |
| `ATM_HOME` | 直接 `run`、`resume` | ATM home；默认是当前操作系统用户 home 下的 `.atm` |
| `VISUAL` | `atm append` 交互输入 | 首选编辑器 |
| `EDITOR` | `atm append` 交互输入 | 备用编辑器 |

## `ATM_HOME`

直接运行的源副本、工作副本、manifest、执行结果和恢复索引默认保存在：

```txt
~/.atm/runs/<run-id>/
```

如果要把这些托管运行目录放到其他位置，可以设置：

```sh
ATM_HOME=/path/to/atm-home atm run todo.txt
```

## 编辑器变量

当你运行：

```sh
atm append todo.txt
```

并且没有直接提供 prompt，stdin 也是终端时，ATM 会按顺序尝试：

1. `VISUAL`
2. `EDITOR`
3. 平台默认小编辑器

示例：

```sh
VISUAL=vim atm append todo.txt
EDITOR=nano atm append todo.txt
```

## 普通系统环境

ATM 会继承父进程环境。常见影响包括：

| 变量 | 影响 |
| --- | --- |
| `PATH` | 查找 `codex`、`claude`、shell 命令和编辑器 |
| 语言/区域变量 | 影响 shell 命令输出格式 |
| 认证变量 | 如果 Codex/Claude 或脚本依赖这些变量，它们会随父环境传入 |

ATM 不会把 `/let` 自动导出为 shell 环境变量。`/let` 是模板变量，只在 ATM 渲染 prompt、bash 脚本、`/args`、`/cd`、`/output` schema 和 `/return` 时使用。

`/db` 声明也不会导出环境变量。数据库路径和权限通过临时 工具配置传给 agent runtime；普通任务通过 `atm_db_*` 工具访问 DB。
