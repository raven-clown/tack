package bind

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A Check is the compiled validate tag of one scalar parameter. Routes whose
// every validate tag compiles run checks instead of the validator, which
// costs a reflective walk per request; anything else (bodies with rules,
// slices, rules the compiler does not know) keeps the validator.
type Check struct {
	index     []int
	loc       string
	kind      reflect.Kind // the scalar kind, behind a pointer when ptr is set
	ptr       bool
	required  bool
	omitempty bool
	rules     []rule
}

// rule is one comparison. The parameter is parsed the way the validator
// parses it for the field's kind: i for ints and string lengths, u for
// uints, n for floats, set for oneof.
type rule struct {
	tag   string
	param string
	i     int64
	u     uint64
	n     float64
	set   []string
}

// Compile returns the checks for t's parameters, or false when the
// input needs the validator: a rule on the body or on a field tack does not
// bind, a slice, a struct, an unknown rule, or a parameter that does not
// parse.
func Compile(plan *Plan, t reflect.Type) ([]Check, bool) {
	if hasUnboundRules(t) {
		return nil, false
	}
	var checks []Check
	params := append(plan.Params(), plan.FormFields()...)
	for _, p := range params {
		tag := p.Field.Tag.Get("validate")
		if tag == "" {
			continue
		}
		c, ok := compileCheck(p, tag)
		if !ok {
			return nil, false
		}
		checks = append(checks, c)
	}
	return checks, true
}

// hasUnboundRules reports whether a field without a source tag (Body, or one
// tagged json:"-") carries validate rules, which only the validator applies.
func hasUnboundRules(t reflect.Type) bool {
	for i := range t.NumField() {
		f := t.Field(i)
		bound := false
		for _, tag := range []string{"path", "query", "header", "cookie", "form"} {
			if _, ok := f.Tag.Lookup(tag); ok {
				bound = true
			}
		}
		if bound {
			continue
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			if hasUnboundRules(f.Type) {
				return true
			}
			continue
		}
		if f.Tag.Get("validate") != "" || (f.IsExported() && HasValidateTags(f.Type, map[reflect.Type]bool{})) {
			return true
		}
	}
	return false
}

func compileCheck(p Param, tag string) (Check, bool) {
	c := Check{index: p.Index, loc: p.Location}
	ft := p.Field.Type
	if ft.Kind() == reflect.Pointer {
		c.ptr = true
		ft = ft.Elem()
	}
	c.kind = ft.Kind()
	scalar := false
	switch {
	case c.kind == reflect.String, c.kind == reflect.Bool, isInt(c.kind), isUint(c.kind), isFloat(c.kind):
		scalar = true
	case c.ptr && c.kind == reflect.Struct:
		// A pointer to a struct, such as an uploaded file, can only be required.
	default:
		return c, false
	}
	for _, r := range strings.Split(tag, ",") {
		name, param, _ := strings.Cut(r, "=")
		switch name {
		case "":
			continue
		case "required", "omitempty":
			if len(c.rules) > 0 {
				return c, false // the validator applies rules in order; keep its order
			}
			if name == "required" {
				c.required = true
			} else {
				c.omitempty = true
			}
			continue
		}
		if !scalar || c.kind == reflect.Bool {
			return c, false
		}
		rl := rule{tag: name, param: param}
		switch name {
		case "min", "max", "gte", "lte", "gt", "lt", "len", "eq":
			if c.kind == reflect.String && (name == "gt" || name == "lt") {
				return c, false
			}
			if name == "eq" && c.kind == reflect.String {
				break
			}
			if !rl.parse(param, c.kind, ft.Bits) {
				return c, false
			}
		case "oneof":
			if strings.ContainsAny(param, "'\"") {
				return c, false
			}
			rl.set = strings.Fields(param)
			if len(rl.set) == 0 {
				return c, false
			}
		default:
			return c, false
		}
		c.rules = append(c.rules, rl)
	}
	return c, true
}

// parse reads the parameter the way the validator does for kind.
func (r *rule) parse(param string, kind reflect.Kind, bits func() int) bool {
	var err error
	switch {
	case kind == reflect.String, isInt(kind):
		r.i, err = strconv.ParseInt(param, 0, 64)
	case isUint(kind):
		r.u, err = strconv.ParseUint(param, 0, 64)
	default:
		r.n, err = strconv.ParseFloat(param, bits())
	}
	return err == nil
}

