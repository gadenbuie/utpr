package ui

import "testing"

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello", "hello"},
		{"with color", "\x1b[31mred\x1b[0m", "red"},
		{"mixed", "\x1b[1;34m#42\x1b[0m title \x1b[3mauthor\x1b[0m", "#42 title author"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripANSI(tt.input)
			if got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStyleBranchName(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		plain  string // expected after stripping ANSI
	}{
		{"feat prefix", "feat/my-feature", "feat/my-feature"},
		{"fix prefix", "fix/bug-123", "fix/bug-123"},
		{"no prefix", "my-branch", "my-branch"},
		{"chore prefix", "chore/deps", "chore/deps"},
		{"nested slashes", "feat/sub/path", "feat/sub/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			styled := StyleBranchName(tt.branch)
			plain := StripANSI(styled)
			if plain != tt.plain {
				t.Errorf("StyleBranchName(%q) stripped = %q, want %q", tt.branch, plain, tt.plain)
			}
		})
	}
}
