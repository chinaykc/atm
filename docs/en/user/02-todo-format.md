# 2. ATM Files And Task Boundaries

ATM's input is a Markdown or plain-text task file. Both use the same Markdown-native parser: ordinary text and Markdown are kept as background context, and executable work starts at `/task`, a task-start control command, or a task header command followed by prompt text.

## Task Boundaries

Plain-text files are parsed as documents. Ordinary text is background context. Executable tasks start at `/task`, task-start control commands, or task header commands followed by prompt text. Use `/task` for ordinary prompts:

```txt
/task
First task.

/task
Second task.

/go
Third task, running in the background.
```

Blank lines stay inside the current task prompt. A task ends when a later root-level task-start/control command appears after a blank line, when a same-or-higher Markdown heading appears, when a report block appears, or at end of file.

## Comments And Rules

The following whole-line forms are ignored in task files and `//` task-list sections:

```txt
# whole-line comment
   # leading spaces are accepted
<!-- HTML comment -->
[//]: # (Markdown reference comment)
[comment]: <> (Markdown reference comment)
---
===
```

Only whole-line comments are recognized. This line is prompt text:

```txt
Explain package # this remains prompt text
```

## Markdown Task Documents

Markdown headings create sections, context, and scope. A heading by itself does not start a task. Ordinary Markdown before a task is preserved and passed as context to tasks in the same section.

Use `/task` when a prompt has no header or control command. A block that starts with `/let`, `/args`, `/cd`, `/output`, `/db use`, `/skill use`, `/webhook`, or another task header command and then has prompt text is also a task. Standalone declaration blocks such as `/let`, `/flag`, and `/webhook new` are visible to later tasks in the current Markdown scope.

Task header commands can be combined on one line or split across lines. Configuration commands are merged into the current task, and flow commands run in the order they appear.

Quote an argument that looks like a command, such as `"/task"` in a `/bash` message. A fenced `/output` schema or fenced `/webhook` payload is written on its own header line.

```md
# Release Context

This is documentation, not a task.

/for 2
Run go test ./... and fix failures.

/task
Run go vet ./... and fix actionable issues.

## Discuss

/task
This is an ordinary prompt task.
```

## Explicit Context And Private Notes

Tasks see ordinary Markdown in their section by default. Add a distant section with `/context #Heading`:

```md
# Database Rules

All migrations must be reversible.

# Fix Migration

/context #Database Rules /task
Fix the latest migration.
```

Use `/doc` for notes that are only for humans:

````md
# Internal Notes

/doc This does not enter task context.

/doc
```
This also stays out of default context and `/context`.
```
````

## Headings And Tasks

| Form | Meaning |
| --- | --- |
| `# Title` | Create a document section and context |
| `/task` | Start an ordinary prompt task |
| `/for`, `/go`, `/call`, `/webhook`, etc. | Start a task with control flow or a pre-agent action |
| Deeper heading | Part of the current task prompt by default; a task-start command inside it creates a child-heading task |

Child-heading tasks inherit the parent task's root prompt, `/let` bindings in the parent task header, and ordinary Markdown in their own heading path. They do not inherit sibling child-heading text or sibling tasks.

```md
# Review

/task
Review backend.

### Scope1

API and migrations.

/for 2
Fix tests {{n}}.

### Scope2

Docs.

/task
Fix docs.
```

The `Scope1` task sees `Review backend.` plus `API and migrations.`. The `Scope2` task sees `Review backend.` plus `Docs.`. ATM runs pending child-heading tasks before the parent task. When the parent task runs, completed child task summaries are included in its prompt.

## Commands Must Be At The Start

Task commands are recognized before prompt text starts:

```txt
/for 3
Fix tests.
```

Slash text inside a prompt is prompt text. If it looks like an ATM command, parsing fails and asks you to move it to the task header or start a new sibling/child task after a blank line.

Format task files with:

```sh
atm format todo.txt
```

Formatting writes composed task headers as one command per line without changing execution order.

Direct `run` keeps generated state out of the source file by default. Use `atm clean` on `~/.atm/runs/<run-id>/result.todo.md` when you need to remove generated state from a result document.
