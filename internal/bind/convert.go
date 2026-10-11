package bind

import (
	"encoding"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"
)

type setter func(dst reflect.Value, s string) error

var (
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	timeType            = reflect.TypeFor[time.Time]()
)

func converter(t reflect.Type) (setter, error) {
	if t.Kind() == reflect.Pointer {
		elem, err := converter(t.Elem())
		if err != nil {
			return nil, err
		}
		et := t.Elem()
		return func(dst reflect.Value, s string) error {
			v := reflect.New(et)
			if err := elem(v.Elem(), s); err != nil {
				return err
			}
			dst.Set(v)
			return nil
		}, nil
	}

	if reflect.PointerTo(t).Implements(textUnmarshalerType) {
		msg := "must be a valid " + t.String()
		if t == timeType {
			msg = "must be an RFC 3339 date-time"
		}
		return func(dst reflect.Value, s string) error {
			if err := dst.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(s)); err != nil {
				return errors.New(msg)
			}
			return nil
		}, nil
	}

	switch t.Kind() {
	case reflect.String:
		return func(dst reflect.Value, s string) error {
			dst.SetString(s)
			return nil
		}, nil
	case reflect.Bool:
		return func(dst reflect.Value, s string) error {
			b, err := strconv.ParseBool(s)
			if err != nil {
				return errors.New("must be true or false")
			}
			dst.SetBool(b)
			return nil
		}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		bits := t.Bits()
		return func(dst reflect.Value, s string) error {
			n, err := strconv.ParseInt(s, 10, bits)
			if errors.Is(err, strconv.ErrRange) {
				lim := int64(1) << (bits - 1)
				return fmt.Errorf("must be between %d and %d", -lim, lim-1)
			}
			if err != nil {
				return errors.New("must be an integer")
			}
			dst.SetInt(n)
			return nil
		}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		bits := t.Bits()
		return func(dst reflect.Value, s string) error {
			n, err := strconv.ParseUint(s, 10, bits)
			if errors.Is(err, strconv.ErrRange) {
				return fmt.Errorf("must be at most %d", uint64(math.MaxUint64)>>(64-bits))
			}
			if err != nil {
				return errors.New("must be a non-negative integer")
			}
			dst.SetUint(n)
			return nil
		}, nil
	case reflect.Float32, reflect.Float64:
		bits := t.Bits()
		return func(dst reflect.Value, s string) error {
			f, err := strconv.ParseFloat(s, bits)
			if err != nil {
				return errors.New("must be a number")
			}
			dst.SetFloat(f)
			return nil
		}, nil
	}
	return nil, fmt.Errorf("unsupported type %s", t)
}
