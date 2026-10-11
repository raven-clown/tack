package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Shop-API")
	code, stdout, stderr := runCLI(t, "", "new", dir, "-module", "example.com/shop")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Created "+dir) || !strings.Contains(stdout, "go test . -update") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for name, want := range map[string]string{
		"go.mod":       "module example.com/shop\n\ngo 1.",
		"main.go":      `tack.New(tack.Config{Title: "Shop-API", Version: "0.1.0"})`,
		"main_test.go": `{"client/schemas.ts", tsgen.GenerateZod, tsgen.CheckZod},`,
		".gitignore":   "/Shop-API\n/Shop-API.exe\n",
		"README.md":    "# Shop-API\n",
	} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s lacks %q:\n%s", name, want, b)
		}
	}
	out, err := exec.Command("gofmt", "-l", dir).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		t.Errorf("generated Go is not gofmt clean: %v %s", err, out)
	}
	if !testing.Short() {
		buildGenerated(t, dir)
	}

	// Default module path is the directory name, lower-cased.
	dir2 := filepath.Join(t.TempDir(), "MyApi")
	code, _, _ = runCLI(t, "", "new", dir2)
	if b, _ := os.ReadFile(filepath.Join(dir2, "go.mod")); code != 0 || !strings.HasPrefix(string(b), "module myapi\n") {
		t.Errorf("default module: code %d, go.mod %q", code, b)
	}

	for _, tt := range []struct {
		name, want string
		args       []string
	}{
		{"no dir", "new needs one directory", []string{"new"}},
		{"two dirs", "new needs one directory", []string{"new", "a", "b"}},
		{"dot", "new needs a directory name", []string{"new", "."}},
		{"not empty", "exists and is not empty", []string{"new", dir}},
		{"bad module", "characters Go does not allow", []string{"new", filepath.Join(t.TempDir(), "x"), "-module", "a b"}},
		{"unknown flag", "flag provided but not defined", []string{"new", "-x", "y"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, "", tt.args...)
			if code == 0 || !strings.Contains(stderr, tt.want) {
				t.Fatalf("code %d, stderr %q; want %q", code, stderr, tt.want)
			}
		})
	}
}

// buildGenerated points the new project at this checkout and runs its tests,
// which also generate its client, using only the module cache.
func buildGenerated(t *testing.T, dir string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "go.mod"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, "\nrequire github.com/raven-clown/tack v0.0.0\n\nreplace github.com/raven-clown/tack => %s\n", root)
	f.Close()
	for _, args := range [][]string{{"mod", "tidy"}, {"vet", "./..."}, {"test", ".", "-update"}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			if os.Getenv("CI") == "" && strings.Contains(string(out), "cannot find module") {
				t.Logf("skipping the build of the generated project: a module could not be fetched\n%s", out)
				return
			}
			t.Fatalf("go %s in the generated project: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	for _, name := range []string{"client/api.ts", "client/schemas.ts"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not generated: %v", name, err)
		}
	}
}

func TestVersion(t *testing.T) {
	code, stdout, _ := runCLI(t, "", "version")
	if code != 0 || !strings.HasPrefix(stdout, "tack ") {
		t.Fatalf("code %d, stdout %q", code, stdout)
	}
	version = "v9.9.9"
	defer func() { version = "dev" }()
	if _, stdout, _ = runCLI(t, "", "--version"); stdout != "tack v9.9.9\n" {
		t.Fatalf("stdout %q", stdout)
	}
}
