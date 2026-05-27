# 4. Reuse: Definitions, Calls, Returns, And Imports

Use `/def` to define reusable task templates, `/call` to run them, and `/return` to return values to the caller.

## Define And Call

```txt
/def summarize area
Summarize risks in {{area}}.

/return {{agent.last_message}}

/task
/let api_summary /call summarize api
Use this API summary: {{api_summary}}
```

Definitions are ordinary ATM task flows packaged for reuse. They can use prompts, `/bash`, `/output`, `/db`, `/skill`, loops, and local expressions.

Call a definition as a task/header command when its side effects matter:

```txt
/call summarize api
```

Bind a returned value when prompt text needs it:

```txt
/let summary /call summarize api
Review this summary: {{summary}}
```

`/let name /call ...` is lazy. It runs when the variable is rendered or read by an expression. Multiple reads in the same task invocation reuse the cached value.

## Return Values

Return plain text:

```txt
/return {{agent.last_message}}
```

Return bash output:

```txt
/return /bash jq -r .version package.json
```

Return structured JSON with a schema fence:

````txt
/return
```schema
passed:boolean:whether the gate passed
reason:string:short reason
```
````

The caller can access fields:

```txt
/let gate /call check_gate
Gate passed: {{gate.passed}}
Reason: {{gate.reason}}
```

`agent.*` values exist during `/return` rendering. They describe the assistant messages produced by the current definition call. They are not global variables for ordinary prompts.

## Parameters And Scope

Definition parameters become template variables:

```txt
/def review area owner
Review {{area}} and mention {{owner}}.

/call review api Ada
```

Markdown scope applies to definitions, imports, pools, databases, skills, tool declarations, and standalone `/let` declarations. Root declarations are visible to later tasks in the whole document. Declarations under a heading are visible to later tasks in that heading and child headings.

Task-header `/let` bindings are visible only to the current task block and child-heading tasks. A child section can shadow a parent binding with the same name.

## Imports

Import definitions from another ATM file:

```txt
/import shared.todo.md
/import release from shared.todo.md
```

Imported definitions are called by name. Namespaced imports use the namespace prefix:

```txt
/call release.plan_shards
```

Imports are resolved relative to the source ATM file. Direct runs copy source and import files into the managed run directory and rewrite import paths inside the working copy.


## Recommended Patterns

Use `/def` for work that has a stable interface:

```txt
/def check_area area
/cd services/{{area}}
/bash go test ./...
Review failures and fix them.

/for area in [api billing docs]
/call check_area {{area}}
```

Use structured `/return` for machine-readable decisions:

````txt
/def release_gate
Inspect test output and release notes.

/return
```schema
passed:boolean:whether release can proceed
reason:string:why
```

/let gate /call release_gate

/if (gate.passed)
Continue release: {{gate.reason}}

/else
Stop release: {{gate.reason}}
````
