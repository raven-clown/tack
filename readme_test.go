package tack

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readmePrelude declares what the README fragments refer to without defining.
const readmePrelude = `package main

import (
	"context"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/raven-clown/tack"
	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

var (
	_ = fmt.Sprintf
	_ = log.Fatal
	_ multipart.FileHeader
	_ = http.StatusOK
	_ = os.Interrupt
	_ = signal.NotifyContext
	_ = syscall.SIGTERM
	_ testing.T
	_ = middleware.RequestID
	_ openapi.Schema
	_ = tsgen.Check
)

type GetPetInput struct {
	ID int ` + "`path:\"id\"`" + `
}

type Pet struct {
	ID   int    ` + "`json:\"id\"`" + `
	Name string ` + "`json:\"name\"`" + `
}

type Change struct {
	Action string ` + "`json:\"action\"`" + `
}

var (
	app     = tack.New(tack.Config{})
	changes = make(chan Change)
)

func newApp() *tack.App { return app }

func listPets(context.Context, struct{}) ([]Pet, error)  { return nil, nil }
func listUsers(context.Context, struct{}) ([]Pet, error) { return nil, nil }

func requireAdmin(h http.Handler) http.Handler { return h }
func audit(h http.Handler) http.Handler        { return h }

func main() {}
`

var shortDecl = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*(?:, [A-Za-z_][A-Za-z0-9_]*)*) :=`)

// TestREADMECompiles builds every Go block in README.md against this checkout:
// a block with a package clause as a program, the others as fragments of one
// file with the prelude above.
func TestREADMECompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a module")
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	var programs []string
	var fragments strings.Builder
	for i, block := range regexp.MustCompile("(?s)```go\n(.*?)```").FindAllStringSubmatch(string(readme), -1) {
		code := block[1]
		if strings.HasPrefix(code, "package ") {
			programs = append(programs, code)
			continue
		}
		if strings.HasPrefix(code, "func ") {
			fmt.Fprintf(&fragments, "\n// Block %d.\n%s", i+1, code)
			continue
		}
		fmt.Fprintf(&fragments, "\n// Block %d.\nfunc _() {\n%s", i+1, code)
		for _, m := range shortDecl.FindAllStringSubmatch(code, -1) {
			for _, name := range strings.Split(m[1], ", ") {
				fmt.Fprintf(&fragments, "\t_ = %s\n", name)
			}
		}
		fragments.WriteString("}\n")
	}
	if len(programs) == 0 || fragments.Len() == 0 {
		t.Fatal("README.md has no Go blocks to compile")
	}

	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", fmt.Sprintf("module readme\n\ngo 1.26.0\n\nrequire github.com/raven-clown/tack v0.0.0\n\nreplace github.com/raven-clown/tack => %s\n", filepath.ToSlash(root)))
	for i, p := range programs {
		write(fmt.Sprintf("program%d/main.go", i+1), p)
	}
	write("fragments/main.go", readmePrelude+fragments.String())
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s\n\n%s", strings.Join(args, " "), err, out, fragments.String())
		}
	}
}
