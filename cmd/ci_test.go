package cmd

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/cilog"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/ui"
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

func TestRenderCIHeaderPlain(t *testing.T) {
	var got strings.Builder
	renderCIHeader(&got, "feature/agent-output", "0123456789abcdef", false)

	want := "CI status\n" +
		"Branch: feature/agent-output\n" +
		"Commit: 0123456789abcdef\n\n"
	if got.String() != want {
		t.Errorf("renderCIHeader() = %q, want %q", got.String(), want)
	}
	if strings.Contains(got.String(), "\x1b[") {
		t.Errorf("renderCIHeader() contains ANSI escape codes: %q", got.String())
	}
}

func TestSpinCIWithResultSkipsSpinnerForAgent(t *testing.T) {
	oldCIAgent, oldCILogsAgent := flagCIAgent, flagCILogsAgent
	defer func() {
		flagCIAgent, flagCILogsAgent = oldCIAgent, oldCILogsAgent
	}()
	flagCIAgent = true
	flagCILogsAgent = false

	spinnerCalled := false
	restore := ui.SetSpinFunc(func(title string, action func() error) error {
		spinnerCalled = true
		return action()
	})
	defer restore()

	got, err := spinCIWithResult("Fetching CI status...", func() (string, error) {
		return "status", nil
	})
	if err != nil {
		t.Fatalf("spinCIWithResult() error = %v", err)
	}
	if got != "status" {
		t.Errorf("spinCIWithResult() = %q, want %q", got, "status")
	}
	if spinnerCalled {
		t.Error("spinCIWithResult() invoked the spinner in agent mode")
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

// ciLogsFixture builds a raw job log whose tail is post-failure noise:
// the failing assertion appears mid-log, followed by artifact upload and
// post-run cleanup steps.
func ciLogsFixture(fill int, tailNoise int) string {
	var b strings.Builder
	line := func(s string) { b.WriteString("2026-07-01T10:00:00.0000000Z " + s + "\n") }
	for i := 0; i < fill; i++ {
		line(fmt.Sprintf("checkout output %d", i))
	}
	line("##[group]Run R CMD check")
	line("##[endgroup]")
	line("running tests for package 'utpr'")
	line("Failed tests:")
	line("expect_equal(x, 2) is not TRUE")
	line("Execution halted")
	line("##[error]Process completed with exit code 1.")
	line("##[group]Run actions/upload-artifact@v4")
	line("##[endgroup]")
	for i := 0; i < tailNoise; i++ {
		line(fmt.Sprintf("upload noise %d", i))
	}
	line("##[group]Post Run actions/checkout@v4")
	line("##[endgroup]")
	line("Cleaning up repository...")
	return b.String()
}

func TestProcessLogLinesAnchorsWindowOnFailure(t *testing.T) {
	raw := ciLogsFixture(150, 120)

	processed, sel := processLogLines(raw, false, 100)
	if sel.Mode != cilog.ModeLandmark {
		t.Fatalf("processLogLines() mode = %v, want ModeLandmark", sel.Mode)
	}
	if len(processed) > 100 {
		t.Errorf("processLogLines() returned %d lines, want at most 100", len(processed))
	}
	got := strings.Join(processed, "\n")
	for _, want := range []string{"Failed tests:", "expect_equal(x, 2)", "Execution halted"} {
		if !strings.Contains(got, want) {
			t.Errorf("processLogLines() window missing failure context %q", want)
		}
	}
	for _, noise := range []string{"upload noise", "Post Run", "Cleaning up"} {
		if strings.Contains(got, noise) {
			t.Errorf("processLogLines() window includes post-failure noise %q", noise)
		}
	}
	for _, line := range processed {
		if strings.Contains(line, "2026-07-01T10:00:00") {
			t.Errorf("processLogLines() left a timestamp on line %q", line)
		}
	}
}

func TestProcessLogLinesFullWindow(t *testing.T) {
	raw := ciLogsFixture(150, 5)
	total := strings.Count(raw, "\n")

	processed, sel := processLogLines(raw, false, 0)
	if sel.Mode != cilog.ModeFull {
		t.Fatalf("processLogLines(raw, _, 0) mode = %v, want ModeFull", sel.Mode)
	}
	if len(processed) != total {
		t.Errorf("processLogLines(raw, _, 0) returned %d lines, want all %d", len(processed), total)
	}
	got := strings.Join(processed, "\n")
	if !strings.Contains(got, "upload noise") || !strings.Contains(got, "Cleaning up repository") {
		t.Errorf("processLogLines(raw, _, 0) dropped post-job step output")
	}

	// A log that fits within n lines is shown in full.
	small := ciLogsFixture(3, 2)
	total = strings.Count(small, "\n")
	processed, sel = processLogLines(small, false, 100)
	if sel.Mode != cilog.ModeFull {
		t.Errorf("processLogLines(short log) mode = %v, want ModeFull", sel.Mode)
	}
	if len(processed) != total {
		t.Errorf("processLogLines(short log) returned %d lines, want all %d", len(processed), total)
	}
}

func TestProcessLogLinesKeepsTimestamps(t *testing.T) {
	raw := ciLogsFixture(0, 0)

	processed, _ := processLogLines(raw, true, 50)
	if len(processed) == 0 {
		t.Fatal("processLogLines() returned no lines")
	}
	if !strings.Contains(processed[0], "2026-07-01T10:00:00") {
		t.Errorf("processLogLines(showTimestamps) first line %q lost its timestamp", processed[0])
	}
}

func TestCILogsFullFlag(t *testing.T) {
	if ciLogsCmd.Flags().Lookup("full") == nil {
		t.Fatal("ci logs command is missing the --full flag")
	}

	// Behavior is covered by TestProcessLogLinesFullWindow: -n 0 and
	// --full both select the complete log.
}
