# Language Packages

`pkg/lang` is split by compiler pipeline stage and consumer intent:

| Package | Responsibility |
| --- | --- |
| `syntax` | Public source-level AST for editors, linters, and source tooling. |
| `document` | Task document block discovery and Markdown heading helpers. |
| `compiler` | Full source compilation: command parsing, imports, definitions, scope, validation, and lowering. |
| `ir` | Execution-oriented types and helpers consumed by runtime, plan views, integrations, and embedders. |
| `marker` | Generated ATM status/report block helpers. |
| `format` | Source and flow formatting helpers. |
| `expr` | Expression evaluator used by conditions, loops, and output helpers. |

The public consumption model is:

```txt
syntax
document/marker/format/ir
compiler
runtime/view/integration/app
```

Consumers should choose the narrowest package that matches their task:

- Use `syntax` or `compiler.ParseSyntax` to build editor features or linters.
- Use `compiler.CompileProgram` to validate and lower a document into executable IR.
- Use `ir` for runtime-facing data structures, flow traversal, and run-option/value helpers.
- Use `marker` and `document` for file maintenance without compiling a plan.
