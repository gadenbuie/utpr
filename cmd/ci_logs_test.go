package cmd

import (
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
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

// resetCIFlags restores the ci flags runCI reads to their defaults.
func resetCIFlags(t *testing.T) {
	t.Helper()
	old := map[string]any{
		"logs": flagCILogs, "web": flagCIWeb, "watch": flagCIWatch,
		"wait": flagCIWait, "pick": flagCIPick, "agent": flagCIAgent,
		"pretty": flagCIPretty, "noReasons": flagCINoReasons,
		"full": flagCILogsFull, "grep": flagCILogsGrep,
		"after": flagCILogsAfter, "before": flagCILogsBefore,
		"lines": flagCILogsLines,
	}
	t.Cleanup(func() {
		flagCILogs, flagCIWeb, flagCIWatch = old["logs"].(bool), old["web"].(bool), old["watch"].(bool)
		flagCIWait, flagCIPick, flagCIAgent = old["wait"].(string), old["pick"].(bool), old["agent"].(bool)
		flagCIPretty, flagCINoReasons = old["pretty"].(bool), old["noReasons"].(bool)
		flagCILogsFull, flagCILogsGrep = old["full"].(bool), old["grep"].(string)
		flagCILogsAfter, flagCILogsBefore = old["after"].(int), old["before"].(int)
		flagCILogsLines = old["lines"].(int)
	})
}

// withRunCISeams fakes remote detection, target resolution, and the run
// picker so runCI never touches git or the network beyond the gh seams.
func withRunCISeams(t *testing.T, target ciTarget, picked *gh.WorkflowRun) *[]string {
	t.Helper()
	pickArgs := &[]string{}
	oldDetect, oldResolve, oldPick := ciRemoteDetect, ciResolveCITarget, ciPickRunForBranch
	ciRemoteDetect = func() (*remote.Config, error) { return &remote.Config{}, nil }
	ciResolveCITarget = func(_ *remote.Config, _ []string, _ bool) (ciTarget, error) { return target, nil }
	ciPickRunForBranch = func(ownerRepo, branch string, _ int) (*gh.WorkflowRun, error) {
		*pickArgs = append(*pickArgs, ownerRepo+"/"+branch)
		return picked, nil
	}
	t.Cleanup(func() { ciRemoteDetect, ciResolveCITarget, ciPickRunForBranch = oldDetect, oldResolve, oldPick })
	return pickArgs
}

func TestRunCILogsFlagConflicts(t *testing.T) {
	resetCIFlags(t)
	flagCILogs = true
	for _, conflict := range []struct {
		name  string
		setup func()
	}{
		{"web", func() { flagCIWeb = true }},
		{"watch", func() { flagCIWatch = true }},
		{"wait", func() { flagCIWait = "all" }},
	} {
		t.Run(conflict.name, func(t *testing.T) {
			flagCIWeb, flagCIWatch, flagCIWait = false, false, ""
			conflict.setup()
			err := runCI(nil, nil)
			if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
				t.Errorf("runCI() error = %v, want --logs conflict", err)
			}
		})
	}
}

func TestRunCIRejectsLogFlagsWithoutLogs(t *testing.T) {
	resetCIFlags(t)
	flagCILogsGrep = "Error"
	err := runCI(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "require --logs") {
		t.Errorf("runCI() error = %v, want rejection of --grep without --logs", err)
	}
}

func TestRunCILogsNoFetchWhenChecksPass(t *testing.T) {
	resetCIFlags(t)
	flagCILogs = true
	s := &reasonSeam{}
	withReasonSeam(t, s)
	s.checkRuns = [][]gh.CheckRun{{reasonCheckRun(1, "test", "success")}}
	withRunCISeams(t, ciTarget{ownerRepo: "o/r", sha: "sha"}, nil)

	out := captureStdout(t, func() {
		if err := runCI(nil, nil); err != nil {
			t.Errorf("runCI() = %v", err)
		}
	})

	if len(s.calls.getJobLogs) != 0 || s.calls.listWorkflowRunJobs != 0 {
		t.Errorf("log fetches ran for a passing status: jobs=%d logs=%v",
			s.calls.listWorkflowRunJobs, s.calls.getJobLogs)
	}
	if strings.Contains(out, "failing — run") {
		t.Errorf("hint printed for a passing status:\n%s", out)
	}
}

func TestRunCILogsPickerFallbackSwapsTarget(t *testing.T) {
	resetCIFlags(t)
	flagCILogs = true
	s := &reasonSeam{}
	withReasonSeam(t, s)
	// An informative annotation keeps the failure-reasons path from
	// fetching the check run's log, so the only log fetch is --logs.
	s.annotations[1] = []gh.CheckRunAnnotation{
		{AnnotationLevel: "failure", Message: "test failed: boom"},
	}
	// First status call finds no checks; the picked run has a failing one.
	s.checkRuns = [][]gh.CheckRun{
		{},
		{reasonCheckRun(1, "test", "failure")},
	}
	s.wfRuns = []gh.WorkflowRun{{ID: 1, Name: "test", Status: "completed", Conclusion: "failure"}}
	s.jobs = map[int64][]gh.WorkflowJob{
		1: {{ID: 11, Name: "test", Status: "completed", Conclusion: "failure"}},
	}
	s.logs = map[int64]string{11: "Error: boom\n"}

	// stdin looks like a terminal so the picker fallback runs; stdout stays
	// a pipe so output stays in agent mode and lands on stdout.
	restoreTTY := ui.SetTTYFuncs(func() bool { return false }, func() bool { return true })
	defer restoreTTY()
	withRunCISeams(t, ciTarget{ownerRepo: "o/r", sha: "sha", pickOwnerRepo: "p/r", pickBranch: "b"},
		&gh.WorkflowRun{HeadSHA: "pickedsha"})

	out := captureStdout(t, func() {
		if err := runCI(nil, nil); err != nil {
			t.Errorf("runCI() = %v", err)
		}
	})

	// Status was re-fetched for the picked run...
	if s.calls.getCheckRuns != 2 {
		t.Fatalf("GetCheckRuns calls = %d, want 2", s.calls.getCheckRuns)
	}
	// ...and the failed-job logs were fetched for the swapped target.
	if len(s.calls.getJobLogs) != 1 || s.calls.getJobLogs[0] != 11 {
		t.Errorf("getJobLogs calls = %v, want failed job 11 of the picked run", s.calls.getJobLogs)
	}
	if !strings.Contains(out, "## test / test") || !strings.Contains(out, "Error: boom") {
		t.Errorf("runCI() --logs output missing picked run's failed-job logs:\n%s", out)
	}
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

func TestRunCILogsInfoWhenNoRuns(t *testing.T) {
	resetCIFlags(t)
	flagCILogs = true
	s := &reasonSeam{}
	withReasonSeam(t, s)
	s.checkRuns = [][]gh.CheckRun{{reasonCheckRun(1, "test", "failure")}}
	// No workflow runs at all: errNoCIRuns surfaces from showCILogsFailed.
	withRunCISeams(t, ciTarget{ownerRepo: "o/r", sha: "sha"}, nil)

	out := captureStdout(t, func() {
		if err := runCI(nil, nil); err != nil {
			t.Errorf("runCI() = %v", err)
		}
	})
	if !strings.Contains(out, "No CI runs found for this commit.") {
		t.Errorf("runCI() missing info line for missing runs:\n%s", out)
	}
}
