package tack

import (
	"fmt"

	"github.com/raven-clown/tack/openapi"
)

// Bearer returns an HTTP bearer token scheme for Config.Security. format
// names the token format shown in the docs, such as "JWT", and may be empty.
func Bearer(format string) openapi.SecurityScheme {
	return openapi.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: format}
}

// Basic returns an HTTP basic authentication scheme for Config.Security.
func Basic() openapi.SecurityScheme {
	return openapi.SecurityScheme{Type: "http", Scheme: "basic"}
}

// APIKey returns an API key scheme for Config.Security. in is "header",
// "query", or "cookie", and name is the header, parameter, or cookie name.
func APIKey(in, name string) openapi.SecurityScheme {
	return openapi.SecurityScheme{Type: "apiKey", In: in, Name: name}
}

func checkScheme(name string, s openapi.SecurityScheme) error {
	if name == "" {
		return fmt.Errorf("scheme name must not be empty")
	}
	switch s.Type {
	case "http":
		if s.Scheme == "" {
			return fmt.Errorf("scheme %q: http schemes need Scheme, such as \"bearer\" or \"basic\"", name)
		}
	case "apiKey":
		if s.In != "header" && s.In != "query" && s.In != "cookie" {
			return fmt.Errorf("scheme %q: In must be \"header\", \"query\", or \"cookie\", not %q", name, s.In)
		}
		if s.Name == "" {
			return fmt.Errorf("scheme %q: apiKey schemes need Name", name)
		}
	default:
		return fmt.Errorf("scheme %q: type %q is not supported; use Bearer, Basic, or APIKey", name, s.Type)
	}
	return nil
}
