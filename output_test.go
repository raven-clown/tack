package tack

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type level int

func (l level) MarshalText() ([]byte, error) {
	if l < 0 {
		return nil, fmt.Errorf("negative level")
	}
	return []byte(fmt.Sprintf("L%d", int(l))), nil
}

type createdPet struct {
	Status   int
	Location string    `header:"Location"`
	Count    int       `header:"X-Count"`
	Ratio    float64   `header:"X-Ratio"`
	Flag     bool      `header:"X-Flag"`
	ETag     *string   `header:"ETag"`
	Warnings []string  `header:"Warning"`
	Since    time.Time `header:"X-Since"`
	Level    level     `header:"X-Level"`
	Levels   []level   `header:"X-Levels"`
	Body     Pet
}

func TestOutputHeadersAndStatus(t *testing.T) {
	app := New(Config{})
	etag := `"v1"`
	Post(app, "/pets", func(_ context.Context, in struct{ Body Pet }) (createdPet, error) {
		out := createdPet{Body: in.Body, Count: 3, Ratio: 0.5, Since: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Level: 2, Levels: []level{1, 3}}
		switch in.Body.Name {
		case "full":
			out.Status = http.StatusAccepted
			out.Location = "/pets/1"
			out.ETag = &etag
			out.Warnings = []string{"a", "b"}
			out.Flag = true
		case "bad":
			out.Status = 99
		case "redirect":
			out.Status = http.StatusNotFound
		case "broken":
			out.Level = -1
		}
		return out, nil
	}, Status(http.StatusCreated))
	Get(app, "/empty", func(context.Context, struct{}) (struct {
		Body   struct{}
		Status int
	}, error) {
		return struct {
			Body   struct{}
			Status int
		}{Status: http.StatusNoContent}, nil
	})

	tests := []struct {
		name, method, url, body string
		wantStatus              int
		wantBody                string
		wantHeaders             map[string][]string
		absent                  []string
	}{
		{"full", "POST", "/pets", `{"name":"full"}`, 202, `{"id":0,"name":"full"}`,
			map[string][]string{"Location": {"/pets/1"}, "X-Count": {"3"}, "X-Ratio": {"0.5"}, "X-Flag": {"true"}, "Etag": {`"v1"`}, "Warning": {"a", "b"}, "X-Since": {"2026-01-02T03:04:05Z"}, "X-Level": {"L2"}, "X-Levels": {"L1", "L3"}, "Content-Type": {"application/json"}}, nil},
		{"defaults", "POST", "/pets", `{"name":"min"}`, 201, `{"id":0,"name":"min"}`,
			map[string][]string{"X-Count": {"3"}, "X-Flag": {"false"}}, []string{"Location", "Etag", "Warning"}},
		{"bad status", "POST", "/pets", `{"name":"bad"}`, 500, `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`, nil, nil},
		{"error status", "POST", "/pets", `{"name":"redirect"}`, 500, `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`, nil, []string{"X-Count"}},
		{"failing marshaler", "POST", "/pets", `{"name":"broken"}`, 500, `{"type":"about:blank","title":"Internal Server Error","status":500,"code":"internal"}`, nil, []string{"X-Count", "Location"}},
		{"status no content", "GET", "/empty", "", 204, "", nil, []string{"Content-Type"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveOnce(app, tt.method, tt.url, tt.body)
			if rec.Code != tt.wantStatus || rec.Body.String() != tt.wantBody {
				t.Fatalf("got %d %s\nwant %d %s", rec.Code, rec.Body, tt.wantStatus, tt.wantBody)
			}
			for k, want := range tt.wantHeaders {
				if got := rec.Header().Values(k); !reflect.DeepEqual(got, want) {
					t.Errorf("header %s = %q, want %q", k, got, want)
				}
			}
			for _, k := range tt.absent {
				if _, ok := rec.Header()[k]; ok {
					t.Errorf("header %s should be absent, got %q", k, rec.Header().Values(k))
				}
			}
		})
	}

	op := app.OpenAPI().Paths["/pets"].Post
	res := op.Responses["201"]
	if res.Content["application/json"].Schema.Ref != "#/components/schemas/Pet" {
		t.Fatalf("201 schema = %+v", res.Content["application/json"].Schema)
	}
	wantRequired := map[string]bool{"Location": false, "X-Count": true, "X-Ratio": true, "X-Flag": true, "ETag": false, "Warning": false, "X-Since": true, "X-Level": true, "X-Levels": false}
	for name, req := range wantRequired {
		h := res.Headers[name]
		if h == nil || h.Required != req {
			t.Errorf("header %s = %+v, want required %v", name, h, req)
		}
	}
	if _, ok := op.Responses["202"]; ok {
		t.Error("dynamic Status must not add a response")
	}
}

