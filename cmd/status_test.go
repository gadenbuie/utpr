package cmd

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/muesli/termenv"
)

func statusTestPR() *gh.PRInfo {
	pr := &gh.PRInfo{
		State:          "open",
		Draft:          false,
		MergeableState: "clean",
		Number:         42,
		Title:          "Add status command",
		HTMLURL:        "https://github.com/owner/repo/pull/42",
	}
	pr.Head.Ref = "feat/status"
	pr.Head.SHA = "abc123"
	pr.Head.Repo.FullName = "owner/repo"
	pr.Base.Ref = "main"
	pr.Base.Repo.FullName = "owner/repo"
	pr.User.Login = "gadenbuie"
	return pr
}

func statusTestReview(login, state, at string) gh.PRReview {
	var r gh.PRReview
	r.User.Login = login
	r.State = state
	r.SubmittedAt = at
	return r
}

func statusTestIssue() *gh.IssueInfo {
	issue := &gh.IssueInfo{
		Number:    7,
		Title:     "Something is broken",
		State:     "open",
		HTMLURL:   "https://github.com/owner/repo/issues/7",
		CreatedAt: "2026-09-01T10:00:00Z",
		Comments:  3,
	}
	issue.User.Login = "gadenbuie"
	issue.Labels = []struct {
		Name string `json:"name"`
	}{{Name: "bug"}, {Name: "ui"}}
	issue.Assignees = []struct {
		Login string `json:"login"`
	}{{Login: "helper"}}
	return issue
}

func TestStatusHasFlags(t *testing.T) {
	for _, name := range []string{"json", "issue", "state"} {
		if statusCmd.Flags().Lookup(name) == nil {
			t.Errorf("status command is missing the --%s flag", name)
		}
	}
}

func TestSummarizeChecks(t *testing.T) {
	tests := []struct {
		name        string
		checks      []gh.CheckRun
		wantOverall string
		wantPassed  int
		wantFailed  int
		wantPending int
		wantSkipped int
		wantItems   int
	}{
		{
			name:        "no checks",
			wantOverall: "none",
		},
		{
			name: "all passing",
			checks: []gh.CheckRun{
				{Name: "lint", Status: "completed", Conclusion: "success"},
				{Name: "build", Status: "completed", Conclusion: "success"},
			},
			wantOverall: "success",
			wantPassed:  2,
			wantItems:   2,
		},
		{
			name: "one failure blocks success",
			checks: []gh.CheckRun{
				{Name: "lint", Status: "completed", Conclusion: "success"},
				{Name: "go-test", Status: "completed", Conclusion: "failure"},
				{Name: "build", Status: "in_progress"},
			},
			wantOverall: "failure",
			wantPassed:  1,
			wantFailed:  1,
			wantPending: 1,
			wantItems:   3,
		},
		{
			name: "pending without failures",
			checks: []gh.CheckRun{
				{Name: "lint", Status: "in_progress"},
				{Name: "build", Status: "queued"},
			},
			wantOverall: "pending",
			wantPending: 2,
			wantItems:   2,
		},
		{
			name: "skipped and cancelled counts",
			checks: []gh.CheckRun{
				{Name: "lint", Status: "completed", Conclusion: "skipped"},
				{Name: "e2e", Status: "completed", Conclusion: "cancelled"},
				{Name: "build", Status: "completed", Conclusion: "success"},
			},
			wantOverall: "success",
			wantPassed:  1,
			wantSkipped: 2,
			wantItems:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeChecks(tt.checks)
			if got.Overall != tt.wantOverall {
				t.Errorf("Overall = %q, want %q", got.Overall, tt.wantOverall)
			}
			if got.Passed != tt.wantPassed {
				t.Errorf("Passed = %d, want %d", got.Passed, tt.wantPassed)
			}
			if got.Failed != tt.wantFailed {
				t.Errorf("Failed = %d, want %d", got.Failed, tt.wantFailed)
			}
			if got.Pending != tt.wantPending {
				t.Errorf("Pending = %d, want %d", got.Pending, tt.wantPending)
			}
			if got.Skipped != tt.wantSkipped {
				t.Errorf("Skipped = %d, want %d", got.Skipped, tt.wantSkipped)
			}
			if got.Total != len(tt.checks) {
				t.Errorf("Total = %d, want %d", got.Total, len(tt.checks))
			}
			if len(got.Items) != tt.wantItems {
				t.Errorf("len(Items) = %d, want %d", len(got.Items), tt.wantItems)
			}
		})
	}
}

