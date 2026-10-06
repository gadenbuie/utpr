package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

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

// withCIListFlags resets the ci list flags around a test.
func withCIListFlags(t *testing.T, all, watch bool, limit int) {
	t.Helper()
	oldAll, oldWatch, oldLimit := flagCIListAll, flagCIListWatch, flagCIListLimit
	flagCIListAll, flagCIListWatch, flagCIListLimit = all, watch, limit
	t.Cleanup(func() {
		flagCIListAll, flagCIListWatch, flagCIListLimit = oldAll, oldWatch, oldLimit
	})
}

// ciListSeamOpts configures the fakes installed by withCIListSeams.
type ciListSeamOpts struct {
	defaultBranch string
	branch        string
	running       func(call int) ([]gh.WorkflowRun, error)
	recent        func(call int) ([]gh.WorkflowRun, int, error)
	perSHA        map[string][]gh.WorkflowRun
	prs           func() ([]gh.PRInfo, error)
}

// ciListSeamCalls records activity of the fakes installed by withCIListSeams.
type ciListSeamCalls struct {
	runningCalls  int
	recentCalls   int
	perSHACalls   int
	prsCalls      int
	lastBranch    string
	lastLimit     int
	perSHAFetched []string
}

// withCIListSeams replaces the ci list API seams with fakes. running maps
// the 1-based poll number to its result (used by --watch); recent serves
// the one-shot listing; perSHA completes commit groups. Polls after the
// last scripted entry repeat the final one.
func withCIListSeams(t *testing.T, o ciListSeamOpts) *ciListSeamCalls {
	t.Helper()
	calls := &ciListSeamCalls{}

	running := func(call int) ([]gh.WorkflowRun, error) { return nil, nil }
	if o.running != nil {
		running = o.running
	}
	recent := func(call int) ([]gh.WorkflowRun, int, error) { return nil, 0, nil }
	if o.recent != nil {
		recent = o.recent
	}
	prs := func() ([]gh.PRInfo, error) { return nil, nil }
	if o.prs != nil {
		prs = o.prs
	}

	oldDetect, oldRepo, oldCurBranch := ciRemoteDetect, ciListRepoFromConfig, ciListCurrentBranch
	oldRunning, oldRecent, oldPRs, oldPerSHA := ghListRunningWorkflowRuns, ghListRecentWorkflowRuns, ghListOpenPRs, ghListWorkflowRunsForSHA
	oldPoll := ciPollInterval

	ciRemoteDetect = func() (*remote.Config, error) {
		return &remote.Config{DefaultBranch: o.defaultBranch}, nil
	}
	ciListRepoFromConfig = func(cfg *remote.Config) (string, error) { return "gadenbuie/utpr", nil }
	ciListCurrentBranch = func() (string, error) { return o.branch, nil }
	ghListRunningWorkflowRuns = func(ownerRepo, b string) ([]gh.WorkflowRun, error) {
		calls.runningCalls++
		calls.lastBranch = b
		return running(calls.runningCalls)
	}
	ghListRecentWorkflowRuns = func(ownerRepo, b string, limit int) ([]gh.WorkflowRun, int, error) {
		calls.recentCalls++
		calls.lastBranch = b
		calls.lastLimit = limit
		return recent(calls.recentCalls)
	}
	ghListWorkflowRunsForSHA = func(ownerRepo, sha string) ([]gh.WorkflowRun, error) {
		calls.perSHACalls++
		calls.perSHAFetched = append(calls.perSHAFetched, sha)
		return o.perSHA[sha], nil
	}
	ghListOpenPRs = func(ownerRepo, state string) ([]gh.PRInfo, error) {
		calls.prsCalls++
		return prs()
	}
	ciPollInterval = time.Millisecond

	t.Cleanup(func() {
		ciRemoteDetect, ciListRepoFromConfig, ciListCurrentBranch = oldDetect, oldRepo, oldCurBranch
		ghListRunningWorkflowRuns, ghListRecentWorkflowRuns, ghListOpenPRs, ghListWorkflowRunsForSHA = oldRunning, oldRecent, oldPRs, oldPerSHA
		ciPollInterval = oldPoll
	})
	return calls
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

func TestCIListTopHeading(t *testing.T) {
	cfg := &remote.Config{DefaultBranch: "main"}
	tests := []struct {
		name         string
		branch       string
		total, shown int
		want         string
	}{
		{"repo-wide", "", 27, 4, ""},
		{"default branch", "main", 27, 4, ""},
		{"all shown", "feat", 3, 3, "branch 'feat' — 3 runs"},
		{"one run", "feat", 1, 1, "branch 'feat' — 1 run"},
		{"subset", "feat", 27, 4, "branch 'feat' — 27 runs, showing latest 4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ui.StripANSI(ciListTopHeading(cfg, tt.branch, tt.total, tt.shown))
			if got != tt.want {
				t.Errorf("ciListTopHeading(%q, %d, %d) = %q, want %q", tt.branch, tt.total, tt.shown, got, tt.want)
			}
		})
	}
}

