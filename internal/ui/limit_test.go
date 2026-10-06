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
	want := "aaa\nbbbb\n" + "… truncated, 3 bytes hidden — use --max-bytes 0 for full output"
	if got != want {
		t.Fatalf("Limit() = %q, want %q", got, want)
	}
}

func TestByteLimiterNoMidLineCut(t *testing.T) {
	l := NewByteLimiter(5)
	s := "averyveryverylongline\n"
	got := l.Limit(s)
	if got != "… truncated, 22 bytes hidden — use --max-bytes 0 for full output" {
		t.Fatalf("Limit() = %q, want marker only", got)
	}
}

func TestByteLimiterSharedBudgetAcrossChunks(t *testing.T) {
	l := NewByteLimiter(12)
	first := l.Limit("aaaa\nbbbb\n") // 10 bytes kept, budget has 2 left
	if first != "aaaa\nbbbb\n" {
		t.Fatalf("first chunk = %q, want unchanged", first)
	}
	second := l.Limit("cccc\ndddd\n") // budget exhausted
	want := "… truncated, 10 bytes hidden — use --max-bytes 0 for full output"
	if second != want {
		t.Fatalf("second chunk = %q, want marker only %q", second, want)
	}
}

func TestByteLimiterTotalPayloadWithinBudget(t *testing.T) {
	l := NewByteLimiter(100)
	var payload strings.Builder
	for i := 0; i < 5; i++ {
		payload.WriteString(l.Limit(strings.Repeat("line\n", 10)))
	}
	out := payload.String()
	markerLen := len("… truncated, 0 bytes hidden — use --max-bytes 0 for full output")
	if len(out) > 100+5*markerLen {
		t.Fatalf("output length %d exceeds cap plus markers", len(out))
	}
}
