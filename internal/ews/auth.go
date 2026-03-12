package ews

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	body := fmt.Sprintf(soapEnvelopeTemplate, "a")

	req, err := http.NewRequest("POST", url, bytes.NewBufferString(body))
	if err != nil {
		return AuthError, fmt.Errorf("creating request: %w", err)
	}

	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return AuthError, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return AuthError, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return AuthFailed, nil
	}

	if resp.StatusCode != http.StatusOK {
		bodyStr := string(respBody)
		if strings.Contains(bodyStr, "LogonDenied") || strings.Contains(bodyStr, "AccessDenied") {
			return AuthFailed, nil
		}
		return AuthError, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(bodyStr, 200))
	}

	return AuthSuccess, nil
}
