package tack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

func TestClientGolden(t *testing.T) {
	got, err := tsgen.Generate(specApp().OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "client.golden.ts")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("client differs from %s (run go test . -update)\ngot:\n%s", path, got)
	}
	if err := tsgen.Check(specApp().OpenAPI(), path); err != nil {
		t.Errorf("Check on the golden file: %v", err)
	}
}

// The CLI reads the document back from JSON; it must produce the same client
// as the in-process document.
func TestClientFromJSON(t *testing.T) {
	app := specApp()
	direct, err := tsgen.Generate(app.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(fetch(t, app, "/openapi.json").Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	fromJSON, err := tsgen.Generate(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(fromJSON) != string(direct) {
		t.Errorf("client from JSON differs from the in-process one:\n%s", fromJSON)
	}
}
