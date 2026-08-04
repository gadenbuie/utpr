package cmd

import "testing"

func TestPRNumberFromStoredURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want int
	}{
		{"standard PR URL", "https://github.com/owner/repo/pull/42", 42},
		{"fork PR URL", "https://github.com/owner/repo/pull/123", 123},
		{"empty string (no stored URL)", "", 0},
		{"malformed URL", "https://github.com/owner/repo/pull/", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prNumberFromStoredURL(tt.url)
			if got != tt.want {
				t.Errorf("prNumberFromStoredURL(%q) = %d, want %d", tt.url, got, tt.want)
			}
		})
	}
}

func TestRemoteBranchName(t *testing.T) {
	tests := []struct {
		name        string
		trackingRef string
		localBranch string
		want        string
	}{
		{
			name:        "simple remote/branch",
			trackingRef: "origin/feature-x",
			localBranch: "feature-x",
			want:        "feature-x",
		},
		{
			name:        "branch name contains slash",
			trackingRef: "origin/feature/my-thing",
			localBranch: "my-local-name",
			want:        "feature/my-thing",
		},
		{
			name:        "local name differs from remote (renamed branch)",
			trackingRef: "origin/real-branch-name",
			localBranch: "my-custom-local-name",
			want:        "real-branch-name",
		},
		{
			name:        "fork PR remote/branch",
			trackingRef: "contributor/feature-x",
			localBranch: "pr/42-contributor-feature-x",
			want:        "feature-x",
		},
		{
			name:        "no tracking ref falls back to local branch",
			trackingRef: "",
			localBranch: "my-local-branch",
			want:        "my-local-branch",
		},
		{
			name:        "tracking ref with no slash falls back to local branch",
			trackingRef: "no-slash-ref",
			localBranch: "my-local-branch",
			want:        "my-local-branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteBranchName(tt.trackingRef, tt.localBranch)
			if got != tt.want {
				t.Errorf("remoteBranchName(%q, %q) = %q, want %q",
					tt.trackingRef, tt.localBranch, got, tt.want)
			}
		})
	}
}

func TestShouldDeleteRemoteBranch(t *testing.T) {
	tests := []struct {
		name               string
		prMerged           bool
		prHeadRepoFullName string
		pushRepoFullName   string
		want               bool
	}{
		{
			name:               "merged and repos match",
			prMerged:           true,
			prHeadRepoFullName: "owner/repo",
			pushRepoFullName:   "owner/repo",
			want:               true,
		},
		{
			name:               "merged but repos differ (fork)",
			prMerged:           true,
			prHeadRepoFullName: "contributor/repo",
			pushRepoFullName:   "owner/repo",
			want:               false,
		},
		{
			name:               "not merged repos match",
			prMerged:           false,
			prHeadRepoFullName: "owner/repo",
			pushRepoFullName:   "owner/repo",
			want:               false,
		},
		{
			name:               "not merged repos differ",
			prMerged:           false,
			prHeadRepoFullName: "contributor/repo",
			pushRepoFullName:   "owner/repo",
			want:               false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldDeleteRemoteBranch(tt.prMerged, tt.prHeadRepoFullName, tt.pushRepoFullName)
			if got != tt.want {
				t.Errorf("shouldDeleteRemoteBranch(%v, %q, %q) = %v, want %v",
					tt.prMerged, tt.prHeadRepoFullName, tt.pushRepoFullName, got, tt.want)
			}
		})
	}
}

func TestResolveFinishArgNumeric(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want int
	}{
		{"plain PR number", "42", 42},
		{"PR number 1", "1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveFinishArg(tt.arg, "owner/repo")
			if err != nil {
				t.Fatalf("resolveFinishArg(%q) returned error: %v", tt.arg, err)
			}
			if got != tt.want {
				t.Errorf("resolveFinishArg(%q) = %d, want %d", tt.arg, got, tt.want)
			}
		})
	}
}

func TestExtractPRNumberFromURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want int
	}{
		{"standard PR URL", "https://github.com/owner/repo/pull/42", 42},
		{"PR number 1", "https://github.com/owner/repo/pull/1", 1},
		{"no trailing number", "https://github.com/owner/repo", 0},
		{"empty string", "", 0},
		{"trailing slash", "https://github.com/owner/repo/pull/42/", 0},
		{"subpath after number", "https://github.com/owner/repo/pull/42/files", 0},
		{"non-numeric", "https://github.com/owner/repo/pull/abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPRNumberFromURL(tt.url)
			if got != tt.want {
				t.Errorf("extractPRNumberFromURL(%q) = %d, want %d", tt.url, got, tt.want)
			}
		})
	}
}
