package cmd

import "testing"

func TestIssueToSlug(t *testing.T) {
	tests := []struct {
		number int
		title  string
		want   string
	}{
		{42, "Add bisect command", "fix/42-add-bisect-command"},
		{1, "Fix: authentication timeout", "fix/1-fix-authentication-timeout"},
		{99, "UPPER CASE Title!", "fix/99-upper-case-title"},
		{7, "simple", "fix/7-simple"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := issueToSlug(tt.number, tt.title)
			if got != tt.want {
				t.Errorf("issueToSlug(%d, %q) = %q, want %q", tt.number, tt.title, got, tt.want)
			}
		})
	}
}

func TestParsePRNumber(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"simple", "#42 some title", 42, false},
		{"with ansi", "\x1b[36m#42\x1b[0m title", 42, false},
		{"no hash", "no number here", 0, true},
		{"just number", "#123", 123, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePRNumber(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parsePRNumber(%q) expected error, got %d", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("parsePRNumber(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("parsePRNumber(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitRemoteRef(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		remotes    []string
		wantRemote string
		wantBranch string
		wantOK     bool
	}{
		{
			name:       "remote branch",
			ref:        "origin/main",
			remotes:    []string{"origin"},
			wantRemote: "origin",
			wantBranch: "main",
			wantOK:     true,
		},
		{
			name:       "remote branch with slash",
			ref:        "origin/docs/fix-typo",
			remotes:    []string{"origin"},
			wantRemote: "origin",
			wantBranch: "docs/fix-typo",
			wantOK:     true,
		},
		{
			name:    "local branch with slash",
			ref:     "docs/fix-typo",
			remotes: []string{"origin"},
			wantOK:  false,
		},
		{
			name:       "longest matching remote",
			ref:        "upstream/fork/main",
			remotes:    []string{"upstream", "upstream/fork"},
			wantRemote: "upstream/fork",
			wantBranch: "main",
			wantOK:     true,
		},
		{
			name:    "remote without branch",
			ref:     "origin/",
			remotes: []string{"origin"},
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote, branch, ok := splitRemoteRef(tt.ref, tt.remotes)
			if remote != tt.wantRemote || branch != tt.wantBranch || ok != tt.wantOK {
				t.Errorf("splitRemoteRef(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.ref, tt.remotes, remote, branch, ok, tt.wantRemote, tt.wantBranch, tt.wantOK)
			}
		})
	}
}
