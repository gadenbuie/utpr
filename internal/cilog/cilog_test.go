package cilog

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// ts prefixes a log line with a raw GitHub Actions log timestamp.
func ts(line string) string {
	return "2026-07-01T10:00:00.0000000Z " + line
}

// testthatFailureLog builds a raw job log where the failure block is buried
// under tailNoise post-failure lines: an artifact upload step followed by a
// post-run cleanup step.
func testthatFailureLog(tailNoise int) []string {
	var lines []string
	lines = append(lines,
		ts("##[group]Run actions/checkout@v4"),
		ts("##[endgroup]"),
	)
	for i := 0; i < 150; i++ {
		lines = append(lines, ts(fmt.Sprintf("checkout output %d", i)))
	}
	lines = append(lines,
		ts("##[group]Run R CMD check"),
		ts("##[endgroup]"),
		ts("* using log directory '/tmp/Rcmdcheck'"),
		ts("running tests for package 'utpr'"),
		ts("Failed tests:"),
		ts("── Failure (test-utpr:12) ───────────────"),
		ts("expect_equal(x, 2) is not TRUE"),
		ts("##[error]── Failure (test-utpr:12) ───────────────"),
		ts("Execution halted"),
		ts("##[error]Process completed with exit code 1."),
	)
	lines = append(lines,
		ts("##[group]Run actions/upload-artifact@v4"),
		ts("##[endgroup]"),
	)
	for i := 0; i < tailNoise; i++ {
		lines = append(lines, ts(fmt.Sprintf("upload noise %d", i)))
	}
	lines = append(lines,
		ts("##[group]Post Run actions/checkout@v4"),
		ts("##[endgroup]"),
		ts("Cleaning up repository..."),
		ts("Cleaning up orphan processes"),
	)
	return lines
}

func join(lines []string) string {
	return strings.Join(lines, "\n")
}

func containsLine(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

func TestSelectAnchorsWindowOnFailure(t *testing.T) {
	lines := testthatFailureLog(120)

	sel := Select(lines, 100)
	if sel.Mode != ModeLandmark {
		t.Fatalf("Select() mode = %v, want ModeLandmark", sel.Mode)
	}
	if len(sel.Lines) > 100 {
		t.Errorf("Select() returned %d lines, want at most 100", len(sel.Lines))
	}
	for _, want := range []string{"Failed tests:", "expect_equal(x, 2)", "Execution halted"} {
		if !containsLine(sel.Lines, want) {
			t.Errorf("Select() window missing landmark context %q", want)
		}
	}
	for _, noise := range []string{"upload noise", "Post Run", "Cleaning up"} {
		if containsLine(sel.Lines, noise) {
			t.Errorf("Select() window includes post-failure noise %q", noise)
		}
	}
}

func TestSelectFullMode(t *testing.T) {
	lines := testthatFailureLog(5)

	sel := Select(lines, 0)
	if sel.Mode != ModeFull {
		t.Fatalf("Select(lines, 0) mode = %v, want ModeFull", sel.Mode)
	}
	if len(sel.Lines) != len(lines) {
		t.Errorf("Select(lines, 0) returned %d lines, want all %d", len(sel.Lines), len(lines))
	}
	for _, want := range []string{"upload noise", "Post Run", "Cleaning up"} {
		if !containsLine(sel.Lines, want) {
			t.Errorf("Select(lines, 0) missing post-job step %q", want)
		}
	}

	// A log that fits within n lines is also shown in full.
	sel = Select(testthatFailureLog(3), 200)
	if sel.Mode != ModeFull {
		t.Errorf("Select(small log, 200) mode = %v, want ModeFull", sel.Mode)
	}
}

func TestSelectTailFallbackWithoutLandmarks(t *testing.T) {
	var lines []string
	lines = append(lines, ts("##[group]Run make"), ts("##[endgroup]"))
	for i := 0; i < 148; i++ {
		lines = append(lines, ts(fmt.Sprintf("build output %d", i)))
	}

	sel := Select(lines, 50)
	if sel.Mode != ModeTail {
		t.Fatalf("Select() mode = %v, want ModeTail", sel.Mode)
	}
	if len(sel.Lines) != 50 {
		t.Errorf("Select() returned %d lines, want 50", len(sel.Lines))
	}
	if sel.Lines[0] != lines[len(lines)-50] {
		t.Errorf("Select() tail starts at %q, want %q", sel.Lines[0], lines[len(lines)-50])
	}
}

func TestSelectKeepsFailedPostStep(t *testing.T) {
	lines := []string{
		ts("##[group]Run make test"),
		ts("##[endgroup]"),
		ts("all tests passed"),
		ts("##[group]Post Run actions/cache@v4"),
		ts("##[endgroup]"),
		ts("##[error]Failed to save cache: disk full"),
		ts("Execution halted"),
	}

	sel := Select(lines, 3)
	if !containsLine(sel.Lines, "Failed to save cache") {
		t.Errorf("Select() dropped a post step that contains the failure: %q", join(sel.Lines))
	}
}

func TestSelectDensestWindow(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, ts(fmt.Sprintf("filler %d", i)))
	}
	// Cluster A: three landmarks at lines 9-11.
	lines[9] = ts("Failed tests:")
	lines[10] = ts("Error: cluster A root cause")
	lines[11] = ts("Execution halted")
	// Cluster B: one landmark at line 150.
	lines[150] = ts("##[error]cluster B")

	sel := Select(lines, 60)
	if sel.Mode != ModeLandmark {
		t.Fatalf("Select() mode = %v, want ModeLandmark", sel.Mode)
	}
	if !containsLine(sel.Lines, "cluster A root cause") {
		t.Errorf("Select() window skipped the densest landmark cluster: %q", join(sel.Lines))
	}
	if containsLine(sel.Lines, "cluster B") {
		t.Errorf("Select() window includes the sparser landmark cluster over the densest one")
	}
}

