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
			name:             "other user PR no tracking",
			existingTracking: "",
			currentUser:      "me",
			prAuthor:         "other",
			prNumber:         42,
			headRef:          "feature-x",
			want:             "pr/42-other-feature-x",
		},
		{
			name:             "own PR no tracking",
			existingTracking: "",
			currentUser:      "me",
			prAuthor:         "me",
			prNumber:         42,
			headRef:          "feature-x",
			want:             "feature-x",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineFetchBranchName(tt.existingTracking, tt.currentUser, tt.prAuthor, tt.prNumber, tt.headRef)
			if got != tt.want {
				t.Errorf("determineFetchBranchName() = %q, want %q", got, tt.want)
			}
		})
	}
}
