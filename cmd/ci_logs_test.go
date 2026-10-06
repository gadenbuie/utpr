package cmd

import (
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/gh"
)

func TestCINextStepHints(t *testing.T) {
	tests := []struct {
		name         string
		failed       int
		showLogsHint bool
		noReasons    bool
		want         []string
	}{
		{
			name:         "failing with logs hint",
			failed:       2,
			showLogsHint: true,
			want:         []string{"2 failing — run 'utpr ci logs <ref> --failed'"},
		},
		{
			name:         "logs already shown via --logs",
			failed:       2,
			showLogsHint: false,
			want:         nil,
		},
		{
			name:         "reasons suppressed adds hint",
			failed:       1,
			showLogsHint: true,
			noReasons:    true,
			want: []string{
				"1 failing — run 'utpr ci logs <ref> --failed'",
				"failure reasons skipped (--no-reasons); rerun without it to show inline reasons",
			},
		},
		{
			name:         "no failures no hints",
			failed:       0,
			showLogsHint: true,
			noReasons:    true,
			want:         nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withNoReasonsFlag(t, tt.noReasons)
			got := ciNextStepHints(tt.failed, tt.showLogsHint)
			if len(got) != len(tt.want) {
				t.Fatalf("ciNextStepHints() = %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ciNextStepHints()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// oneShotSeam is a reasonSeam with fixtures for the one-shot --logs tests.
func oneShotSeam(t *testing.T) *reasonSeam {
	t.Helper()
	s := &reasonSeam{}
	withReasonSeam(t, s)
	s.wfRuns = []gh.WorkflowRun{
		{ID: 1, Name: "test", Status: "completed", Conclusion: "failure"},
		{ID: 2, Name: "lint", Status: "completed", Conclusion: "success"},
	}
	s.jobs = map[int64][]gh.WorkflowJob{
		1: {
			{ID: 11, Name: "test (linux)", Status: "completed", Conclusion: "failure"},
			{ID: 12, Name: "test (mac)", Status: "completed", Conclusion: "success"},
		},
		2: {
			{ID: 21, Name: "lint", Status: "completed", Conclusion: "success"},
		},
	}
	s.logs = map[int64]string{
		11: "step one\nError: tests failed\nExecution halted\n",
	}
	return s
}

func TestShowCILogsFailedFetchesFailedJobsOnly(t *testing.T) {
	s := oneShotSeam(t)

	out := captureStdout(t, func() {
		if err := showCILogsFailed("o/r", "sha", nil); err != nil {
			t.Errorf("showCILogsFailed() = %v", err)
		}
	})

	if len(s.calls.getJobLogs) != 1 || s.calls.getJobLogs[0] != 11 {
		t.Errorf("getJobLogs calls = %v, want only failed job 11", s.calls.getJobLogs)
	}
	for _, want := range []string{"## test / test (linux)", "Error: tests failed", "Execution halted"} {
		if !strings.Contains(out, want) {
			t.Errorf("showCILogsFailed() output missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "test (mac)") || strings.Contains(out, "lint") {
		t.Errorf("showCILogsFailed() rendered non-failed jobs:\n%s", out)
	}
}

func TestShowCILogsFailedNoFailedJobs(t *testing.T) {
	s := oneShotSeam(t)
	s.wfRuns = s.wfRuns[:1]
	s.jobs[1] = []gh.WorkflowJob{
		{ID: 12, Name: "test (mac)", Status: "completed", Conclusion: "success"},
	}

	out := captureStdout(t, func() {
		if err := showCILogsFailed("o/r", "sha", nil); err != nil {
			t.Errorf("showCILogsFailed() = %v", err)
		}
	})

	if !strings.Contains(out, "No failed jobs.") {
		t.Errorf("showCILogsFailed() output missing 'No failed jobs.'; got:\n%s", out)
	}
	if len(s.calls.getJobLogs) != 0 {
		t.Errorf("getJobLogs called for %v, want none", s.calls.getJobLogs)
	}
}

func TestShowCIChecksHint(t *testing.T) {
	newSeam := func(conclusions ...string) *reasonSeam {
		s := &reasonSeam{}
		runs := make([]gh.CheckRun, 0, len(conclusions))
		for i, c := range conclusions {
			runs = append(runs, reasonCheckRun(int64(100+i), "test "+c, c))
		}
		s.checkRuns = append(s.checkRuns, runs)
		withReasonSeam(t, s)
		return s
	}

	t.Run("failing status includes hint", func(t *testing.T) {
		newSeam("success", "failure")
		out := captureStdout(t, func() {
			failed, err := showCIChecks("o/r", "b", "sha")
			if err != nil {
				t.Errorf("showCIChecks() = %v", err)
			}
			if failed != 1 {
				t.Errorf("showCIChecks() failed = %d, want 1", failed)
			}
		})
		if !strings.Contains(out, "1 failing — run 'utpr ci logs <ref> --failed'") {
			t.Errorf("showCIChecks() output missing hint; got:\n%s", out)
		}
	})

	t.Run("--no-reasons adds reasons hint", func(t *testing.T) {
		newSeam("success", "failure")
		withNoReasonsFlag(t, true)
		out := captureStdout(t, func() {
			if _, err := showCIChecks("o/r", "b", "sha"); err != nil {
				t.Errorf("showCIChecks() = %v", err)
			}
		})
		if !strings.Contains(out, "failure reasons skipped (--no-reasons); rerun without it to show inline reasons") {
			t.Errorf("showCIChecks() output missing reasons hint; got:\n%s", out)
		}
	})

	t.Run("passing status has no hint", func(t *testing.T) {
		newSeam("success")
		out := captureStdout(t, func() {
			failed, err := showCIChecks("o/r", "b", "sha")
			if err != nil {
				t.Errorf("showCIChecks() = %v", err)
			}
			if failed != 0 {
				t.Errorf("showCIChecks() failed = %d, want 0", failed)
			}
		})
		if strings.Contains(out, "failing") {
			t.Errorf("showCIChecks() output has a hint for a passing status:\n%s", out)
		}
	})
}

func TestShowCILogsFailedInProgressRuns(t *testing.T) {
	t.Run("failed job in in-progress run is shown", func(t *testing.T) {
		s := &reasonSeam{}
		withReasonSeam(t, s)
		s.wfRuns = []gh.WorkflowRun{{ID: 1, Name: "test", Status: "in_progress", Conclusion: ""}}
		s.jobs = map[int64][]gh.WorkflowJob{
			1: {{ID: 11, Name: "test", Status: "completed", Conclusion: "failure"}},
		}
		s.logs = map[int64]string{11: "Error: boom\n"}

		out := captureStdout(t, func() {
			if err := showCILogsFailed("o/r", "sha", nil); err != nil {
				t.Errorf("showCILogsFailed() = %v", err)
			}
		})
		if !strings.Contains(out, "## test / test") || !strings.Contains(out, "Error: boom") {
			t.Errorf("showCILogsFailed() missing failed job from in-progress run:\n%s", out)
		}
	})

	t.Run("only in-progress failures get a pending message", func(t *testing.T) {
		s := &reasonSeam{}
		withReasonSeam(t, s)
		s.wfRuns = []gh.WorkflowRun{{ID: 1, Name: "test", Status: "in_progress", Conclusion: ""}}
		s.jobs = map[int64][]gh.WorkflowJob{
			1: {{ID: 11, Name: "test", Status: "in_progress", Conclusion: ""}},
		}

		out := captureStdout(t, func() {
			if err := showCILogsFailed("o/r", "sha", nil); err != nil {
				t.Errorf("showCILogsFailed() = %v", err)
			}
		})
		if !strings.Contains(out, "still in progress") {
			t.Errorf("showCILogsFailed() missing in-progress message:\n%s", out)
		}
		if len(s.calls.getJobLogs) != 0 {
			t.Errorf("getJobLogs called for %v, want none", s.calls.getJobLogs)
		}
	})
}
