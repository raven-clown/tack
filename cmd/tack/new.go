package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"text/template"
)

//go:embed template
var templates embed.FS

// version is set by the release build; otherwise it comes from the module.
var version = "dev"

// versionString returns the version to show: the release tag, the module
// version `go install` recorded, or dev.
func versionString() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

var moduleName = regexp.MustCompile(`^[A-Za-z0-9._~/-]+$`)

// newCmd implements `tack new`: a runnable project with one resource, typed
// errors, graceful shutdown, and a test that keeps the client fresh.
func newCmd(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("tack new", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	module := flags.String("module", "", "module path for go.mod (default: the directory name)")
	// The directory may come before or after the flags.
	var dir string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return wrapFlag(err)
	}
	switch {
	case dir == "" && flags.NArg() == 1:
		dir = flags.Arg(0)
	case dir == "" || flags.NArg() > 0:
		return fmt.Errorf("%w: new needs one directory to create", errUsage)
	}
	name := filepath.Base(filepath.Clean(dir))
	if name == "." || name == string(filepath.Separator) {
		return fmt.Errorf("%w: new needs a directory name, not %q", errUsage, dir)
	}
	if *module == "" {
		*module = strings.ToLower(name)
	}
	if !moduleName.MatchString(*module) {
		return fmt.Errorf("module path %q has characters Go does not allow; set one with -module", *module)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s exists and is not empty; choose a new directory", dir)
	}
	goVersion := strings.TrimPrefix(runtime.Version(), "go")
	if i := strings.Index(goVersion, " "); i > 0 {
		goVersion = goVersion[:i]
	}
	data := map[string]string{"Name": name, "Module": *module, "Title": name, "Go": goVersion}

	files := map[string]string{
		"main.go.tmpl": "main.go", "main_test.go.tmpl": "main_test.go", "go.mod.tmpl": "go.mod",
		"gitignore.tmpl": ".gitignore", "README.md.tmpl": "README.md",
	}
	for src, dst := range files {
		b, err := fs.ReadFile(templates, "template/"+src)
		if err != nil {
			return err
		}
		t, err := template.New(src).Parse(string(b))
		if err != nil {
			return err
		}
		path := filepath.Join(dir, dst)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		err = t.Execute(f, data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	fmt.Fprintf(stdout, `Created %s with an API, a test, and a README.

Next:
  cd %s
  go mod tidy            # fetches tack
  go test . -update      # writes client/api.ts and client/schemas.ts
  go run .               # http://localhost:8080/docs
`, dir, dir)
	return nil
}
