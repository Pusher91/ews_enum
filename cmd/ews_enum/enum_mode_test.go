package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"ews_enum/internal/ews"
)

type enumRoundTripFunc func(*http.Request) (*http.Response, error)

func (f enumRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func enumTestClient(status int, body string) *http.Client {
	return &http.Client{
		Transport: enumRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
}

func TestEnumerateGALFailsClosedOnUniversalServerErrors(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:     "https://mail.example.com/EWS/Exchange.asmx",
		User:    "alice",
		Pass:    "secret",
		Depth:   1,
		Workers: 3,
	}

	_, err := enumerateGAL(
		enumTestClient(http.StatusServiceUnavailable, "<html><body>proxy error</body></html>"),
		cfg,
		enumReporter{},
	)
	if err == nil {
		t.Fatal("expected enumeration failure")
	}

	enumErr, ok := err.(*enumFailure)
	if !ok {
		t.Fatalf("error type = %T, want *enumFailure", err)
	}
	if enumErr.Kind != ews.ErrorKindServer {
		t.Fatalf("error kind = %v, want %v", enumErr.Kind, ews.ErrorKindServer)
	}
}
