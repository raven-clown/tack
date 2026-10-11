package tack

import "context"

// ContextValue returns the value stored under key in ctx as a T. ok is false
// when the key is absent or holds another type. Middleware stores values with
// context.WithValue; handlers read them with ContextValue.
func ContextValue[T any](ctx context.Context, key any) (v T, ok bool) {
	v, ok = ctx.Value(key).(T)
	return v, ok
}
