package futurefit

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/raven-clown/tack"
)

// WebSocket. A typed socket would be a named function type in the root
// package, found at registration the way Stream is: every instantiation
// implements an unexported interface, so the route sends it with a type
// assertion. That is additive. Until then a socket is a plain handler next to
// the app, as in TestWebSocketToday.

// Socket is what tack.Socket[In, Out] would be: the handler returns it, tack
// upgrades the connection, decodes In messages, and encodes Out messages.
type Socket[In, Out any] func(ctx context.Context, in <-chan In, out chan<- Out)

// socketer is the unexported interface the route would assert; it mirrors
// the streamer interface Stream uses.
type socketer interface {
	socketTypes() (in, out reflect.Type)
}

func (Socket[In, Out]) socketTypes() (in, out reflect.Type) {
	return reflect.TypeFor[In](), reflect.TypeFor[Out]()
}

type chatMessage struct {
	Text string `json:"text" validate:"required,max=500"`
}

type chatReply struct {
	Echo string `json:"echo"`
}

func TestSocketDetection(t *testing.T) {
	// The detection mechanism works for a two-parameter type exactly as it
	// does for Stream: no reflection on function types is needed.
	typ := reflect.TypeFor[Socket[chatMessage, chatReply]]()
	if !typ.Implements(reflect.TypeFor[socketer]()) {
		t.Fatal("Socket does not implement socketer")
	}
	in, out := reflect.Zero(typ).Interface().(socketer).socketTypes()
	if in != reflect.TypeFor[chatMessage]() || out != reflect.TypeFor[chatReply]() {
		t.Fatalf("types %s, %s", in, out)
	}
}

func TestWebSocketToday(t *testing.T) {
	app := newApp()
	tack.Get(app, "/pets/{id}", getPet)

	// Today a raw handler, such as a WebSocket upgrade, is mounted beside the
	// app. An App.Handle(pattern, h) method for raw handlers would be
	// additive and would keep app middleware in front of it.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /chat", func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			w.WriteHeader(http.StatusUpgradeRequired)
			return
		}
		// A WebSocket library would hijack the connection here.
		w.WriteHeader(http.StatusSwitchingProtocols)
	})
	mux.Handle("/", app)

	if status, _, _ := do(t, mux, http.MethodGet, "/pets/1", "", nil); status != 200 {
		t.Errorf("app route through the outer mux: %d", status)
	}
	if status, _, _ := do(t, mux, http.MethodGet, "/chat", "", nil); status != http.StatusUpgradeRequired {
		t.Errorf("socket route: %d", status)
	}
	if _, ok := app.OpenAPI().Paths["/chat"]; ok {
		t.Error("a raw handler must not appear in the document")
	}
}
