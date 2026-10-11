package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

// devCmd implements `tack dev`: build and run the app, regenerate the client
// from its document, and do it again whenever a Go file changes.
func devCmd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("tack dev", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	d := &devLoop{stdout: stdout, stderr: stderr, interval: 500 * time.Millisecond, startTimeout: 30 * time.Second}
	fs.StringVar(&d.spec, "spec", "http://localhost:8080/openapi.json", "URL the running app serves its OpenAPI document at")
	fs.StringVar(&d.out, "o", "api.ts", "TypeScript client to keep fresh")
	fs.StringVar(&d.zod, "zod", "", "Zod schemas file to keep fresh (none by default)")
	fs.StringVar(&d.dir, "dir", ".", "directory to watch and build")
	if err := fs.Parse(args); err != nil {
		return wrapFlag(err)
	}
	d.command = fs.Args()
	if !strings.HasPrefix(d.spec, "http://") && !strings.HasPrefix(d.spec, "https://") {
		return fmt.Errorf("%w: -spec must be the URL of the running app, such as http://localhost:8080/openapi.json", errUsage)
	}
	return d.run(ctx)
}

type devLoop struct {
	dir, spec, out, zod string
	command             []string // empty: go build the directory and run the binary
	stdout, stderr      io.Writer
	interval            time.Duration
	startTimeout        time.Duration

	binary string
	proc   *exec.Cmd
	exited chan error
}

func (d *devLoop) logf(format string, args ...any) {
	fmt.Fprintf(d.stdout, "tack dev: "+format+"\n", args...)
}

func (d *devLoop) run(ctx context.Context) error {
	if len(d.command) == 0 {
		tmp, err := os.MkdirTemp("", "tack-dev-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		d.binary = filepath.Join(tmp, "app")
		if runtime.GOOS == "windows" {
			d.binary += ".exe"
		}
	}
	defer d.stop()
	defer http.DefaultClient.CloseIdleConnections()

	snapshot, err := scan(d.dir)
	if err != nil {
		return err
	}
	d.restart(ctx)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			d.logf("stopping")
			return nil
		case err := <-d.exited:
			d.exited = nil
			d.logf("the app exited (%v); waiting for a change", exitReason(err))
		case <-ticker.C:
			current, err := scan(d.dir)
			if err != nil {
				return err
			}
			if changed := diff(snapshot, current); changed != "" {
				snapshot = current
				d.logf("%s changed", changed)
				d.restart(ctx)
			}
		}
	}
}

func exitReason(err error) string {
	if err == nil {
		return "status 0"
	}
	return err.Error()
}

// restart builds when needed, replaces the running app, and regenerates the
// files. A failed build keeps the current app running.
func (d *devLoop) restart(ctx context.Context) {
	if len(d.command) == 0 {
		d.logf("building %s", d.dir)
		build := exec.CommandContext(ctx, "go", "build", "-o", d.binary, ".")
		build.Dir = d.dir
		if out, err := build.CombinedOutput(); err != nil {
			d.logf("build failed; fix it and save again\n%s", strings.TrimRight(string(out), "\n"))
			return
		}
	}
	d.stop()
	var cmd *exec.Cmd
	if len(d.command) == 0 {
		cmd = exec.CommandContext(ctx, d.binary)
	} else {
		cmd = exec.CommandContext(ctx, d.command[0], d.command[1:]...)
	}
	cmd.Dir = d.dir
	cmd.Stdout, cmd.Stderr = d.stdout, d.stderr
	// The command may be a launcher such as go run, so the whole tree is
	// killed, not only the direct child.
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killTree(cmd.Process) }
	cmd.WaitDelay = time.Second // do not hang on pipes a grandchild still holds
	if err := cmd.Start(); err != nil {
		d.logf("cannot start the app: %v", err)
		return
	}
	d.proc = cmd
	d.exited = make(chan error, 1)
	go func(exited chan error) { exited <- cmd.Wait() }(d.exited)
	d.logf("app started (pid %d)", cmd.Process.Pid)

	doc, err := d.waitForSpec(ctx)
	if err != nil {
		d.logf("%v", err)
		return
	}
	d.regenerate(doc)
}

func (d *devLoop) stop() {
	if d.proc == nil {
		return
	}
	_ = killTree(d.proc.Process)
	if d.exited != nil {
		<-d.exited
		d.exited = nil
	}
	d.proc = nil
}

// waitForSpec polls the document URL until the app answers.
func (d *devLoop) waitForSpec(ctx context.Context) (*openapi.Document, error) {
	deadline := time.Now().Add(d.startTimeout)
	var last error
	for time.Now().Before(deadline) {
		if d.exited != nil {
			select {
			case err := <-d.exited:
				d.exited = nil
				return nil, fmt.Errorf("the app exited before serving %s (%v); waiting for a change", d.spec, exitReason(err))
			default:
			}
		}
		b, err := fetchSpec(ctx, d.spec)
		if err == nil {
			var doc openapi.Document
			if err := json.Unmarshal(b, &doc); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", d.spec, err)
			}
			return &doc, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("%s did not answer within %s (%w); is -spec the right URL?", d.spec, d.startTimeout, last)
}

func (d *devLoop) regenerate(doc *openapi.Document) {
	targets := []struct {
		path     string
		generate func(*openapi.Document) ([]byte, error)
	}{{d.out, tsgen.Generate}}
	if d.zod != "" {
		targets = append(targets, struct {
			path     string
			generate func(*openapi.Document) ([]byte, error)
		}{d.zod, tsgen.GenerateZod})
	}
	for _, t := range targets {
		b, err := t.generate(doc)
		if err != nil {
			d.logf("%v", err)
			continue
		}
		changed, err := writeIfChanged(t.path, b)
		switch {
		case err != nil:
			d.logf("writing %s: %v", t.path, err)
		case changed:
			d.logf("%s updated", t.path)
		default:
			d.logf("%s is up to date", t.path)
		}
	}
}

func writeIfChanged(path string, b []byte) (bool, error) {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(bytes.ReplaceAll(old, []byte("\r\n"), []byte("\n")), b) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, b, 0o644)
}

// fileState is what a change to a watched file looks like.
type fileState struct {
	size    int64
	modTime time.Time
}

// scan records every Go source file, go.mod, and go.sum under dir, skipping
// hidden directories, node_modules, vendor, and testdata.
func scan(dir string) (map[string]fileState, error) {
	files := map[string]fileState{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir // unreadable; nothing to watch there
			}
			return nil // gone between the listing and the stat
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") && name != "go.mod" && name != "go.sum" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil //nolint:nilerr // replaced by the editor mid-scan; the next scan sees it
		}
		files[path] = fileState{size: info.Size(), modTime: info.ModTime()}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("watching %s: %w", dir, err)
	}
	return files, nil
}

// diff names one file that differs between two scans, or "" when none does.
func diff(before, after map[string]fileState) string {
	for path, state := range after {
		if old, ok := before[path]; !ok || old != state {
			return path
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			return path
		}
	}
	return ""
}
