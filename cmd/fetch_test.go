package cmd

import "testing"

func TestDetermineFetchRemote(t *testing.T) {
	tests := []struct {
		name         string
		prHeadRepo   string
		prBaseRepo   string
		sourceRemote string
		wantRemote   string
		wantFork     bool
	}{
		{
			name:         "same repo PR",
			prHeadRepo:   "owner/repo",
			prBaseRepo:   "owner/repo",
			sourceRemote: "origin",
			wantRemote:   "origin",
			wantFork:     false,
		},
		{
			name:         "fork PR",
			prHeadRepo:   "contributor/repo",
			prBaseRepo:   "owner/repo",
			sourceRemote: "origin",
			wantRemote:   "contributor",
			wantFork:     true,
		},
		{
			name:         "head repo without slash",
			prHeadRepo:   "contributor",
			prBaseRepo:   "owner/repo",
			sourceRemote: "origin",
			wantRemote:   "contributor",
			wantFork:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRemote, gotFork := determineFetchRemote(tt.prHeadRepo, tt.prBaseRepo, tt.sourceRemote)
			if gotRemote != tt.wantRemote {
				t.Errorf("determineFetchRemote() remote = %q, want %q", gotRemote, tt.wantRemote)
			}
			if gotFork != tt.wantFork {
				t.Errorf("determineFetchRemote() isFork = %v, want %v", gotFork, tt.wantFork)
			}
		})
	}
}

func TestDetermineFetchBranchName(t *testing.T) {
	tests := []struct {
		name             string
		existingTracking string
		currentUser      string
		prAuthor         string
		prNumber         int
		headRef          string
		isFork           bool
		existing         []string // local branches that already exist
		want             string
	}{
		{
			name:             "existing tracking branch",
			existingTracking: "my-tracked-branch",
			currentUser:      "me",
			prAuthor:         "other",
			prNumber:         42,
			headRef:          "feature-x",
			want:             "my-tracked-branch",
		},
		{
			name:        "other user same-repo PR uses real branch name",
			currentUser: "me",
			prAuthor:    "other",
			prNumber:    42,
			headRef:     "feature-x",
			want:        "feature-x",
		},
		{
			name:        "other user fork PR keeps pr/ prefix",
			currentUser: "me",
			prAuthor:    "other",
			prNumber:    42,
			headRef:     "feature-x",
			isFork:      true,
			want:        "pr/42-other-feature-x",
		},
		{
			name:        "own fork PR uses real branch name",
			currentUser: "me",
			prAuthor:    "me",
			prNumber:    42,
			headRef:     "feature-x",
			isFork:      true,
			want:        "feature-x",
		},
		{
			name:        "real branch name taken falls back to pr/ prefix",
			currentUser: "me",
			prAuthor:    "other",
			prNumber:    42,
			headRef:     "feature-x",
			existing:    []string{"feature-x"},
			want:        "pr/42-other-feature-x",
		},
		{
			name:        "own PR with taken branch name falls back to pr/ prefix",
			currentUser: "me",
			prAuthor:    "me",
			prNumber:    42,
			headRef:     "feature-x",
			existing:    []string{"feature-x"},
			want:        "pr/42-me-feature-x",
		},
		{
			name:             "own PR but existing tracking wins",
			existingTracking: "tracked",
			currentUser:      "me",
			prAuthor:         "me",
			prNumber:         42,
			headRef:          "feature-x",
			want:             "tracked",
		},
		{
			name:        "gh login failed (unknown author) same-repo PR uses headRef",
			currentUser: "",
			prAuthor:    "other",
			prNumber:    42,
			headRef:     "feature-x",
			want:        "feature-x",
		},
		{
			name:        "both users empty returns headRef",
			currentUser: "",
			prAuthor:    "",
			prNumber:    42,
			headRef:     "feature-x",
			want:        "feature-x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			existing := map[string]bool{}
			for _, b := range tt.existing {
				existing[b] = true
			}
			branchExists := func(b string) bool { return existing[b] }
			got := determineFetchBranchName(tt.existingTracking, tt.currentUser, tt.prAuthor, tt.prNumber, tt.headRef, tt.isFork, branchExists)
			if got != tt.want {
				t.Errorf("determineFetchBranchName() = %q, want %q", got, tt.want)
			}
		})
	}
}
