package atm_test

import (
	"github.com/chinaykc/atm"
	"github.com/chinaykc/atm/pkg/lang/syntax"
	"testing"
)

func TestFacadeCompilesAndFormatsContent(t *testing.T) {
	plan, err := atm.Compile("todo.txt", "/task\nhello\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(plan.Tasks))
	}

	formatted, blocks := atm.FormatContent("/task\nhello\n")
	if blocks != 1 {
		t.Fatalf("formatted block count = %d, want 1", blocks)
	}
	if formatted == "" {
		t.Fatal("formatted content is empty")
	}
}

func TestFacadeParsesSyntaxDocument(t *testing.T) {
	doc := atm.ParseSyntax("todo.md", "# Release\n\n/task /go reviewers\nship it\n")
	if doc.SourcePath != "todo.md" {
		t.Fatalf("source path = %q, want todo.md", doc.SourcePath)
	}
	if len(doc.Blocks) != 1 {
		t.Fatalf("block count = %d, want 1", len(doc.Blocks))
	}
	if doc.Blocks[0].Kind != syntax.BlockTask {
		t.Fatalf("block kind = %q, want task", doc.Blocks[0].Kind)
	}
	if len(doc.Blocks[0].Commands) == 0 || doc.Blocks[0].Commands[0].Kind != syntax.CommandTask {
		t.Fatalf("commands = %#v, want leading /task", doc.Blocks[0].Commands)
	}
}
