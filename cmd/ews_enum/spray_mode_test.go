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
	"time"

	"ews_enum/internal/ews"
)

func writeUserFile(t *testing.T, users ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "users.txt")
	if err := os.WriteFile(path, []byte(strings.Join(users, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write userfile: %v", err)
	}
	return path
}

func TestRunSprayReturnsErrorWhenAllAttemptsFailOperationally(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:      "https://mail.example.com/EWS/Exchange.asmx",
		UserFile: writeUserFile(t, "alice", "bob"),
		Pass:     "secret",
		Format:   "csv",
		Workers:  1,
		Conns:    1,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runSprayWithTester(cfg, &stdout, &stderr, func(username string) (ews.AuthResult, error) {
		return ews.AuthError, fmt.Errorf("proxy error for %s", username)
	})
	if code != 2 {
		t.Fatalf("runSprayWithTester returned %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no output", stdout.String())
	}
}

func TestRunSprayPreservesHitsButReturnsErrorOnMixedResults(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:      "https://mail.example.com/EWS/Exchange.asmx",
		UserFile: writeUserFile(t, "alice", "bob"),
		Pass:     "secret",
		Format:   "json",
		Workers:  1,
		Conns:    1,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runSprayWithTester(cfg, &stdout, &stderr, func(username string) (ews.AuthResult, error) {
		if username == "alice" {
			return ews.AuthSuccess, nil
		}
		return ews.AuthError, fmt.Errorf("proxy error for %s", username)
	})
	if code != 2 {
		t.Fatalf("runSprayWithTester returned %d, want 2", code)
	}
	if !strings.Contains(stdout.String(), `"user": "alice"`) {
		t.Fatalf("stdout = %q, want alice hit preserved", stdout.String())
	}
}

func TestRunSprayDedupesUserfileEntries(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:      "https://mail.example.com/EWS/Exchange.asmx",
		UserFile: writeUserFile(t, "alice", "Alice", "bob", "alice", "BOB"),
		Pass:     "secret",
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

	code := runSprayWithTester(cfg, &stdout, &stderr, func(username string) (ews.AuthResult, error) {
		mu.Lock()
		attempts = append(attempts, username)
		mu.Unlock()
		return ews.AuthFailed, nil
	})
	if code != 0 {
		t.Fatalf("runSprayWithTester returned %d, want 0", code)
	}

	sort.Strings(attempts)
	if strings.Join(attempts, ",") != "alice,bob" {
		t.Fatalf("attempts = %v, want unique usernames only", attempts)
	}
	if !strings.Contains(stderr.String(), "Skipping 3 duplicate usernames") {
		t.Fatalf("stderr = %q, want duplicate warning", stderr.String())
	}
}

func TestRunSprayDebugDupesShowsCanonicalMappings(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:        "https://mail.example.com/EWS/Exchange.asmx",
		UserFile:   writeUserFile(t, "alice", "Alice", "bob", "BOB"),
		Pass:       "secret",
		Format:     "csv",
		Workers:    2,
		Conns:      1,
		DebugDupes: true,
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runSprayWithTester(cfg, &stdout, &stderr, func(username string) (ews.AuthResult, error) {
		return ews.AuthFailed, nil
	})
	if code != 0 {
		t.Fatalf("runSprayWithTester returned %d, want 0", code)
	}

	errText := stderr.String()
	if !strings.Contains(errText, "Skipping 2 duplicate usernames") {
		t.Fatalf("stderr = %q, want duplicate summary", errText)
	}
	if !strings.Contains(errText, "duplicate: Alice -> alice") {
		t.Fatalf("stderr = %q, want Alice duplicate mapping", errText)
	}
	if !strings.Contains(errText, "duplicate: BOB -> bob") {
		t.Fatalf("stderr = %q, want BOB duplicate mapping", errText)
	}
}

func TestRunSprayDelayIsSharedAcrossWorkers(t *testing.T) {
	t.Parallel()

	cfg := appConfig{
		URL:      "https://mail.example.com/EWS/Exchange.asmx",
		UserFile: writeUserFile(t, "alice", "bob", "carol"),
		Pass:     "secret",
		Format:   "csv",
		Workers:  3,
		Conns:    1,
		DelayMS:  30,
	}

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
		mu     sync.Mutex
		starts []time.Time
	)

	code := runSprayWithTester(cfg, &stdout, &stderr, func(username string) (ews.AuthResult, error) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		return ews.AuthFailed, nil
	})
	if code != 0 {
		t.Fatalf("runSprayWithTester returned %d, want 0", code)
	}
	if len(starts) != 3 {
		t.Fatalf("starts = %d, want 3", len(starts))
	}

	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
	if gap := starts[1].Sub(starts[0]); gap < 20*time.Millisecond {
		t.Fatalf("first gap = %v, want shared pacing across workers", gap)
	}
	if gap := starts[2].Sub(starts[1]); gap < 20*time.Millisecond {
		t.Fatalf("second gap = %v, want shared pacing across workers", gap)
	}
}
