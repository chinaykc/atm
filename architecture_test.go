package atm_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPackageArchitecture(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}}|{{join .Imports \",\"}}", "./...").Output()
	if err != nil {
		t.Fatalf("go list package graph: %v", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, imports, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("unexpected go list line: %q", line)
		}
		if strings.Contains(imports, "github.com/chinaykc/atm/pkg/lang/dsl") {
			t.Fatalf("%s imports removed pkg/lang/dsl path", pkg)
		}
		if strings.HasPrefix(pkg, "github.com/chinaykc/atm/pkg/integration/") && strings.Contains(imports, "github.com/chinaykc/atm/pkg/lang/compiler") {
			t.Fatalf("%s must depend on pkg/lang/ir or narrower APIs, not compiler", pkg)
		}
		switch pkg {
		case "github.com/chinaykc/atm/pkg/lang/format",
			"github.com/chinaykc/atm/pkg/lang/marker":
			if strings.Contains(imports, "github.com/chinaykc/atm/pkg/lang/compiler") {
				t.Fatalf("%s must expose reusable language helpers without depending on compiler", pkg)
			}
		case "github.com/chinaykc/atm/pkg/lang/ir":
			if strings.Contains(imports, "github.com/chinaykc/atm/pkg/lang/compiler") {
				t.Fatalf("%s must own IR types and not depend on compiler", pkg)
			}
		case "github.com/chinaykc/atm/pkg/lang/syntax":
			if strings.Contains(imports, "github.com/chinaykc/atm/") {
				t.Fatalf("%s must remain dependency-free inside this module", pkg)
			}
		case "github.com/chinaykc/atm/pkg/runtime/store",
			"github.com/chinaykc/atm/pkg/workspace/taskdoc":
			if strings.Contains(imports, "github.com/chinaykc/atm/pkg/lang/compiler") {
				t.Fatalf("%s must use document/marker/format APIs instead of compiler", pkg)
			}
		}
	}
}

func TestRemovedFlatPackageDirsStayRemoved(t *testing.T) {
	for _, dir := range []string{
		"pkg/cli",
		"pkg/dsl",
		"pkg/engine",
		"pkg/expr",
		"pkg/mcp",
		"pkg/planview",
		"pkg/store",
		"pkg/todo",
		"pkg/tools",
	} {
		if _, err := os.Stat(dir); err == nil {
			t.Fatalf("removed flat package directory still exists: %s", dir)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", dir, err)
		}
	}
}
