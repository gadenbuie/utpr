package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
)

// ciListSeamCalls records activity of the fakes installed by withCIListSeams.
type ciListSeamCalls struct {
	runningCalls int
	recentCalls  int
	prsCalls     int
	lastBranch   string
}

// withCIListFlags resets the ci list flags around a test.
func withCIListFlags(t *testing.T, all, watch bool) {
	t.Helper()
	oldAll, oldWatch := flagCIListAll, flagCIListWatch
	flagCIListAll, flagCIListWatch = all, watch
	t.Cleanup(func() {
		flagCIListAll, flagCIListWatch = oldAll, oldWatch
	})
}

// withCIListSeams replaces the ci list API seams with fakes. running maps
// the 1-based poll number to its result; the poll after the last entry
// repeats the final one. The fakes count calls and record the branch they
// were queried with.
func withCIListSeams(t *testing.T, branch string, running func(call int) []gh.WorkflowRun, recent func() []gh.WorkflowRun, prs func() ([]gh.PRInfo, error)) *ciListSeamCalls {
	t.Helper()
	calls := &ciListSeamCalls{}

	oldDetect, oldRepo, oldCurBranch := ciRemoteDetect, ciListRepoFromConfig, ciListCurrentBranch
	oldRunning, oldRecent, oldPRs := ghListRunningWorkflowRuns, ghListRecentWorkflowRuns, ghListOpenPRs
	oldPoll := ciPollInterval

	ciRemoteDetect = func() (*remote.Config, error) { return &remote.Config{}, nil }
	ciListRepoFromConfig = func(cfg *remote.Config) (string, error) { return "gadenbuie/utpr", nil }
	ciListCurrentBranch = func() (string, error) { return branch, nil }
	ghListRunningWorkflowRuns = func(ownerRepo, b string) ([]gh.WorkflowRun, error) {
		calls.runningCalls++
		calls.lastBranch = b
		return running(calls.runningCalls), nil
	}
	ghListRecentWorkflowRuns = func(ownerRepo, b string, limit int) ([]gh.WorkflowRun, error) {
		calls.recentCalls++
		return recent(), nil
	}
	ghListOpenPRs = func(ownerRepo, state string) ([]gh.PRInfo, error) {
		calls.prsCalls++
		return prs()
	}
	ciPollInterval = time.Millisecond

	t.Cleanup(func() {
		ciRemoteDetect, ciListRepoFromConfig, ciListCurrentBranch = oldDetect, oldRepo, oldCurBranch
		ghListRunningWorkflowRuns, ghListRecentWorkflowRuns, ghListOpenPRs = oldRunning, oldRecent, oldPRs
		ciPollInterval = oldPoll
	})
	return calls
}

// ciListTestPR builds a PRInfo with the given number, title, and head SHA.
func ciListTestPR(number int, title, sha string) gh.PRInfo {
	var pr gh.PRInfo
	pr.Number = number
	pr.Title = title
	pr.Head.SHA = sha
	return pr
}

// ciListTestRun builds a workflow run with the given fields.
func ciListTestRun(id int64, name, branch, sha, status, conclusion, started, updated string) gh.WorkflowRun {
	return gh.WorkflowRun{
		ID:           id,
		Name:         name,
		Status:       status,
		Conclusion:   conclusion,
		HeadSHA:      sha,
		HeadBranch:   branch,
		RunStartedAt: started,
		UpdatedAt:    updated,
		CreatedAt:    started,
	}
}

