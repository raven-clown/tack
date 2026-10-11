package schema

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/raven-clown/tack/openapi"
)

func (r *Registry) object(t reflect.Type) (*openapi.Schema, error) {
	s := &openapi.Schema{Type: "object", Properties: map[string]*openapi.Schema{}}
	for _, c := range dominant(collect(t, nil, 0, false, map[reflect.Type]bool{})) {
		f := c.field
		if _, ok := f.Tag.Lookup("default"); ok {
			return nil, fmt.Errorf("field %s: default tags only apply to path, query, header, and cookie values; remove it", f.Name)
		}
		fs, err := r.Field(f)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", f.Name, err)
		}
		if c.asString {
			fs = &openapi.Schema{Type: "string", Description: fs.Description, Nullable: fs.Nullable}
		}
		s.Properties[c.name] = fs
		if !c.optional && ((!c.omit && f.Type.Kind() != reflect.Pointer) || hasRule(f.Tag.Get("validate"), "required")) {
			s.Required = append(s.Required, c.name)
		}
	}
	return s, nil
}

type candidate struct {
	name     string
	field    reflect.StructField
	index    []int
	depth    int
	tagged   bool
	omit     bool
	asString bool
	optional bool
}

// collect lists JSON fields the way encoding/json sees them, including fields
// promoted from embedded structs.
func collect(t reflect.Type, index []int, depth int, optional bool, visiting map[reflect.Type]bool) []candidate {
	if visiting[t] {
		return nil
	}
	visiting[t] = true
	defer delete(visiting, t)

	var out []candidate
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		idx := append(slices.Clone(index), i)
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			et, ptr := f.Type, false
			if et.Kind() == reflect.Pointer {
				et, ptr = et.Elem(), true
			}
			if et.Kind() == reflect.Struct {
				out = append(out, collect(et, idx, depth+1, optional || ptr, visiting)...)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		c := candidate{name: name, field: f, index: idx, depth: depth, tagged: name != "", optional: optional}
		if c.name == "" {
			c.name = f.Name
		}
		o := "," + opts + ","
		c.omit = strings.Contains(o, ",omitempty,") || strings.Contains(o, ",omitzero,")
		c.asString = strings.Contains(o, ",string,") && isScalar(f.Type)
		out = append(out, c)
	}
	return out
}

// dominant applies encoding/json's rules: the shallowest field wins, a tagged
// field beats untagged ones at the same depth, and other ties are dropped.
func dominant(cs []candidate) []candidate {
	byName := map[string][]candidate{}
	var names []string
	for _, c := range cs {
		if _, ok := byName[c.name]; !ok {
			names = append(names, c.name)
		}
		byName[c.name] = append(byName[c.name], c)
	}
	var out []candidate
	for _, n := range names {
		group := byName[n]
		best := group[0].depth
		for _, c := range group {
			best = min(best, c.depth)
		}
		var top, tagged []candidate
		for _, c := range group {
			if c.depth == best {
				top = append(top, c)
				if c.tagged {
					tagged = append(tagged, c)
				}
			}
		}
		switch {
		case len(top) == 1:
			out = append(out, top[0])
		case len(tagged) == 1:
			out = append(out, tagged[0])
		}
	}
	slices.SortFunc(out, func(a, b candidate) int { return slices.Compare(a.index, b.index) })
	return out
}

func isScalar(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Float32, reflect.Float64:
		return true
	}
	return isInt(t.Kind()) || isUint(t.Kind())
}

func isInt(k reflect.Kind) bool { return k >= reflect.Int && k <= reflect.Int64 }

func isUint(k reflect.Kind) bool { return k >= reflect.Uint && k <= reflect.Uint64 }
