package cmd

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
)

func TestWorktreeJSONEntries(t *testing.T) {
	worktrees := []git.Worktree{
		{Path: "/repo", HEAD: "abc123", Branch: "refs/heads/main"},
		{Path: "/repo.worktrees/feature", HEAD: "def456", Branch: "refs/heads/feature"},
		{Path: "/repo.worktrees/detached", HEAD: "789abc"},
	}

	got := worktreeJSONEntries(worktrees, func(branch string) string {
		if branch == "feature" {
			return "https://github.com/owner/repo/pull/42"
		}
		return ""
	})

	if len(got) != 3 {
		t.Fatalf("worktreeJSONEntries() returned %d entries, want 3", len(got))
	}
	if !got[0].IsMain || got[1].IsMain || got[2].IsMain {
		t.Errorf("unexpected is_main values: %+v", got)
	}
	if got[0].Branch != "main" || got[1].Branch != "feature" || got[2].Branch != "" {
		t.Errorf("unexpected branches: %+v", got)
	}
	if got[1].PRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("unexpected PR URL: %q", got[1].PRURL)
	}
}

func TestWorktreeListHasJSONFlag(t *testing.T) {
	if worktreeListCmd.Flags().Lookup("json") == nil {
		t.Fatal("worktree list command is missing the --json flag")
	}
}
