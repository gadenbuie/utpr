package ui

import (
	"fmt"
	"strings"
)

// DefaultMaxOutputBytes is the default output cap applied when --max-bytes
// is omitted.
const DefaultMaxOutputBytes = 256 * 1024

const truncationMarkerFmt = "… truncated, %d bytes hidden — use --max-bytes 0 for full output"

// ByteLimiter caps accumulated output at a total byte budget. Multiple
// outputs (e.g. several jobs) can share one limiter so the budget spans the
// whole invocation. A maxBytes of 0 disables the cap; a negative value means
// "use DefaultMaxOutputBytes".
type ByteLimiter struct {
	disabled  bool
	remaining int
}

// NewByteLimiter returns a limiter with the given total budget.
func NewByteLimiter(maxBytes int) *ByteLimiter {
	if maxBytes < 0 {
		maxBytes = DefaultMaxOutputBytes
	}
	return &ByteLimiter{disabled: maxBytes == 0, remaining: maxBytes}
}

// Limit returns s, cut at a line boundary so it fits within the remaining
// budget, plus a trailing marker line reporting how many bytes were hidden.
// The marker itself does not count against the budget. Subsequent calls draw
// from the same budget; once exhausted, only the marker line is returned.
func (l *ByteLimiter) Limit(s string) string {
	if l.disabled || s == "" {
		return s
	}
	keep := s
	if len(keep) > l.remaining {
		keep = cutAtLineBoundary(keep, l.remaining)
	}
	if len(keep) < len(s) {
		hidden := len(s) - len(keep)
		l.remaining -= len(keep)
		return keep + fmt.Sprintf(truncationMarkerFmt, hidden)
	}
	l.remaining -= len(keep)
	return keep
}

// cutAtLineBoundary returns the longest prefix of s that is at most max bytes
// long and ends at a line boundary. A single line longer than max yields an
// empty result.
func cutAtLineBoundary(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	window := s[:max]
	// Keep only complete lines: exclude a trailing partial line.
	if i := strings.LastIndexByte(window, '\n'); i >= 0 {
		return s[:i+1]
	}
	return ""
}