func TestRenderCIListGroups(t *testing.T) {
	sha1, sha2 := "abcdef1234567890", "fedcba0987654321"
	prBySHA := map[string]gh.PRInfo{
		sha2: ciListTestPR(123, "Fix login", sha2),
	}
	done := ciListTestRun(1, "pkgdown.yaml", "main", sha1, "completed", "success", "2020-01-02T14:52:00Z", "2020-01-02T15:10:00Z")
	failed := ciListTestRun(3, "R-CMD-check.yaml", "main", sha2, "completed", "failure", "2020-01-02T14:45:00Z", "2020-01-02T15:10:00Z")
	groups := []ciListGroup{
		{sha: sha1, runs: []gh.WorkflowRun{done}},
		{sha: sha2, runs: []gh.WorkflowRun{failed}},
	}

	t.Run("group headings with branch, PR, and start time", func(t *testing.T) {
		frame := renderCIListGroups(groups, prBySHA, true, "", time.Now())
		out := ui.StripANSI(frame.content)
		for _, want := range []string{
			"abcdef1 on main · started Jan 2 14:52",
			"fedcba0 on main · #123 Fix login · started Jan 2 14:45",
			"✓  pkgdown.yaml      18m",
			"✗  R-CMD-check.yaml  25m",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("frame missing %q in:\n%s", want, out)
			}
		}
		wantState := sha1 + "/1:completed:success;" + sha2 + "/3:completed:failure;"
		if frame.state != wantState {
			t.Errorf("state = %q, want %q", frame.state, wantState)
		}
	})

	t.Run("top heading replaces branch in group headings", func(t *testing.T) {
		frame := renderCIListGroups(groups, prBySHA, false, ciListTopHeading(&remote.Config{DefaultBranch: "main"}, "feat", 27, 2), time.Now())
		out := ui.StripANSI(frame.content)
		for _, want := range []string{"branch 'feat' — 27 runs, showing latest 2", "abcdef1 · started"} {
			if !strings.Contains(out, want) {
				t.Errorf("frame missing %q in:\n%s", want, out)
			}
		}
		if strings.Contains(out, "on main") {
			t.Errorf("group heading repeats the branch with a top heading:\n%s", out)
		}
	})

	t.Run("blank lines separate top heading and groups", func(t *testing.T) {
		frame := renderCIListGroups(groups, prBySHA, true, "", time.Now())
		out := ui.StripANSI(frame.content)
		if got := strings.Count(out, "\n\n"); got != 1 {
			t.Errorf("frame has %d blank separators, want 1 (between groups):\n%s", got, out)
		}
	})

	t.Run("in-flight rows show elapsed time", func(t *testing.T) {
		started := time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339)
		run := ciListTestRun(5, "ci/main", "main", sha1, "in_progress", "", started, "")
		frame := renderCIListGroups([]ciListGroup{{sha: sha1, runs: []gh.WorkflowRun{run}}}, nil, true, "", time.Now())
		out := ui.StripANSI(frame.content)
		for _, want := range []string{"…", "ci/main", "3m"} {
			if !strings.Contains(out, want) {
				t.Errorf("frame missing %q in:\n%s", want, out)
			}
		}
	})

	t.Run("long PR titles are truncated", func(t *testing.T) {
		long := ciListTestRun(7, "wf", "main", sha1, "completed", "success", "2020-01-02T14:00:00Z", "2020-01-02T14:01:00Z")
		prBySHA := map[string]gh.PRInfo{sha1: ciListTestPR(9, strings.Repeat("x", 45), sha1)}
		frame := renderCIListGroups([]ciListGroup{{sha: sha1, runs: []gh.WorkflowRun{long}}}, prBySHA, true, "", time.Now())
		out := ui.StripANSI(frame.content)
		if want := "#9 " + strings.Repeat("x", 39) + "…"; !strings.Contains(out, want) {
			t.Errorf("frame missing truncated title %q in:\n%s", want, out)
		}
	})

	t.Run("runs without timestamps show an em dash", func(t *testing.T) {
		none := ciListTestRun(7, "wf", "main", sha1, "in_progress", "", "", "")
		frame := renderCIListGroups([]ciListGroup{{sha: sha1, runs: []gh.WorkflowRun{none}}}, nil, true, "", time.Now())
		out := ui.StripANSI(frame.content)
		if want := "abcdef1 on main"; !strings.Contains(out, want) {
			t.Errorf("frame missing heading without start time %q in:\n%s", want, out)
		}
		if !strings.Contains(out, "—") {
			t.Errorf("frame missing elapsed placeholder:\n%s", out)
		}
	})
}

