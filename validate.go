package tack

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

func newValidate() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return f.Name
		}
		return name
	})
	return v
}

func hasValidateTags(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Tag.Get("validate") != "" || hasValidateTags(f.Type, seen) {
			return true
		}
	}
	return false
}

func (a *App) validateInput(in any, locs map[string]string) []ErrorDetail {
	err := a.validate.Struct(in)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return []ErrorDetail{{Location: "request", Message: err.Error()}}
	}
	details := make([]ErrorDetail, 0, len(verrs))
	for _, fe := range verrs {
		details = append(details, ErrorDetail{Location: location(fe, locs), Message: message(fe)})
	}
	return details
}

// location maps a validator namespace such as "Input.Body[0].name" to "body[0].name".
func location(fe validator.FieldError, locs map[string]string) string {
	goSegs := strings.Split(fe.StructNamespace(), ".")[1:]
	segs := strings.Split(fe.Namespace(), ".")[1:]
	for n := len(goSegs); n > 0; n-- {
		last := goSegs[n-1]
		name, suffix := last, ""
		if i := strings.IndexByte(last, '['); i >= 0 {
			name, suffix = last[:i], last[i:]
		}
		key := strings.Join(append(append([]string(nil), goSegs[:n-1]...), name), ".")
		if loc, ok := locs[key]; ok {
			rest := segs[n:]
			if len(rest) == 0 {
				return loc + suffix
			}
			return loc + suffix + "." + strings.Join(rest, ".")
		}
	}
	return strings.Join(segs, ".")
}

func message(fe validator.FieldError) string {
	p := fe.Param()
	switch fe.Tag() {
	case "required", "required_if", "required_unless", "required_with", "required_without":
		return "is required"
	case "min", "gte":
		return size(fe.Kind(), "at least", p)
	case "max", "lte":
		return size(fe.Kind(), "at most", p)
	case "gt":
		return size(fe.Kind(), "more than", p)
	case "lt":
		return size(fe.Kind(), "less than", p)
	case "len", "eq":
		return size(fe.Kind(), "exactly", p)
	case "email":
		return "must be a valid email address"
	case "url", "http_url":
		return "must be a valid URL"
	case "uuid", "uuid3", "uuid4", "uuid5":
		return "must be a valid UUID"
	case "oneof":
		return "must be one of: " + strings.Join(strings.Fields(p), ", ")
	}
	return fmt.Sprintf("failed the %q rule", fe.Tag())
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