func TestFormatCIListElapsed(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-time.Minute, "0s"},
		{0, "0s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{4 * time.Minute, "4m"},
		{61 * time.Minute, "1h 01m"},
		{2*time.Hour + 5*time.Minute, "2h 05m"},
	}
	for _, tt := range tests {
		if got := formatCIListElapsed(tt.d); got != tt.want {
			t.Errorf("formatCIListElapsed(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestRenderCIListFrame(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	running := ciListTestRun(5, "ci/main", "feature-branch", "abcdef1234567890", "in_progress", "", "2026-10-06T11:56:00Z", "")
	queued := ciListTestRun(6, "ci/lint", "other-branch", "fedcba0987654321", "queued", "", "2026-10-06T11:55:00Z", "")
	prBySHA := map[string]gh.PRInfo{
		"abcdef1234567890": ciListTestPR(123, "Fix login", "abcdef1234567890"),
	}

	t.Run("branch mode omits the branch column", func(t *testing.T) {
		frame := renderCIListFrame([]gh.WorkflowRun{running}, prBySHA, false, now, false)
		out := ui.StripANSI(frame.content)
		for _, want := range []string{"ci/main", "abcdef1", "#123 Fix login", "4m", "…"} {
			if !strings.Contains(out, want) {
				t.Errorf("frame missing %q in:\n%s", want, out)
			}
		}
		if strings.Contains(out, "feature-branch") {
			t.Errorf("frame shows the branch column in branch mode:\n%s", out)
		}
		if frame.state != "5:in_progress:;" {
			t.Errorf("state = %q, want %q", frame.state, "5:in_progress:;")
		}
	})

	t.Run("repo-wide mode shows the branch column and unassociated PRs", func(t *testing.T) {
		frame := renderCIListFrame([]gh.WorkflowRun{running, queued}, prBySHA, true, now, false)
		out := ui.StripANSI(frame.content)
		for _, want := range []string{"feature-branch", "other-branch", "ci/lint", "fedcba0", "—", "○", "…"} {
			if !strings.Contains(out, want) {
				t.Errorf("frame missing %q in:\n%s", want, out)
			}
		}
	})

	t.Run("completed frame shows the conclusion icon and total duration", func(t *testing.T) {
		done := ciListTestRun(5, "ci/main", "feature-branch", "abcdef1234567890", "completed", "success", "2026-10-06T11:00:00Z", "2026-10-06T11:02:00Z")
		frame := renderCIListFrame([]gh.WorkflowRun{done}, prBySHA, false, now, true)
		out := ui.StripANSI(frame.content)
		for _, want := range []string{"✓", "2m"} {
			if !strings.Contains(out, want) {
				t.Errorf("completed frame missing %q in:\n%s", want, out)
			}
		}
		if strings.Contains(out, "…") {
			t.Errorf("completed frame shows a running icon:\n%s", out)
		}
	})

	t.Run("long PR titles are truncated", func(t *testing.T) {
		long := running
		long.HeadSHA = "1111111111111111"
		prBySHA["1111111111111111"] = ciListTestPR(9, strings.Repeat("x", 45), "1111111111111111")
		frame := renderCIListFrame([]gh.WorkflowRun{long}, prBySHA, false, now, false)
		out := ui.StripANSI(frame.content)
		if want := "#9 " + strings.Repeat("x", 39) + "…"; !strings.Contains(out, want) {
			t.Errorf("frame missing truncated title %q in:\n%s", want, out)
		}
	})

	t.Run("runs without timestamps show an em dash", func(t *testing.T) {
		none := ciListTestRun(7, "ci/main", "b", "sha", "in_progress", "", "", "")
		frame := renderCIListFrame([]gh.WorkflowRun{none}, nil, false, now, false)
		if out := ui.StripANSI(frame.content); !strings.Contains(out, "—") {
			t.Errorf("frame missing elapsed placeholder:\n%s", out)
		}
	})
}

func TestRunCIListBranchMode(t *testing.T) {
	started := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339)
	running := ciListTestRun(5, "ci/main", "ci-list", "abcdef1234567890", "in_progress", "", started, "")
	withCIListFlags(t, false, false)
	calls := withCIListSeams(t, "ci-list",
		func(call int) []gh.WorkflowRun { return []gh.WorkflowRun{running} },
		func() []gh.WorkflowRun { return nil },
		func() ([]gh.PRInfo, error) {
			return []gh.PRInfo{ciListTestPR(42, "Add ci list", "abcdef1234567890")}, nil
		})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})

	for _, want := range []string{"ci/main", "abcdef1", "#42 Add ci list", "2m", "…"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if calls.lastBranch != "ci-list" {
		t.Errorf("runs queried with branch %q, want %q", calls.lastBranch, "ci-list")
	}
	if calls.recentCalls != 0 {
		t.Errorf("recent runs fetched %d times in one-shot mode, want 0", calls.recentCalls)
	}
	if strings.Contains(out, "ci-list") {
		t.Errorf("branch column shown in branch mode:\n%s", out)
	}
}

