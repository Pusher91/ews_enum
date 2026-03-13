package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ews_enum/internal/ews"
)

func TestWriteContactsSortsBlankEmailDeterministically(t *testing.T) {
	t.Parallel()

	contacts := []ews.Contact{
		{DisplayName: "Shared Room", Title: "West Wing"},
		{DisplayName: "Shared Room", Title: "East Wing"},
	}

	var out bytes.Buffer
	if err := writeContacts(&out, "csv", contacts); err != nil {
		t.Fatalf("writeContacts returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(lines))
	}
	if !strings.Contains(lines[1], "East Wing") {
		t.Fatalf("first data row = %q, want East Wing first", lines[1])
	}
	if !strings.Contains(lines[2], "West Wing") {
		t.Fatalf("second data row = %q, want West Wing second", lines[2])
	}
}

func TestOpenOutputCreatesPrivateFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "results.txt")
	out, closeOut, err := openOutput(&bytes.Buffer{}, path)
	if err != nil {
		t.Fatalf("openOutput returned error: %v", err)
	}
	if out == nil {
		t.Fatal("openOutput returned nil writer")
	}
	if err := closeOut(); err != nil {
		t.Fatalf("closeOut returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat output file: %v", err)
	}
	if perms := info.Mode().Perm(); perms&0o077 != 0 {
		t.Fatalf("permissions = %o, want no group/other access", perms)
	}
}
