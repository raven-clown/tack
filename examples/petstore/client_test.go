package main

import (
	"flag"
	"os"
	"testing"

	"github.com/raven-clown/tack/tsgen"
)

var update = flag.Bool("update", false, "regenerate client/api.ts")

// TestClient fails when client/api.ts is older than the routes. Regenerate it
// with go test . -update.
func TestClient(t *testing.T) {
	const path = "client/api.ts"
	if *update {
		ts, err := tsgen.Generate(newApp().OpenAPI())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, ts, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := tsgen.Check(newApp().OpenAPI(), path); err != nil {
		t.Fatal(err)
	}
}
