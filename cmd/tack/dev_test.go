package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestMain lets the test binary double as a tiny server for the dev tests:
// with TACK_TEST_SERVE set it serves that file at /openapi.json on
// TACK_TEST_ADDR until it is killed.
func TestMain(m *testing.M) {
	if os.Getenv("TACK_TEST_PARENT") != "" {
		// Stand in for a launcher such as go run: the server is a grandchild.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "TACK_TEST_PARENT=")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if spec := os.Getenv("TACK_TEST_SERVE"); spec != "" {
		http.HandleFunc("/openapi.json", func(w http.ResponseWriter, _ *http.Request) {
			b, err := os.ReadFile(spec)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
		})
		if err := http.ListenAndServe(os.Getenv("TACK_TEST_ADDR"), nil); err != nil { //nolint:gosec // test helper
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*persistConn).writeLoop"))
}

// syncBuffer collects output written from several goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// waitFor polls cond for up to timeout.
func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func fileContains(path, s string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), s)
}

const devServer = `package main

import (
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(` + "`" + `{"openapi":"3.1.0","info":{"title":"TITLE","version":"1"},"paths":{"/":{"get":{"responses":{"204":{"description":"ok"}}}}}}` + "`" + `))
	})
	http.ListenAndServe(os.Getenv("TACK_TEST_ADDR"), nil)
}
`

// TestDevBuilds runs the default mode: go build, run, regenerate, and again
// after a save, with a broken build in between.
func TestDevBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	dir := t.TempDir()
	addr := freeAddr(t)
	t.Setenv("TACK_TEST_ADDR", addr)
	write := func(title string) {
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(strings.Replace(devServer, "TITLE", title, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module devtest\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("One")
	out := filepath.Join(dir, "client", "api.ts")
	zod := filepath.Join(dir, "client", "schemas.ts")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	})
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"dev", "-spec", "http://" + addr + "/openapi.json", "-o", out, "-zod", zod, "-dir", dir}, strings.NewReader(""), &stdout, &stderr)
	}()

	waitFor(t, "the first client", 2*time.Minute, func() bool { return fileContains(out, "from One 1") })
	if !fileContains(zod, "from One 1") {
		t.Errorf("zod schemas not written:\n%s", stdout.String())
	}

	// A broken save keeps the running app and reports the error.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { undefined() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the build failure", time.Minute, func() bool { return strings.Contains(stdout.String(), "build failed") })
	if res, err := http.Get("http://" + addr + "/openapi.json"); err != nil || res.StatusCode != http.StatusOK { //nolint:noctx // test
		t.Errorf("the first app should still be running after a failed build: %v", err)
	} else {
		res.Body.Close()
	}

	write("Two")
	waitFor(t, "the regenerated client", 2*time.Minute, func() bool { return fileContains(out, "from Two 1") })
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("dev exited with %d\n%s\n%s", code, stdout.String(), stderr.String())
		}
	case <-time.After(30 * time.Second):
		t.Fatal("dev did not stop")
	}
	log := stdout.String()
	for _, want := range []string{"building", "app started", "main.go changed", "api.ts updated", "schemas.ts updated", "stopping"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if res, err := http.Get("http://" + addr + "/openapi.json"); err == nil { //nolint:noctx // test
		res.Body.Close()
		t.Error("the app should be stopped with dev")
	}
}

// TestDevCommand uses a command after -- instead of go build, and sees the
// client follow the document and a file deletion.
func TestDevCommand(t *testing.T) {
	dir := t.TempDir()
	addr := freeAddr(t)
	spec := filepath.Join(dir, "spec.json")
	write := func(title string) {
		doc := `{"openapi":"3.1.0","info":{"title":"` + title + `","version":"2"},"paths":{"/":{"get":{"responses":{"204":{"description":"ok"}}}}}}`
		if err := os.WriteFile(spec, []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("First")
	goFile := filepath.Join(dir, "a.go")
	if err := os.WriteFile(goFile, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TACK_TEST_SERVE", spec)
	t.Setenv("TACK_TEST_ADDR", addr)
	out := filepath.Join(dir, "api.ts")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"dev", "-spec", "http://" + addr + "/openapi.json", "-o", out, "-dir", dir, "--", os.Args[0]}, strings.NewReader(""), &stdout, &stderr)
	}()
	waitFor(t, "the first client", time.Minute, func() bool { return fileContains(out, "from First 2") })

	write("Second")
	if err := os.Remove(goFile); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the regenerated client", time.Minute, func() bool { return fileContains(out, "from Second 2") })
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("dev exited with %d\n%s", code, stderr.String())
	}
	if log := stdout.String(); !strings.Contains(log, "a.go changed") || strings.Count(log, "api.ts updated") != 2 {
		t.Errorf("unexpected log:\n%s", log)
	}
}

func TestDevErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"spec not a url", []string{"dev", "-spec", "openapi.json"}, "-spec must be the URL"},
		{"unknown flag", []string{"dev", "-x"}, "flag provided but not defined"},
		{"missing dir", []string{"dev", "-spec", "http://127.0.0.1:1/openapi.json", "-dir", filepath.Join(t.TempDir(), "nope")}, "watching"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, "", tt.args...)
			if code == 0 || !strings.Contains(stderr, tt.want) {
				t.Fatalf("code %d, stderr %q; want %q", code, stderr, tt.want)
			}
		})
	}

	// An app that never answers is reported, and dev keeps watching.
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"dev", "-spec", "http://127.0.0.1:1/openapi.json", "-o", filepath.Join(dir, "api.ts"), "-dir", dir, "--", "go", "version"}, strings.NewReader(""), &stdout, &stderr)
	}()
	waitFor(t, "the exit report", time.Minute, func() bool { return strings.Contains(stdout.String(), "exited before serving") })
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("dev exited with %d", code)
	}
}

func TestScanAndDiff(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"main.go", "go.mod", "pkg/x.go", "pkg/x_test.go", "notes.md", ".git/a.go", "node_modules/m/b.go", "vendor/v.go", "testdata/t.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before, err := scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 4 {
		t.Fatalf("scanned %d files, want 4: %v", len(before), before)
	}
	if diff(before, before) != "" {
		t.Error("identical scans should not differ")
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "x.go"), []byte("xy"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, _ := scan(dir)
	if d := diff(before, after); d != filepath.Join(dir, "pkg", "x.go") {
		t.Errorf("diff = %q", d)
	}
	if err := os.Remove(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	removed, _ := scan(dir)
	if d := diff(after, removed); d != filepath.Join(dir, "go.mod") {
		t.Errorf("diff with a removed file = %q", d)
	}
	changed, err := writeIfChanged(filepath.Join(dir, "new", "f.ts"), []byte("a\n"))
	if err != nil || !changed {
		t.Fatalf("first write: %v %v", changed, err)
	}
	changed, err = writeIfChanged(filepath.Join(dir, "new", "f.ts"), []byte("a\n"))
	if err != nil || changed {
		t.Fatalf("same write: %v %v", changed, err)
	}
	if _, err := writeIfChanged(filepath.Join(dir, "new"), []byte("a\n")); err == nil {
		t.Fatal("writing over a directory should fail")
	}
}

func TestMock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"mock", "-spec", golden, "-addr", "127.0.0.1:0"}, strings.NewReader(""), &stdout, &stderr)
	}()
	var url string
	waitFor(t, "the mock to listen", time.Minute, func() bool {
		m := regexp.MustCompile(`listening on (http://[^\s]+)`).FindStringSubmatch(stdout.String())
		if m == nil {
			return false
		}
		url = m[1]
		return true
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/admin/animals/3", nil)
	req.Header.Set("Prefer", "code=404")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"code":"animal_not_found"`) {
		t.Fatalf("got %d %s", res.StatusCode, body)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("mock exited with %d: %s", code, stderr.String())
	}
	if log := stdout.String(); !strings.Contains(log, "mock of Pet API 1.2.3") || !strings.Contains(log, "  GET /pets/{id}") {
		t.Errorf("unexpected log:\n%s", log)
	}

	for _, tt := range []struct {
		name, want string
		args       []string
	}{
		{"extra arg", `unexpected argument "x"`, []string{"mock", "-spec", golden, "x"}},
		{"bad addr", "listen", []string{"mock", "-spec", golden, "-addr", "999.0.0.1:0"}},
		{"no paths", "no paths", []string{"mock", "-spec", "-"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, `{"openapi":"3.1.0","info":{"title":"x","version":"1"},"paths":{}}`, tt.args...)
			if code == 0 || !strings.Contains(stderr, tt.want) {
				t.Fatalf("code %d, stderr %q; want %q", code, stderr, tt.want)
			}
		})
	}
}

// TestDevKillsTheProcessTree runs the app behind a launcher, as go run does,
// and expects the server it started to be gone once dev stops.
func TestDevKillsTheProcessTree(t *testing.T) {
	dir := t.TempDir()
	addr := freeAddr(t)
	spec := filepath.Join(dir, "spec.json")
	doc := `{"openapi":"3.1.0","info":{"title":"Tree","version":"1"},"paths":{"/":{"get":{"responses":{"204":{"description":"ok"}}}}}}`
	if err := os.WriteFile(spec, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TACK_TEST_PARENT", "1")
	t.Setenv("TACK_TEST_SERVE", spec)
	t.Setenv("TACK_TEST_ADDR", addr)
	out := filepath.Join(dir, "api.ts")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"dev", "-spec", "http://" + addr + "/openapi.json", "-o", out, "-dir", dir, "--", os.Args[0]}, strings.NewReader(""), &stdout, &stderr)
	}()
	waitFor(t, "the client", time.Minute, func() bool { return fileContains(out, "from Tree 1") })
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("dev exited with %d\n%s", code, stderr.String())
	}
	waitFor(t, "the server to be gone", 10*time.Second, func() bool {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
		}
		return err != nil
	})
}
