package cmd

import "testing"

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
