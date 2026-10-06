package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
)

// reasonSeam swaps the GitHub API function vars for fakes that record
// calls, restoring them on cleanup.
type reasonSeam struct {
	checkRuns   [][]gh.CheckRun // one entry per GetCheckRuns call
	annotations map[int64][]gh.CheckRunAnnotation
	wfRuns      []gh.WorkflowRun
	jobs        map[int64][]gh.WorkflowJob
	logs        map[int64]string

	calls struct {
		getCheckRuns            int
		listWorkflowRunsForSHA  int
		listCheckRunAnnotations int
		listWorkflowRunJobs     int
		getJobLogs              []int64
	}
}

func withReasonSeam(t *testing.T, s *reasonSeam) {
	t.Helper()
	s.annotations = map[int64][]gh.CheckRunAnnotation{}
	s.jobs = map[int64][]gh.WorkflowJob{}
	s.logs = map[int64]string{}

	oldGet, oldRuns, oldAnns := ghGetCheckRuns, ghListWorkflowRunsForSHA, ghListCheckRunAnnotations
	oldJobs, oldLogs := ghListWorkflowRunJobs, ghGetJobLogs
	oldPoll := ciPollInterval

	ghGetCheckRuns = func(ownerRepo, sha string) ([]gh.CheckRun, error) {
		s.calls.getCheckRuns++
		if s.calls.getCheckRuns > len(s.checkRuns) {
			t.Fatalf("unexpected GetCheckRuns call %d (scripted %d)", s.calls.getCheckRuns, len(s.checkRuns))
		}
		return s.checkRuns[s.calls.getCheckRuns-1], nil
	}
	ghListWorkflowRunsForSHA = func(ownerRepo, sha string) ([]gh.WorkflowRun, error) {
		s.calls.listWorkflowRunsForSHA++
		return s.wfRuns, nil
	}
	ghListCheckRunAnnotations = func(ownerRepo string, checkRunID int64) ([]gh.CheckRunAnnotation, error) {
		s.calls.listCheckRunAnnotations++
		return s.annotations[checkRunID], nil
	}
	ghListWorkflowRunJobs = func(ownerRepo string, runID int64) ([]gh.WorkflowJob, error) {
		s.calls.listWorkflowRunJobs++
		return s.jobs[runID], nil
	}
	ghGetJobLogs = func(ownerRepo string, jobID int64) (string, error) {
		s.calls.getJobLogs = append(s.calls.getJobLogs, jobID)
		return s.logs[jobID], nil
	}
	ciPollInterval = time.Millisecond

	t.Cleanup(func() {
		ghGetCheckRuns, ghListWorkflowRunsForSHA, ghListCheckRunAnnotations = oldGet, oldRuns, oldAnns
		ghListWorkflowRunJobs, ghGetJobLogs = oldJobs, oldLogs
		ciPollInterval = oldPoll
	})
}

// withNoReasonsFlag sets flagCINoReasons for the test's duration.
func withNoReasonsFlag(t *testing.T, noReasons bool) {
	t.Helper()
	old := flagCINoReasons
	flagCINoReasons = noReasons
	t.Cleanup(func() { flagCINoReasons = old })
}

func reasonCheckRun(id int64, name, conclusion string) gh.CheckRun {
	r := gh.CheckRun{
		ID:         id,
		Name:       name,
		Status:     "completed",
		Conclusion: conclusion,
		ExternalID: "0", // overwritten per test
	}
	r.App.Slug = "github-actions"
	r.CheckSuite.ID = 100
	return r
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// what it wrote. In tests stdout is never a TTY, so CI output lands there
// via agent mode.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out)
}

