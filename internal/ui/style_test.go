package ui

import "testing"

func TestPadLeft(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"pads plain text", "49s", 6, "   49s"},
		{"already wide enough", "5m 7s", 5, "5m 7s"},
		{"pads ANSI-styled text by visible width", StyleMuted.Render("49s"), 6, "   " + StyleMuted.Render("49s")},
		{"empty pads fully", "", 3, "   "},
	}
	for _, tt := range tests {
		if got := PadLeft(tt.in, tt.width); got != tt.want {
			t.Errorf("PadLeft(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{"pads plain text", "49s", 6, "49s   "},
		{"already wide enough", "5m 7s", 5, "5m 7s"},
		{"pads ANSI-styled text by visible width", StyleMuted.Render("49s"), 6, StyleMuted.Render("49s") + "   "},
	}
	for _, tt := range tests {
		if got := PadRight(tt.in, tt.width); got != tt.want {
			t.Errorf("PadRight(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}
