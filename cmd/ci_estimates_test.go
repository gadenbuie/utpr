package cmd

import (
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/ui"
)

// runningElapsedRE matches a bare elapsed duration, e.g. "30s".
var runningElapsedRE = regexp.MustCompile(`^\d+s$`)

// estimateTestRun builds a check run in the given check suite.
func estimateTestRun(suite int64, name string) gh.CheckRun {
	r := gh.CheckRun{ID: suite*100 + 1, Name: name, Status: "queued"}
	r.App.Slug = "github-actions"
	r.CheckSuite.ID = suite
	return r
}

// estimateTestJob builds a successful job with the given duration.
func estimateTestJob(name string, start time.Time, d time.Duration) gh.WorkflowJob {
	return gh.WorkflowJob{
		Name:        name,
		Status:      "completed",
		Conclusion:  "success",
		StartedAt:   start.Format(time.RFC3339),
		CompletedAt: start.Add(d).Format(time.RFC3339),
	}
}

func TestMedianDuration(t *testing.T) {
	tests := []struct {
		name string
		in   []time.Duration
		want time.Duration
	}{
		{"empty", nil, 0},
		{"single", []time.Duration{time.Minute}, time.Minute},
		{"odd", []time.Duration{time.Minute, 2 * time.Minute, 10 * time.Minute}, 2 * time.Minute},
		{"even averages middle two", []time.Duration{time.Minute, 2 * time.Minute, 3 * time.Minute, 10 * time.Minute}, 2*time.Minute + 30*time.Second},
		{"unsorted input", []time.Duration{10 * time.Minute, time.Minute, 2 * time.Minute}, 2 * time.Minute},
	}
	for _, tt := range tests {
		if got := medianDuration(tt.in); got != tt.want {
			t.Errorf("medianDuration(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
	// The input slice must not be reordered in place.
	in := []time.Duration{3 * time.Minute, time.Minute}
	_ = medianDuration(in)
	if in[0] != 3*time.Minute || in[1] != time.Minute {
		t.Errorf("medianDuration modified its input: %v", in)
	}
}

func TestEstimateCache(t *testing.T) {
	oldRuns, oldJobs := ghListWorkflowRunsForWorkflow, ghListWorkflowRunJobs
	t.Cleanup(func() {
		ghListWorkflowRunsForWorkflow, ghListWorkflowRunJobs = oldRuns, oldJobs
	})

	base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	// Three successful historical runs; the "build" job took 60s, 120s,
	// and 240s, so its median is 120s. "lint" only ran once (100s). One
	// historical run failed a job and must be ignored.
	ghListWorkflowRunsForWorkflow = func(ownerRepo string, workflowID int64, limit int, status string) ([]gh.WorkflowRun, error) {
		if status != "success" {
			t.Errorf("history fetched with status %q, want %q", status, "success")
		}
		if limit != estimateSampleRuns {
			t.Errorf("history fetched with limit %d, want %d", limit, estimateSampleRuns)
		}
		var runs []gh.WorkflowRun
		for i := 0; i < 3; i++ {
			runs = append(runs, gh.WorkflowRun{ID: int64(1000 + i), Status: "completed", Conclusion: "success"})
		}
		return runs, nil
	}
	ghListWorkflowRunJobs = func(ownerRepo string, runID int64) ([]gh.WorkflowJob, error) {
		start := base.Add(time.Duration(runID) * time.Minute)
		jobs := []gh.WorkflowJob{
			estimateTestJob("build", start, time.Duration(runID-999)*time.Minute),
			estimateTestJob("lint", start, 100*time.Second),
		}
		if runID == 1001 {
			jobs = append(jobs, gh.WorkflowJob{Name: "flaky", Conclusion: "failure",
				StartedAt: start.Format(time.RFC3339), CompletedAt: start.Add(time.Hour).Format(time.RFC3339)})
		}
		return jobs, nil
	}

	wf := gh.WorkflowRun{ID: 42, WorkflowID: 555, CheckSuiteID: 77}
	c := newEstimateCache()
	c.ensure("owner/repo", []gh.WorkflowRun{wf})
	// ensure must not re-fetch a workflow it has already started.
	c.ensure("owner/repo", []gh.WorkflowRun{wf})

	select {
	case <-c.readyChan():
	case <-time.After(5 * time.Second):
		t.Fatal("estimates never became ready")
	}

	est := c.snapshot()
	jobs, ok := est[77]
	if !ok {
		t.Fatalf("snapshot() has no entry for check suite 77: %v", est)
	}
	if got, want := jobs["build"], 2*time.Minute; got != want {
		t.Errorf("build estimate = %v, want %v", got, want)
	}
	if got, want := jobs["lint"], 100*time.Second; got != want {
		t.Errorf("lint estimate = %v, want %v", got, want)
	}
	if _, ok := jobs["flaky"]; ok {
		t.Error("flaky estimate recorded from a failed job, want ignored")
	}

	// A check run in another suite, or with an unknown job name, has no
	// estimate.
	if _, ok := estimateFor(est, estimateTestRun(78, "build")); ok {
		t.Error("estimateFor matched a check run from an unknown suite")
	}
	if _, ok := estimateFor(est, estimateTestRun(77, "deploy")); ok {
		t.Error("estimateFor matched an unknown job name")
	}
}

func TestEstimateCacheSharesHistoryAcrossSuites(t *testing.T) {
	for _, discovery := range []string{"same poll", "while loading", "after loading"} {
		t.Run(discovery, func(t *testing.T) {
			oldRuns, oldJobs := ghListWorkflowRunsForWorkflow, ghListWorkflowRunJobs
			t.Cleanup(func() {
				ghListWorkflowRunsForWorkflow, ghListWorkflowRunJobs = oldRuns, oldJobs
			})

			started := make(chan struct{})
			release := make(chan struct{})
			var historyCalls atomic.Int32
			ghListWorkflowRunsForWorkflow = func(ownerRepo string, workflowID int64, limit int, status string) ([]gh.WorkflowRun, error) {
				if historyCalls.Add(1) == 1 {
					close(started)
				}
				<-release
				return []gh.WorkflowRun{{ID: 1000}}, nil
			}
			ghListWorkflowRunJobs = func(ownerRepo string, runID int64) ([]gh.WorkflowJob, error) {
				base := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
				return []gh.WorkflowJob{estimateTestJob("build", base, 2*time.Minute)}, nil
			}

			first := gh.WorkflowRun{ID: 42, WorkflowID: 555, CheckSuiteID: 77}
			second := gh.WorkflowRun{ID: 43, WorkflowID: 555, CheckSuiteID: 78}
			initial := []gh.WorkflowRun{first}
			if discovery == "same poll" {
				initial = append(initial, second)
			}
			c := newEstimateCache()
			ready := c.readyChan()
			c.ensure("owner/repo", initial)
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				close(release)
				t.Fatal("history fetch never started")
			}
			if discovery == "while loading" {
				c.ensure("owner/repo", []gh.WorkflowRun{second})
			}
			close(release)
			select {
			case <-ready:
			case <-time.After(5 * time.Second):
				t.Fatal("estimates never became ready")
			}
			if discovery == "after loading" {
				c.ensure("owner/repo", []gh.WorkflowRun{second})
			}

			est := c.snapshot()
			for _, suite := range []int64{77, 78} {
				d, ok := estimateFor(est, estimateTestRun(suite, "build"))
				if !ok || d != 2*time.Minute {
					t.Errorf("suite %d build estimate = %v, %v; want 2m, true", suite, d, ok)
				}
			}
			if calls := historyCalls.Load(); calls != 1 {
				t.Errorf("history fetched %d times, want once for the shared workflow", calls)
			}
		})
	}
}

func TestEstimateCacheFetchErrorsSilent(t *testing.T) {
	oldRuns, oldJobs := ghListWorkflowRunsForWorkflow, ghListWorkflowRunJobs
	t.Cleanup(func() {
		ghListWorkflowRunsForWorkflow, ghListWorkflowRunJobs = oldRuns, oldJobs
	})

	var jobsCalls int32
	ghListWorkflowRunsForWorkflow = func(ownerRepo string, workflowID int64, limit int, status string) ([]gh.WorkflowRun, error) {
		return []gh.WorkflowRun{{ID: 1}, {ID: 2}}, nil
	}
	ghListWorkflowRunJobs = func(ownerRepo string, runID int64) ([]gh.WorkflowJob, error) {
		atomic.AddInt32(&jobsCalls, 1)
		return nil, errNoChecksFound
	}

	c := newEstimateCache()
	c.ensure("owner/repo", []gh.WorkflowRun{{ID: 9, WorkflowID: 1, CheckSuiteID: 5}})
	// Fetch failures must not close the ready channel or record anything.
	if ch := c.readyChan(); !isBlocking(ch) {
		t.Error("ready channel closed after fetch error, want still open")
	}
	// Give the background fetches a moment, then re-check.
	time.Sleep(50 * time.Millisecond)
	if ch := c.readyChan(); !isBlocking(ch) {
		t.Error("ready channel closed after fetch error, want still open")
	}
	if est := c.snapshot(); len(est) != 0 {
		t.Errorf("snapshot() = %v after fetch errors, want empty", est)
	}
	if n := atomic.LoadInt32(&jobsCalls); n != 2 {
		t.Errorf("job fetches = %d, want 2 (one per historical run)", n)
	}
}

// isBlocking reports whether reading from ch would block right now.
func isBlocking(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return false
	default:
		return true
	}
}

func TestFormatCheckDurationWithEstimate(t *testing.T) {
	now := time.Now()

	// Queued with estimate: bare estimated duration.
	queued := estimateTestRun(7, "build")
	queued.Status = "queued"
	dur, suffix := formatCheckDuration(queued, 4*time.Minute, true)
	if suffix != "" {
		t.Errorf("formatCheckDuration(queued, est) suffix = %q, want empty", suffix)
	}
	if ui.StripANSI(dur) != "est 4m 0s" {
		t.Errorf("formatCheckDuration(queued, est) dur = %q, want est 4m 0s", ui.StripANSI(dur))
	}

	// Running with estimate: elapsed cell plus remaining countdown suffix.
	running := estimateTestRun(7, "build")
	running.Status = "in_progress"
	running.StartedAt = now.Add(-30 * time.Second).Format(time.RFC3339)
	dur, suffix = formatCheckDuration(running, 4*time.Minute, true)
	if !runningElapsedRE.MatchString(ui.StripANSI(dur)) {
		t.Errorf("formatCheckDuration(running, est) dur = %q, want a bare elapsed duration like 30s", ui.StripANSI(dur))
	}
	if !strings.HasPrefix(ui.StripANSI(suffix), "(est ") || !strings.HasSuffix(ui.StripANSI(suffix), " remaining)") {
		t.Errorf("formatCheckDuration(running, est) suffix = %q, want (est Ns remaining)", ui.StripANSI(suffix))
	}

	// Running without estimate: bare elapsed, no suffix.
	dur, suffix = formatCheckDuration(running, 0, false)
	if suffix != "" {
		t.Errorf("formatCheckDuration(running, no est) suffix = %q, want empty", suffix)
	}
	if !runningElapsedRE.MatchString(ui.StripANSI(dur)) {
		t.Errorf("formatCheckDuration(running, no est) dur = %q, want a bare elapsed duration like 30s", ui.StripANSI(dur))
	}

	// Running past the estimate: no countdown, estimated total still shown.
	past := estimateTestRun(7, "build")
	past.Status = "in_progress"
	past.StartedAt = now.Add(-10 * time.Minute).Format(time.RFC3339)
	_, suffix = formatCheckDuration(past, 4*time.Minute, true)
	if ui.StripANSI(suffix) != "(est 4m 0s)" {
		t.Errorf("formatCheckDuration(past est) suffix = %q, want (est 4m 0s)", ui.StripANSI(suffix))
	}

	// Completed runs ignore estimates.
	done := estimateTestRun(7, "build")
	done.Status = "completed"
	done.Conclusion = "success"
	done.StartedAt = now.Add(-5 * time.Minute).Format(time.RFC3339)
	done.CompletedAt = now.Add(-1 * time.Minute).Format(time.RFC3339)
	dur, suffix = formatCheckDuration(done, 9*time.Minute, true)
	if suffix != "" {
		t.Errorf("formatCheckDuration(completed, est) suffix = %q, want empty", suffix)
	}
	if ui.StripANSI(dur) != "4m  0s" {
		t.Errorf("formatCheckDuration(completed, est) dur = %q, want 4m  0s", ui.StripANSI(dur))
	}
}

func TestRenderCheckRunsAlignsDurations(t *testing.T) {
	now := time.Now()
	completed := estimateTestRun(7, "py-test")
	completed.Status = "completed"
	completed.Conclusion = "success"
	completed.StartedAt = now.Add(-49 * time.Second).Format(time.RFC3339)
	completed.CompletedAt = now.Format(time.RFC3339)

	// Long-running job with a countdown: its duration string is the
	// widest in the group.
	running := estimateTestRun(7, "py-playwright")
	running.Status = "in_progress"
	running.StartedAt = now.Add(-2*time.Minute - 57*time.Second).Format(time.RFC3339)
	est := map[int64]map[string]time.Duration{7: {"py-playwright": 7 * time.Minute}}

	got := renderCheckRunsPlain(
		[]gh.CheckRun{completed, running},
		map[int64]string{7: "CI"}, false, nil, est)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	var jobLines []string
	for _, line := range lines {
		if strings.Contains(line, "py-") {
			jobLines = append(jobLines, line)
		}
	}
	if len(jobLines) != 2 {
		t.Fatalf("renderCheckRunsPlain() job lines = %v, want 2", jobLines)
	}
	completedLine, runningLine := jobLines[0], jobLines[1]
	// The completed row ends at its duration cell; the running row's cell
	// ends where its " (est" suffix begins. Both must end at the same
	// column so the unit digits line up.
	suffixAt := strings.Index(runningLine, " (est ")
	if suffixAt < 0 {
		t.Fatalf("running row %q has no estimate suffix", runningLine)
	}
	if endCol := len(strings.TrimRight(completedLine, " ")); endCol != suffixAt {
		t.Errorf("duration cells not aligned: completed ends at column %d, running cell ends at %d\n%q\n%q",
			endCol, suffixAt, completedLine, runningLine)
	}
	for _, line := range jobLines {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("job line has trailing whitespace: %q", line)
		}
	}

	// A queued job with no estimate renders a blank duration cell rather
	// than padding-only trailing spaces.
	queued := estimateTestRun(7, "py-pending")
	got = renderCheckRunsPlain(
		[]gh.CheckRun{completed, queued},
		map[int64]string{7: "CI"}, false, nil, nil)
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.Contains(line, "py-pending") && strings.TrimRight(line, " ") != line {
			t.Errorf("queued row has trailing whitespace: %q", line)
		}
	}
}

func TestRenderCheckRunsWithEstimates(t *testing.T) {
	running := estimateTestRun(7, "build")
	running.Status = "in_progress"
	running.StartedAt = time.Now().Add(-30 * time.Second).Format(time.RFC3339)

	// Without estimates: plain running line.
	got := renderCheckRunsPlain([]gh.CheckRun{running}, map[int64]string{7: "CI"}, false, nil, nil)
	if strings.Contains(got, "(est ") {
		t.Errorf("renderCheckRunsPlain(no estimates) = %q, want no estimate text", got)
	}

	// With estimates: the line shows the remaining countdown.
	est := map[int64]map[string]time.Duration{7: {"build": 4 * time.Minute}}
	got = renderCheckRunsPlain([]gh.CheckRun{running}, map[int64]string{7: "CI"}, false, nil, est)
	if !strings.Contains(got, " remaining)") {
		t.Errorf("renderCheckRunsPlain(estimates) = %q, missing remaining countdown", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Errorf("renderCheckRunsPlain(estimates) = %q, contains ANSI escape codes", got)
	}
}
