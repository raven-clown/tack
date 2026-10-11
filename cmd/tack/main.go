// Command tack generates and checks TypeScript clients for APIs built with
// tack, from their OpenAPI document.
//
// Usage:
//
//	tack gen ts -spec <file|url|-> [-o api.ts]
//	tack gen zod -spec <file|url|-> [-o schemas.ts]
//	tack check -spec <file|url|-> [-zod] api.ts
//	tack dev [-spec http://localhost:8080/openapi.json] [-o api.ts] [-zod schemas.ts] [-dir .] [-- command...]
//	tack mock -spec <file|url|-> [-addr 127.0.0.1:8081]
//
// gen ts writes the client for the document to -o, or to standard output;
// gen zod writes Zod schemas with the server's validation rules. check exits
// with status 1 when the file differs from what the document generates, so
// CI fails when the client is older than the routes; -zod compares a schemas
// file. The document is read from a file, fetched from a URL such as
// http://localhost:8080/openapi.json, or read from standard input with "-".
//
// dev builds the app in -dir with go build, runs it, waits for -spec to
// answer, and regenerates the client (and the Zod schemas with -zod). Save a
// Go file and it does it all again; a build error keeps the running app and
// waits for the next save. A command after -- replaces the build and run step.
//
// mock serves every operation in the document with example responses and
// cross-origin headers, so a frontend can be built before the API runs.
// A request with a Prefer: code=404 header gets that documented error.
//
// An app can also check its files in a test with tsgen.Check and
// tsgen.CheckZod, with no running server or exported document.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/raven-clown/tack/internal/mock"
	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

const maxSpecBytes = 64 << 20

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  tack gen ts -spec <file|url|-> [-o api.ts]        Write the TypeScript client
  tack gen zod -spec <file|url|-> [-o schemas.ts]   Write Zod schemas with the validation rules
  tack check -spec <file|url|-> [-zod] api.ts       Exit 1 when the file is stale
  tack dev [-spec URL] [-o api.ts] [-zod schemas.ts] [-dir .] [-- command...]
                                                    Build, run, and regenerate on every save
  tack mock -spec <file|url|-> [-addr 127.0.0.1:8081]
                                                    Serve example responses for the document

The OpenAPI document is read from a file, a URL such as
http://localhost:8080/openapi.json, or standard input with "-".
`)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	var err error
	switch args[0] {
	case "gen":
		err = gen(ctx, args[1:], stdin, stdout)
	case "check":
		err = check(ctx, args[1:], stdin, stdout)
	case "dev":
		err = devCmd(ctx, args[1:], stdout, stderr)
	case "mock":
		err = mockCmd(ctx, args[1:], stdin, stdout)
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

// generators maps a gen target to its generator and check.
var generators = map[string]struct {
	generate func(*openapi.Document) ([]byte, error)
	check    func(*openapi.Document, string) error
}{
	"ts":  {tsgen.Generate, tsgen.Check},
	"zod": {tsgen.GenerateZod, tsgen.CheckZod},
}

func gen(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: gen needs a target: ts or zod", errUsage)
	}
	target, ok := generators[args[0]]
	if !ok {
		return fmt.Errorf("%w: unknown target %q; use ts or zod", errUsage, args[0])
	}
	fs := flag.NewFlagSet("tack gen "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	spec := fs.String("spec", "", "OpenAPI document: a file, a URL, or - for standard input")
	out := fs.String("o", "", "output file (default standard output)")
	if err := fs.Parse(args[1:]); err != nil {
		return wrapFlag(err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	doc, err := loadSpec(ctx, *spec, stdin)
	if err != nil {
		return err
	}
	ts, err := target.generate(doc)
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

func check(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("tack check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	spec := fs.String("spec", "", "OpenAPI document: a file, a URL, or - for standard input")
	zod := fs.Bool("zod", false, "the file holds Zod schemas (tack gen zod) rather than the client")
	if err := fs.Parse(args); err != nil {
		return wrapFlag(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: check needs the file to compare", errUsage)
	}
	doc, err := loadSpec(ctx, *spec, stdin)
	if err != nil {
		return err
	}
	target := generators["ts"]
	if *zod {
		target = generators["zod"]
	}
	if err := target.check(doc, fs.Arg(0)); err != nil {
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
func loadSpec(ctx context.Context, src string, stdin io.Reader) (*openapi.Document, error) {
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
		b, err = fetchSpec(ctx, src)
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

func fetchSpec(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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

func mockCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("tack mock", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	spec := fs.String("spec", "", "OpenAPI document: a file, a URL, or - for standard input")
	addr := fs.String("addr", "127.0.0.1:8081", "address to listen on")
	if err := fs.Parse(args); err != nil {
		return wrapFlag(err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	doc, err := loadSpec(ctx, *spec, stdin)
	if err != nil {
		return err
	}
	h, err := mock.Handler(doc)
	if err != nil {
		return err
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", *addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "mock of %s %s listening on http://%s\n", doc.Info.Title, doc.Info.Version, ln.Addr())
	for _, route := range mock.Routes(h) {
		fmt.Fprintf(stdout, "  %s\n", route)
	}
	fmt.Fprintln(stdout, "Send a Prefer: code=<status> header to get a documented error response.")
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