func TestSummarizeReviews(t *testing.T) {
	tests := []struct {
		name        string
		reviews     []gh.PRReview
		requested   []string
		wantSummary string
		wantOrder   []string
		wantPending []string
		wantTotal   int
	}{
		{
			name:        "no reviews",
			wantSummary: "none",
		},
		{
			name: "latest review per reviewer wins",
			reviews: []gh.PRReview{
				statusTestReview("alice", "CHANGES_REQUESTED", "2026-10-01T00:00:00Z"),
				statusTestReview("alice", "APPROVED", "2026-10-02T00:00:00Z"),
				statusTestReview("bob", "APPROVED", "2026-10-01T00:00:00Z"),
			},
			wantSummary: "approved",
			wantOrder:   []string{"alice", "bob"},
			wantTotal:   2,
		},
		{
			name: "changes requested takes precedence",
			reviews: []gh.PRReview{
				statusTestReview("alice", "APPROVED", "2026-10-01T00:00:00Z"),
				statusTestReview("bob", "CHANGES_REQUESTED", "2026-10-02T00:00:00Z"),
			},
			wantSummary: "changes_requested",
			wantOrder:   []string{"alice", "bob"},
			wantTotal:   2,
		},
		{
			name: "dismissed counts as neither approval nor block",
			reviews: []gh.PRReview{
				statusTestReview("alice", "APPROVED", "2026-10-01T00:00:00Z"),
				statusTestReview("bob", "DISMISSED", "2026-10-02T00:00:00Z"),
				statusTestReview("carol", "PENDING", "2026-10-03T00:00:00Z"),
			},
			wantSummary: "approved",
			wantOrder:   []string{"alice", "bob"},
			wantTotal:   2,
		},
		{
			name: "requested reviewers who have not reviewed are pending",
			reviews: []gh.PRReview{
				statusTestReview("alice", "APPROVED", "2026-10-01T00:00:00Z"),
			},
			requested:   []string{"bob", "alice", "carol"},
			wantSummary: "approved",
			wantOrder:   []string{"alice"},
			wantPending: []string{"bob", "carol"},
			wantTotal:   3,
		},
		{
			name:        "only pending requests still count reviewers",
			requested:   []string{"bob"},
			wantSummary: "none",
			wantPending: []string{"bob"},
			wantTotal:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeReviews(tt.reviews, tt.requested)
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if len(got.Items) != len(tt.wantOrder) {
				t.Fatalf("len(Items) = %d, want %d", len(got.Items), len(tt.wantOrder))
			}
			for i, want := range tt.wantOrder {
				if got.Items[i].Reviewer != want {
					t.Errorf("Items[%d].Reviewer = %q, want %q", i, got.Items[i].Reviewer, want)
				}
			}
			if strings.Join(got.Pending, ",") != strings.Join(tt.wantPending, ",") {
				t.Errorf("Pending = %v, want %v", got.Pending, tt.wantPending)
			}
			if got.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", got.Total, tt.wantTotal)
			}
		})
	}
}

func TestBuildIssueStatus(t *testing.T) {
	got := buildIssueStatus(statusTestIssue(), "owner/repo")

	if got.Type != "issue" || got.Number != 7 {
		t.Errorf("got %+v", got)
	}
	if strings.Join(got.Labels, ",") != "bug,ui" {
		t.Errorf("Labels = %v, want [bug ui]", got.Labels)
	}
	if strings.Join(got.Assignees, ",") != "helper" {
		t.Errorf("Assignees = %v, want [helper]", got.Assignees)
	}
	if got.Comments != 3 {
		t.Errorf("Comments = %d, want 3", got.Comments)
	}
	if got.CreatedAt != "2026-09-01T10:00:00Z" {
		t.Errorf("CreatedAt = %q", got.CreatedAt)
	}
	if got.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", got.Repo, "owner/repo")
	}
}

