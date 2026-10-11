package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/raven-clown/tack/internal/mock"
)

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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
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
