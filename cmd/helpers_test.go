package cmd

import (
	"reflect"
	"testing"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/testutil"
)

func TestFindLocalBranchForPR(t *testing.T) {
	tests := []struct {
		name          string
		prNumber      int
		headRef       string
		author        string
		defaultBranch string
		existing      []string
		want          string
	}{
		{
			name:          "exact headRef exists",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{"feature-x"},
			want:          "feature-x",
		},
		{
			name:          "new scheme exists",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{"pr/42-alice-feature-x"},
			want:          "pr/42-alice-feature-x",
		},
		{
			name:          "legacy scheme exists",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{"pr-42/feature-x"},
			want:          "pr-42/feature-x",
		},
		{
			name:          "usethis scheme exists",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{"alice-feature-x"},
			want:          "alice-feature-x",
		},
		{
			name:          "no branch exists",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{},
			want:          "",
		},
		{
			name:          "headRef wins over new scheme",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{"feature-x", "pr/42-alice-feature-x"},
			want:          "feature-x",
		},
		{
			name:          "new scheme wins over legacy",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{"pr/42-alice-feature-x", "pr-42/feature-x"},
			want:          "pr/42-alice-feature-x",
		},
		{
			name:          "legacy wins over usethis",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "alice",
			defaultBranch: "main",
			existing:      []string{"pr-42/feature-x", "alice-feature-x"},
			want:          "pr-42/feature-x",
		},
		{
			name:          "empty author skips usethis",
			prNumber:      42,
			headRef:       "feature-x",
			author:        "",
			defaultBranch: "main",
			existing:      []string{"-feature-x"},
			want:          "",
		},
		{
			name:          "headRef matching default branch is skipped",
			prNumber:      119,
			headRef:       "main",
			author:        "bob",
			defaultBranch: "main",
			existing:      []string{"main", "bob-main"},
			want:          "bob-main",
		},
		{
			name:          "headRef matching default branch with no other match",
			prNumber:      119,
			headRef:       "main",
			author:        "bob",
			defaultBranch: "main",
			existing:      []string{"main"},
			want:          "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existingSet := make(map[string]bool)
			for _, b := range tt.existing {
				existingSet[b] = true
			}
			branchExists := func(name string) bool {
				return existingSet[name]
			}
			got := findLocalBranchForPRWith(tt.prNumber, tt.headRef, tt.author, tt.defaultBranch, branchExists)
			if got != tt.want {
				t.Errorf("findLocalBranchForPRWith(%d, %q, %q, %q) = %q, want %q",
					tt.prNumber, tt.headRef, tt.author, tt.defaultBranch, got, tt.want)
			}
		})
	}
}

func TestFallbackRepos(t *testing.T) {
	dir := testutil.TempRepo(t)

	testutil.RunGit(t, dir, "remote", "add", "origin", "https://github.com/gadenbuie/scorecard-db.git")
	testutil.RunGit(t, dir, "remote", "add", "upstream", "https://github.com/jcheng5/scorecard-db.git")
	// PR-fetch remotes and local paths must never be searched.
	testutil.RunGit(t, dir, "remote", "add", "pr-123", "https://github.com/alice/patch.git")
	testutil.RunGit(t, dir, "remote", "add", "local", "/tmp/some/repo.git")

	forkCfg := &remote.Config{Layout: "fork", PushRemote: "origin"}
	got := fallbackRepos(forkCfg, "jcheng5/scorecard-db")
	want := []string{"gadenbuie/scorecard-db"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("fallbackRepos(fork) = %v, want %v", got, want)
	}

	// Source and push repo are the same repo: nothing to fall back to.
	if got := fallbackRepos(forkCfg, "gadenbuie/scorecard-db"); got != nil {
		t.Errorf("fallbackRepos() with push repo as source = %v, want nil", got)
	}

	// "ours" layout has no fallback at all.
	oursCfg := &remote.Config{Layout: "ours", PushRemote: "origin"}
	if got := fallbackRepos(oursCfg, "jcheng5/scorecard-db"); got != nil {
		t.Errorf("fallbackRepos(ours) = %v, want nil", got)
	}
}

