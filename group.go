package tack

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// Group registers routes under a shared path prefix, middleware, and route
// options. Create one with App.Group or Group.Group and pass it to Get, Post,
// and the other registration functions.
type Group struct {
	parent Router
	path   string
	mw     []func(http.Handler) http.Handler
	opts   []RouteOption
}

func newGroup(parent Router, prefix string, mw []func(http.Handler) http.Handler) *Group {
	if prefix != "" && (!strings.HasPrefix(prefix, "/") || strings.HasSuffix(prefix, "/")) {
		panic(fmt.Sprintf("tack: group prefix %q must start with \"/\" and not end with one", prefix))
	}
	return &Group{parent: parent, path: prefix, mw: slices.Clone(mw)}
}

// Group returns a nested group. Its routes run the parent's middleware first.
func (g *Group) Group(prefix string, mw ...func(http.Handler) http.Handler) *Group {
	return newGroup(g, prefix, mw)
}

// Use adds middleware to routes registered on g, or on groups made from it,
// after this call.
func (g *Group) Use(mw ...func(http.Handler) http.Handler) {
	g.mw = append(g.mw, mw...)
}

// With returns a group with the same prefix and middleware whose routes also
// get opts, such as Security or Errors, before their own options.
func (g *Group) With(opts ...RouteOption) *Group {
	return &Group{parent: g, opts: slices.Clone(opts)}
}

func (g *Group) app() *App      { return g.parent.app() }
func (g *Group) prefix() string { return g.parent.prefix() + g.path }
func (g *Group) middleware() []func(http.Handler) http.Handler {
	return slices.Concat(g.parent.middleware(), g.mw)
}
func (g *Group) options() []RouteOption { return slices.Concat(g.parent.options(), g.opts) }
