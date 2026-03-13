package main

import (
	"fmt"
	"io"

	"ews_enum/internal/ews"
)

func runAuthCheck(cfg appConfig, stderr io.Writer) int {
	client := ews.NewClient(ews.ClientOpts{
		NTLM: cfg.NTLM, TimeoutSec: cfg.Timeout, MaxConns: cfg.Conns, UseCookies: true,
	})

	fmt.Fprintf(stderr, "[*] Testing credentials against %s\n", cfg.URL)
	fmt.Fprintf(stderr, "[*] User: %s\n", cfg.User)

	result, err := ews.TestAuth(client, cfg.URL, cfg.User, cfg.Pass)
	switch result {
	case ews.AuthSuccess:
		fmt.Fprintf(stderr, "[+] VALID credentials: %s\n", cfg.User)
		return 0
	case ews.AuthFailed:
		fmt.Fprintf(stderr, "[-] INVALID credentials: %s\n", cfg.User)
		return 1
	default:
		fmt.Fprintf(stderr, "[!] ERROR: %v\n", err)
		return 2
	}
}