// statusPRStatusFixture returns a populated prStatus for render tests.
func statusPRStatusFixture() *prStatus {
	s := &prStatus{
		Type:              "pull_request",
		Number:            42,
		Title:             "Add status command",
		URL:               "https://github.com/owner/repo/pull/42",
		State:             "open",
		Draft:             true,
		Author:            "gadenbuie",
		HeadBranch:        "feat/status",
		HeadRepo:          "owner/repo",
		BaseBranch:        "main",
		BaseRepo:          "owner/repo",
		MergeableState:    "clean",
		UnresolvedThreads: 2,
	}
	s.Checks = summarizeChecks([]gh.CheckRun{
		{Name: "lint", Status: "completed", Conclusion: "success", HTMLURL: "https://github.com/owner/repo/runs/1"},
		{Name: "go-test (1.23)", Status: "completed", Conclusion: "failure", HTMLURL: "https://github.com/owner/repo/runs/2"},
		{Name: "build", Status: "in_progress", HTMLURL: "https://github.com/owner/repo/runs/3"},
	})
	s.Reviews = summarizeReviews([]gh.PRReview{
		statusTestReview("alice", "APPROVED", "2026-10-02T15:04:05Z"),
		statusTestReview("bob", "CHANGES_REQUESTED", "2026-10-01T00:00:00Z"),
	}, []string{"carol"})
	s.LocalSync = &statusLocalSync{Branch: "feat/status", Status: "ahead", AheadBy: 1}
	return s
}

func withPlainMode(t *testing.T, plain bool) {
	t.Helper()
	ui.SetPlainMode(plain)
	t.Cleanup(func() { ui.SetPlainMode(false) })
}

func TestRenderPRStatusRich(t *testing.T) {
	withPlainMode(t, false)

	// Force a color profile: stdout in tests is a pipe, so lipgloss would
	// otherwise strip all styling.
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	out := captureStdout(t, func() {
		renderPRStatus(statusPRStatusFixture())
	})

	for _, want := range []string{
		"#42 Add status command",
		"State", "open (draft)",
		"Author", "gadenbuie",
		"Branch", "feat/status", "main", "→",
		"Checks", "1 passed, 1 failed, 1 pending",
		"lint", "(success)",
		"go-test (1.23)", "(failure)",
		"build", "(in progress)",
		"Reviewers", "changes requested · 1 / 3 approved",
		"alice", "(approved · 2026-10-02)",
		"bob", "(changes requested · 2026-10-01)",
		"carol", "(requested)",
		"Unresolved threads", "2",
		"Mergeable", "clean",
		"Local branch", "1 commit ahead",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "\x1b]8;;https://github.com/owner/repo/pull/42") {
		t.Errorf("title should be an OSC 8 hyperlink to the PR")
	}
	if !strings.Contains(out, "\x1b]8;;https://github.com/owner/repo/tree/feat/status") {
		t.Errorf("head branch should link to its GitHub tree URL")
	}
	if !strings.Contains(out, "\x1b]8;;https://github.com/owner/repo/runs/2") {
		t.Errorf("check names should be OSC 8 hyperlinks to the check runs")
	}
	if !strings.Contains(out, "\x1b]8;;https://github.com/gadenbuie") {
		t.Errorf("author should be an OSC 8 hyperlink to their profile")
	}
	// Tone colors: green for passing, red for failures and changes requested,
	// bold for the title.
	if !strings.Contains(out, "\x1b[32m") {
		t.Errorf("styled output should contain green tones")
	}
	if !strings.Contains(out, "\x1b[31m") {
		t.Errorf("styled output should contain red tones")
	}
	if !strings.Contains(out, "\x1b[1m") {
		t.Errorf("title should be bold")
	}
	if !strings.Contains(out, "└") {
		t.Errorf("table should have a bottom border")
	}
}

