package tack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raven-clown/tack/openapi"
	"github.com/raven-clown/tack/tsgen"
)

type e2ePet struct {
	ID   int      `json:"id"`
	Name string   `json:"name" validate:"required"`
	Tags []string `json:"tags,omitempty"`
}

type e2eCreated struct {
	Location string `header:"Location"`
	Count    int    `header:"X-Count"`
	Body     e2ePet
}

var errE2ENotFound = DefineError(http.StatusNotFound, "pet_not_found", "pet not found")

// e2eApp exercises every part of the generated runtime: path, query, and
// header parameters, bodies, typed errors, response headers, 204, security,
// and a response the document does not describe.
func e2eApp() *App {
	app := New(Config{Title: "E2E", Version: "1", Security: map[string]openapi.SecurityScheme{"bearer": Bearer("JWT")}})
	pets := map[int]e2ePet{1: {ID: 1, Name: "Milo"}}
	Get(app, "/pets", func(_ context.Context, in struct {
		Tag   []string `query:"tag"`
		Limit int      `query:"limit" default:"10"`
		Trace string   `header:"X-Trace"`
	}) ([]e2ePet, error) {
		return []e2ePet{{ID: in.Limit, Name: strings.Join(in.Tag, "+"), Tags: []string{in.Trace}}}, nil
	})
	Get(app, "/pets/{id}", func(_ context.Context, in struct {
		ID int `path:"id"`
	}) (e2ePet, error) {
		p, ok := pets[in.ID]
		if !ok {
			return e2ePet{}, errE2ENotFound.WithDetail(fmt.Sprintf("pet %d not found", in.ID))
		}
		return p, nil
	}, Errors(errE2ENotFound))
	Post(app, "/pets", func(_ context.Context, in struct{ Body e2ePet }) (e2eCreated, error) {
		p := e2ePet{ID: len(pets) + 1, Name: in.Body.Name}
		pets[p.ID] = p
		return e2eCreated{Location: fmt.Sprintf("/pets/%d", p.ID), Count: len(pets), Body: p}, nil
	}, Status(http.StatusCreated))
	Delete(app, "/pets/{id}", func(_ context.Context, in struct {
		ID int `path:"id"`
	}) (struct{}, error) {
		delete(pets, in.ID)
		return struct{}{}, nil
	}, Status(http.StatusNoContent))
	Get(app, "/boom", func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, errors.New("database down")
	})
	Get(app, "/teapot", func(context.Context, struct{}) (struct{}, error) {
		return struct{}{}, nil
	}, Middleware(func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte("short and stout"))
		})
	}))
	requireToken := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer secret" {
				app.WriteError(w, r, ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	admin := app.Group("/admin", requireToken).With(Security("bearer"))
	Get(admin, "/me", func(context.Context, struct{}) (map[string]string, error) {
		return map[string]string{"user": "admin"}, nil
	})
	return app
}

const e2eScript = `import { ApiError, Client, unwrap } from "./api.js";

declare const process: { argv: string[] };

const baseUrl = process.argv[2];
const api = new Client({ baseUrl, credentials: { bearer: async () => "secret" }, headers: { "X-Trace": "default" } });
const out: Record<string, unknown> = {};

const list = await api.getPets({ tag: ["a", "b"], limit: 5 });
out.list = list.ok && { status: list.status, data: list.data };

const traced = await api.getPets({ "X-Trace": "t1" }, { headers: { "X-Trace": "override" } });
out.traced = traced.ok && traced.data[0]?.tags;

const missing = await api.getPetsById({ id: 9 });
if (!missing.ok) {
  switch (missing.error.code) {
    case "pet_not_found":
      out.missing = { status: missing.status, detail: missing.error.detail, title: missing.error.title };
      break;
    default:
      out.missing = "unexpected " + missing.error.code;
  }
}

const created = await api.postPets({ body: { id: 0, name: "Rex" } });
out.created = created.ok && { status: created.status, location: created.headers.Location, count: created.headers["X-Count"], data: created.data };

const invalid = await api.postPets({ body: { id: 0, name: "" } });
out.invalid = !invalid.ok && invalid.error.code === "validation_failed" && { status: invalid.status, errors: invalid.error.errors };

const deleted = await api.deletePetsById({ id: 1 });
out.deleted = deleted.ok && { status: deleted.status, data: deleted.data };

out.me = unwrap(await api.getAdminMe());

const anonymous = await new Client({ baseUrl }).getAdminMe();
out.anonymous = !anonymous.ok && { status: anonymous.status, code: anonymous.error.code };

const boom = await api.getBoom();
out.boom = !boom.ok && { status: boom.status, code: boom.error.code, detail: boom.error.detail };

try {
  await api.getTeapot();
  out.teapot = "no error";
} catch (e) {
  out.teapot = e instanceof ApiError && { status: e.status, error: e.error, message: e.message, name: e.name };
}

try {
  unwrap(await api.getPetsById({ id: 9 }));
} catch (e) {
  out.unwrapped = e instanceof ApiError && { status: e.status, message: e.message };
}

console.log(JSON.stringify(out));
`

