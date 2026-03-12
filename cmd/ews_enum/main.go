package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"ews_enum/internal/ews"
)

func main() {
	url := flag.String("url", "", "EWS endpoint URL (e.g. https://mail.target.com/EWS/Exchange.asmx)")
	user := flag.String("user", "", "Username (DOMAIN\\user or user@domain.com)")
	pass := flag.String("pass", "", "Password")
	format := flag.String("format", "csv", "Output format: csv, json, emails")
	ntlm := flag.Bool("ntlm", true, "Use NTLM authentication (default true, set -ntlm=false for basic)")
	timeout := flag.Int("timeout", 30, "HTTP timeout in seconds")
	depth := flag.Int("depth", 3, "Max prefix depth for enumeration (2=aa, 3=aaa)")
	delay := flag.Int("delay", 0, "Delay in milliseconds between requests")
	flag.Parse()

	if *url == "" || *user == "" || *pass == "" {
		fmt.Fprintf(os.Stderr, "Usage: ews_enum -url <EWS_URL> -user <USER> -pass <PASS> [options]\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	client := ews.NewClient(*ntlm, *timeout)

	seen := make(map[string]ews.Contact)
	requestCount := 0
	chars := "abcdefghijklmnopqrstuvwxyz"

	var enumerate func(prefix string, currentDepth int)
	enumerate = func(prefix string, currentDepth int) {
		if currentDepth > *depth {
			return
		}

		if *delay > 0 && requestCount > 0 {
			time.Sleep(time.Duration(*delay) * time.Millisecond)
		}

		requestCount++
		fmt.Fprintf(os.Stderr, "\r[*] Resolving prefix: %-6s (found: %d, requests: %d)", prefix, len(seen), requestCount)

		contacts, truncated, err := ews.ResolveNames(client, *url, *user, *pass, prefix)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[!] Error on prefix %q: %v\n", prefix, err)
			errStr := err.Error()
			if strings.Contains(errStr, "authentication failed") ||
				strings.Contains(errStr, "LogonDenied") ||
				strings.Contains(errStr, "AccessDenied") {
				fmt.Fprintf(os.Stderr, "[!] Authentication error — aborting. Check your credentials and auth method.\n")
				os.Exit(2)
			}
			return
		}

		for _, c := range contacts {
			key := strings.ToLower(c.EmailAddress)
			if key == "" {
				key = strings.ToLower(c.DisplayName)
			}
			if _, exists := seen[key]; !exists {
				seen[key] = c
			}
		}

		// If EWS returned 100+ results (truncated), go deeper
		if truncated && currentDepth < *depth {
			for _, ch := range chars {
				enumerate(prefix+string(ch), currentDepth+1)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "[*] Starting GAL enumeration against %s\n", *url)
	fmt.Fprintf(os.Stderr, "[*] Max prefix depth: %d\n", *depth)

	for _, ch := range chars {
		enumerate(string(ch), 1)
	}

	// Also try numeric and common special prefixes
	for _, ch := range "0123456789" {
		enumerate(string(ch), 1)
	}

	fmt.Fprintf(os.Stderr, "\n[*] Enumeration complete: %d unique entries, %d requests\n", len(seen), requestCount)

	// Sort results by email
	results := make([]ews.Contact, 0, len(seen))
	for _, c := range seen {
		results = append(results, c)
	}
	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].EmailAddress) < strings.ToLower(results[j].EmailAddress)
	})

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			log.Fatalf("JSON encode: %v", err)
		}

	case "emails":
		for _, c := range results {
			if c.EmailAddress != "" {
				fmt.Println(c.EmailAddress)
			}
		}

	default: // csv
		w := csv.NewWriter(os.Stdout)
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
}
