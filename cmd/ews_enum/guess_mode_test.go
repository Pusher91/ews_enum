package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"ews_enum/internal/ews"
)

func writeCredFile(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "creds.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write credfile: %v", err)
	}
	return path
}

func TestReadCredentialAttemptsKeepsFirstUsernameAndTracksDuplicates(t *testing.T) {
	t.Parallel()

	attempts, duplicates, err := readCredentialAttempts(writeCredFile(
		t,
		"# comment",
		"alice:one",
		"Alice:two",
		"bob:three",
		"alice:four",
	))
	if err != nil {
		t.Fatalf("readCredentialAttempts returned error: %v", err)
	}

	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(attempts))
	}
	if attempts[0].User != "alice" || attempts[0].Password != "one" {
		t.Fatalf("first attempt = %+v, want alice:one", attempts[0])
	}
	if attempts[1].User != "bob" || attempts[1].Password != "three" {
		t.Fatalf("second attempt = %+v, want bob:three", attempts[1])
	}

	if len(duplicates) != 2 {
		t.Fatalf("duplicates = %d, want 2", len(duplicates))
	}
	if duplicates[0].Duplicate.Password != "two" || duplicates[0].Canonical.Password != "one" {
		t.Fatalf("first duplicate = %+v, want Alice:two mapped to alice:one", duplicates[0])
	}
	if duplicates[1].Duplicate.Password != "four" || duplicates[1].Canonical.Password != "one" {
		t.Fatalf("second duplicate = %+v, want alice:four mapped to alice:one", duplicates[1])
	}
}

func TestReadCredentialAttemptsPreservesRawPasswordAfterFirstColon(t *testing.T) {
	t.Parallel()

	attempts, duplicates, err := readCredentialAttempts(writeCredFile(
		t,
		"  alice  : pa:ss word  ",
	))
	if err != nil {
		t.Fatalf("readCredentialAttempts returned error: %v", err)
	}
	if len(duplicates) != 0 {
		t.Fatalf("duplicates = %d, want 0", len(duplicates))
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0].User != "alice" {
		t.Fatalf("user = %q, want %q", attempts[0].User, "alice")
	}
	if attempts[0].Password != " pa:ss word  " {
		t.Fatalf("password = %q, want raw password preserved", attempts[0].Password)
	}
}

func TestReadCredentialAttemptsAllowsEmptyPassword(t *testing.T) {
	t.Parallel()

	attempts, duplicates, err := readCredentialAttempts(writeCredFile(
		t,
		"alice:",
	))
	if err != nil {
		t.Fatalf("readCredentialAttempts returned error: %v", err)
	}
	if len(duplicates) != 0 {
		t.Fatalf("duplicates = %d, want 0", len(duplicates))
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0].User != "alice" {
		t.Fatalf("user = %q, want %q", attempts[0].User, "alice")
	}
	if attempts[0].Password != "" {
		t.Fatalf("password = %q, want empty password", attempts[0].Password)
	}
}

func TestRunGuessSkipsDuplicateUsernameAttemptsAndLogsThem(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:      "https://mail.example.com/EWS/Exchange.asmx",
		CredFile: writeCredFile(t, "alice:one", "Alice:two", "bob:three", "alice:four", "bob:three"),
		Format:   "csv",
		Workers:  2,
		Conns:    1,
	}

	var (
		stdout   bytes.Buffer
		stderr   bytes.Buffer
		mu       sync.Mutex
		attempts []string
	)

	code := runGuessWithTester(cfg, &stdout, &stderr, func(attempt credentialAttempt) (ews.AuthResult, error) {
		mu.Lock()
		attempts = append(attempts, attempt.User+":"+attempt.Password)
		mu.Unlock()
		return ews.AuthFailed, nil
	})
	if code != 0 {
		t.Fatalf("runGuessWithTester returned %d, want 0", code)
	}

	sort.Strings(attempts)
	if strings.Join(attempts, ",") != "alice:one,bob:three" {
		t.Fatalf("attempts = %v, want first credential per username only", attempts)
	}

	errText := stderr.String()
	if !strings.Contains(errText, "Skipping 3 duplicate username attempts") {
		t.Fatalf("stderr = %q, want duplicate summary", errText)
	}
	if !strings.Contains(errText, "Unique credential combinations not attempted (2):") {
		t.Fatalf("stderr = %q, want skipped credential summary", errText)
	}
	if !strings.Contains(errText, "[*]   Alice:two") {
		t.Fatalf("stderr = %q, want Alice:two in skipped credential list", errText)
	}
	if !strings.Contains(errText, "[*]   alice:four") {
		t.Fatalf("stderr = %q, want alice:four in skipped credential list", errText)
	}
	if strings.Contains(errText, "[*]   bob:three") {
		t.Fatalf("stderr = %q, did not want repeated attempted combo in skipped list", errText)
	}
}

func TestRunGuessWritesValidCredentialsAndReturnsOperationalError(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:      "https://mail.example.com/EWS/Exchange.asmx",
		CredFile: writeCredFile(t, "alice:one", "bob:two"),
		Format:   "json",
		Workers:  1,
		Conns:    1,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runGuessWithTester(cfg, &stdout, &stderr, func(attempt credentialAttempt) (ews.AuthResult, error) {
		if attempt.User == "alice" {
			return ews.AuthSuccess, nil
		}
		return ews.AuthError, fmt.Errorf("proxy error for %s", attempt.User)
	})
	if code != 2 {
		t.Fatalf("runGuessWithTester returned %d, want 2", code)
	}

	out := stdout.String()
	if !strings.Contains(out, `"user": "alice"`) {
		t.Fatalf("stdout = %q, want alice result", out)
	}
	if !strings.Contains(out, `"password": "one"`) {
		t.Fatalf("stdout = %q, want password in JSON output", out)
	}
	if !strings.Contains(stderr.String(), "Guessing encountered 1 operational errors") {
		t.Fatalf("stderr = %q, want operational error summary", stderr.String())
	}
}
