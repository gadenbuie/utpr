package gh

import "testing"

func TestSplitOwnerRepo(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"valid", "owner/repo", "owner", "repo", false},
		{"repo with subpath", "a/b/c", "a", "b/c", false},
		{"empty owner", "/repo", "", "", true},
		{"empty repo", "owner/", "", "", true},
		{"empty string", "", "", "", true},
		{"no slash", "noslash", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := splitOwnerRepo(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("splitOwnerRepo(%q) expected error, got %q %q", tt.input, owner, repo)
				}
				return
			}
			if err != nil {
				t.Errorf("splitOwnerRepo(%q) unexpected error: %v", tt.input, err)
				return
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("splitOwnerRepo(%q) = (%q, %q), want (%q, %q)", tt.input, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestNormalizeMerged(t *testing.T) {
	mergedAt := "2026-10-16T18:24:02Z"
	prs := []PRInfo{
		// List endpoints return merged: null even for merged PRs, with
		// merged_at carrying the signal.
		{State: "closed", MergedAt: &mergedAt},
		// Closed-but-unmerged PR: no merged_at, Merged stays false.
		{State: "closed"},
		// Single-PR endpoint already returns merged: true.
		{State: "closed", Merged: true, MergedAt: &mergedAt},
	}
	normalizeMerged(prs)

	if !prs[0].Merged {
		t.Error("expected merged_at to set Merged true for list-endpoint PR")
	}
	if prs[1].Merged {
		t.Error("expected closed PR without merged_at to stay unmerged")
	}
	if !prs[2].Merged {
		t.Error("expected already-merged PR to stay merged")
	}
}
