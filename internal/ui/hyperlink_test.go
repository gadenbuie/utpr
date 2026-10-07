package ui

import "testing"

func TestHyperlink(t *testing.T) {
	SetPlainMode(true)
	t.Cleanup(func() { SetPlainMode(false) })

	if got := Hyperlink("https://example.com", "text"); got != "text" {
		t.Errorf("plain mode Hyperlink = %q, want %q", got, "text")
	}
	if got := Hyperlink("", "text"); got != "text" {
		t.Errorf("empty URL Hyperlink = %q, want %q", got, "text")
	}

	SetPlainMode(false)
	want := "\x1b]8;;https://example.com\x1b\\text\x1b]8;;\x1b\\"
	if got := Hyperlink("https://example.com", "text"); got != want {
		t.Errorf("styled Hyperlink = %q, want %q", got, want)
	}
}
