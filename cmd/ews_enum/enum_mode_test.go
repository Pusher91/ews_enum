package main

import (
	"bytes"
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

func enumDynamicClient(fn func(*http.Request) (*http.Response, error)) *http.Client {
	return &http.Client{Transport: enumRoundTripFunc(fn)}
}

func enumNoResultsSOAP() string {
	return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <ResolveNamesResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessages>
        <ResolveNamesResponseMessage ResponseClass="Warning">
          <ResponseCode>ErrorNameResolutionNoResults</ResponseCode>
          <ResolutionSet TotalItemsInView="0" IncludesLastItemInRange="true"></ResolutionSet>
        </ResolveNamesResponseMessage>
      </ResponseMessages>
    </ResolveNamesResponse>
  </soap:Body>
</soap:Envelope>`
}

func enumErrorSOAP(code string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <ResolveNamesResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessages>
        <ResolveNamesResponseMessage ResponseClass="Error">
          <ResponseCode>` + code + `</ResponseCode>
          <ResolutionSet TotalItemsInView="0" IncludesLastItemInRange="true"></ResolutionSet>
        </ResolveNamesResponseMessage>
      </ResponseMessages>
    </ResolveNamesResponse>
  </soap:Body>
</soap:Envelope>`
}

func enumSingleContactSOAP(displayName, emailAddress, title string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <ResolveNamesResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessages>
        <ResolveNamesResponseMessage ResponseClass="Success">
          <ResponseCode>NoError</ResponseCode>
          <ResolutionSet TotalItemsInView="1" IncludesLastItemInRange="true">
            <Resolution>
              <Mailbox>
                <Name>` + displayName + `</Name>
                <EmailAddress>` + emailAddress + `</EmailAddress>
              </Mailbox>
              <Contact>
                <DisplayName>` + displayName + `</DisplayName>
                <JobTitle>` + title + `</JobTitle>
              </Contact>
            </Resolution>
          </ResolutionSet>
        </ResolveNamesResponseMessage>
      </ResponseMessages>
    </ResolveNamesResponse>
  </soap:Body>
</soap:Envelope>`
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

func TestEnumerateGALTreatsAuthLookingErrorsAfterSuccessAsGenericFailure(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:     "https://mail.example.com/EWS/Exchange.asmx",
		User:    "alice",
		Pass:    "secret",
		Depth:   1,
		Workers: 1,
	}

	_, err := enumerateGAL(
		enumDynamicClient(func(req *http.Request) (*http.Response, error) {
			bodyBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			body := string(bodyBytes)

			respBody := enumErrorSOAP("ErrorLogonFailure")
			if strings.Contains(body, "<m:UnresolvedEntry>a</m:UnresolvedEntry>") {
				respBody = enumNoResultsSOAP()
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(respBody)),
				Request:    req,
			}, nil
		}),
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
		t.Fatalf("error kind = %v, want generic server failure after partial success", enumErr.Kind)
	}
}

func TestEnumerateGALKeepsDistinctContactsWithoutEmail(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:     "https://mail.example.com/EWS/Exchange.asmx",
		User:    "alice",
		Pass:    "secret",
		Depth:   1,
		Workers: 1,
	}

	result, err := enumerateGAL(
		enumDynamicClient(func(req *http.Request) (*http.Response, error) {
			bodyBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			body := string(bodyBytes)

			respBody := enumNoResultsSOAP()
			switch {
			case strings.Contains(body, "<m:UnresolvedEntry>a</m:UnresolvedEntry>"):
				respBody = enumSingleContactSOAP("Shared Room", "", "East Wing")
			case strings.Contains(body, "<m:UnresolvedEntry>b</m:UnresolvedEntry>"):
				respBody = enumSingleContactSOAP("Shared Room", "", "West Wing")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(respBody)),
				Request:    req,
			}, nil
		}),
		cfg,
		enumReporter{},
	)
	if err != nil {
		t.Fatalf("enumerateGAL returned error: %v", err)
	}
	if len(result.Contacts) != 2 {
		t.Fatalf("contacts = %d, want 2", len(result.Contacts))
	}
}

func TestEnumerateGALFailsWhenLaterRequestsError(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:     "https://mail.example.com/EWS/Exchange.asmx",
		User:    "alice",
		Pass:    "secret",
		Depth:   1,
		Workers: 1,
	}

	_, err := enumerateGAL(
		enumDynamicClient(func(req *http.Request) (*http.Response, error) {
			bodyBytes, readErr := io.ReadAll(req.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			body := string(bodyBytes)

			status := http.StatusServiceUnavailable
			respBody := "<html><body>proxy error</body></html>"
			if strings.Contains(body, "<m:UnresolvedEntry>a</m:UnresolvedEntry>") {
				status = http.StatusOK
				respBody = enumNoResultsSOAP()
			}

			return &http.Response{
				StatusCode: status,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(respBody)),
				Request:    req,
			}, nil
		}),
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

func TestRunEnumWritesPartialResultsOnLateError(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:     "https://mail.example.com/EWS/Exchange.asmx",
		User:    "alice",
		Pass:    "secret",
		Depth:   1,
		Workers: 1,
		Format:  "emails",
	}

	client := enumDynamicClient(func(req *http.Request) (*http.Response, error) {
		bodyBytes, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			t.Fatalf("read request body: %v", readErr)
		}
		body := string(bodyBytes)

		status := http.StatusServiceUnavailable
		respBody := "<html><body>proxy error</body></html>"
		if strings.Contains(body, "<m:UnresolvedEntry>a</m:UnresolvedEntry>") {
			status = http.StatusOK
			respBody = enumSingleContactSOAP("Alice Smith", "alice@example.com", "Engineer")
		}

		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(respBody)),
			Request:    req,
		}, nil
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runEnumWithClient(cfg, &stdout, &stderr, client)
	if code != 2 {
		t.Fatalf("runEnumWithClient returned %d, want 2", code)
	}
	if !strings.Contains(stdout.String(), "alice@example.com") {
		t.Fatalf("stdout = %q, want partial result output", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Writing 1 partial results") {
		t.Fatalf("stderr = %q, want partial-results warning", stderr.String())
	}
}
