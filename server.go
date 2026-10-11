package tack

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ShutdownTimeout is how long Run and Serve wait for in-flight requests
// after ctx is done.
const ShutdownTimeout = 10 * time.Second

// Run serves the app on addr until ctx is done, then stops accepting
// connections, cancels the context of every request still running (which
// ends open streams), and waits up to ShutdownTimeout for handlers to finish.
// It returns nil after a clean shutdown. The server reads headers with a 10
// second limit and closes idle connections after two minutes; it sets no
// limit on request or response bodies, so uploads and streams are not cut
// off. Use Serve to configure the server yourself.
//
//	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	log.Fatal(app.Run(ctx, ":8080"))
func (a *App) Run(ctx context.Context, addr string) error {
	return a.Serve(ctx, &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	})
}

// Serve is Run with a server the caller configured. It sets srv.Handler to
// the app when nil and derives every request's context from one that is
// canceled at shutdown, composing with srv.BaseContext when set.
func (a *App) Serve(ctx context.Context, srv *http.Server) error {
	if srv.Handler == nil {
		srv.Handler = a
	}
	base, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	parent := srv.BaseContext
	srv.BaseContext = func(ln net.Listener) context.Context {
		if parent == nil {
			return base
		}
		merged, cancel := context.WithCancel(parent(ln))
		context.AfterFunc(base, cancel)
		return merged
	}
	srv.RegisterOnShutdown(cancelRequests)

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return err
	}
	a.logger.InfoContext(ctx, "tack: listening", slog.String("addr", ln.Addr().String()))
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil // the caller shut the server down itself
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	if serveErr := <-served; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return err
}
