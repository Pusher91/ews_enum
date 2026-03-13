package main

import (
	"bytes"
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
