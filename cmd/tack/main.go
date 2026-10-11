// Command tack generates and checks TypeScript clients for APIs built with
// tack, from their OpenAPI document.
//
// Usage:
//
//	tack gen ts -spec <file|url|-> [-o api.ts]
//	tack check -spec <file|url|-> api.ts
//
// gen ts writes the client for the document to -o, or to standard output.
// check exits with status 1 when the file differs from what the document
// generates, so CI fails when the client is older than the routes. The
// document is read from a file, fetched from a URL such as
// http://localhost:8080/openapi.json, or read from standard input with "-".
//
// An app can also check its client in a test with tsgen.Check, with no
// running server or exported document.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

const maxSpecBytes = 64 << 20

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  tack gen ts -spec <file|url|-> [-o api.ts]   Write the TypeScript client
  tack check -spec <file|url|-> api.ts         Exit 1 when the client is stale

The OpenAPI document is read from a file, a URL such as
http://localhost:8080/openapi.json, or standard input with "-".
`)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "gen":
		err = gen(args[1:], stdin, stdout)
	case "check":
		err = check(args[1:], stdin, stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, tsgen.ErrStale):
		fmt.Fprintln(stderr, "tack:", err)
		return 1
	case errors.Is(err, errUsage):
		fmt.Fprintln(stderr, "tack:", err)
		fmt.Fprintln(stderr)
		usage(stderr)
		return 2
	}
	fmt.Fprintln(stderr, "tack:", err)
	return 1
}

var errUsage = errors.New("usage")

func gen(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "ts" {
		return fmt.Errorf("%w: gen needs a target; the only one is ts", errUsage)
	}
	fs := flag.NewFlagSet("tack gen ts", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	spec := fs.String("spec", "", "OpenAPI document: a file, a URL, or - for standard input")
	out := fs.String("o", "", "output file (default standard output)")
	if err := fs.Parse(args[1:]); err != nil {
		return wrapFlag(err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	doc, err := loadSpec(*spec, stdin)
	if err != nil {
		return err
	}
	ts, err := tsgen.Generate(doc)
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = stdout.Write(ts)
		return err
	}
	if err := os.WriteFile(*out, ts, 0o644); err != nil {
		return fmt.Errorf("writing client: %w", err)
	}
	return nil
}

func check(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("tack check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	spec := fs.String("spec", "", "OpenAPI document: a file, a URL, or - for standard input")
	if err := fs.Parse(args); err != nil {
		return wrapFlag(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: check needs the client file to compare", errUsage)
	}
	doc, err := loadSpec(*spec, stdin)
	if err != nil {
		return err
	}
	if err := tsgen.Check(doc, fs.Arg(0)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s is up to date\n", fs.Arg(0))
	return nil
}

func wrapFlag(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	return fmt.Errorf("%w: %w", errUsage, err)
}

// loadSpec reads the OpenAPI document from a file, a URL, or stdin.
func loadSpec(src string, stdin io.Reader) (*openapi.Document, error) {
	if src == "" {
		return nil, fmt.Errorf("%w: -spec is required", errUsage)
	}
	var (
		b   []byte
		err error
	)
	switch {
	case src == "-":
		b, err = io.ReadAll(io.LimitReader(stdin, maxSpecBytes))
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		b, err = fetchSpec(src)
	default:
		b, err = os.ReadFile(src)
	}
	if err != nil {
		return nil, fmt.Errorf("reading OpenAPI document: %w", err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parsing OpenAPI document %s: %w", src, err)
	}
	if doc.Paths == nil {
		return nil, fmt.Errorf("parsing OpenAPI document %s: no paths; is this an OpenAPI 3 document in JSON?", src)
	}
	return &doc, nil
}

func fetchSpec(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxSpecBytes))
}
