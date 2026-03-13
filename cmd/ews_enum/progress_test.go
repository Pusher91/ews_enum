package main

import (
	"strings"
	"testing"
)

func TestFormatAttemptProgressShowsDoneRunningAndValid(t *testing.T) {
	t.Parallel()

	line := formatAttemptProgress(3, 4, 1, "user4", 2)
	if !strings.Contains(line, "[3/4 done, 1 running, valid: 2]") {
		t.Fatalf("line = %q, want done/running/valid summary", line)
	}
	if !strings.Contains(line, "Trying: user4") {
		t.Fatalf("line = %q, want current username", line)
	}
}

func TestFormatAttemptProgressTruncatesLongUsernames(t *testing.T) {
	t.Parallel()

	longUser := strings.Repeat("a", progressCurrentWidth+5)
	line := formatAttemptProgress(1, 2, 1, longUser, 0)
	want := "Trying: " + strings.Repeat("a", progressCurrentWidth-3) + "..."
	if !strings.Contains(line, want) {
		t.Fatalf("line = %q, want truncated username %q", line, want)
	}
	if strings.Contains(line, longUser) {
		t.Fatalf("line = %q, did not want full long username", line)
	}
}