func TestRunCIListAllMode(t *testing.T) {
	withCIListFlags(t, true, false)
	calls := withCIListSeams(t, "ci-list",
		func(call int) []gh.WorkflowRun { return nil },
		func() []gh.WorkflowRun { return nil },
		func() ([]gh.PRInfo, error) { return nil, nil })

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})

	if calls.lastBranch != "" {
		t.Errorf("runs queried with branch %q in --all mode, want empty", calls.lastBranch)
	}
	if want := "No running CI runs in gadenbuie/utpr."; !strings.Contains(out, want) {
		t.Errorf("output missing %q in:\n%s", want, out)
	}
}

func TestRunCIListEmptyBranchMode(t *testing.T) {
	withCIListFlags(t, false, false)
	withCIListSeams(t, "main",
		func(call int) []gh.WorkflowRun { return nil },
		func() []gh.WorkflowRun { return nil },
		func() ([]gh.PRInfo, error) { return nil, nil })

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})

	if want := "No running CI runs on branch 'main'."; !strings.Contains(out, want) {
		t.Errorf("output missing %q in:\n%s", want, out)
	}
}

func TestRunCIListFetchError(t *testing.T) {
	withCIListFlags(t, false, false)
	withCIListSeams(t, "main",
		func(call int) []gh.WorkflowRun { return nil },
		func() []gh.WorkflowRun { return nil },
		func() ([]gh.PRInfo, error) { return nil, nil })
	oldRunning := ghListRunningWorkflowRuns
	ghListRunningWorkflowRuns = func(ownerRepo, branch string) ([]gh.WorkflowRun, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { ghListRunningWorkflowRuns = oldRunning })

	captureStdout(t, func() {
		err := runCIList(nil, nil)
		if err == nil {
			t.Error("runCIList = nil error, want error")
		}
	})
}

func TestWatchCIListStateChangePrinting(t *testing.T) {
	started := time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339)
	runA := ciListTestRun(5, "ci/main", "ci-list", "abcdef1234567890", "in_progress", "", started, "")
	// Same state on the second poll: no new frame should print.
	same := runA
	completed := ciListTestRun(5, "ci/main", "ci-list", "abcdef1234567890", "completed", "success", "2026-10-06T11:00:00Z", "2026-10-06T11:02:00Z")

	withCIListFlags(t, false, true)
	withCIListSeams(t, "ci-list",
		func(call int) []gh.WorkflowRun {
			switch call {
			case 1, 2:
				return []gh.WorkflowRun{runA}
			case 3:
				return []gh.WorkflowRun{same}
			default:
				return nil
			}
		},
		func() []gh.WorkflowRun { return []gh.WorkflowRun{completed} },
		func() ([]gh.PRInfo, error) { return nil, nil })

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "ci-list") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}

	if got := strings.Count(out, "ci/main"); got != 2 {
		t.Errorf("frame printed %d times, want 2 (initial + final) in:\n%s", got, out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("running frame missing icon in:\n%s", out)
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("final frame missing conclusion icon in:\n%s", out)
	}
	if !strings.Contains(out, "2m") {
		t.Errorf("final frame missing total duration in:\n%s", out)
	}
}

