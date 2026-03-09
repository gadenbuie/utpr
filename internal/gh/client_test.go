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
