package ews

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testClient(status int, body string) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
}

func soapResponse(class, code string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <ResolveNamesResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessages>
        <ResolveNamesResponseMessage ResponseClass="%s">
          <ResponseCode>%s</ResponseCode>
          <ResolutionSet TotalItemsInView="0" IncludesLastItemInRange="true"></ResolutionSet>
        </ResolveNamesResponseMessage>
      </ResponseMessages>
    </ResolveNamesResponse>
  </soap:Body>
</soap:Envelope>`, class, code)
}

func TestAuthClassifiesResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		body        string
		wantResult  AuthResult
		wantErrText string
	}{
		{
			name:       "401 unauthorized",
			status:     http.StatusUnauthorized,
			body:       "unauthorized",
			wantResult: AuthFailed,
		},
		{
			name:       "no results still authenticates",
			status:     http.StatusOK,
			body:       soapResponse("Warning", "ErrorNameResolutionNoResults"),
			wantResult: AuthSuccess,
		},
		{
			name:       "access denied means creds valid",
			status:     http.StatusOK,
			body:       soapResponse("Error", "ErrorAccessDenied"),
			wantResult: AuthSuccess,
		},
		{
			name:       "non existent mailbox means creds valid",
			status:     http.StatusInternalServerError,
			body:       soapResponse("Error", "ErrorNonExistentMailbox"),
			wantResult: AuthSuccess,
		},
		{
			name:        "unexpected ews error",
			status:      http.StatusOK,
			body:        soapResponse("Error", "ErrorServerBusy"),
			wantResult:  AuthError,
			wantErrText: "ErrorServerBusy",
		},
		{
			name:        "malformed xml is not success",
			status:      http.StatusOK,
			body:        "<soap:Envelope>",
			wantResult:  AuthError,
			wantErrText: "parsing EWS response",
		},
		{
			name:        "non ews response is not success",
			status:      http.StatusOK,
			body:        "<html><body>login</body></html>",
			wantResult:  AuthError,
			wantErrText: "unexpected non-EWS response",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := TestAuth(testClient(tc.status, tc.body), "https://mail.example.com/EWS/Exchange.asmx", "user", "pass")
			if result != tc.wantResult {
				t.Fatalf("result = %v, want %v", result, tc.wantResult)
			}

			if tc.wantErrText == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErrText)
			}
			if !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErrText)
			}
		})
	}
}

func TestResolveNamesSurfacesSOAPErrorCode(t *testing.T) {
	t.Parallel()

	_, _, err := ResolveNames(
		testClient(http.StatusInternalServerError, soapResponse("Error", "ErrorAccessDenied")),
		"https://mail.example.com/EWS/Exchange.asmx",
		"user",
		"pass",
		"a",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ErrorAccessDenied") {
		t.Fatalf("error = %q, want AccessDenied code", err.Error())
	}
}
