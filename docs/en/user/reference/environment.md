# Environment Variables

[中文](../../../zh/user/reference/environment.md)

ATM keeps its environment interface small and explicit.

## Variables ATM Sets

| Variable | Used by | Meaning |
| --- | --- | --- |
| `ATM_TODO_FILE` | `/bash`, lazy bash, Codex, Claude, and temporary structured tool processes | Current working ATM file path for the run |

During direct runs, `ATM_TODO_FILE` points to the managed working copy, not the original source path. `/cd` changes the task workdir for bash, agents, and local file expressions, but `ATM_TODO_FILE` still points at the working ATM file.

## Variables ATM Reads

| Variable | Used by | Meaning |
| --- | --- | --- |
| `ATM_HOME` | `run`, `resume`, reports | ATM home directory; default is `.atm` under the current OS user home |
| `VISUAL` | `atm append` | Preferred editor when interactive input is needed |
| `EDITOR` | `atm append` | Fallback editor |

ATM also inherits normal process environment such as `PATH`. That affects locating `codex`, `claude`, shell commands, and commands declared by external task tool services.

## Credential Handling

Webhook URLs and secrets can use `env:` references:

```txt
/webhook new alarm provider:dingtalk url:env:DINGTALK_WEBHOOK secret:env:DINGTALK_SECRET keyword:monitor-alert
```

Environment variables are preferred for real credentials. Inline values remain in source files and can appear in runtime artifacts or tool configuration.

## Examples

Use a separate ATM home:

```sh
ATM_HOME=/tmp/atm-home atm run todo.txt
```

Use an editor for append:

```sh
VISUAL=nvim atm append todo.txt
```