func TestOutputPanics(t *testing.T) {
	tests := []struct {
		name string
		do   func(a *App)
		want string
	}{
		{"headers without body", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct {
				Location string `header:"Location"`
			}, error) {
				return struct {
					Location string `header:"Location"`
				}{}, nil
			})
		}, `has field Location but no Body; add a Body field for the response body, or tag Location with json:"Location" to send it as JSON`},
		{"status without body", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct{ Status int }, error) { return struct{ Status int }{}, nil })
		}, "has field Status but no Body"},
		{"untagged field", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct {
				Body  Pet
				Extra string
			}, error) {
				return struct {
					Body  Pet
					Extra string
				}{}, nil
			})
		}, `field Extra has no header tag; tag it header:"X-Extra", or name it Body or Status`},
		{"status not int", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct {
				Body   Pet
				Status string
			}, error) {
				return struct {
					Body   Pet
					Status string
				}{}, nil
			})
		}, "Status must be an int, not string"},
		{"empty header name", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct {
				Body Pet
				X    string `header:""`
			}, error) {
				return struct {
					Body Pet
					X    string `header:""`
				}{}, nil
			})
		}, `field X: header tag needs a name, e.g. header:"Location"`},
		{"interface header", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct {
				Body Pet
				X    fmt.Stringer `header:"X"`
			}, error) {
				return struct {
					Body Pet
					X    fmt.Stringer `header:"X"`
				}{}, nil
			})
		}, "field X: unsupported header type fmt.Stringer"},
		{"unsupported header type", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct {
				Body Pet
				X    map[string]int `header:"X"`
			}, error) {
				return struct {
					Body Pet
					X    map[string]int `header:"X"`
				}{}, nil
			})
		}, "field X: unsupported header type map[string]int; use a string, bool, number, time.Time, encoding.TextMarshaler, or a slice or pointer of one"},
		{"no content with body", func(a *App) {
			Get(a, "/x", func(context.Context, struct{}) (struct {
				Body Pet
				X    string `header:"X"`
			}, error) {
				return struct {
					Body Pet
					X    string `header:"X"`
				}{}, nil
			}, Status(http.StatusNoContent))
		}, "Status(204) sends no body, but the handler returns tack.Pet; return struct{} instead"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Fatalf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.do(New(Config{}))
		})
	}
}

func TestPlainOutputsStayPlain(t *testing.T) {
	// Structs without a Body field are JSON as before, even with a Status
	// field, as long as it has a json tag or is not an int.
	type health struct {
		Status string `json:"status"`
	}
	type tagged struct {
		Status int `json:"status"`
	}
	app := New(Config{})
	Get(app, "/health", func(context.Context, struct{}) (health, error) { return health{Status: "ok"}, nil })
	Get(app, "/tagged", func(context.Context, struct{}) (tagged, error) { return tagged{Status: 1}, nil })
	if got := serveOnce(app, "GET", "/health", "").Body.String(); got != `{"status":"ok"}` {
		t.Fatalf("health = %s", got)
	}
	if got := serveOnce(app, "GET", "/tagged", "").Body.String(); got != `{"status":1}` {
		t.Fatalf("tagged = %s", got)
	}
}

func TestResetContentSpec(t *testing.T) {
	app := New(Config{})
	Post(app, "/reset", func(context.Context, struct{}) (struct{}, error) { return struct{}{}, nil }, Status(http.StatusResetContent))
	if res := app.OpenAPI().Paths["/reset"].Post.Responses["205"]; res.Content != nil {
		t.Fatalf("205 must document no body, got %+v", res.Content)
	}
}