func TestRenderPRStatusPlain(t *testing.T) {
	withPlainMode(t, true)

	out := captureStdout(t, func() {
		renderPRStatus(statusPRStatusFixture())
	})

	for _, want := range []string{
		"# [#42 Add status command](https://github.com/owner/repo/pull/42)",
		"| Field | Value |",
		"| State | open (draft) |",
		"| Author | [gadenbuie](https://github.com/gadenbuie) |",
		"| Branch | [feat/status](https://github.com/owner/repo/tree/feat/status) → [main](https://github.com/owner/repo/tree/main) |",
		"| Checks | 1 passed, 1 failed, 1 pending |",
		"|  | ✓ [lint](https://github.com/owner/repo/runs/1) (success) |",
		"|  | ✗ [go-test (1.23)](https://github.com/owner/repo/runs/2) (failure) |",
		"|  | … [build](https://github.com/owner/repo/runs/3) (in progress) |",
		"| Reviewers | changes requested · 1 / 3 approved |",
		"|  | ✓ [alice](https://github.com/alice) (approved · 2026-10-02) |",
		"|  | ✗ [bob](https://github.com/bob) (changes requested · 2026-10-01) |",
		"|  | … [carol](https://github.com/carol) (requested) |",
		"| Unresolved threads | 2 |",
		"| Mergeable | clean |",
		"| Local branch | 1 commit ahead |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("plain output must not contain OSC 8 hyperlink escapes")
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("plain output must not contain ANSI escapes")
	}
}

func TestRenderPRStatusUnavailableSections(t *testing.T) {
	withPlainMode(t, true)

	s := &prStatus{
		Number: 42, Title: "T", State: "open", Author: "a",
		HeadBranch: "b", BaseBranch: "main",
		UnresolvedThreads: -1,
		MergeableState:    "unknown", // GitHub reports this for merged/closed PRs
	}
	s.Checks = statusChecks{Overall: "unknown", Items: []statusCheck{}}
	s.Reviews = statusReviews{Summary: "unknown", Items: []statusReview{}}

	out := captureStdout(t, func() {
		renderPRStatus(s)
	})

	for _, want := range []string{
		"| Checks | unavailable |",
		"| Reviewers | unavailable |",
		"| Unresolved threads | unavailable |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "| Mergeable") {
		t.Errorf("unknown mergeable state should be omitted:\n%s", out)
	}
}

func TestRenderIssueStatusPlain(t *testing.T) {
	withPlainMode(t, true)

	out := captureStdout(t, func() {
		renderIssueStatus(buildIssueStatus(statusTestIssue(), "owner/repo"))
	})

	for _, want := range []string{
		"# [#7 Something is broken](https://github.com/owner/repo/issues/7)",
		"| State | open |",
		"| Author | [gadenbuie](https://github.com/gadenbuie) |",
		"| Created | 2026-09-01 |",
		"| Labels | [bug](https://github.com/owner/repo/labels/bug), [ui](https://github.com/owner/repo/labels/ui) |",
		"| Assignees | [helper](https://github.com/helper) |",
		"| Comments | 3 |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderIssueStatusRich(t *testing.T) {
	withPlainMode(t, false)

	out := captureStdout(t, func() {
		renderIssueStatus(buildIssueStatus(statusTestIssue(), "owner/repo"))
	})

	for _, want := range []string{
		"#7 Something is broken",
		"Labels", "bug", "ui",
		"Assignees", "helper",
		"Comments", "3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "\x1b]8;;https://github.com/owner/repo/labels/bug") {
		t.Errorf("labels should link to the GitHub label filter")
	}
}

func TestPrintStatusJSON(t *testing.T) {
	s := buildIssueStatus(statusTestIssue(), "owner/repo")

	out := captureStdout(t, func() {
		if err := printStatusJSON(s); err != nil {
			t.Errorf("printStatusJSON: %v", err)
		}
	})

	for _, want := range []string{
		`"type": "issue"`,
		`"number": 7`,
		`"repo": "owner/repo"`,
		`"labels": [`,
		`"bug"`,
		`"comments": 3`,
		`"assignees": [`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %q in:\n%s", want, out)
		}
	}
}

