package tack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// Stream is a handler result that is sent as server-sent events: an iterator
// whose every value is one event, encoded as JSON on its data line. The
// handler returns the iterator and tack drives it, so the function can be a
// loop that yields until the client goes away:
//
//	tack.Get(app, "/ticks", func(ctx context.Context, _ struct{}) (tack.Stream[Tick], error) {
//		return func(yield func(Tick) bool) {
//			for i := 0; ; i++ {
//				select {
//				case <-ctx.Done():
//					return
//				case <-time.After(time.Second):
//				}
//				if !yield(Tick{N: i}) {
//					return
//				}
//			}
//		}, nil
//	})
//
// ctx is done when the client disconnects or the server shuts down, and
// yield returns false once the connection is gone. Use Event as T to name
// events or give them IDs. The response is documented as text/event-stream
// with the schema of T, and the generated client reads it as an async
// iterable. A Stream cannot be the Body of an output wrapper.
type Stream[T any] func(yield func(T) bool)

// Event is one server-sent event with an optional name and ID, for use as the
// T of a Stream. Only Data is in the schema; Name and ID become the event and
// id lines.
type Event[T any] struct {
	Name string
	ID   string
	Data T
}

// streamer is implemented by every Stream instantiation, so routes can send
// one with a type assertion rather than reflection.
type streamer interface {
	streamItem() reflect.Type
	serve(w http.ResponseWriter, r *http.Request, status int) error
}

// eventer is implemented by every Event instantiation.
type eventer interface {
	event() (name, id string, data any)
}

var streamerType = reflect.TypeFor[streamer]()

func (Stream[T]) streamItem() reflect.Type { return reflect.TypeFor[T]() }

func (e Event[T]) event() (name, id string, data any) { return e.Name, e.ID, e.Data }

// serve writes the stream. It returns an error only before the first byte,
// so the caller can still send a problem response.
func (s Stream[T]) serve(w http.ResponseWriter, r *http.Request, status int) error {
	if s == nil {
		return fmt.Errorf("the handler returned a nil Stream; return an empty iterator or an error")
	}
	if !canFlush(w) {
		return fmt.Errorf("the ResponseWriter cannot flush, so events would never leave the server; give the middleware that wraps it an Unwrap() http.ResponseWriter method")
	}
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(status)
	if err := rc.Flush(); err != nil {
		return nil //nolint:nilerr // The status is sent; the connection is already gone.
	}
	var buf bytes.Buffer
	done := r.Context().Done()
	s(func(item T) bool {
		select {
		case <-done:
			return false
		default:
		}
		buf.Reset()
		var data any = item
		if e, ok := any(item).(eventer); ok {
			var name, id string
			name, id, data = e.event()
			if name != "" {
				fmt.Fprintf(&buf, "event: %s\n", sanitizeLine(name))
			}
			if id != "" {
				fmt.Fprintf(&buf, "id: %s\n", sanitizeLine(id))
			}
		}
		b, err := json.Marshal(data)
		if err != nil {
			// The stream is open, so the error can only go on the stream.
			fmt.Fprintf(&buf, "event: error\ndata: %s\n\n", jsonString("encoding event: "+err.Error()))
			_, _ = w.Write(buf.Bytes())
			_ = rc.Flush()
			return false
		}
		buf.WriteString("data: ")
		buf.Write(b)
		buf.WriteString("\n\n")
		if _, err := w.Write(buf.Bytes()); err != nil {
			return false
		}
		return rc.Flush() == nil
	})
	return nil
}

// canFlush reports whether w, or a writer it unwraps to, is an http.Flusher.
func canFlush(w http.ResponseWriter) bool {
	for {
		if _, ok := w.(http.Flusher); ok {
			return true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = u.Unwrap()
	}
}

// sanitizeLine keeps a field value on one line.
func sanitizeLine(s string) string {
	return strings.NewReplacer("\r", "", "\n", " ").Replace(s)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
