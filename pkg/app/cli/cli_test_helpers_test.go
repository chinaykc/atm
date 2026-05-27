package cli

import (
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"testing"
)

var doneBlockForTest = regexp.MustCompile(`(?s)\n(?:<!-- atm:report [^\n]+ -->\n)?> \[!ATM\]\n> status: done\n> started: [^\n]+\n> finished: [^\n]+\n> duration: [^\n]+\n> runs: [0-9]+x(?:\n> id: [^\n]+)?(?:\n> source: [^\n]+)?(?:\n> rendered: [^\n]+)?(?:\n> report: [^\n]+)?(?:\n<!-- /atm:report -->)?`)

func normalizeDoneMarkersForTest(content string) string {
	return doneBlockForTest.ReplaceAllString(content, "\n[done]")
}

func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell fake executable")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("test requires sh")
	}
}

func assertSameFile(t *testing.T, got, want string) {
	t.Helper()
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat registered file %q: %v", got, err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat expected file %q: %v", want, err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("registered file %q does not identify expected file %q", got, want)
	}
}
