package cmd

import (
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
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
	got := formatCheckDuration(queued, 4*time.Minute, true)
	if got == "" || !strings.Contains(got, "est 4m 0s") {
		t.Errorf("formatCheckDuration(queued, est) = %q, want est 4m 0s", got)
	}

	// Running with estimate: elapsed plus remaining countdown.
	running := estimateTestRun(7, "build")
	running.Status = "in_progress"
	running.StartedAt = now.Add(-30 * time.Second).Format(time.RFC3339)
	got = formatCheckDuration(running, 4*time.Minute, true)
	if !strings.Contains(got, "s (est ") || !strings.Contains(got, " remaining)") {
		t.Errorf("formatCheckDuration(running, est) = %q, want elapsed + remaining countdown", got)
	}
	if strings.Contains(got, "running") {
		t.Errorf("formatCheckDuration(running, est) = %q, want no \"running\" word", got)
	}
	if strings.Contains(got, "est ~") {
		t.Errorf("formatCheckDuration(running, est) = %q, want no tilde prefix", got)
	}

	// Running without estimate: bare elapsed, still no "running" word.
	got = formatCheckDuration(running, 0, false)
	if !runningElapsedRE.MatchString(got) {
		t.Errorf("formatCheckDuration(running, no est) = %q, want a bare elapsed duration like 30s", got)
	}

	// Running past the estimate: no countdown, estimated total still shown.
	past := estimateTestRun(7, "build")
	past.Status = "in_progress"
	past.StartedAt = now.Add(-10 * time.Minute).Format(time.RFC3339)
	got = formatCheckDuration(past, 4*time.Minute, true)
	if strings.Contains(got, "remaining") {
		t.Errorf("formatCheckDuration(past est) = %q, want no remaining countdown", got)
	}
	if !strings.Contains(got, "est 4m 0s") {
		t.Errorf("formatCheckDuration(past est) = %q, want estimate still shown", got)
	}

	// Completed runs ignore estimates.
	done := estimateTestRun(7, "build")
	done.Status = "completed"
	done.Conclusion = "success"
	done.StartedAt = now.Add(-5 * time.Minute).Format(time.RFC3339)
	done.CompletedAt = now.Add(-1 * time.Minute).Format(time.RFC3339)
	got = formatCheckDuration(done, 9*time.Minute, true)
	if got != "4m 0s" {
		t.Errorf("formatCheckDuration(completed, est) = %q, want 4m 0s", got)
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
