package tack

import (
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/raven-clown/tack/internal/bind"
)

// A check is the compiled validate tag of one scalar parameter. Routes whose
// every validate tag compiles run checks instead of the validator, which
// costs a reflective walk per request; anything else (bodies with rules,
// slices, rules the compiler does not know) keeps the validator.
type check struct {
	index     []int
	loc       string
	kind      reflect.Kind // the scalar kind, behind a pointer when ptr is set
	ptr       bool
	required  bool
	omitempty bool
	rules     []rule
}

type rule struct {
	tag   string
	param string
	n     float64  // the numeric parameter, for min, max, and friends
	set   []string // the oneof options
}

// compileChecks returns the checks for t's parameters, or false when the
// input needs the validator: a body with rules, a slice, a struct, an
// unknown rule, or a parameter that does not parse.
func compileChecks(plan *bind.Plan) ([]check, bool) {
	if f, _, ok := plan.Body(); ok && hasValidateTags(f.Type, map[reflect.Type]bool{}) {
		return nil, false
	}
	var checks []check
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

func compileCheck(p bind.Param, tag string) (check, bool) {
	c := check{index: p.Index, loc: p.Location}
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
		case "required":
			c.required = true
			continue
		case "omitempty":
			c.omitempty = true
			continue
		}
		if !scalar {
			return c, false
		}
		rl := rule{tag: name, param: param}
		switch name {
		case "min", "max", "gte", "lte", "gt", "lt", "len":
			n, err := strconv.ParseFloat(param, 64)
			if err != nil || (c.kind == reflect.Bool) {
				return c, false
			}
			if c.kind == reflect.String && (name == "gt" || name == "lt") {
				return c, false // validator compares lengths with the same inclusive semantics here; keep it simple
			}
			rl.n = n
		case "eq":
			if c.kind != reflect.String {
				n, err := strconv.ParseFloat(param, 64)
				if err != nil {
					return c, false
				}
				rl.n = n
			}
		case "oneof":
			if strings.ContainsAny(param, "'\"") || c.kind == reflect.Bool {
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

// runChecks validates v, a pointer to the input, reporting the first
// failing rule of each field the way the validator does.
func runChecks(v reflect.Value, checks []check) []ErrorDetail {
	var details []ErrorDetail
	v = v.Elem()
	for i := range checks {
		c := &checks[i]
		fv := v.FieldByIndex(c.index)
		if c.ptr {
			if fv.IsNil() {
				switch {
				case c.required:
					details = append(details, ErrorDetail{Location: c.loc, Message: "is required"})
				case c.omitempty || len(c.rules) == 0:
				default:
					details = append(details, ErrorDetail{Location: c.loc, Message: ruleMessage(c.rules[0].tag, c.rules[0].param, c.kind)})
				}
				continue
			}
			fv = fv.Elem()
		}
		if c.required && fv.IsZero() {
			details = append(details, ErrorDetail{Location: c.loc, Message: "is required"})
			continue
		}
		if c.omitempty && fv.IsZero() {
			continue
		}
		for _, r := range c.rules {
			if !r.holds(fv, c.kind) {
				details = append(details, ErrorDetail{Location: c.loc, Message: ruleMessage(r.tag, r.param, c.kind)})
				break
			}
		}
	}
	return details
}

func (r *rule) holds(v reflect.Value, kind reflect.Kind) bool {
	var n float64
	switch {
	case kind == reflect.String:
		s := v.String()
		switch r.tag {
		case "eq":
			return s == r.param
		case "oneof":
			for _, o := range r.set {
				if s == o {
					return true
				}
			}
			return false
		}
		n = float64(utf8.RuneCountInString(s))
	case isInt(kind):
		n = float64(v.Int())
	case isUint(kind):
		n = float64(v.Uint())
	default:
		n = v.Float()
	}
	switch r.tag {
	case "min", "gte":
		return n >= r.n
	case "max", "lte":
		return n <= r.n
	case "gt":
		return n > r.n
	case "lt":
		return n < r.n
	case "len", "eq":
		return n == r.n
	case "oneof":
		for _, o := range r.set {
			if f, err := strconv.ParseFloat(o, 64); err == nil && f == n {
				return true
			}
		}
	}
	return false
}

func isInt(k reflect.Kind) bool {
	return k == reflect.Int || k == reflect.Int8 || k == reflect.Int16 || k == reflect.Int32 || k == reflect.Int64
}

func isUint(k reflect.Kind) bool {
	return k == reflect.Uint || k == reflect.Uint8 || k == reflect.Uint16 || k == reflect.Uint32 || k == reflect.Uint64
}

func isFloat(k reflect.Kind) bool { return k == reflect.Float32 || k == reflect.Float64 }
