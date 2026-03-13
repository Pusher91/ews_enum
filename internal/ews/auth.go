package ews

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// AuthResult represents the outcome of an authentication attempt.
type AuthResult int

const (
	AuthSuccess AuthResult = iota
	AuthFailed
	AuthError
)

// TestAuth attempts to authenticate against EWS with the given credentials.
// Returns AuthSuccess if the credentials are valid, AuthFailed if rejected,
// or AuthError for network/server issues.
func TestAuth(client *http.Client, url, user, pass string) (AuthResult, error) {
	body := fmt.Sprintf(soapEnvelopeTemplate, xmlEscape("a"))

	req, err := http.NewRequest("POST", url, bytes.NewBufferString(body))
	if err != nil {
		return AuthError, &ResponseError{
			Op:     "TestAuth",
			Kind:   ErrorKindTransport,
			Detail: "creating request",
			Cause:  err,
		}
	}

	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return AuthError, &ResponseError{
			Op:     "TestAuth",
			Kind:   ErrorKindTransport,
			Detail: "request failed",
			Cause:  err,
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return AuthError, &ResponseError{
			Op:     "TestAuth",
			Kind:   ErrorKindTransport,
			Detail: "reading response",
			Cause:  err,
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return AuthFailed, nil
	}

	msg, recognized, parseErr := parseResolveNamesMessage(respBody)
	if recognized {
		switch msg.ResponseCode {
		case "ErrorNameResolutionNoResults":
			return AuthSuccess, nil
		case "ErrorAccessDenied", "ErrorNonExistentMailbox", "ErrorMailboxMoveInProgress":
			// Credentials are valid, but this account cannot enumerate via ResolveNames.
			return AuthSuccess, nil
		case "ErrorAccountDisabled", "ErrorLogonFailure":
			return AuthFailed, nil
		}

		if msg.ResponseClass == "Error" {
			return AuthError, &ResponseError{
				Op:   "TestAuth",
				Kind: classifyResponseCode(msg.ResponseCode),
				Code: msg.ResponseCode,
			}
		}

		return AuthSuccess, nil
	}

	bodyStr := string(respBody)
	if resp.StatusCode != http.StatusOK {
		return AuthError, &ResponseError{
			Op:         "TestAuth",
			Kind:       ErrorKindServer,
			StatusCode: resp.StatusCode,
			Detail:     truncate(bodyStr, 200),
		}
	}

	if parseErr != nil {
		return AuthError, &ResponseError{
			Op:     "TestAuth",
			Kind:   ErrorKindParse,
			Detail: "parsing EWS response",
			Cause:  parseErr,
		}
	}

	return AuthError, &ResponseError{
		Op:     "TestAuth",
		Kind:   ErrorKindParse,
		Detail: fmt.Sprintf("unexpected non-EWS response: %s", truncate(bodyStr, 200)),
	}
}
