package cmd

import "testing"

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