func TestCheckDetail(t *testing.T) {
	tests := []struct {
		check statusCheck
		want  string
	}{
		{statusCheck{Status: "completed", Conclusion: "success"}, "success"},
		{statusCheck{Status: "in_progress"}, "in progress"},
		{statusCheck{Status: "queued"}, "queued"},
	}
	for _, tt := range tests {
		if got := checkDetail(tt.check); got != tt.want {
			t.Errorf("checkDetail(%+v) = %q, want %q", tt.check, got, tt.want)
		}
	}
}

func TestCheckResultText(t *testing.T) {
	tests := []struct {
		check statusCheck
		want  string
	}{
		{statusCheck{Status: "completed", Conclusion: "success"}, "success"},
		{statusCheck{Status: "completed", Conclusion: "success", Duration: "4m 12s"}, "success · 4m 12s"},
		{statusCheck{Status: "in_progress"}, "in progress"},
	}
	for _, tt := range tests {
		if got := checkResultText(tt.check); got != tt.want {
			t.Errorf("checkResultText(%+v) = %q, want %q", tt.check, got, tt.want)
		}
	}
}

func TestCheckDuration(t *testing.T) {
	// Completed check: duration between start and completion.
	c := gh.CheckRun{
		Status:      "completed",
		StartedAt:   "2026-10-02T14:00:00Z",
		CompletedAt: "2026-10-02T14:04:30Z",
	}
	if got := checkDuration(c); got != "4m 30s" {
		t.Errorf("checkDuration(completed) = %q, want %q", got, "4m 30s")
	}

	// No start time: no duration.
	if got := checkDuration(gh.CheckRun{}); got != "" {
		t.Errorf("checkDuration(no start) = %q, want empty", got)
	}
}

func TestLocalSyncText(t *testing.T) {
	tests := []struct {
		sync statusLocalSync
		want string
	}{
		{statusLocalSync{Branch: "b", Status: "in_sync"}, "up to date"},
		{statusLocalSync{Branch: "b", Status: "ahead", AheadBy: 1}, "1 commit ahead"},
		{statusLocalSync{Branch: "b", Status: "behind", BehindBy: 3}, "3 commits behind"},
		{statusLocalSync{Branch: "b", Status: "diverged", AheadBy: 1, BehindBy: 2}, "diverged: 1 commit ahead, 2 commits behind"},
		{statusLocalSync{Branch: "b", Status: "unknown"}, "unknown"},
	}
	for _, tt := range tests {
		if got := localSyncText(&tt.sync); got != tt.want {
			t.Errorf("localSyncText(%+v) = %q, want %q", tt.sync, got, tt.want)
		}
	}
}

func TestReviewsSummaryText(t *testing.T) {
	tests := []struct {
		reviews statusReviews
		want    string
	}{
		{statusReviews{Summary: "changes_requested", Approved: 1, Total: 3}, "changes requested · 1 / 3 approved"},
		{statusReviews{Summary: "approved", Approved: 2, Total: 2}, "2 / 2 approved"},
		{statusReviews{Summary: "none", Approved: 0, Total: 1}, "0 / 1 approved"},
	}
	for _, tt := range tests {
		if got := reviewsSummaryText(&tt.reviews); got != tt.want {
			t.Errorf("reviewsSummaryText(%+v) = %q, want %q", tt.reviews, got, tt.want)
		}
	}
}

func TestGitHubURLHelpers(t *testing.T) {
	if got := githubUserURL("octocat"); got != "https://github.com/octocat" {
		t.Errorf("githubUserURL = %q", got)
	}
	if got := githubUserURL(""); got != "" {
		t.Errorf("githubUserURL(empty) = %q, want empty", got)
	}
	if got := githubBranchURL("owner/repo", "feat/x"); got != "https://github.com/owner/repo/tree/feat/x" {
		t.Errorf("githubBranchURL = %q", got)
	}
	if got := githubLabelURL("owner/repo", "needs help"); got != "https://github.com/owner/repo/labels/needs%20help" {
		t.Errorf("githubLabelURL = %q", got)
	}
}