func TestRunCIListFeatureBranchHeading(t *testing.T) {
	sha := "abcdef1234567890"
	completed := ciListTestRun(4, "ci/lint", "ci-list", sha, "completed", "success", "2020-01-02T14:00:00Z", "2020-01-02T14:05:00Z")
	running := ciListTestRun(5, "ci/main", "ci-list", sha, "in_progress", "", "2020-01-02T14:00:00Z", "")
	withCIListFlags(t, false, false, 10)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "ci-list",
		recent: func(call int) ([]gh.WorkflowRun, int, error) {
			return []gh.WorkflowRun{running, completed}, 27, nil
		},
		perSHA: map[string][]gh.WorkflowRun{sha: {running, completed}},
		prs: func() ([]gh.PRInfo, error) {
			return []gh.PRInfo{ciListTestPR(42, "Add ci list", sha)}, nil
		},
	})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})

	for _, want := range []string{
		"branch 'ci-list' — 27 runs, showing latest 2",
		"abcdef1 · #42 Add ci list · started Jan 2 14:00",
		"✓  ci/lint  5m",
		"…  ci/main",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "on ci-list") {
		t.Errorf("group heading repeats the branch with a top heading:\n%s", out)
	}
	if calls.lastBranch != "ci-list" {
		t.Errorf("runs queried with branch %q, want %q", calls.lastBranch, "ci-list")
	}
	if calls.lastLimit != 10 {
		t.Errorf("runs queried with limit %d, want 10", calls.lastLimit)
	}
	if calls.perSHACalls != 1 {
		t.Errorf("per-SHA fetches = %d, want 1", calls.perSHACalls)
	}
}

func TestRunCIListDefaultBranchStyle(t *testing.T) {
	sha := "abcdef1234567890"
	running := ciListTestRun(5, "ci/main", "main", sha, "in_progress", "", "2020-01-02T14:00:00Z", "")
	withCIListFlags(t, false, false, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		recent: func(call int) ([]gh.WorkflowRun, int, error) {
			return []gh.WorkflowRun{running}, 27, nil
		},
		perSHA: map[string][]gh.WorkflowRun{sha: {running}},
		prs:    func() ([]gh.PRInfo, error) { return nil, nil },
	})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})

	if !strings.Contains(out, "abcdef1 on main · started Jan 2 14:00") {
		t.Errorf("group heading missing branch part:\n%s", out)
	}
	if strings.Contains(out, "branch 'main'") {
		t.Errorf("default branch got a status heading:\n%s", out)
	}
}

func TestRunCIListAllMode(t *testing.T) {
	shaMain, shaFeat := "abcdef1234567890", "fedcba0987654321"
	onMain := ciListTestRun(5, "ci/main", "main", shaMain, "in_progress", "", "2020-01-02T14:00:00Z", "")
	onFeat := ciListTestRun(6, "ci/lint", "feature-b", shaFeat, "in_progress", "", "2020-01-02T14:05:00Z", "")
	withCIListFlags(t, true, false, 10)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "ci-list",
		recent: func(call int) ([]gh.WorkflowRun, int, error) {
			return []gh.WorkflowRun{onMain, onFeat}, 27, nil
		},
		perSHA: map[string][]gh.WorkflowRun{shaMain: {onMain}, shaFeat: {onFeat}},
		prs:    func() ([]gh.PRInfo, error) { return nil, nil },
	})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})

	if calls.lastBranch != "" {
		t.Errorf("runs queried with branch %q in --all mode, want empty", calls.lastBranch)
	}
	for _, want := range []string{"abcdef1 on main", "fedcba0 on feature-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "branch 'ci-list'") {
		t.Errorf("--all mode got a branch status heading:\n%s", out)
	}
}

