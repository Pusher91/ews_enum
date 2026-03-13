package ews

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func cookieBoundFlowClient(useCookies bool) *http.Client {
	base := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			user, pass, ok := req.BasicAuth()
			if !ok || user != "alice" || pass != "secret" {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("unauthorized")),
					Request:    req,
				}, nil
			}

			if req.URL.Path == "/challenge" {
				cookie, err := req.Cookie("ews_session")
				if err != nil || cookie.Value != "1" {
					return &http.Response{
						StatusCode: http.StatusUnauthorized,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("missing cookie")),
						Request:    req,
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(soapResponse("Warning", "ErrorNameResolutionNoResults"))),
					Request:    req,
				}, nil
			}

			header := make(http.Header)
			header.Set("Location", "https://mail.example.com/challenge")
			header.Add("Set-Cookie", "ews_session=1; Path=/")
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("redirect")),
				Request:    req,
			}, nil
		}),
		Timeout: 5 * time.Second,
	}

	return CloneClientWithFreshJar(base, useCookies)
}

func TestCloneClientWithFreshJarSupportsCookieBoundAuthFlow(t *testing.T) {
	t.Parallel()

	result, err := TestAuth(
		cookieBoundFlowClient(true),
		"https://mail.example.com/EWS/Exchange.asmx",
		"alice",
		"secret",
	)
	if err != nil {
		t.Fatalf("TestAuth returned error: %v", err)
	}
	if result != AuthSuccess {
		t.Fatalf("result = %v, want %v", result, AuthSuccess)
	}
}

func TestAuthFailsCookieBoundFlowWithoutJar(t *testing.T) {
	t.Parallel()

	result, err := TestAuth(
		cookieBoundFlowClient(false),
		"https://mail.example.com/EWS/Exchange.asmx",
		"alice",
		"secret",
	)
	if err != nil {
		t.Fatalf("TestAuth returned error: %v", err)
	}
	if result != AuthFailed {
		t.Fatalf("result = %v, want %v", result, AuthFailed)
	}
}

func TestNewIsolatedAuthClientReturnsFreshStatePerAttempt(t *testing.T) {
	t.Parallel()

	first := NewIsolatedAuthClient(false, 5)
	second := NewIsolatedAuthClient(false, 5)

	if first == second {
		t.Fatal("expected distinct clients per attempt")
	}
	if first.Transport == second.Transport {
		t.Fatal("expected distinct transports per attempt")
	}
	if first.Jar == nil || second.Jar == nil {
		t.Fatal("expected cookie jars on isolated auth clients")
	}
	if first.Jar == second.Jar {
		t.Fatal("expected distinct cookie jars per attempt")
	}
}
