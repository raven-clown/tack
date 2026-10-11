package tack

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/raven-clown/tack/internal/bind"
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

const maxErrors = 50

func (a *App) validateInput(in any, prefix string, locs map[string]string) []ErrorDetail {
	err := a.validate.Struct(in)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return []ErrorDetail{{Location: "request", Message: err.Error()}}
	}
	details := make([]ErrorDetail, 0, min(len(verrs), maxErrors+1))
	for i, fe := range verrs {
		if i == maxErrors {
			details = append(details, ErrorDetail{Location: "request", Message: fmt.Sprintf("and %d more errors", len(verrs)-maxErrors)})
			break
		}
		details = append(details, ErrorDetail{Location: location(fe, prefix, locs), Message: message(fe)})
	}
	return details
}

// location maps a validator namespace such as "Input.Body[0].name" to
// "body[0].name". prefix is the input type's name plus a dot, or empty for
// anonymous structs.
func location(fe validator.FieldError, prefix string, locs map[string]string) string {
	goSegs := strings.Split(strings.TrimPrefix(fe.StructNamespace(), prefix), ".")
	segs := strings.Split(strings.TrimPrefix(fe.Namespace(), prefix), ".")
	for n := len(goSegs); n > 0; n-- {
		last := goSegs[n-1]
		name, suffix := last, ""
		if i := strings.IndexByte(last, '['); i >= 0 {
			name, suffix = last[:i], last[i:]
		}
		key := strings.Join(append(append([]string(nil), goSegs[:n-1]...), name), ".")
		if loc, ok := locs[key]; ok {
			rest := segs[min(n, len(segs)):]
			if len(rest) == 0 {
				return loc + suffix
			}
			return loc + suffix + "." + strings.Join(rest, ".")
		}
	}
	return strings.Join(segs, ".")
}

// checkDive rejects slice, array, and map fields whose elements have validate
// rules that validator would silently skip without dive.
func checkDive(t reflect.Type, path string, seen map[reflect.Type]bool) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return nil
	}
	seen[t] = true
	for i := range t.NumField() {
		f := t.Field(i)
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		name := path + f.Name
		switch ft.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map:
			if ft.Elem().Kind() == reflect.Uint8 {
				continue
			}
			if !strings.Contains(","+f.Tag.Get("validate")+",", ",dive,") && bind.HasValidateTags(ft.Elem(), map[reflect.Type]bool{}) {
				return fmt.Errorf("field %s: elements of %s have validate rules that are skipped without dive; add validate:\"dive\" to %s", name, ft, f.Name)
			}
			if err := checkDive(ft.Elem(), name+"[].", seen); err != nil {
				return err
			}
		case reflect.Struct:
			if err := checkDive(ft, name+".", seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func message(fe validator.FieldError) string {
	return bind.RuleMessage(fe.Tag(), fe.Param(), fe.Kind())
}