func TestRunCIListAllModeEmpty(t *testing.T) {
	withCIListFlags(t, true, false, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "ci-list",
		recent:        func(call int) ([]gh.WorkflowRun, int, error) { return nil, 0, nil },
	})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})
	if want := "No CI runs found in gadenbuie/utpr."; !strings.Contains(out, want) {
		t.Errorf("output missing %q in:\n%s", want, out)
	}
}

func TestRunCIListEmptyBranchMode(t *testing.T) {
	withCIListFlags(t, false, false, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		recent:        func(call int) ([]gh.WorkflowRun, int, error) { return nil, 0, nil },
	})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})
	if want := "No CI runs found on branch 'main'."; !strings.Contains(out, want) {
		t.Errorf("output missing %q in:\n%s", want, out)
	}
}

func TestRunCIListLimitCompletesGroups(t *testing.T) {
	sha1, sha2 := "abcdef1234567890", "fedcba0987654321"
	a1 := ciListTestRun(1, "wf-a1", "feat", sha1, "in_progress", "", "2020-01-02T14:00:00Z", "")
	a2 := ciListTestRun(2, "wf-a2", "feat", sha1, "queued", "", "2020-01-02T14:00:00Z", "")
	b1 := ciListTestRun(3, "wf-b1", "feat", sha2, "in_progress", "", "2020-01-02T14:05:00Z", "")
	b2 := ciListTestRun(4, "wf-b2", "feat", sha2, "completed", "success", "2020-01-02T14:05:00Z", "2020-01-02T14:08:00Z")

	withCIListFlags(t, false, false, 3)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "feat",
		// The 3-run window touches two groups; both are shown complete.
		recent: func(call int) ([]gh.WorkflowRun, int, error) {
			return []gh.WorkflowRun{a1, a2, b1}, 27, nil
		},
		perSHA: map[string][]gh.WorkflowRun{
			sha1: {a2, a1},
			sha2: {b2, b1},
		},
		prs: func() ([]gh.PRInfo, error) { return nil, nil },
	})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})

	// limit 3 touches two groups, but both complete groups are shown: 4 jobs.
	for _, want := range []string{"wf-a1", "wf-a2", "wf-b1", "wf-b2"} {
		if got := strings.Count(out, want); got != 1 {
			t.Errorf("%s printed %d times, want 1 in:\n%s", want, got, out)
		}
	}
	if want := "branch 'feat' — 27 runs, showing latest 4"; !strings.Contains(out, want) {
		t.Errorf("output missing %q in:\n%s", want, out)
	}
	if len(calls.perSHAFetched) != 2 {
		t.Errorf("per-SHA fetches = %v, want both touched SHAs", calls.perSHAFetched)
	}
}

func TestRunCIListGroupFetchFallback(t *testing.T) {
	sha1, sha2 := "abcdef1234567890", "fedcba0987654321"
	a1 := ciListTestRun(1, "wf-a1", "feat", sha1, "in_progress", "", "2020-01-02T14:00:00Z", "")
	b1 := ciListTestRun(3, "wf-b1", "feat", sha2, "in_progress", "", "2020-01-02T14:05:00Z", "")

	withCIListFlags(t, false, false, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "feat",
		recent: func(call int) ([]gh.WorkflowRun, int, error) {
			return []gh.WorkflowRun{a1, b1}, 27, nil
		},
		// sha2 has no per-SHA result: the group falls back to the window.
		perSHA: map[string][]gh.WorkflowRun{sha1: {a1}},
		prs:    func() ([]gh.PRInfo, error) { return nil, nil },
	})

	out := captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})
	for _, want := range []string{"wf-a1", "wf-b1", "fedcba0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
}

func TestRunCIListLimitFlag(t *testing.T) {
	running := ciListTestRun(5, "ci/main", "main", "abcdef1234567890", "in_progress", "", "2020-01-02T14:00:00Z", "")
	withCIListFlags(t, false, false, 3)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		recent: func(call int) ([]gh.WorkflowRun, int, error) {
			return []gh.WorkflowRun{running}, 27, nil
		},
		perSHA: map[string][]gh.WorkflowRun{"abcdef1234567890": {running}},
	})

	captureStdout(t, func() {
		if err := runCIList(nil, nil); err != nil {
			t.Errorf("runCIList: %v", err)
		}
	})
	if calls.lastLimit != 3 {
		t.Errorf("runs queried with limit %d, want 3", calls.lastLimit)
	}
}