func TestWatchCIListNewRunAppearsMidWatch(t *testing.T) {
	runA := ciListTestRun(5, "ci/main", "ci-list", "abcdef1234567890", "in_progress", "", "2026-10-06T11:56:00Z", "")
	runB := ciListTestRun(6, "ci/lint", "ci-list", "fedcba0987654321", "queued", "", "2026-10-06T11:57:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "ci-list", "abcdef1234567890", "completed", "success", "2026-10-06T11:00:00Z", "2026-10-06T11:02:00Z")
	doneB := ciListTestRun(6, "ci/lint", "ci-list", "fedcba0987654321", "completed", "failure", "2026-10-06T11:00:00Z", "2026-10-06T11:01:00Z")

	withCIListFlags(t, false, true)
	withCIListSeams(t, "ci-list",
		func(call int) []gh.WorkflowRun {
			switch call {
			case 1:
				return []gh.WorkflowRun{runA}
			case 2:
				return []gh.WorkflowRun{runA, runB}
			default:
				return nil
			}
		},
		func() []gh.WorkflowRun { return []gh.WorkflowRun{doneA, doneB} },
		func() ([]gh.PRInfo, error) { return nil, nil })

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "ci-list") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}

	// Initial frame, frame after ci/lint appears, final frame with both
	// conclusions — ci/main appears in all three, ci/lint in two.
	if got := strings.Count(out, "ci/main"); got != 3 {
		t.Errorf("ci/main printed %d times, want 3 in:\n%s", got, out)
	}
	if got := strings.Count(out, "ci/lint"); got != 2 {
		t.Errorf("ci/lint printed %d times, want 2 in:\n%s", got, out)
	}
	for _, want := range []string{"✓", "✗"} {
		if !strings.Contains(out, want) {
			t.Errorf("final frame missing %q in:\n%s", want, out)
		}
	}
}

func TestWatchCIListEmptyAtStart(t *testing.T) {
	withCIListFlags(t, false, true)
	calls := withCIListSeams(t, "main",
		func(call int) []gh.WorkflowRun { return nil },
		func() []gh.WorkflowRun { return nil },
		func() ([]gh.PRInfo, error) { return nil, nil })

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	if want := "No running CI runs on branch 'main'."; !strings.Contains(out, want) {
		t.Errorf("output missing %q in:\n%s", want, out)
	}
	if calls.recentCalls != 0 {
		t.Errorf("recent runs fetched %d times with no watched runs, want 0", calls.recentCalls)
	}
}

func TestWatchCIListPRsFetchedOncePerSHA(t *testing.T) {
	runA := ciListTestRun(5, "ci/main", "ci-list", "abcdef1234567890", "in_progress", "", "2026-10-06T11:56:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "ci-list", "abcdef1234567890", "completed", "success", "2026-10-06T11:00:00Z", "2026-10-06T11:02:00Z")

	withCIListFlags(t, false, true)
	calls := withCIListSeams(t, "ci-list",
		func(call int) []gh.WorkflowRun {
			if call <= 3 {
				return []gh.WorkflowRun{runA}
			}
			return nil
		},
		func() []gh.WorkflowRun { return []gh.WorkflowRun{doneA} },
		func() ([]gh.PRInfo, error) {
			return []gh.PRInfo{ciListTestPR(42, "Add ci list", "abcdef1234567890")}, nil
		})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "ci-list") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	if calls.prsCalls != 1 {
		t.Errorf("PRs fetched %d times across polls, want 1", calls.prsCalls)
	}
	if got := strings.Count(out, "#42 Add ci list"); got != 2 {
		t.Errorf("PR label printed %d times, want 2 (initial + final) in:\n%s", got, out)
	}
}

func TestCIListFlagsRegistered(t *testing.T) {
	for _, name := range []string{"all", "watch", "agent", "pretty"} {
		if ciListCmd.Flags().Lookup(name) == nil {
			t.Errorf("ci list command is missing the --%s flag", name)
		}
	}
}

func TestCIListAgentAndPrettyFlags(t *testing.T) {
	t.Run("--agent forces agent mode on a TTY", func(t *testing.T) {
		restoreTTY := ui.SetTTYFuncs(func() bool { return true }, func() bool { return true })
		oldAgent, oldPretty := flagCIListAgent, flagCIPretty
		flagCIListAgent, flagCIPretty = true, false
		t.Cleanup(func() {
			restoreTTY()
			flagCIListAgent, flagCIPretty = oldAgent, oldPretty
		})
		if !ciAgentMode() {
			t.Error("ciAgentMode() = false with flagCIListAgent set, want true")
		}
	})

	t.Run("--pretty forces styled output when piped", func(t *testing.T) {
		restoreTTY := ui.SetTTYFuncs(func() bool { return false }, func() bool { return false })
		oldAgent, oldPretty := flagCIListAgent, flagCIPretty
		flagCIListAgent, flagCIPretty = false, true
		t.Cleanup(func() {
			restoreTTY()
			flagCIListAgent, flagCIPretty = oldAgent, oldPretty
		})
		if ciAgentMode() {
			t.Error("ciAgentMode() = true with --pretty on piped stdout, want false")
		}
	})
}

