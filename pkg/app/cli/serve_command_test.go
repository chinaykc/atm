package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeRequiresExplicitFileOrRegistration(t *testing.T) {
	withWorkingDirectory(t, t.TempDir())

	_, err := discoverAPIRoutes(nil)
	if err == nil || !strings.Contains(err.Error(), "atm serve register") {
		t.Fatalf("expected registration error, got %v", err)
	}
}

func TestServeRegisterLoadsOnlyRegisteredFiles(t *testing.T) {
	dir := t.TempDir()
	withWorkingDirectory(t, dir)

	file := filepath.Join("workflows", "create.todo.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("/flag string name user\n\n/task\nhello {{name}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"serve", "register", file, "--path", "/user/create"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "/user/create -> workflows/create.todo.md") {
		t.Fatalf("unexpected register output: %s", out.String())
	}

	generated := filepath.Join(".atm", "api", "runs", "user-create", "20260527120000", "source.todo.md")
	if err := os.MkdirAll(filepath.Dir(generated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generated, []byte("/task\ninternal copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	routes, err := discoverAPIRoutes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected registered path aliases only, got %#v", routes)
	}
	if _, ok := routes["/user/create"]; !ok {
		t.Fatalf("missing primary route: %#v", routes)
	}
	if _, ok := routes["/user/create.todo.md"]; !ok {
		t.Fatalf("missing suffix route: %#v", routes)
	}
	for route := range routes {
		if strings.Contains(route, "runs") {
			t.Fatalf("generated run artifact exposed as route: %s", route)
		}
	}
}

func TestServeScanRegistersProjectLocalAPIFiles(t *testing.T) {
	dir := t.TempDir()
	withWorkingDirectory(t, dir)

	file := filepath.Join(".atm", "api", "user", "create.todo.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("/task\ncreate user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(".atm", "api", "runs", "old", "source.todo.md")
	if err := os.MkdirAll(filepath.Dir(generated), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generated, []byte("/task\ninternal copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := Run([]string{"serve", "scan"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "registered 1 API file") {
		t.Fatalf("unexpected scan output: %s", out.String())
	}

	routes, err := discoverAPIRoutes(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := routes["/user/create"]; !ok {
		t.Fatalf("missing scanned route: %#v", routes)
	}
	if _, ok := routes["/user/create.todo.md"]; !ok {
		t.Fatalf("missing scanned suffix route: %#v", routes)
	}
	for _, endpoint := range routes {
		if strings.Contains(endpoint.File, "runs") {
			t.Fatalf("generated run artifact registered: %#v", endpoint)
		}
	}
}

func TestServeRegisterGlobalWritesGlobalRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	withWorkingDirectory(t, dir)

	file := filepath.Join(dir, "api.todo.md")
	if err := os.WriteFile(file, []byte("/task\napi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"serve", "register", file, "--path", "/api", "-g"}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".atm", "api", "index.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no local registry, stat err=%v", err)
	}
	globalPath, err := apiRegistryPathForScope(true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"path": "/api"`) || !strings.Contains(string(data), file) {
		t.Fatalf("unexpected global registry:\n%s", data)
	}
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}
