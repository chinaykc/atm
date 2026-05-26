# Quick Start

## About

/doc
```md
Smallest useful ATM queue: one normal task, one resume task, and one counted loop.

Run: `atm run -file examples/quick-start.todo.md`.
```

## validate

/task
Run the project's normal validation command and fix any failures.

## continue

/resume
Continue the previous agent session and finish the current change.

## final review

/for 2
Review the final diff for correctness. Pass {{n}} should look for a different class of issue, then make only necessary cleanup edits.