func TestWatchCIListNegativePRCache(t *testing.T) {
	// A run whose SHA has no open PR must not trigger a refetch on every poll.
	runA := ciListTestRun(5, "ci/main", "main", "abcdef1234567890", "in_progress", "", "2026-10-06T11:56:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "main", "abcdef1234567890", "completed", "success", "2026-10-06T11:00:00Z", "2026-10-06T11:02:00Z")

	withCIListFlags(t, false, true)
	calls := withCIListSeams(t, "main",
		func(call int) []gh.WorkflowRun {
			if call <= 3 {
				return []gh.WorkflowRun{runA}
			}
			return nil
		},
		func() []gh.WorkflowRun { return []gh.WorkflowRun{doneA} },
		func() ([]gh.PRInfo, error) { return nil, nil })

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	if calls.prsCalls != 1 {
		t.Errorf("PRs fetched %d times for a PR-less run, want 1", calls.prsCalls)
	}
	if !strings.Contains(out, "—") {
		t.Errorf("frame missing unassociated PR placeholder in:\n%s", out)
	}
}

func TestWatchCIListUnknownConclusionMarked(t *testing.T) {
	// A watched run missing from the final recent-runs page renders as
	// unknown, not as still running.
	runA := ciListTestRun(5, "ci/main", "main", "abcdef1234567890", "in_progress", "", "2026-10-06T11:56:00Z", "")

	withCIListFlags(t, false, true)
	withCIListSeams(t, "main",
		func(call int) []gh.WorkflowRun {
			if call == 1 {
				return []gh.WorkflowRun{runA}
			}
			return nil
		},
		func() []gh.WorkflowRun { return nil },
		func() ([]gh.PRInfo, error) { return nil, nil })

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	// Only the final frame (after the last blank-line separator) must be
	// free of running icons; the initial frame legitimately shows one.
	parts := strings.Split(out, "\n\n")
	final := parts[len(parts)-1]
	if strings.Contains(final, "…") {
		t.Errorf("final frame shows a running icon for an unknown conclusion:\n%s", out)
	}
	if !strings.Contains(final, "?") {
		t.Errorf("final frame missing unknown-conclusion icon:\n%s", out)
	}
	if !strings.Contains(final, "—") {
		t.Errorf("final frame missing unknown-duration placeholder:\n%s", out)
	}
}

func TestWatchCIListFramesSeparatedWhenPiped(t *testing.T) {
	runA := ciListTestRun(5, "ci/main", "main", "abcdef1234567890", "in_progress", "", "2026-10-06T11:56:00Z", "")
	runB := ciListTestRun(6, "ci/lint", "main", "fedcba0987654321", "in_progress", "", "2026-10-06T11:57:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "main", "abcdef1234567890", "completed", "success", "2026-10-06T11:00:00Z", "2026-10-06T11:02:00Z")
	doneB := ciListTestRun(6, "ci/lint", "main", "fedcba0987654321", "completed", "failure", "2026-10-06T11:00:00Z", "2026-10-06T11:01:00Z")

	withCIListFlags(t, false, true)
	withCIListSeams(t, "main",
		func(call int) []gh.WorkflowRun {
			switch call {
			case 1:
				return []gh.WorkflowRun{runA}
			case 2:
				return []gh.WorkflowRun{runA, runB}
			default:
				return nil
			}
		},
		func() []gh.WorkflowRun { return []gh.WorkflowRun{doneA, doneB} },
		func() ([]gh.PRInfo, error) { return nil, nil })

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	// Three frames (initial, after ci/lint appears, final) need two
	// blank-line separators between them.
	if got := strings.Count(out, "\n\n"); got < 2 {
		t.Errorf("found %d blank-line separators, want 2, in:\n%s", got, out)
	}
}
