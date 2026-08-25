package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
)

func TestCIAgentFlags(t *testing.T) {
	for _, cmd := range []*struct {
		name string
		has  func(string) bool
	}{
		{name: "ci", has: func(name string) bool { return ciCmd.Flags().Lookup(name) != nil }},
		{name: "ci logs", has: func(name string) bool { return ciLogsCmd.Flags().Lookup(name) != nil }},
	} {
		if !cmd.has("agent") {
			t.Errorf("%s command is missing the --agent flag", cmd.name)
		}
	}
}

func TestRenderCheckRunsPlain(t *testing.T) {
	run := gh.CheckRun{
		Name:        "build / test",
		Status:      "completed",
		Conclusion:  "success",
		StartedAt:   "2026-07-01T11:59:00Z",
		CompletedAt: "2026-07-01T12:00:00Z",
	}
	run.CheckSuite.ID = 42

	got := renderCheckRunsPlain([]gh.CheckRun{run}, map[int64]string{42: "build"}, false)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("renderCheckRunsPlain() contains ANSI escape codes: %q", got)
	}
	if !strings.Contains(got, "test") || !strings.Contains(got, "1m 0s") {
		t.Errorf("renderCheckRunsPlain() = %q, missing check details", got)
	}
}

func TestLooksLikeGitRef(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"HEAD", true},
		{"HEAD~2", true},
		{"HEAD^", true},
		{"main~1", true},
		{"abc123", true},
		{"a1b2c3d4e5f6789012345678901234567890abcd", true},
		{"main", false},
		{"feature/foo", false},
		{"gh-pages", false},
	}
	for _, tt := range tests {
		if got := looksLikeGitRef(tt.arg); got != tt.want {
			t.Errorf("looksLikeGitRef(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}

func TestFormatRelativeTimeAt(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"seconds only", 5 * time.Second, "5s ago"},
		{"minutes and seconds", 2*time.Minute + 12*time.Second, "2m 12s ago"},
		{"exact minute", 3 * time.Minute, "3m 0s ago"},
		{"hours and minutes", 4*time.Hour + 30*time.Minute, "4h 30m ago"},
		{"exact hour", 1 * time.Hour, "1h 0m ago"},
		{"days and hours", 2*24*time.Hour + 3*time.Hour, "2d 3h ago"},
		{"just under 7 days", 6*24*time.Hour + 23*time.Hour, "6d 23h ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRelativeTimeAt(now.Add(-tt.ago), now)
			if got != tt.want {
				t.Errorf("formatRelativeTimeAt(now-%v) = %q, want %q", tt.ago, got, tt.want)
			}
		})
	}
}

func TestFormatRelativeTimeAt_AbsoluteTimestampAfter7Days(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	then := now.Add(-7 * 24 * time.Hour)

	got := formatRelativeTimeAt(then, now)
	want := then.Local().Format("2006-01-02 15:04")
	if got != want {
		t.Errorf("formatRelativeTimeAt(now-7d) = %q, want %q", got, want)
	}
}
