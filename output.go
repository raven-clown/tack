package tack

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

// output is the registration-time plan for a handler's Out type. A struct
// with a field named Body is split into body, Status, and header fields;
// any other type is the body itself.
type output struct {
	typ     reflect.Type
	body    []int
	status  []int
	headers []headerField
}

type headerField struct {
	field    reflect.StructField
	name     string
	index    []int
	format   func(reflect.Value) ([]string, error)
	required bool
}

var (
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
	timeType          = reflect.TypeFor[time.Time]()
)

func newOutput(t reflect.Type) (*output, error) {
	o := &output{typ: t}
	if t.Kind() != reflect.Struct {
		return o, nil
	}
	bodyField, wrapped := t.FieldByName("Body")
	if wrapped && (!bodyField.IsExported() || len(bodyField.Index) != 1) {
		wrapped = false
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		header, hasHeader := f.Tag.Lookup("header")
		_, hasJSON := f.Tag.Lookup("json")
		isStatus := f.Name == "Status" && !hasJSON && f.Type.Kind() == reflect.Int
		if !wrapped {
			if hasHeader || isStatus {
				return nil, fmt.Errorf("output type %s has field %s but no Body; add a Body field for the response body, or tag %s with json:%q to send it as JSON", t, f.Name, f.Name, f.Name)
			}
			continue
		}
		switch {
		case f.Name == "Body":
			o.body = f.Index
			o.typ = f.Type
		case f.Name == "Status":
			if f.Type.Kind() != reflect.Int {
				return nil, fmt.Errorf("output type %s: Status must be an int, not %s", t, f.Type)
			}
			o.status = f.Index
		case hasHeader:
			if header == "" {
				return nil, fmt.Errorf("output type %s: field %s: header tag needs a name, e.g. header:\"Location\"", t, f.Name)
			}
			format, required, err := formatter(f.Type)
			if err != nil {
				return nil, fmt.Errorf("output type %s: field %s: %w; use a string, bool, number, time.Time, encoding.TextMarshaler, or a slice or pointer of one", t, f.Name, err)
			}
			o.headers = append(o.headers, headerField{field: f, name: header, index: f.Index, format: format, required: required})
		default:
			return nil, fmt.Errorf("output type %s: field %s has no header tag; tag it header:\"X-%s\", or name it Body or Status", t, f.Name, f.Name)
		}
	}
	return o, nil
}

// wrapped reports whether Out has a Body field.
func (o *output) wrapped() bool { return o.body != nil }

// split returns the status, header values, and body of res.
func (o *output) split(res any, status int) (int, [][]string, any, error) {
	v := reflect.ValueOf(res)
	if o.status != nil {
		if s := int(v.FieldByIndex(o.status).Int()); s != 0 {
			status = s
		}
	}
	var headers [][]string
	if len(o.headers) > 0 {
		headers = make([][]string, len(o.headers))
		for i := range o.headers {
			values, err := o.headers[i].format(v.FieldByIndex(o.headers[i].index))
			if err != nil {
				return 0, nil, nil, fmt.Errorf("formatting header %s: %w", o.headers[i].name, err)
			}
			headers[i] = values
		}
	}
	return status, headers, v.FieldByIndex(o.body).Interface(), nil
}

// formatter returns a function that turns a header field value into header
// lines, and whether the header is always present. Nil pointers, empty slices,
// and empty strings produce no header.
func formatter(t reflect.Type) (format func(reflect.Value) ([]string, error), required bool, err error) {
	one := func(f func(reflect.Value) string) func(reflect.Value) ([]string, error) {
		return func(v reflect.Value) ([]string, error) { return []string{f(v)}, nil }
	}
	switch {
	case t.Kind() == reflect.Interface:
		return nil, false, fmt.Errorf("unsupported header type %s", t)
	case t.Kind() == reflect.Pointer:
		elem, _, err := formatter(t.Elem())
		if err != nil {
			return nil, false, err
		}
		return func(v reflect.Value) ([]string, error) {
			if v.IsNil() {
				return nil, nil
			}
			return elem(v.Elem())
		}, false, nil
	case t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8:
		elem, _, err := formatter(t.Elem())
		if err != nil {
			return nil, false, err
		}
		return func(v reflect.Value) ([]string, error) {
			var out []string
			for i := range v.Len() {
				values, err := elem(v.Index(i))
				if err != nil {
					return nil, err
				}
				out = append(out, values...)
			}
			return out, nil
		}, false, nil
	case t == timeType:
		return one(func(v reflect.Value) string { return v.Interface().(time.Time).Format(time.RFC3339) }), true, nil
	case t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType):
		return func(v reflect.Value) ([]string, error) {
			if !v.CanAddr() {
				c := reflect.New(t).Elem()
				c.Set(v)
				v = c
			}
			b, err := v.Addr().Interface().(encoding.TextMarshaler).MarshalText()
			if err != nil {
				return nil, err
			}
			return []string{string(b)}, nil
		}, true, nil
	}
	switch t.Kind() {
	case reflect.String:
		return func(v reflect.Value) ([]string, error) {
			if s := v.String(); s != "" {
				return []string{s}, nil
			}
			return nil, nil
		}, false, nil
	case reflect.Bool:
		return one(func(v reflect.Value) string { return strconv.FormatBool(v.Bool()) }), true, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return one(func(v reflect.Value) string { return strconv.FormatInt(v.Int(), 10) }), true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return one(func(v reflect.Value) string { return strconv.FormatUint(v.Uint(), 10) }), true, nil
	case reflect.Float32, reflect.Float64:
		bits := t.Bits()
		return one(func(v reflect.Value) string { return strconv.FormatFloat(v.Float(), 'g', -1, bits) }), true, nil
	}
	return nil, false, fmt.Errorf("unsupported header type %s", t)
}
