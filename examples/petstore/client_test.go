package main

import (
	"flag"
	"os"
	"testing"

	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

var update = flag.Bool("update", false, "regenerate client/api.ts")

// TestClient fails when client/api.ts or client/schemas.ts is older than the
// routes. Regenerate them with go test . -update.
func TestClient(t *testing.T) {
	files := []struct {
		path     string
		generate func(*openapi.Document) ([]byte, error)
		check    func(*openapi.Document, string) error
	}{
		{"client/api.ts", tsgen.Generate, tsgen.Check},
		{"client/schemas.ts", tsgen.GenerateZod, tsgen.CheckZod},
	}
	for _, f := range files {
		if *update {
			ts, err := f.generate(newApp().OpenAPI())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f.path, ts, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.check(newApp().OpenAPI(), f.path); err != nil {
			t.Fatal(err)
		}
	}
}