func TestInformativeAnnotation(t *testing.T) {
	tests := []struct {
		name string
		anns []gh.CheckRunAnnotation
		want string
	}{
		{
			name: "informative failure annotation",
			anns: []gh.CheckRunAnnotation{
				{AnnotationLevel: "failure", Message: "Testing error: `x` is not TRUE"},
			},
			want: "Testing error: `x` is not TRUE",
		},
		{
			name: "generic exit code only",
			anns: []gh.CheckRunAnnotation{
				{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
			},
			want: "",
		},
		{
			name: "generic exit code without period",
			anns: []gh.CheckRunAnnotation{
				{AnnotationLevel: "failure", Message: "Process completed with exit code 137"},
			},
			want: "",
		},
		{
			name: "generic exit code mixed case",
			anns: []gh.CheckRunAnnotation{
				{AnnotationLevel: "failure", Message: "process completed with exit code 1."},
			},
			want: "",
		},
		{
			name: "generic annotation after informative one",
			anns: []gh.CheckRunAnnotation{
				{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
				{AnnotationLevel: "failure", Message: "File not found: 'a.yml'"},
			},
			want: "File not found: 'a.yml'",
		},
		{
			name: "warning annotations are not failure reasons",
			anns: []gh.CheckRunAnnotation{
				{AnnotationLevel: "warning", Message: "deprecation notice"},
			},
			want: "",
		},
		{
			name: "no annotations",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := informativeAnnotation(tt.anns); got != tt.want {
				t.Errorf("informativeAnnotation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeReason(t *testing.T) {
	if got := normalizeReason("  Error:   object\n'x' not found  "); got != "Error: object 'x' not found" {
		t.Errorf("normalizeReason() = %q, want collapsed whitespace", got)
	}

	long := strings.Repeat("x", 300)
	if got := normalizeReason(long); len([]rune(got)) != reasonLineMaxRunes {
		t.Errorf("normalizeReason() length = %d, want %d", len([]rune(got)), reasonLineMaxRunes)
	}
	if !strings.HasSuffix(normalizeReason(long), "…") {
		t.Error("normalizeReason() should mark truncation with …")
	}
}

func TestFetchCheckRunReasonsAnnotationOnly(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)

	run := reasonCheckRun(7, "CI / build", "failure")
	s.checkRuns = [][]gh.CheckRun{{run}}
	s.annotations[7] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "Build failed: undefined symbol 'foo'"},
	}

	reasons := fetchCheckRunReasons("o/r", []gh.CheckRun{run}, nil)
	if reasons[7] != "Build failed: undefined symbol 'foo'" {
		t.Errorf("reason = %q, want the annotation message", reasons[7])
	}
	if len(s.calls.getJobLogs) != 0 {
		t.Errorf("job logs fetched %d times, want 0 when the annotation is informative", len(s.calls.getJobLogs))
	}
	if s.calls.listWorkflowRunJobs != 0 {
		t.Errorf("jobs listed %d times, want 0", s.calls.listWorkflowRunJobs)
	}
}

func TestFetchCheckRunReasonsLogFallback(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)

	run := reasonCheckRun(7, "CI / build", "failure")
	run.ExternalID = "99"
	s.checkRuns = [][]gh.CheckRun{{run}}
	s.annotations[7] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
	}
	s.logs[99] = "2026-07-01T10:00:00.0000000Z ##[group]Run tests\n" +
		"2026-07-01T10:00:01.0000000Z Error: object 'foo' not found\n" +
		"2026-07-01T10:00:02.0000000Z Execution halted\n" +
		"2026-07-01T10:00:03.0000000Z ##[error]Process completed with exit code 1.\n"

	reasons := fetchCheckRunReasons("o/r", []gh.CheckRun{run}, nil)
	if reasons[7] != "Error: object 'foo' not found" {
		t.Errorf("reason = %q, want the log landmark", reasons[7])
	}
	if len(s.calls.getJobLogs) != 1 || s.calls.getJobLogs[0] != 99 {
		t.Errorf("job logs fetched = %v, want exactly [99]", s.calls.getJobLogs)
	}
}

func TestFetchCheckRunReasonsSuiteMapping(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)

	run := reasonCheckRun(7, "CI / build", "failure")
	run.ExternalID = "" // fall back to suite mapping
	s.checkRuns = [][]gh.CheckRun{{run}}
	s.annotations[7] = nil
	s.wfRuns = []gh.WorkflowRun{{ID: 500, CheckSuiteID: 100}}
	s.jobs[500] = []gh.WorkflowJob{
		{ID: 1, RunID: 500, Name: "check", Status: "completed", Conclusion: "success"},
		{ID: 99, RunID: 500, Name: "build", Status: "completed", Conclusion: "failure"},
	}
	s.logs[99] = "Error: object 'foo' not found\n"

	reasons := fetchCheckRunReasons("o/r", []gh.CheckRun{run}, s.wfRuns)
	if reasons[7] != "Error: object 'foo' not found" {
		t.Errorf("reason = %q, want the log landmark via job name match", reasons[7])
	}
	if s.calls.listWorkflowRunJobs != 1 {
		t.Errorf("jobs listed %d times, want 1", s.calls.listWorkflowRunJobs)
	}
}

func TestFetchCheckRunReasonsSkipsNonActionsAndUnfailed(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)

	failed := reasonCheckRun(7, "CI / build", "failure")
	failed.App.Slug = "some-app" // not GitHub Actions: no job to map
	failed.ExternalID = ""
	ok := reasonCheckRun(8, "CI / lint", "success")

	reasons := fetchCheckRunReasons("o/r", []gh.CheckRun{failed, ok}, nil)
	if len(reasons) != 0 {
		t.Errorf("reasons = %v, want none for a non-Actions failed check", reasons)
	}
	if s.calls.listCheckRunAnnotations != 1 {
		t.Errorf("annotations fetched %d times, want 1 (failed run only)", s.calls.listCheckRunAnnotations)
	}
}

