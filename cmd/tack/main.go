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
//	tack new <dir> [-module path]
//	tack version
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
// new creates a runnable project: one resource with typed errors, graceful
// shutdown, a README, and a test that regenerates and checks the client.
//
// An app can also check its files in a test with tsgen.Check and
// tsgen.CheckZod, with no running server or exported document.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/raven-clown/tack/tsgen"
)

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
  tack new <dir> [-module path]                     Create a project with an API and a client test
  tack version                                      Print the version

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
	case "new":
		err = newCmd(args[1:], stdout)
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "tack %s\n", versionString())
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
		usage(stdout)
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

func wrapFlag(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	return fmt.Errorf("%w: %w", errUsage, err)
}
