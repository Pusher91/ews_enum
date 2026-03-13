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
