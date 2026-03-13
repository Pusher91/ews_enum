package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"ews_enum/internal/ews"
)

type credentialAttempt struct {
	User     string
	Password string
	Line     int
}

type duplicateCredentialAttempt struct {
	Duplicate credentialAttempt
	Canonical credentialAttempt
}

func runGuess(cfg appConfig, stdout, stderr io.Writer) int {
	client := ews.NewClient(ews.ClientOpts{
		NTLM: cfg.NTLM, TimeoutSec: cfg.Timeout, MaxConns: cfg.Conns,
		DisableKeepAlive: cfg.NTLM, // NTLM binds auth to TCP connection; do not reuse across users.
	})

	return runGuessWithTester(cfg, stdout, stderr, func(attempt credentialAttempt) (ews.AuthResult, error) {
		return ews.TestAuth(client, cfg.URL, attempt.User, attempt.Password)
	})
}

func readCredentialAttempts(path string) ([]credentialAttempt, []duplicateCredentialAttempt, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	seen := make(map[string]credentialAttempt)
	attempts := make([]credentialAttempt, 0)
	duplicates := make([]duplicateCredentialAttempt, 0)
	scanner := bufio.NewScanner(f)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		user, password, ok := strings.Cut(rawLine, ":")
		if !ok {
			return nil, nil, fmt.Errorf("invalid credential on line %d: expected username:password", lineNo)
		}

		user = strings.TrimSpace(user)
		if user == "" {
			return nil, nil, fmt.Errorf("invalid credential on line %d: username must be non-empty", lineNo)
		}

		attempt := credentialAttempt{
			User:     user,
			Password: password,
			Line:     lineNo,
		}

		key := strings.ToLower(user)
		if canonical, exists := seen[key]; exists {
			duplicates = append(duplicates, duplicateCredentialAttempt{
				Duplicate: attempt,
				Canonical: canonical,
			})
			continue
		}

		seen[key] = attempt
		attempts = append(attempts, attempt)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}

	return attempts, duplicates, nil
}

func runGuessWithTester(cfg appConfig, stdout, stderr io.Writer, testAuth func(credentialAttempt) (ews.AuthResult, error)) int {
	attempts, duplicates, err := readCredentialAttempts(cfg.CredFile)
	if err != nil {
		fmt.Fprintf(stderr, "[!] Cannot read credfile: %v\n", err)
		return 1
	}
	if len(attempts) == 0 {
		fmt.Fprintf(stderr, "[!] No credentials found in %s\n", cfg.CredFile)
		return 1
	}

	fmt.Fprintf(stderr, "[*] Credential guessing against %s\n", cfg.URL)
	fmt.Fprintf(stderr, "[*] Unique usernames: %d, workers: %d, connections: %d\n", len(attempts), cfg.Workers, cfg.Conns)
	if len(duplicates) > 0 {
		fmt.Fprintf(stderr, "[!] Skipping %d duplicate username attempts from %s\n", len(duplicates), cfg.CredFile)
		for _, dup := range duplicates {
			fmt.Fprintf(
				stderr,
				"[!]   skipping line %d (%s:%s): username %s was already attempted on line %d (%s:%s)\n",
				dup.Duplicate.Line,
				dup.Duplicate.User,
				dup.Duplicate.Password,
				dup.Duplicate.User,
				dup.Canonical.Line,
				dup.Canonical.User,
				dup.Canonical.Password,
			)
		}
	}

	var stateMu sync.Mutex
	var outputMu sync.Mutex
	validResults := make([]credentialResult, 0)
	var attemptCount atomic.Int64
	var opErrorCount atomic.Int64
	totalAttempts := int64(len(attempts))
	workCh := make(chan credentialAttempt, cfg.Workers*2)
	pacer := newRequestPacer(cfg.DelayMS)
	var wg sync.WaitGroup

	clearProgress := func() {
		fmt.Fprintf(stderr, "\r%-100s\r", "")
	}

	worker := func() {
		defer wg.Done()
		for attempt := range workCh {
			if !pacer.Wait(nil) {
				return
			}

			count := attemptCount.Add(1)
			stateMu.Lock()
			hits := len(validResults)
			stateMu.Unlock()
			outputMu.Lock()
			fmt.Fprintf(stderr, "\r[*] [%d/%d] Trying: %-40s (valid: %d)", count, totalAttempts, attempt.User, hits)
			outputMu.Unlock()

			result, authErr := testAuth(attempt)
			switch result {
			case ews.AuthSuccess:
				stateMu.Lock()
				validResults = append(validResults, credentialResult{
					User:     attempt.User,
					Password: attempt.Password,
					Status:   "valid",
				})
				stateMu.Unlock()
				outputMu.Lock()
				clearProgress()
				fmt.Fprintf(stderr, "[+] VALID: %s:%s\n", attempt.User, attempt.Password)
				outputMu.Unlock()
			case ews.AuthError:
				opErrorCount.Add(1)
				outputMu.Lock()
				clearProgress()
				fmt.Fprintf(stderr, "[!] ERROR: %s — %v\n", attempt.User, authErr)
				outputMu.Unlock()
			}
		}
	}

	wg.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		go worker()
	}

	for _, attempt := range attempts {
		workCh <- attempt
	}
	close(workCh)
	wg.Wait()

	stateMu.Lock()
	validCount := len(validResults)
	results := append([]credentialResult(nil), validResults...)
	stateMu.Unlock()

	outputMu.Lock()
	clearProgress()
	fmt.Fprintf(stderr, "[*] Guessing complete: %d/%d valid credentials\n", validCount, len(attempts))
	if count := opErrorCount.Load(); count > 0 {
		fmt.Fprintf(stderr, "[!] Guessing encountered %d operational errors\n", count)
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

	if err := writeCredentialResults(out, cfg.Format, results); err != nil {
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