func TestFindPRForBranch(t *testing.T) {
	prInFork := &gh.PRInfo{Number: 7}

	tests := []struct {
		name        string
		layout      string
		sourceRepo  string
		prsByRepo   map[string]*gh.PRInfo
		queriedRepo string
		wantQueried []string
		wantNumber  int
	}{
		{
			name:        "found in source repo, no fallback",
			layout:      "fork",
			sourceRepo:  "jcheng5/scorecard-db",
			prsByRepo:   map[string]*gh.PRInfo{"jcheng5/scorecard-db": {Number: 3}},
			queriedRepo: "jcheng5/scorecard-db",
			wantQueried: []string{"jcheng5/scorecard-db"},
			wantNumber:  3,
		},
		{
			name:        "falls back to the fork",
			layout:      "fork",
			sourceRepo:  "jcheng5/scorecard-db",
			prsByRepo:   map[string]*gh.PRInfo{"gadenbuie/scorecard-db": prInFork},
			queriedRepo: "gadenbuie/scorecard-db",
			wantQueried: []string{"jcheng5/scorecard-db", "gadenbuie/scorecard-db"},
			wantNumber:  7,
		},
		{
			name:        "not found anywhere",
			layout:      "fork",
			sourceRepo:  "jcheng5/scorecard-db",
			prsByRepo:   map[string]*gh.PRInfo{},
			queriedRepo: "",
			wantQueried: []string{"jcheng5/scorecard-db", "gadenbuie/scorecard-db"},
			wantNumber:  0,
		},
		{
			name:        "ours layout does not fall back",
			layout:      "ours",
			sourceRepo:  "gadenbuie/scorecard-db",
			prsByRepo:   map[string]*gh.PRInfo{},
			queriedRepo: "",
			wantQueried: []string{"gadenbuie/scorecard-db"},
			wantNumber:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempRepo(t)
			testutil.RunGit(t, dir, "remote", "add", "origin", "https://github.com/gadenbuie/scorecard-db.git")
			testutil.RunGit(t, dir, "remote", "add", "upstream", "https://github.com/jcheng5/scorecard-db.git")

			var queried []string
			restore := stubSearchPRForBranch(func(repo, branch, state string, owners ...string) (*gh.PRInfo, error) {
				queried = append(queried, repo)
				return tt.prsByRepo[repo], nil
			})
			defer restore()

			cfg := &remote.Config{Layout: tt.layout, PushRemote: "origin"}
			pr, prRepo := findPRForBranch(cfg, tt.sourceRepo, "fix/thing", "open")
			if tt.wantNumber == 0 {
				if pr != nil {
					t.Fatalf("findPRForBranch() = %+v, want nil", pr)
				}
				if prRepo != tt.sourceRepo {
					t.Errorf("findPRForBranch() repo = %q, want %q", prRepo, tt.sourceRepo)
				}
			} else {
				if pr == nil || pr.Number != tt.wantNumber {
					t.Fatalf("findPRForBranch() = %+v, want PR #%d", pr, tt.wantNumber)
				}
				if prRepo != tt.queriedRepo {
					t.Errorf("findPRForBranch() repo = %q, want %q", prRepo, tt.queriedRepo)
				}
			}
			if !reflect.DeepEqual(queried, tt.wantQueried) {
				t.Errorf("searched repos = %v, want %v", queried, tt.wantQueried)
			}
		})
	}
}

// stubSearchPRForBranch replaces the PR search used by findPRForBranch.
func stubSearchPRForBranch(fn func(repo, branch, state string, owners ...string) (*gh.PRInfo, error)) func() {
	old := searchPRForBranch
	searchPRForBranch = fn
	return func() { searchPRForBranch = old }
}
