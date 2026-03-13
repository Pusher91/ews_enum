package main

import "fmt"

const progressCurrentWidth = 40

func formatAttemptProgress(completed, total, running int64, current string, valid int) string {
	current = truncateProgressCurrent(current, progressCurrentWidth)
	return fmt.Sprintf(
		"\r[*] [%d/%d done, %d running, valid: %d] Trying: %-*s",
		completed,
		total,
		running,
		valid,
		progressCurrentWidth,
		current,
	)
}

func truncateProgressCurrent(current string, width int) string {
	if width <= 0 || len(current) <= width {
		return current
	}
	if width <= 3 {
		return current[:width]
	}
	return current[:width-3] + "..."
}
