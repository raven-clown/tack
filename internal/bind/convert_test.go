package bind

import (
	"net/netip"
	"reflect"
	"testing"
	"time"
)

func TestConverter(t *testing.T) {
	ts := time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name    string
		typ     reflect.Type
		in      string
		want    any
		wantErr string
	}{
		{"string", reflect.TypeFor[string](), "milo", "milo", ""},
		{"bool", reflect.TypeFor[bool](), "true", true, ""},
		{"bool invalid", reflect.TypeFor[bool](), "yes", nil, "must be true or false"},
		{"int", reflect.TypeFor[int](), "-42", -42, ""},
		{"int invalid", reflect.TypeFor[int](), "4x", nil, "must be an integer"},
		{"int8 range", reflect.TypeFor[int8](), "200", nil, "must be between -128 and 127"},
		{"int16", reflect.TypeFor[int16](), "300", int16(300), ""},
		{"int32", reflect.TypeFor[int32](), "7", int32(7), ""},
		{"int64", reflect.TypeFor[int64](), "9000000000", int64(9000000000), ""},
		{"uint", reflect.TypeFor[uint](), "5", uint(5), ""},
		{"uint negative", reflect.TypeFor[uint](), "-1", nil, "must be a non-negative integer"},
		{"uint8 range", reflect.TypeFor[uint8](), "256", nil, "must be at most 255"},
		{"uint16", reflect.TypeFor[uint16](), "65535", uint16(65535), ""},
		{"uint32", reflect.TypeFor[uint32](), "1", uint32(1), ""},
		{"uint64", reflect.TypeFor[uint64](), "18446744073709551615", uint64(18446744073709551615), ""},
		{"float32", reflect.TypeFor[float32](), "1.5", float32(1.5), ""},
		{"float64", reflect.TypeFor[float64](), "2.25", 2.25, ""},
		{"float invalid", reflect.TypeFor[float64](), "abc", nil, "must be a number"},
		{"time", reflect.TypeFor[time.Time](), "2026-10-11T09:30:00Z", ts, ""},
		{"time invalid", reflect.TypeFor[time.Time](), "yesterday", nil, "must be an RFC 3339 date-time"},
		{"text unmarshaler", reflect.TypeFor[netip.Addr](), "10.0.0.1", netip.MustParseAddr("10.0.0.1"), ""},
		{"text unmarshaler invalid", reflect.TypeFor[netip.Addr](), "nope", nil, "must be a valid netip.Addr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := converter(tt.typ)
			if err != nil {
				t.Fatal(err)
			}
			v := reflect.New(tt.typ).Elem()
			err = set(v, tt.in)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := v.Interface(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConverterPointer(t *testing.T) {
	set, err := converter(reflect.TypeFor[*int]())
	if err != nil {
		t.Fatal(err)
	}
	v := reflect.New(reflect.TypeFor[*int]()).Elem()
	if err := set(v, "3"); err != nil {
		t.Fatal(err)
	}
	if got := *v.Interface().(*int); got != 3 {
		t.Fatalf("got %d, want 3", got)
	}
	if err := set(v, "x"); err == nil {
		t.Fatal("want error for invalid pointer value")
	}
}

func TestConverterUnsupported(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[map[string]int](), reflect.TypeFor[chan int](), reflect.TypeFor[*struct{}]()} {
		if _, err := converter(typ); err == nil {
			t.Errorf("%s: want error", typ)
		}
	}
}
