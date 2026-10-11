package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

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
