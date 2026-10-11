package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/raven-clown/tack/openapi"
)

const maxSpecBytes = 64 << 20

// loadSpec reads the OpenAPI document from a file, a URL, or stdin.
func loadSpec(ctx context.Context, src string, stdin io.Reader) (*openapi.Document, error) {
	if src == "" {
		return nil, fmt.Errorf("%w: -spec is required", errUsage)
	}
	var (
		b   []byte
		err error
	)
	switch {
	case src == "-":
		b, err = io.ReadAll(io.LimitReader(stdin, maxSpecBytes))
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		b, err = fetchSpec(ctx, src)
	default:
		b, err = os.ReadFile(src)
	}
	if err != nil {
		return nil, fmt.Errorf("reading OpenAPI document: %w", err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parsing OpenAPI document %s: %w", src, err)
	}
	if doc.Paths == nil {
		return nil, fmt.Errorf("parsing OpenAPI document %s: no paths; is this an OpenAPI 3 document in JSON?", src)
	}
	return &doc, nil
}

func fetchSpec(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxSpecBytes))
}
