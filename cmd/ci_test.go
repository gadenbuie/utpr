package cmd

import "testing"

func TestLooksLikeGitRef(t *testing.T) {
	tests := []struct {
		arg  string
		want bool
	}{
		{"HEAD", true},
		{"HEAD~2", true},
		{"HEAD^", true},
		{"main~1", true},
		{"abc123", true},
		{"a1b2c3d4e5f6789012345678901234567890abcd", true},
		{"main", false},
		{"feature/foo", false},
		{"gh-pages", false},
	}
	for _, tt := range tests {
		if got := looksLikeGitRef(tt.arg); got != tt.want {
			t.Errorf("looksLikeGitRef(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}