// e2eZodScript validates data with the generated schemas; its output is
// compared with e2eZodWant.
const e2eZodScript = `import { PostPetsInput, GetPetsInput, PetNotFoundProblem } from "./schemas.js";

const out: Record<string, unknown> = {};
out.valid = PostPetsInput.safeParse({ body: { id: 1, name: "Milo", tags: ["a"] } }).success;
const invalid = PostPetsInput.safeParse({ body: { id: "1", tags: "a" } });
out.invalidPaths = invalid.success ? [] : invalid.error.issues.map((i) => i.path.join(".")).sort();
out.defaults = GetPetsInput.parse({});
out.problem = PetNotFoundProblem.safeParse({ type: "about:blank", title: "Not Found", status: 404, code: "pet_not_found" }).success;
out.otherCode = PetNotFoundProblem.safeParse({ type: "about:blank", title: "Not Found", status: 404, code: "internal" }).success;
console.log(JSON.stringify(out));
`

const e2eZodWant = `{
  "valid": true,
  "invalidPaths": ["body.id", "body.name", "body.tags"],
  "defaults": {"limit": 10},
  "problem": true,
  "otherCode": false
}`

const e2eWant = `{
  "list": {"status": 200, "data": [{"id": 5, "name": "a+b", "tags": ["default"]}]},
  "traced": ["override"],
  "missing": {"status": 404, "detail": "pet 9 not found", "title": "Not Found"},
  "created": {"status": 201, "location": "/pets/2", "count": 2, "data": {"id": 2, "name": "Rex"}},
  "invalid": {"status": 422, "errors": [{"location": "body.name", "message": "is required"}]},
  "deleted": {"status": 204},
  "me": {"user": "admin"},
  "anonymous": {"status": 401, "code": "unauthorized"},
  "boom": {"status": 500, "code": "internal"},
  "teapot": {"status": 418, "error": "short and stout", "message": "HTTP 418", "name": "ApiError"},
  "unwrapped": {"status": 404, "message": "pet 9 not found"}
}`

// TestClientEndToEnd compiles the generated client with tsc and runs it with
// node against the app. It needs both on PATH and is skipped otherwise.
func TestClientEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in -short mode")
	}
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not on PATH")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}

	app := e2eApp()
	client, err := tsgen.Generate(app.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"api.ts":        string(client),
		"main.ts":       e2eScript,
		"package.json":  `{"type": "module"}`,
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noUnusedLocals": true, "target": "es2022", "module": "nodenext", "lib": ["es2022", "dom"], "types": []}, "files": ["api.ts", "main.ts"]}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compile := exec.Command(tsc, "-p", dir)
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}

	srv := httptest.NewServer(app)
	defer srv.Close()
	run := exec.Command(node, filepath.Join(dir, "main.js"), srv.URL)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}

	compareJSON(t, "client results", out, e2eWant)

	// The Zod schemas need the zod package; skip that half when npm cannot
	// install it here, except in CI where it must work.
	npm, err := exec.LookPath("npm")
	if err != nil {
		t.Log("npm not on PATH; skipping the Zod check")
		return
	}
	install := exec.Command(npm, "install", "--no-audit", "--no-fund", "--silent", "zod@4")
	install.Dir = dir
	if out, err := install.CombinedOutput(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("npm install zod: %v\n%s", err, out)
		}
		t.Logf("npm install zod failed, skipping the Zod check: %v\n%s", err, out)
		return
	}
	schemas, err := tsgen.GenerateZod(app.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"schemas.ts":    string(schemas),
		"zod.ts":        e2eZodScript,
		"tsconfig.json": `{"compilerOptions": {"strict": true, "noUnusedLocals": true, "target": "es2022", "module": "nodenext", "lib": ["es2022", "dom"], "skipLibCheck": true}, "files": ["schemas.ts", "zod.ts"]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compile = exec.Command(tsc, "-p", dir)
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("tsc (zod): %v\n%s", err, out)
	}
	run = exec.Command(node, filepath.Join(dir, "zod.js"))
	out, err = run.CombinedOutput()
	if err != nil {
		t.Fatalf("node (zod): %v\n%s", err, out)
	}
	compareJSON(t, "zod results", out, e2eZodWant)
}

func compareJSON(t *testing.T, what string, gotRaw []byte, wantRaw string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("%s: node output is not JSON: %v\n%s", what, err, gotRaw)
	}
	if err := json.Unmarshal([]byte(wantRaw), &want); err != nil {
		t.Fatal(err)
	}
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	wantJSON, _ := json.MarshalIndent(want, "", "  ")
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("%s differ\ngot:\n%s\nwant:\n%s", what, gotJSON, wantJSON)
	}
}