func TestRenderCheckRunsWithReasons(t *testing.T) {
	failed := reasonCheckRun(7, "CI / build", "failure")
	passed := reasonCheckRun(8, "CI / lint", "success")
	runs := []gh.CheckRun{failed, passed}

	got := renderCheckRunsPlain(runs, nil, false, map[int64]string{7: "Error: object 'foo' not found"})
	if strings.Contains(got, "\x1b[") {
		t.Errorf("renderCheckRunsPlain() contains ANSI escape codes: %q", got)
	}
	buildLine := -1
	reasonLine := -1
	for i, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.Contains(line, "build") {
			buildLine = i
		}
		if strings.Contains(line, "↳ Error: object 'foo' not found") {
			reasonLine = i
		}
	}
	if buildLine == -1 {
		t.Fatalf("renderCheckRunsPlain() = %q, missing build line", got)
	}
	if reasonLine != buildLine+1 {
		t.Errorf("reason line at %d, want it directly under the failed job at %d:\n%s", reasonLine, buildLine, got)
	}
	if !strings.HasPrefix(strings.Split(strings.TrimRight(got, "\n"), "\n")[reasonLine], "      ") {
		t.Errorf("reason line should be indented under the job: %q", got)
	}
	if strings.Count(got, "↳") != 1 {
		t.Errorf("successful checks must not render a reason: %q", got)
	}

	// Nil reasons must render identically to the pre-feature layout.
	if withNone := renderCheckRunsPlain(runs, nil, false, nil); strings.Contains(withNone, "↳") {
		t.Errorf("renderCheckRunsPlain(nil reasons) = %q, want no reason lines", withNone)
	}
}

func TestShowCIChecksNoReasonsNoExtraCalls(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)
	withNoReasonsFlag(t, true)

	failed := reasonCheckRun(7, "CI / build", "failure")
	failed.ExternalID = "99"
	passed := reasonCheckRun(8, "CI / lint", "success")
	s.checkRuns = [][]gh.CheckRun{{passed, failed}}
	s.annotations[7] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
	}

	if _, err := showCIChecks("o/r", "b", "sha"); err != nil {
		t.Fatalf("showCIChecks() = %v", err)
	}
	if s.calls.listCheckRunAnnotations != 0 {
		t.Errorf("annotations fetched %d times with --no-reasons, want 0", s.calls.listCheckRunAnnotations)
	}
	if len(s.calls.getJobLogs) != 0 {
		t.Errorf("job logs fetched with --no-reasons, want 0")
	}
	if s.calls.listWorkflowRunJobs != 0 {
		t.Errorf("jobs listed with --no-reasons, want 0")
	}
	if s.calls.getCheckRuns != 1 || s.calls.listWorkflowRunsForSHA != 1 {
		t.Errorf("base calls = %d/%d, want one check runs call and one workflow runs call",
			s.calls.getCheckRuns, s.calls.listWorkflowRunsForSHA)
	}
}

func TestShowCIChecksReasonsBoundedToFailedJobs(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)
	withNoReasonsFlag(t, false)

	failed := reasonCheckRun(7, "CI / build", "failure")
	failed.ExternalID = "99"
	passed := reasonCheckRun(8, "CI / lint", "success")
	s.checkRuns = [][]gh.CheckRun{{passed, failed}}
	s.annotations[7] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
	}
	s.logs[99] = "Error: object 'foo' not found\n"

	if _, err := showCIChecks("o/r", "b", "sha"); err != nil {
		t.Fatalf("showCIChecks() = %v", err)
	}
	if s.calls.listCheckRunAnnotations != 1 {
		t.Errorf("annotations fetched %d times, want 1 (failed run only)", s.calls.listCheckRunAnnotations)
	}
	if len(s.calls.getJobLogs) != 1 {
		t.Errorf("job logs fetched %d times, want 1", len(s.calls.getJobLogs))
	}
	if s.calls.getCheckRuns != 1 || s.calls.listWorkflowRunsForSHA != 1 {
		t.Errorf("base calls = %d/%d, want one each", s.calls.getCheckRuns, s.calls.listWorkflowRunsForSHA)
	}
}