func TestRunCIListLimitValidation(t *testing.T) {
	withCIListFlags(t, false, false, 0)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
	})

	captureStdout(t, func() {
		err := runCIList(nil, nil)
		if err == nil {
			t.Error("runCIList with --limit 0 = nil error, want error")
		}
	})
}

func TestRunCIListLimitWatchConflict(t *testing.T) {
	withCIListFlags(t, false, true, 5)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
	})

	// The conflict check needs the flag's Changed state, which only a
	// cobra-parsed command provides.
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().IntVar(&flagCIListLimit, "limit", 10, "")
	if err := cmd.Flags().Set("limit", "5"); err != nil {
		t.Fatalf("set limit flag: %v", err)
	}

	captureStdout(t, func() {
		err := runCIList(cmd, nil)
		if err == nil || !strings.Contains(fmt.Sprint(err), "--limit cannot be combined with --watch") {
			t.Errorf("runCIList with --watch --limit = %v, want --watch conflict error", err)
		}
	})
}

func TestRunCIListFetchError(t *testing.T) {
	withCIListFlags(t, false, false, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		recent: func(call int) ([]gh.WorkflowRun, int, error) {
			return nil, 0, errors.New("boom")
		},
	})

	captureStdout(t, func() {
		err := runCIList(nil, nil)
		if err == nil {
			t.Error("runCIList = nil error, want error")
		}
	})
}

func TestWatchCIListStateChangePrinting(t *testing.T) {
	sha := "abcdef1234567890"
	runA := ciListTestRun(5, "ci/main", "main", sha, "in_progress", "", "2020-01-02T14:00:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "main", sha, "completed", "success", "2020-01-02T14:00:00Z", "2020-01-02T14:02:00Z")

	withCIListFlags(t, false, true, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		running: func(call int) ([]gh.WorkflowRun, error) {
			if call <= 3 {
				return []gh.WorkflowRun{runA}, nil
			}
			return nil, nil
		},
		perSHA: map[string][]gh.WorkflowRun{sha: {doneA}},
	})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}

	// Initial frame + final frame; unchanged middle polls print nothing.
	if got := strings.Count(out, "ci/main"); got != 2 {
		t.Errorf("ci/main printed %d times, want 2 in:\n%s", got, out)
	}
	if !strings.Contains(out, "abcdef1 on main") {
		t.Errorf("watch frames missing group heading:\n%s", out)
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
	sha1, sha2 := "abcdef1234567890", "fedcba0987654321"
	runA := ciListTestRun(5, "ci/main", "main", sha1, "in_progress", "", "2020-01-02T14:00:00Z", "")
	runB := ciListTestRun(6, "ci/lint", "main", sha2, "in_progress", "", "2020-01-02T14:01:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "main", sha1, "completed", "success", "2020-01-02T14:00:00Z", "2020-01-02T14:02:00Z")
	doneB := ciListTestRun(6, "ci/lint", "main", sha2, "completed", "failure", "2020-01-02T14:01:00Z", "2020-01-02T14:03:00Z")

	withCIListFlags(t, false, true, 10)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		running: func(call int) ([]gh.WorkflowRun, error) {
			switch call {
			case 1:
				return []gh.WorkflowRun{runA}, nil
			case 2:
				return []gh.WorkflowRun{runA, runB}, nil
			default:
				return nil, nil
			}
		},
		perSHA: map[string][]gh.WorkflowRun{sha1: {doneA}, sha2: {doneB}},
	})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}

	// Initial frame, frame after the second group appears, final frame
	// with both complete groups.
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
	if calls.perSHACalls != 2 {
		t.Errorf("per-SHA fetches at drain = %d, want 2", calls.perSHACalls)
	}
}

func TestWatchCIListEmptyAtStart(t *testing.T) {
	withCIListFlags(t, false, true, 10)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		running:       func(call int) ([]gh.WorkflowRun, error) { return nil, nil },
	})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	if want := "No running CI runs on branch 'main'."; !strings.Contains(out, want) {
		t.Errorf("output missing %q in:\n%s", want, out)
	}
	if calls.perSHACalls != 0 {
		t.Errorf("per-SHA fetches with no watched runs = %d, want 0", calls.perSHACalls)
	}
}

