package main

import "fmt"

func formatAttemptProgress(completed, total, running int64, current string, valid int) string {
	return fmt.Sprintf(
		"\r[*] [%d/%d done, %d running, valid: %d] Trying: %-40s",
		completed,
		total,
		running,
		valid,
		current,
	)
}