func TestWaitCIReasonsCallBudget(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)
	withNoReasonsFlag(t, false)

	passed := reasonCheckRun(8, "CI / lint", "success")
	failed := reasonCheckRun(7, "CI / build", "failure")
	failed.ExternalID = "99"

	running := reasonCheckRun(7, "CI / build", "")
	running.Status = "in_progress"
	s.checkRuns = [][]gh.CheckRun{{passed, running}, {passed, failed}}
	s.annotations[7] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
	}
	s.logs[99] = "Error: object 'foo' not found\n"

	var waitErr error
	out := captureStdout(t, func() {
		waitErr = waitCI("o/r", "sha", "all", false)
	})
	if waitErr == nil || waitErr.Error() != "CI checks failed" {
		t.Fatalf("waitCI() = %v, want CI checks failed", waitErr)
	}
	if !strings.Contains(out, "↳ Error: object 'foo' not found") {
		t.Errorf("wait output missing the inline reason:\n%s", out)
	}

	// The loop's existing cadence: one GetCheckRuns per poll, two polls.
	if s.calls.getCheckRuns != 2 {
		t.Errorf("GetCheckRuns called %d times, want 2 (one per poll)", s.calls.getCheckRuns)
	}
	// Annotations collected during polls, at most once per failed run.
	if s.calls.listCheckRunAnnotations != 1 {
		t.Errorf("annotations fetched %d times, want 1 (once for the failed run)", s.calls.listCheckRunAnnotations)
	}
	// Log fetched once at completion; suite mapping not needed thanks to
	// external_id, so no extra workflow-runs or jobs calls.
	if len(s.calls.getJobLogs) != 1 || s.calls.getJobLogs[0] != 99 {
		t.Errorf("job logs fetched = %v, want exactly [99] once", s.calls.getJobLogs)
	}
	if s.calls.listWorkflowRunsForSHA != 0 {
		t.Errorf("workflow runs fetched %d times in compact mode, want 0", s.calls.listWorkflowRunsForSHA)
	}
	if s.calls.listWorkflowRunJobs != 0 {
		t.Errorf("jobs listed %d times, want 0", s.calls.listWorkflowRunJobs)
	}
}

func TestWaitCINoReasonsCallBudget(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)
	withNoReasonsFlag(t, true)

	passed := reasonCheckRun(8, "CI / lint", "success")
	failed := reasonCheckRun(7, "CI / build", "failure")
	failed.ExternalID = "99"
	s.checkRuns = [][]gh.CheckRun{{passed, failed}}
	s.annotations[7] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
	}
	s.logs[99] = "Error: object 'foo' not found\n"

	var waitErr error
	out := captureStdout(t, func() {
		waitErr = waitCI("o/r", "sha", "all", false)
	})
	if waitErr == nil || waitErr.Error() != "CI checks failed" {
		t.Fatalf("waitCI() = %v, want CI checks failed", waitErr)
	}
	if s.calls.getCheckRuns != 1 {
		t.Errorf("GetCheckRuns called %d times, want 1", s.calls.getCheckRuns)
	}
	if s.calls.listCheckRunAnnotations != 0 || len(s.calls.getJobLogs) != 0 ||
		s.calls.listWorkflowRunJobs != 0 || s.calls.listWorkflowRunsForSHA != 0 {
		t.Errorf("--no-reasons made extra calls: %+v", s.calls)
	}
	if strings.Contains(out, "●") || strings.Contains(out, "↳") {
		t.Errorf("--no-reasons wait output grew a grouped listing:\n%s", out)
	}
}

func TestWaitCIReasonsCallBudgetFullDisplay(t *testing.T) {
	s := &reasonSeam{}
	withReasonSeam(t, s)
	withNoReasonsFlag(t, false)

	passed := reasonCheckRun(8, "CI / lint", "success")
	failed := reasonCheckRun(7, "CI / build", "failure")
	failed.ExternalID = "99"

	running := reasonCheckRun(7, "CI / build", "")
	running.Status = "in_progress"
	s.checkRuns = [][]gh.CheckRun{{passed, running}, {passed, failed}}
	s.annotations[7] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "Process completed with exit code 1."},
	}
	s.logs[99] = "Error: object 'foo' not found\n"

	if err := waitCI("o/r", "sha", "all", true); err == nil {
		t.Fatal("waitCI() = nil, want CI checks failed")
	}
	// Watch mode already polls ListWorkflowRunsForSHA each frame; reasons
	// add no calls beyond it and the single log fetch.
	if s.calls.getCheckRuns != 2 || s.calls.listWorkflowRunsForSHA != 2 {
		t.Errorf("poll calls = %d/%d, want 2 each", s.calls.getCheckRuns, s.calls.listWorkflowRunsForSHA)
	}
	if s.calls.listCheckRunAnnotations != 1 {
		t.Errorf("annotations fetched %d times, want 1", s.calls.listCheckRunAnnotations)
	}
	if len(s.calls.getJobLogs) != 1 {
		t.Errorf("job logs fetched %d times, want 1 at completion", len(s.calls.getJobLogs))
	}
	if s.calls.listWorkflowRunJobs != 0 {
		t.Errorf("jobs listed %d times, want 0", s.calls.listWorkflowRunJobs)
	}
}

func TestNoReasonsFlagRegistered(t *testing.T) {
	if flag := ciCmd.Flags().Lookup("no-reasons"); flag == nil {
		t.Error("ci command is missing the --no-reasons flag")
	}
}