func TestWatchCIListPRsFetchedOncePerSHA(t *testing.T) {
	sha := "abcdef1234567890"
	runA := ciListTestRun(5, "ci/main", "main", sha, "in_progress", "", "2020-01-02T14:00:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "main", sha, "completed", "success", "2020-01-02T14:00:00Z", "2020-01-02T14:02:00Z")

	withCIListFlags(t, false, true, 10)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		running: func(call int) ([]gh.WorkflowRun, error) {
			if call <= 3 {
				return []gh.WorkflowRun{runA}, nil
			}
			return nil, nil
		},
		perSHA: map[string][]gh.WorkflowRun{sha: {doneA}},
		prs: func() ([]gh.PRInfo, error) {
			return []gh.PRInfo{ciListTestPR(42, "Add ci list", sha)}, nil
		},
	})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
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

func TestWatchCIListNegativePRCache(t *testing.T) {
	sha := "abcdef1234567890"
	runA := ciListTestRun(5, "ci/main", "main", sha, "in_progress", "", "2020-01-02T14:00:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "main", sha, "completed", "success", "2020-01-02T14:00:00Z", "2020-01-02T14:02:00Z")

	withCIListFlags(t, false, true, 10)
	calls := withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		running: func(call int) ([]gh.WorkflowRun, error) {
			if call <= 3 {
				return []gh.WorkflowRun{runA}, nil
			}
			return nil, nil
		},
		perSHA: map[string][]gh.WorkflowRun{sha: {doneA}},
		prs:    func() ([]gh.PRInfo, error) { return nil, nil },
	})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	if calls.prsCalls != 1 {
		t.Errorf("PRs fetched %d times for a PR-less run, want 1", calls.prsCalls)
	}
	if strings.Contains(out, "#") {
		t.Errorf("frame shows a PR label for an unassociated run:\n%s", out)
	}
}

func TestWatchCIListUnknownConclusionMarked(t *testing.T) {
	// A watched run missing from the final per-SHA fetch renders as
	// unknown, not as still running.
	sha := "abcdef1234567890"
	runA := ciListTestRun(5, "ci/main", "main", sha, "in_progress", "", "2020-01-02T14:00:00Z", "")

	withCIListFlags(t, false, true, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		running: func(call int) ([]gh.WorkflowRun, error) {
			if call == 1 {
				return []gh.WorkflowRun{runA}, nil
			}
			return nil, nil
		},
		perSHA: map[string][]gh.WorkflowRun{}, // fetch returns nothing
	})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
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
	sha1, sha2 := "abcdef1234567890", "fedcba0987654321"
	runA := ciListTestRun(5, "ci/main", "main", sha1, "in_progress", "", "2020-01-02T14:00:00Z", "")
	runB := ciListTestRun(6, "ci/lint", "main", sha2, "in_progress", "", "2020-01-02T14:01:00Z", "")
	doneA := ciListTestRun(5, "ci/main", "main", sha1, "completed", "success", "2020-01-02T14:00:00Z", "2020-01-02T14:02:00Z")
	doneB := ciListTestRun(6, "ci/lint", "main", sha2, "completed", "failure", "2020-01-02T14:01:00Z", "2020-01-02T14:03:00Z")

	withCIListFlags(t, false, true, 10)
	withCIListSeams(t, ciListSeamOpts{
		defaultBranch: "main",
		branch:        "main",
		running: func(call int) ([]gh.WorkflowRun, error) {
			switch call {
			case 1:
				return []gh.WorkflowRun{runA}, nil
			case 2:
				return []gh.WorkflowRun{runA, runB}, nil
			default:
				return nil, nil
			}
		},
		perSHA: map[string][]gh.WorkflowRun{sha1: {doneA}, sha2: {doneB}},
	})

	var err error
	out := captureStdout(t, func() { err = watchCIList("gadenbuie/utpr", "main") })
	if err != nil {
		t.Fatalf("watchCIList: %v", err)
	}
	// Three frames (initial, after the second group appears, final) need
	// two blank-line separators between them.
	if got := strings.Count(out, "\n\n"); got < 2 {
		t.Errorf("found %d blank-line separators, want 2, in:\n%s", got, out)
	}
}

func TestCIListFlagsRegistered(t *testing.T) {
	for _, name := range []string{"all", "watch", "limit", "agent", "pretty"} {
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
