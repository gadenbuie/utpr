package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

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

	got := renderCheckRunsPlain([]gh.CheckRun{run}, map[int64]string{42: "build"}, false, nil)
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

	result := processLogLines(raw, false, 100, nil)
	processed, mode := result.Lines, result.Mode
	if mode != cilog.ModeLandmark {
		t.Fatalf("processLogLines() mode = %v, want ModeLandmark", mode)
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

	result := processLogLines(raw, false, 0, nil)
	processed, mode := result.Lines, result.Mode
	if mode != cilog.ModeFull {
		t.Fatalf("processLogLines(raw, _, 0) mode = %v, want ModeFull", mode)
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
	result = processLogLines(small, false, 100, nil)
	processed, mode = result.Lines, result.Mode
	if mode != cilog.ModeFull {
		t.Errorf("processLogLines(short log) mode = %v, want ModeFull", mode)
	}
	if len(processed) != total {
		t.Errorf("processLogLines(short log) returned %d lines, want all %d", len(processed), total)
	}
}

func TestProcessLogLinesKeepsTimestamps(t *testing.T) {
	raw := ciLogsFixture(0, 0)

	processed := processLogLines(raw, true, 50, nil).Lines
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

	oldFull, oldLines := flagCILogsFull, flagCILogsLines
	t.Cleanup(func() { flagCILogsFull, flagCILogsLines = oldFull, oldLines })

	flagCILogsFull, flagCILogsLines = false, 100
	if got := ciLogsLineCount(); got != 100 {
		t.Errorf("ciLogsLineCount() = %d without --full, want 100", got)
	}
	flagCILogsFull = true
	if got := ciLogsLineCount(); got != 0 {
		t.Errorf("ciLogsLineCount() = %d with --full, want 0 (complete log)", got)
	}
}

func TestCILogsNote(t *testing.T) {
	tests := []struct {
		name   string
		result processedLog
		grep   bool
		want   string
	}{
		{"tail", processedLog{Lines: make([]string, 50), Mode: cilog.ModeTail}, false, "(last 50 lines)"},
		{"landmark", processedLog{Lines: make([]string, 80), Mode: cilog.ModeLandmark}, false, "(80 lines around the failure; use --full for the complete log)"},
		{"full with dropped steps", processedLog{Lines: make([]string, 60), Mode: cilog.ModeFull, Dropped: true}, false, "(post-job steps omitted; use --full for the complete log)"},
		{"full complete", processedLog{Lines: make([]string, 60), Mode: cilog.ModeFull}, false, ""},
		{"grep", processedLog{Lines: make([]string, 10), GrepMatches: 8, GrepTotal: 10}, true, "(8 matching lines)"},
		{"grep capped", processedLog{Lines: make([]string, 5), GrepMatches: 8, GrepTotal: 12}, true, "(8 matching lines, showing last 5; use --full for all matches)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ciLogsNote(tt.result, tt.grep); got != tt.want {
				t.Errorf("ciLogsNote() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCIMaxBytesFlagDefaults(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag *pflag.Flag
	}{
		{name: "ci", flag: ciCmd.Flags().Lookup("max-bytes")},
		{name: "ci logs", flag: ciLogsCmd.Flags().Lookup("max-bytes")},
	} {
		if tc.flag == nil {
			t.Errorf("%s command is missing the --max-bytes flag", tc.name)
			continue
		}
		if tc.flag.DefValue != "262144" {
			t.Errorf("%s --max-bytes default = %s, want 262144 (ui.DefaultMaxOutputBytes)", tc.name, tc.flag.DefValue)
		}
	}
}

func TestProcessLogLinesGrep(t *testing.T) {
	raw := ciLogsFixture(150, 120)

	gf := &grepFilter{re: regexp.MustCompile(`(?i)expect_equal`), before: 2, after: 1}
	result := processLogLines(raw, false, 100, gf)

	if result.GrepMatches != 1 {
		t.Fatalf("processLogLines(grep) matched %d lines, want 1", result.GrepMatches)
	}
	if len(result.Lines) != 4 {
		t.Fatalf("processLogLines(grep) returned %d lines, want 4 (match + context)", len(result.Lines))
	}
	got := strings.Join(result.Lines, "\n")
	if !strings.Contains(got, "expect_equal(x, 2)") {
		t.Errorf("processLogLines(grep) missing the matching line: %q", got)
	}
	if strings.Contains(got, "upload noise") {
		t.Errorf("processLogLines(grep) includes post-failure noise despite the match being earlier in the log")
	}
	for _, line := range result.Lines {
		if strings.Contains(line, "2026-07-01T10:00:00") {
			t.Errorf("processLogLines(grep) left a timestamp on line %q", line)
		}
	}
}

func TestProcessLogLinesGrepCappedTail(t *testing.T) {
	raw := ciLogsFixture(150, 0)

	gf := &grepFilter{re: regexp.MustCompile(`(?i)checkout output`)}
	result := processLogLines(raw, false, 20, gf)

	if result.GrepMatches != 150 {
		t.Fatalf("processLogLines(grep) matched %d lines, want 150", result.GrepMatches)
	}
	if len(result.Lines) != 20 {
		t.Fatalf("processLogLines(grep) returned %d lines, want 20 after the cap", len(result.Lines))
	}
	if !strings.Contains(result.Lines[len(result.Lines)-1], "checkout output 149") {
		t.Errorf("processLogLines(grep) cap kept the wrong tail: %q", result.Lines[len(result.Lines)-1])
	}
}

func TestProcessLogLinesGrepNoMatches(t *testing.T) {
	raw := ciLogsFixture(5, 5)

	gf := &grepFilter{re: regexp.MustCompile(`no-such-line`)}
	result := processLogLines(raw, false, 100, gf)
	if result.GrepMatches != 0 || len(result.Lines) != 0 {
		t.Errorf("processLogLines(grep) = %d matches, %d lines, want none", result.GrepMatches, len(result.Lines))
	}
}

func TestProcessLogLinesGrepFull(t *testing.T) {
	raw := ciLogsFixture(150, 0)

	gf := &grepFilter{re: regexp.MustCompile(`(?i)checkout output`)}
	result := processLogLines(raw, false, 0, gf)
	if len(result.Lines) != 150 || result.GrepTotal != 150 {
		t.Errorf("processLogLines(grep, n=0) returned %d lines, want all 150 uncapped", len(result.Lines))
	}
}

func TestCILogsGrepFlags(t *testing.T) {
	for _, name := range []string{"grep", "after", "before"} {
		if ciLogsCmd.Flags().Lookup(name) == nil {
			t.Errorf("ci logs command is missing the --%s flag", name)
		}
	}
	if !strings.Contains(ciLogsCmd.Long, "--grep") {
		t.Errorf("ci logs long help should document --grep")
	}
}

func TestParseCILogsGrepRequiresGrepForContext(t *testing.T) {
	oldGrep, oldAfter := flagCILogsGrep, flagCILogsAfter
	t.Cleanup(func() { flagCILogsGrep, flagCILogsAfter = oldGrep, oldAfter })

	flagCILogsGrep, flagCILogsAfter = "", 5
	if _, err := parseCILogsGrep(); err == nil {
		t.Error("parseCILogsGrep() = nil error with --after but no --grep, want error")
	}

	flagCILogsAfter = 0
	if _, err := parseCILogsGrep(); err != nil {
		t.Errorf("parseCILogsGrep() = %v without flags, want nil", err)
	}
}

func TestProcessLogLinesGrepCapDropsLeadingSeparator(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 9; i++ {
		fmt.Fprintf(&b, "hit%d\nf\nf\n", i)
	}
	gf := &grepFilter{re: regexp.MustCompile(`hit`), before: 1}

	result := processLogLines(b.String(), false, 6, gf)
	if len(result.Lines) > 0 && result.Lines[0] == "--" {
		t.Errorf("processLogLines(grep) capped output starts with a separator: %q", result.Lines)
	}
}
