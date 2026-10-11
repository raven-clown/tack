package tack

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

type baseKey struct{}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func waitForServer(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get(url) //nolint:noctx // test
		if err == nil {
			res.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not come up", url)
}

func TestRunShutsDownStreams(t *testing.T) {
	app := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	Get(app, "/wait", func(ctx context.Context, _ struct{}) (Stream[int], error) {
		return func(yield func(int) bool) {
			if yield(1) {
				<-ctx.Done()
			}
		}, nil
	})
	Get(app, "/base", func(ctx context.Context, _ struct{}) (string, error) {
		v, _ := ctx.Value(baseKey{}).(string)
		return v, nil
	})
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- app.Serve(ctx, &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: time.Second,
			BaseContext: func(net.Listener) context.Context {
				return context.WithValue(context.Background(), baseKey{}, "from base")
			},
		})
	}()
	waitForServer(t, "http://"+addr+"/base")

	res, err := http.Get("http://" + addr + "/base") //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != `"from base"` {
		t.Errorf("BaseContext value not seen by the handler: %s", b)
	}

	stream, err := http.Get("http://" + addr + "/wait") //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	r := bufio.NewReader(stream.Body)
	if line, _ := r.ReadString('\n'); line != "data: 1\n" {
		t.Fatalf("first event %q", line)
	}

	start := time.Now()
	cancel()
	if _, err := io.ReadAll(r); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("stream did not end cleanly: %v", err)
	}
	if err := <-done; err != nil {
		t.Errorf("Serve returned %v", err)
	}
	if took := time.Since(start); took > ShutdownTimeout {
		t.Errorf("shutdown took %s; the stream's context should have been canceled", took)
	}
	if res, err := http.Get("http://" + addr + "/base"); err == nil { //nolint:noctx // test
		res.Body.Close()
		t.Error("server still accepting connections")
	}
}

func TestRunErrors(t *testing.T) {
	app := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := app.Run(context.Background(), "256.0.0.1:0"); err == nil {
		t.Error("want an error for an invalid address")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := app.Run(context.Background(), ln.Addr().String()); err == nil {
		t.Error("want an error for an address in use")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx, freeAddr(t)); err != nil {
		t.Errorf("Run with a done context should shut down cleanly, got %v", err)
	}
}
