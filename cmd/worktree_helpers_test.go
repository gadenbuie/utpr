package cmd

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
)

func TestComputeSymlinkCandidates(t *testing.T) {
	tests := []struct {
		name       string
		items      []string
		inRepo     map[string]bool
		inWorktree map[string]bool
		want       []string
	}{
		{
			name:       "item in repo but not worktree",
			items:      []string{".env"},
			inRepo:     map[string]bool{".env": true},
			inWorktree: map[string]bool{},
			want:       []string{".env"},
		},
		{
			name:       "item in both",
			items:      []string{".env"},
			inRepo:     map[string]bool{".env": true},
			inWorktree: map[string]bool{".env": true},
			want:       []string{},
		},
		{
			name:       "item in neither",
			items:      []string{".env"},
			inRepo:     map[string]bool{},
			inWorktree: map[string]bool{},
			want:       []string{},
		},
		{
			name:       "all items filtered out",
			items:      []string{".env", ".secrets"},
			inRepo:     map[string]bool{},
			inWorktree: map[string]bool{},
			want:       []string{},
		},
		{
			name:       "multiple candidates",
			items:      []string{".env", ".claude", ".secrets", ".vscode"},
			inRepo:     map[string]bool{".env": true, ".claude": true, ".secrets": true, ".vscode": true},
			inWorktree: map[string]bool{".claude": true},
			want:       []string{".env", ".secrets", ".vscode"},
		},
		{
			name:       "claude dir special case adds settings.json",
			items:      []string{".claude"},
			inRepo:     map[string]bool{".claude": true, ".claude/settings.json": true},
			inWorktree: map[string]bool{".claude": true},
			want:       []string{".claude/settings.json"},
		},
		{
			name:       "claude dir special case skipped when settings.json in worktree",
			items:      []string{".claude"},
			inRepo:     map[string]bool{".claude": true, ".claude/settings.json": true},
			inWorktree: map[string]bool{".claude": true, ".claude/settings.json": true},
			want:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existsInRepo := func(name string) bool { return tt.inRepo[name] }
			existsInWorktree := func(name string) bool { return tt.inWorktree[name] }

			got := computeSymlinkCandidates(tt.items, existsInRepo, existsInWorktree)
			if len(got) != len(tt.want) {
				t.Errorf("computeSymlinkCandidates() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("computeSymlinkCandidates()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMergeSymlinkItems(t *testing.T) {
	tests := []struct {
		name  string
		base  []string
		extra []string
		want  []string
	}{
		{
			name:  "no duplicates",
			base:  []string{".env", ".claude"},
			extra: []string{"DESIGN.md", "PRODUCT.md"},
			want:  []string{".env", ".claude", "DESIGN.md", "PRODUCT.md"},
		},
		{
			name:  "deduplicates overlap",
			base:  []string{".env", ".claude"},
			extra: []string{".env", "DESIGN.md"},
			want:  []string{".env", ".claude", "DESIGN.md"},
		},
		{
			name:  "empty extra",
			base:  []string{".env"},
			extra: []string{},
			want:  []string{".env"},
		},
		{
			name:  "empty base",
			base:  []string{},
			extra: []string{"DESIGN.md"},
			want:  []string{"DESIGN.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeSymlinkItems(tt.base, tt.extra)
			if len(got) != len(tt.want) {
				t.Errorf("mergeSymlinkItems() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("mergeSymlinkItems()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseSymlinkItems(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "normal comma-separated",
			input: ".env,.claude",
			want:  []string{".env", ".claude"},
		},
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
		{
			name:  "commas only",
			input: ",,,",
			want:  []string{},
		},
		{
			name:  "spaces around items",
			input: " .env , .claude ",
			want:  []string{".env", ".claude"},
		},
		{
			name:  "single item",
			input: ".env",
			want:  []string{".env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSymlinkItems(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseSymlinkItems(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseSymlinkItems(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// Tests in this file that need a repo must not run in parallel: TempRepo
// changes the process working directory.

func TestSuggestedSymlinkItemsWithoutMemory(t *testing.T) {
	testutil.TempRepo(t)

	available := []string{".env", ".vscode", "node_modules"}
	got := suggestedSymlinkItems(available)
	want := []string{".env", ".vscode"}
	if !equalStrings(got, want) {
		t.Errorf("suggestedSymlinkItems() = %v, want %v", got, want)
	}
}

func TestSuggestedSymlinkItemsReplaysMemory(t *testing.T) {
	testutil.TempRepo(t)
	if err := git.ConfigSet(symlinkLastConfigKey, ".env,renv,.vscode"); err != nil {
		t.Fatalf("failed to set %s: %v", symlinkLastConfigKey, err)
	}

	available := []string{".env", ".vscode", "node_modules", "secrets"}
	got := suggestedSymlinkItems(available)
	want := []string{".env", ".vscode"}
	if !equalStrings(got, want) {
		t.Errorf("suggestedSymlinkItems() = %v, want %v", got, want)
	}
}

func TestSuggestedSymlinkItemsIgnoresUnsetMemory(t *testing.T) {
	testutil.TempRepo(t)
	// An empty value must not count as a remembered selection: the prompt
	// falls back to the default suggestion instead of pre-checking nothing.
	if err := git.ConfigSet(symlinkLastConfigKey, ""); err != nil {
		t.Fatalf("failed to set %s: %v", symlinkLastConfigKey, err)
	}

	available := []string{".env", "node_modules"}
	got := suggestedSymlinkItems(available)
	want := []string{".env"}
	if !equalStrings(got, want) {
		t.Errorf("suggestedSymlinkItems() = %v, want %v", got, want)
	}
}

func TestRecordSymlinkSelection(t *testing.T) {
	t.Run("records the confirmed selection", func(t *testing.T) {
		testutil.TempRepo(t)
		recordSymlinkSelection([]string{".env", ".vscode", "node_modules"}, []string{".vscode", ".env"})
		if got, want := git.ConfigGet(symlinkLastConfigKey), ".vscode,.env"; got != want {
			t.Errorf("%s = %q, want %q", symlinkLastConfigKey, got, want)
		}
	})

	t.Run("keeps memory for items not offered", func(t *testing.T) {
		testutil.TempRepo(t)
		// Memory: .env and .secrets. This round offers .env and .vscode
		// (.secrets already exists in the new worktree, so it isn't offered).
		// The user deselects .env: offered items follow the new selection, but
		// .secrets keeps its remembered state for future worktrees.
		if err := git.ConfigSet(symlinkLastConfigKey, ".env,.secrets"); err != nil {
			t.Fatalf("failed to set %s: %v", symlinkLastConfigKey, err)
		}
		recordSymlinkSelection([]string{".env", ".vscode"}, []string{".vscode"})
		if got, want := git.ConfigGet(symlinkLastConfigKey), ".vscode,.secrets"; got != want {
			t.Errorf("%s = %q, want %q", symlinkLastConfigKey, got, want)
		}
	})

	t.Run("empty selection records nothing", func(t *testing.T) {
		testutil.TempRepo(t)
		recordSymlinkSelection([]string{".env"}, nil)
		if got := git.ConfigGet(symlinkLastConfigKey); got != "" {
			t.Errorf("%s = %q, want empty", symlinkLastConfigKey, got)
		}
	})
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
