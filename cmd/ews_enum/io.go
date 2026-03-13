package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"ews_enum/internal/ews"
)

type sprayResult struct {
	User   string `json:"user"`
	Status string `json:"status"`
}

func contactSortFields(contact ews.Contact) []string {
	return []string{
		strings.ToLower(contact.EmailAddress),
		strings.ToLower(contact.DisplayName),
		strings.ToLower(contact.GivenName),
		strings.ToLower(contact.Surname),
		strings.ToLower(contact.Title),
		strings.ToLower(contact.Department),
		strings.ToLower(contact.Office),
		strings.ToLower(contact.Company),
		strings.ToLower(contact.Phone),
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

func openOutput(stdout io.Writer, path string) (io.Writer, func() error, error) {
	if path == "" {
		return stdout, func() error { return nil }, nil
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}

	return f, f.Close, nil
}

func writeSprayResults(out io.Writer, format, password string, validUsers []string) error {
	sort.Strings(validUsers)

	switch format {
	case "csv":
		w := csv.NewWriter(out)
		if err := w.Write([]string{"username", "password", "status"}); err != nil {
			return err
		}
		for _, user := range validUsers {
			if err := w.Write([]string{user, password, "valid"}); err != nil {
				return err
			}
		}
		w.Flush()
		return w.Error()
	case "json":
		results := make([]sprayResult, len(validUsers))
		for i, user := range validUsers {
			results[i] = sprayResult{User: user, Status: "valid"}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	default:
		return fmt.Errorf("unsupported spray output format %q", format)
	}
}

func writeContacts(out io.Writer, format string, contacts []ews.Contact) error {
	sort.Slice(contacts, func(i, j int) bool {
		left := contactSortFields(contacts[i])
		right := contactSortFields(contacts[j])
		for idx := range left {
			if left[idx] == right[idx] {
				continue
			}
			return left[idx] < right[idx]
		}
		return false
	})

	switch format {
	case "csv":
		w := csv.NewWriter(out)
		if err := w.Write([]string{"email", "display_name", "given_name", "surname", "title", "department", "office", "company", "phone"}); err != nil {
			return err
		}
		for _, contact := range contacts {
			if err := w.Write([]string{
				contact.EmailAddress,
				contact.DisplayName,
				contact.GivenName,
				contact.Surname,
				contact.Title,
				contact.Department,
				contact.Office,
				contact.Company,
				contact.Phone,
			}); err != nil {
				return err
			}
		}
		w.Flush()
		return w.Error()
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(contacts)
	case "emails":
		for _, contact := range contacts {
			if contact.EmailAddress != "" {
				if _, err := fmt.Fprintln(out, contact.EmailAddress); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported enum output format %q", format)
	}
}

func printContact(stderr io.Writer, contact ews.Contact) {
	parts := []string{}
	if contact.EmailAddress != "" {
		parts = append(parts, contact.EmailAddress)
	}
	if contact.DisplayName != "" {
		parts = append(parts, contact.DisplayName)
	}
	if contact.Title != "" {
		parts = append(parts, contact.Title)
	}
	if contact.Department != "" {
		parts = append(parts, contact.Department)
	}
	fmt.Fprintf(stderr, "[+] %s\n", strings.Join(parts, " | "))
}