// Run validates v, a pointer to the input, reporting the first
// failing rule of each field the way the validator does. A non-nil pointer
// counts as present whatever it points to, as it does for the validator.
func Run(v reflect.Value, checks []Check) []FieldError {
	var details []FieldError
	v = v.Elem()
	for i := range checks {
		c := &checks[i]
		fv := v.FieldByIndex(c.index)
		if c.ptr {
			if fv.IsNil() {
				switch {
				case c.required:
					details = append(details, FieldError{c.loc, "is required"})
				case c.omitempty || len(c.rules) == 0:
				default:
					details = append(details, FieldError{c.loc, RuleMessage(c.rules[0].tag, c.rules[0].param, c.kind)})
				}
				continue
			}
			fv = fv.Elem()
		} else if fv.IsZero() {
			if c.required {
				details = append(details, FieldError{c.loc, "is required"})
				continue
			}
			if c.omitempty {
				continue
			}
		}
		for _, r := range c.rules {
			if !r.holds(fv, c.kind) {
				details = append(details, FieldError{c.loc, RuleMessage(r.tag, r.param, c.kind)})
				break
			}
		}
	}
	return details
}

func (r *rule) holds(v reflect.Value, kind reflect.Kind) bool {
	if r.tag == "oneof" {
		return slices.Contains(r.set, formatValue(v, kind))
	}
	var c int
	switch {
	case kind == reflect.String:
		if r.tag == "eq" {
			return v.String() == r.param
		}
		c = cmp.Compare(int64(utf8.RuneCountInString(v.String())), r.i)
	case isInt(kind):
		c = cmp.Compare(v.Int(), r.i)
	case isUint(kind):
		c = cmp.Compare(v.Uint(), r.u)
	default:
		c = cmp.Compare(v.Float(), r.n)
	}
	switch r.tag {
	case "min", "gte":
		return c >= 0
	case "max", "lte":
		return c <= 0
	case "gt":
		return c > 0
	case "lt":
		return c < 0
	}
	return c == 0 // len, eq
}

// formatValue spells v the way the validator does for oneof.
func formatValue(v reflect.Value, kind reflect.Kind) string {
	switch {
	case kind == reflect.String:
		return v.String()
	case isInt(kind):
		return strconv.FormatInt(v.Int(), 10)
	case isUint(kind):
		return strconv.FormatUint(v.Uint(), 10)
	}
	return strconv.FormatFloat(v.Float(), 'f', -1, 64)
}

func isInt(k reflect.Kind) bool {
	return k == reflect.Int || k == reflect.Int8 || k == reflect.Int16 || k == reflect.Int32 || k == reflect.Int64
}

func isUint(k reflect.Kind) bool {
	return k == reflect.Uint || k == reflect.Uint8 || k == reflect.Uint16 || k == reflect.Uint32 || k == reflect.Uint64
}

func isFloat(k reflect.Kind) bool { return k == reflect.Float32 || k == reflect.Float64 }

// HasValidateTags reports whether t, or a type it reaches, has a validate tag.
func HasValidateTags(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Tag.Get("validate") != "" || HasValidateTags(f.Type, seen) {
			return true
		}
	}
	return false
}

// RuleMessage is the message for a failed rule, shared by the validator
// path and the compiled checks so both say the same thing.
func RuleMessage(tag, p string, kind reflect.Kind) string {
	switch tag {
	case "required", "required_if", "required_unless", "required_with", "required_without":
		return "is required"
	case "min", "gte":
		return size(kind, "at least", p)
	case "max", "lte":
		return size(kind, "at most", p)
	case "gt":
		return size(kind, "more than", p)
	case "lt":
		return size(kind, "less than", p)
	case "eq":
		if kind == reflect.String {
			return "must equal " + p
		}
		return size(kind, "exactly", p)
	case "len":
		return size(kind, "exactly", p)
	case "email":
		return "must be a valid email address"
	case "url", "http_url":
		return "must be a valid URL"
	case "uuid", "uuid3", "uuid4", "uuid5":
		return "must be a valid UUID"
	case "oneof":
		return "must be one of: " + strings.Join(strings.Fields(p), ", ")
	}
	return fmt.Sprintf("failed the %q rule", tag)
}

func size(k reflect.Kind, rel, p string) string {
	switch k {
	case reflect.String:
		return fmt.Sprintf("must be %s %s characters long", rel, p)
	case reflect.Slice, reflect.Array, reflect.Map:
		return fmt.Sprintf("must have %s %s items", rel, p)
	}
	return fmt.Sprintf("must be %s %s", rel, p)
}
