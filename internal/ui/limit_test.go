package ui

import (
	"strings"
	"testing"
)

func TestByteLimiterNoTruncation(t *testing.T) {
	l := NewByteLimiter(100)
	got := l.Limit("hello\n")
	if got != "hello\n" {
		t.Fatalf("Limit() = %q, want unchanged", got)
	}
}

func TestByteLimiterDisabled(t *testing.T) {
	l := NewByteLimiter(0)
	big := strings.Repeat("x\n", 10_000)
	if got := l.Limit(big); got != big {
		t.Fatal("Limit() truncated output with cap disabled")
	}
}

func TestByteLimiterNegativeUsesDefault(t *testing.T) {
	l := NewByteLimiter(-1)
	big := strings.Repeat("x", DefaultMaxOutputBytes+1)
	got := l.Limit(big)
	if !strings.Contains(got, "truncated") {
		t.Fatal("Limit() did not truncate output exceeding the default cap")
	}
}

func TestByteLimiterCutsAtLineBoundary(t *testing.T) {
	l := NewByteLimiter(10)
	s := "aaa\nbbbb\ncc\n"
	got := l.Limit(s)
	// "aaa\nbbbb\n" (10 bytes) fills the budget exactly; "cc\n" is hidden.
	want := "aaa\nbbbb\n" + "… truncated, 3 bytes hidden — use --max-bytes 0 for full output\n"
	if got != want {
		t.Fatalf("Limit() = %q, want %q", got, want)
	}
}

func TestByteLimiterCutsLongLineAtRuneBoundary(t *testing.T) {
	// No newline inside the budget window: cut mid-line rather than
	// hiding the whole payload behind the marker.
	l := NewByteLimiter(5)
	s := "averyveryverylongline\n"
	got := l.Limit(s)
	want := "avery" + "… truncated, 17 bytes hidden — use --max-bytes 0 for full output\n"
	if got != want {
		t.Fatalf("Limit() = %q, want %q", got, want)
	}

	// The cut never splits a multi-byte rune: "…" is 3 bytes, so a
	// 4-byte budget keeps only "ab".
	l = NewByteLimiter(4)
	got = l.Limit("ab…def\n")
	want = "ab" + "… truncated, 7 bytes hidden — use --max-bytes 0 for full output\n"
	if got != want {
		t.Fatalf("Limit() = %q, want %q", got, want)
	}
}

func TestByteLimiterSharedBudgetAcrossChunks(t *testing.T) {
	l := NewByteLimiter(12)
	first := l.Limit("aaaa\nbbbb\n") // 10 bytes kept, budget has 2 left
	if first != "aaaa\nbbbb\n" {
		t.Fatalf("first chunk = %q, want unchanged", first)
	}
	// Only 2 bytes of budget remain: no line boundary fits, so the cut
	// falls back to a mid-line cut.
	second := l.Limit("cccc\ndddd\n")
	want := "cc" + "… truncated, 8 bytes hidden — use --max-bytes 0 for full output\n"
	if second != want {
		t.Fatalf("second chunk = %q, want %q", second, want)
	}
}

func TestByteLimiterTotalPayloadWithinBudget(t *testing.T) {
	l := NewByteLimiter(100)
	var payload strings.Builder
	for i := 0; i < 5; i++ {
		payload.WriteString(l.Limit(strings.Repeat("line\n", 10)))
	}
	out := payload.String()

	const markerPrefix = "… truncated,"
	if !strings.Contains(out, markerPrefix) {
		t.Fatal("output missing truncation marker")
	}
	var content strings.Builder
	for _, line := range strings.SplitAfter(out, "\n") {
		if strings.HasPrefix(line, markerPrefix) {
			continue
		}
		content.WriteString(line)
	}
	if content.Len() > 100 {
		t.Fatalf("kept content is %d bytes, exceeds the 100-byte cap", content.Len())
	}
}
