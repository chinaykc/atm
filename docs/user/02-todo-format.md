# 2. Todo 文件与任务边界

ATM 是 Agent Task Markdown，核心输入是一份 Markdown/纯文本任务文件。纯文本可以继续用空行切分任务；Markdown 文档中，heading 是上下文和作用域，任务由 `/task`、任务启动控制命令，或带 prompt 的 task header 命令切分。

## 旧式文本块

纯文本文件可以使用旧式文本块模式。空行分隔任务：

```txt
第一个任务。

第二个任务。

/go
第三个任务，后台运行。
```

任意数量的空行都可以分隔任务，包括只包含空格的空行。

## 注释与分隔线

旧式任务块和 `//` task-list section 中，以下行会被忽略：

```txt
# 整行注释
   # 前面有空格也可以
<!-- HTML 注释 -->
[//]: # (Markdown 引用式注释)
[comment]: <> (Markdown 引用式注释)
---
===
```

注意：只识别整行注释。下面这行不是注释，`#` 会作为 prompt 内容发送：

```txt
请解释 package # 这里仍然是 prompt 内容
```

## Markdown 任务文档

Markdown heading 不启动任务。普通 Markdown 会保留在文件中，并作为所在 section 内任务的上下文。需要执行完全没有 header/control 命令的普通文本时，显式写 `/task`。如果任务开头已经有 `/let`、`/args`、`/cd`、`/output`、`/db use`、`/skill use`、`/mcp use` 等 task header 命令并跟随 prompt，这个块本身就是任务；如果只有 `/let` 等声明而没有 prompt，它只是当前 Markdown scope 内后续任务可见的声明块。

```md
# 发布背景

这里是说明文字，不执行。

/for 2
运行 go test ./...，修复失败。

/task
运行 go vet ./...，修复可操作问题。

## Discuss

/task
这里是一个普通任务 prompt。
```

### 显式上下文与私有文档

默认情况下，任务会看到所在 section 的普通 Markdown 上下文。需要引用远处 section 时，在 task header 中写 `/context #Heading`：

```md
# Database Rules

所有迁移必须可回滚。

# Fix Migration

/task
/context #Database Rules
修复最新 migration。
```

如果某段说明只是给人看的草稿、运行方法或敏感备注，不希望进入 agent 默认上下文，用 `/doc` 写成行内说明或 fenced block：

````md
# Internal Notes

/doc 这里不会进入后续 task 的默认上下文。

/doc
```
这里也不会进入默认上下文，也不会被 `/context #Internal Notes` 展开。
```
````

## Heading 与任务的关系

| 写法 | 语义 |
| --- | --- |
| `# Title` | 建立文档 section 和上下文 |
| `/task` | 从这里开始一个普通任务 |
| `/for`、`/go`、`/call` 等 | 从这里开始带控制流的任务 |
| 更深层 heading | 默认属于当前 task prompt；其中出现任务启动命令时，创建 child-heading task |

```mermaid
flowchart TB
  A["## Verify"] --> B["section context"]
  B --> C["/for 2 task"]
  B --> D["/task task"]
```

child-heading task 会继承父任务 root prompt、父任务 header 中的 `/let` 绑定，以及自己所在 heading 路径上的普通 Markdown；不会继承 sibling child-heading 的正文或任务。继承到的 lazy provider 在 child task invocation 内解析并缓存，不和 parent 共享缓存；child section 中的同名 `/let` 会遮蔽父 task header 的值。

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

这里 `Scope1` 下的 `/for 2` 会看到 `Review backend.` 和 `API and migrations.`；`Scope2` 下的 `/task` 会看到 `Review backend.` 和 `Docs.`，但不会看到 `Scope1` 的正文或任务。

执行时，ATM 会先执行尚未完成的 child-heading task，再执行父任务。父任务运行时会看到已完成子任务的人类可见 `> [!ATM]` report 摘要；被 skipped 的子任务不会嵌入父任务 prompt。

## 命令必须写在任务开头

任务命令只在 prompt 开始前识别：

```txt
/for 3
修复测试。
```

prompt 开始后的 slash 文本不会执行；如果它看起来像 ATM 命令，解析器会报错，要求你把它移动到 task header，或在空行之后作为新的 sibling/child task 开始。

```txt
解释下面这行：
/for 3
```

## 格式化

整理生成状态：

```sh
atm format -file todo.txt
```

移除生成状态：

```sh
atm untag -file todo.txt
```

保留 done，只移除 running：

```sh
atm untag -file todo.txt -done=false
```