func TestSelectWithoutTimestamps(t *testing.T) {
	lines := []string{"Run make test", "ok", "Failed tests:", "Error: boom", "Execution halted"}
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("filler %d", i))
	}

	sel := Select(lines, 20)
	if sel.Mode != ModeLandmark {
		t.Fatalf("Select() mode = %v, want ModeLandmark", sel.Mode)
	}
	if !containsLine(sel.Lines, "Error: boom") {
		t.Errorf("Select() window missing failure without timestamps: %q", join(sel.Lines))
	}
}

func TestSelectEmptyLog(t *testing.T) {
	sel := Select(nil, 100)
	if len(sel.Lines) != 0 || sel.Mode != ModeFull {
		t.Errorf("Select(nil) = %+v, want empty ModeFull", sel)
	}
}

func TestIsPostStepName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Post Run actions/checkout@v4", true},
		{"post job cleanup", true},
		{"Post: save cache", true},
		{"Run actions/upload-artifact@v4", true},
		{"Upload artifacts", true},
		{"Run R CMD check", false},
		{"Set up job", false},
		{"Posterize the build", false},
	}
	for _, tt := range tests {
		if got := isPostStepName(tt.name); got != tt.want {
			t.Errorf("isPostStepName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestStepGroups(t *testing.T) {
	lines := []string{
		ts("##[group]Run actions/checkout@v4"),
		ts("##[endgroup]"),
		ts("checkout output"),
		ts("##[group]Run R CMD check"),
		ts("##[endgroup]"),
		ts("check output"),
	}
	groups := stepGroups(lines)
	if len(groups) != 2 {
		t.Fatalf("stepGroups() returned %d groups, want 2: %+v", len(groups), groups)
	}
	if groups[0].name != "Run actions/checkout@v4" || groups[0].start != 0 || groups[0].end != 3 {
		t.Errorf("stepGroups()[0] = %+v, want name Run actions/checkout@v4, [0,3)", groups[0])
	}
	if groups[1].name != "Run R CMD check" || groups[1].start != 3 || groups[1].end != len(lines) {
		t.Errorf("stepGroups()[1] = %+v, want name Run R CMD check, [3,%d)", groups[1], len(lines))
	}
}

func TestFindLandmarks(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		{ts("##[error]Process completed with exit code 1."), true},
		{ts("##[error]── Failure (test-x:12) ──"), true},
		{ts("Failed tests:"), true},
		{ts("Error: object 'foo' not found"), true},
		{ts("Execution halted"), true},
		{ts("##[warning]Something suspicious"), false},
		{ts("No errors here"), false},
		{ts("  Error: indented"), false},
	}
	for _, tt := range tests {
		if got := findLandmarks([]string{tt.line}); (len(got) > 0) != tt.want {
			t.Errorf("findLandmarks(%q) matched = %v, want %v", tt.line, len(got) > 0, tt.want)
		}
	}
}

func TestWindowAroundGivesLeadingContext(t *testing.T) {
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	landmark := 200
	lines[landmark] = "##[error]boom"

	got := windowAround(lines, []int{landmark}, 100)
	if len(got) != 100 {
		t.Fatalf("windowAround() returned %d lines, want 100", len(got))
	}
	first, _ := strconv.Atoi(strings.TrimPrefix(got[0], "line "))
	if first != landmark-25 {
		t.Errorf("windowAround() starts at line %d, want %d (25 lines of leading context)", first, landmark-25)
	}
	if !containsLine(got, "##[error]boom") {
		t.Errorf("windowAround() window missing the landmark line")
	}
}

func TestSelectWindowUsesFullBudgetAtLogEnd(t *testing.T) {
	// A Go/JS-style failure: the only landmark is the trailing ##[error]
	// annotation, so the window must extend backwards to fill the budget
	// instead of stopping at the end of the log.
	lines := []string{ts("##[group]Run go test"), ts("##[endgroup]")}
	for i := 0; i < 198; i++ {
		lines = append(lines, ts(fmt.Sprintf("test output %d", i)))
	}
	lines = append(lines, ts("##[error]Process completed with exit code 1."))

	sel := Select(lines, 100)
	if sel.Mode != ModeLandmark {
		t.Fatalf("Select() mode = %v, want ModeLandmark", sel.Mode)
	}
	if len(sel.Lines) != 100 {
		t.Errorf("Select() returned %d lines, want the full 100-line budget", len(sel.Lines))
	}
	if !containsLine(sel.Lines, "test output 105") {
		t.Errorf("Select() window missing failure context above the annotation")
	}
}

func TestSelectReportsDroppedPostSteps(t *testing.T) {
	// Dropping post-job steps brings this log within n lines: the caller
	// must be told lines were omitted even though the mode is ModeFull.
	lines := testthatFailureLog(60)

	sel := Select(lines, 200)
	if sel.Mode != ModeFull {
		t.Fatalf("Select() mode = %v, want ModeFull", sel.Mode)
	}
	if !sel.Dropped {
		t.Error("Select() Dropped = false, want true after post-job steps were omitted")
	}
	if containsLine(sel.Lines, "Cleaning up") {
		t.Error("Select() includes post-job lines")
	}

	sel = Select(testthatFailureLog(3), 200)
	if sel.Dropped {
		t.Error("Select() Dropped = true for a log that fits without filtering, want false")
	}
}
