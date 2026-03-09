package cmd

import "testing"

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
