package ui

// Hyperlink wraps text in an OSC 8 terminal hyperlink when styled output
// is active, so rich terminals render it as a clickable link. In plain
// mode (or when the URL is empty) the text is returned unchanged.
func Hyperlink(url, text string) string {
	if plainMode || url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}
