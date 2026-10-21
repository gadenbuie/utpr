// Package cilog selects failure-relevant windows from GitHub Actions job
// logs. Raw job logs prefix every line with an RFC3339 timestamp and delimit
// steps with ##[group] markers.
package cilog

import (
	"regexp"
	"sort"
	"strings"
)

// Mode describes how a Selection was produced.
type Mode int

const (
	// ModeFull shows every line of the (possibly step-filtered) log.
	ModeFull Mode = iota
	// ModeTail shows the last n lines; no error landmarks were found.
	ModeTail
	// ModeLandmark shows a window anchored on error landmarks.
	ModeLandmark
)

// Selection is the subset of log lines to display and how it was chosen.
type Selection struct {
	Lines []string
	Mode  Mode
	// Headers maps the first selected line of each failed step group to its
	// group name. Indices refer to Lines.
	Headers []GroupHeader
	// Dropped reports that post-job steps were omitted from Lines.
	Dropped bool
	// Scoped reports that failure-unrelated lines were omitted from Lines.
	Scoped bool
}

// GroupHeader labels a selected step group in the rendered log output.
type GroupHeader struct {
	Index int
	Name  string
}

var timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z `)

const groupPrefix = "##[group]"
const endGroupPrefix = "##[endgroup]"

var landmarkRe = regexp.MustCompile(
	`##\[error\]|^Failed tests\b|^Error:|^Execution halted\b`)

var exitCodeMsgRe = regexp.MustCompile(`(?i)^process completed with exit code \d+\.?$`)

// IsGenericExitMessage reports whether msg is GitHub's content-free
// "Process completed with exit code" failure marker.
func IsGenericExitMessage(msg string) bool {
	return exitCodeMsgRe.MatchString(msg)
}

// Select returns concise failure context from a job log:
//   - n <= 0 yields the complete log;
//   - otherwise post-job steps and step groups without error landmarks are
//     omitted, and short windows around landmarks are selected within each
//     failed step group;
//   - without landmarks it returns the complete log if it fits, or the last n
//     lines otherwise.
func Select(lines []string, n int) Selection {
	if len(lines) == 0 {
		return Selection{Lines: lines, Mode: ModeFull}
	}
	if n <= 0 {
		return Selection{Lines: lines, Mode: ModeFull}
	}

	landmarks := findLandmarks(lines)
	if len(landmarks) == 0 {
		if len(lines) <= n {
			return Selection{Lines: lines, Mode: ModeFull}
		}
		return Selection{Lines: lines[len(lines)-n:], Mode: ModeTail}
	}

	kept, dropped := dropPostSteps(lines, landmarks)
	landmarks = findLandmarks(kept)
	selected, headers := selectFailureContext(kept, landmarks, n)
	selectedLines := 0
	for _, line := range selected {
		if line != "--" {
			selectedLines++
		}
	}
	scoped := selectedLines < len(kept)
	mode := ModeLandmark
	if !scoped {
		mode = ModeFull
	}
	return Selection{
		Lines: selected, Mode: mode, Headers: headers,
		Dropped: dropped, Scoped: scoped,
	}
}

// Grep filters lines to those whose timestamp-stripped content matches re,
// expanding each match with before/after context lines. Overlapping
// context windows are merged; returned lines keep their full original
// content. It returns the kept lines and the number of matching lines
// (context lines excluded).
func Grep(lines []string, re *regexp.Regexp, before, after int) ([]string, int) {
	if len(lines) == 0 {
		return nil, 0
	}
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}

	keep := make([]bool, len(lines))
	matched := 0
	for i, line := range lines {
		if !re.MatchString(content(line)) {
			continue
		}
		matched++
		lo := max(0, i-before)
		hi := min(len(lines), i+after+1)
		for j := lo; j < hi; j++ {
			keep[j] = true
		}
	}
	if matched == 0 {
		return nil, 0
	}

	out := make([]string, 0, len(lines))
	prev := -1
	for i, line := range lines {
		if !keep[i] {
			continue
		}
		// Separate non-contiguous match groups, like grep's "--" — but
		// only when context was requested; grep prints no separators
		// for bare matches.
		if prev >= 0 && i > prev+1 && (before > 0 || after > 0) {
			out = append(out, "--")
		}
		out = append(out, line)
		prev = i
	}
	return out, matched
}

// Reason returns a single line summarizing why a job log failed: the
// first informative failure landmark with its timestamp and ##[error]
// marker stripped. Section headers such as testthat's "Failed tests:" or
// "── Failure ..." defer to the message line that follows, and a testthat
// header's title prefixes that message. Generic "Process completed with
// exit code" markers carry no information and are skipped, so Reason
// returns "" when they are all the log has to offer.
func Reason(lines []string) string {
	for i, line := range lines {
		c := content(line)
		if !landmarkRe.MatchString(c) {
			continue
		}
		msg := strings.TrimSpace(stripErrorMarker(c))
		if msg == "" || IsGenericExitMessage(msg) {
			continue
		}
		if isSectionHeader(msg) {
			title, detail := firstDetail(lines[i+1:])
			if detail == "" {
				continue
			}
			if title == "" {
				title = sectionTitle(msg)
			}
			if title != "" {
				return title + ": " + detail
			}
			return detail
		}
		return msg
	}
	return ""
}

func stripErrorMarker(line string) string {
	return strings.TrimPrefix(line, "##[error]")
}

// isSectionHeader reports whether a landmark line is a header whose
// following line carries the actual failure message.
func isSectionHeader(line string) bool {
	return strings.HasPrefix(line, "Failed tests") || strings.HasPrefix(line, "──")
}

// sectionTitle extracts the title of a testthat section header like
// "── Failure (test-x:12) ──────"; it returns "" for other headers.
func sectionTitle(line string) string {
	if !strings.HasPrefix(line, "──") {
		return ""
	}
	return strings.Trim(line, "─ ")
}

// firstDetail returns the first informative line after a section header
// along with the title of the innermost section header it skipped: it
// ignores blank lines, further headers, and generic exit-code markers.
func firstDetail(lines []string) (title, detail string) {
	for _, line := range lines {
		msg := strings.TrimSpace(stripErrorMarker(content(line)))
		if msg == "" || IsGenericExitMessage(msg) {
			continue
		}
		if isSectionHeader(msg) {
			if t := sectionTitle(msg); t != "" {
				title = t
			}
			continue
		}
		return title, msg
	}
	return title, ""
}

// findLandmarks returns the sorted indices of lines that mark failures:
// GitHub Actions error annotations, testthat failure blocks, and R's
// Error/Execution halted messages.
func findLandmarks(lines []string) []int {
	var out []int
	for i, line := range lines {
		if landmarkRe.MatchString(content(line)) {
			out = append(out, i)
		}
	}
	return out
}

func content(line string) string {
	return StripTimestamp(line)
}

// SplitTimestamp splits the GitHub Actions RFC3339 timestamp prefix from
// the line content. ok is false when the line has no timestamp prefix.
func SplitTimestamp(line string) (ts, rest string, ok bool) {
	loc := timestampRe.FindStringIndex(line)
	if loc == nil {
		return "", line, false
	}
	return line[:loc[1]-1], line[loc[1]:], true
}

// StripTimestamp removes the GitHub Actions timestamp prefix from line.
func StripTimestamp(line string) string {
	_, rest, _ := SplitTimestamp(line)
	return rest
}

type stepGroup struct {
	name       string
	start, end int // half-open [start, end)
}

// stepGroups parses top-level ##[group] step boundaries, ignoring nested
// groups. Each step group extends to the next top-level group marker (or the
// end of the log), so it covers both grouped input and ungrouped step output.
// Lines before the first marker form a preamble group.
func stepGroups(lines []string) []stepGroup {
	var groups []stepGroup
	start := -1
	name := ""
	depth := 0
	for i, line := range lines {
		c := content(line)
		switch {
		case strings.HasPrefix(c, groupPrefix):
			if depth == 0 {
				if start >= 0 {
					groups = append(groups, stepGroup{name, start, i})
				} else if i > 0 {
					groups = append(groups, stepGroup{"", 0, i})
				}
				start = i
				name = strings.TrimSpace(c[len(groupPrefix):])
			}
			depth++
		case strings.HasPrefix(c, endGroupPrefix) && depth > 0:
			depth--
		}
	}
	if start >= 0 {
		groups = append(groups, stepGroup{name, start, len(lines)})
	}
	return groups
}

// isPostStepName reports whether a step group runs after the main job
// steps: "Post ..." cleanup steps and artifact uploads.
func isPostStepName(name string) bool {
	l := strings.ToLower(name)
	return strings.HasPrefix(l, "post ") ||
		strings.HasPrefix(l, "post:") ||
		strings.Contains(l, "upload-artifact") ||
		strings.Contains(l, "upload artifacts")
}

const failureContextBefore = 5
const failureContextAfter = 10

type lineSpan struct{ start, end int }

// selectFailureContext keeps small windows around each landmark, independently
// within each failed step group. This prevents a window from spilling into the
// next step's setup/cleanup output and preserves the group name for rendering.
func selectFailureContext(lines []string, landmarks []int, limit int) ([]string, []GroupHeader) {
	groups := stepGroups(lines)
	if len(groups) == 0 {
		groups = []stepGroup{{start: 0, end: len(lines)}}
	}

	groupLandmarks := make([][]int, len(groups))
	for _, landmark := range landmarks {
		for i, group := range groups {
			if landmark >= group.start && landmark < group.end {
				groupLandmarks[i] = append(groupLandmarks[i], landmark)
				break
			}
		}
	}

	before, after := failureContextBefore, failureContextAfter
	spans := make([][]lineSpan, len(groups))
	headerCount := 0
	for i := range groups {
		if groups[i].name != "" && len(groupLandmarks[i]) > 0 {
			headerCount++
		}
	}
	count := 0
	for {
		count = headerCount
		for i, group := range groups {
			spans[i] = landmarkSpans(lines, group, groupLandmarks[i], before, after)
			for j, span := range spans[i] {
				count += span.end - span.start
				if j > 0 {
					count++ // output separator between distant clusters
				}
			}
		}
		if count <= limit || (before == 0 && after == 0) {
			break
		}
		if after > 0 {
			after--
		} else {
			before--
		}
	}

	// If the number of separated failure clusters itself exceeds the budget,
	// fall back to a bounded window per failed group. The window stays inside
	// that group and is centered on its densest cluster.
	if count > limit {
		return selectFailureWindows(lines, groups, groupLandmarks, limit)
	}

	selected := make([]string, 0, count)
	var headers []GroupHeader
	for i, group := range groups {
		if len(groupLandmarks[i]) == 0 {
			continue
		}
		for j, span := range spans[i] {
			if j > 0 {
				selected = append(selected, "--")
			}
			if j == 0 && group.name != "" {
				headers = append(headers, GroupHeader{Index: len(selected), Name: group.name})
			}
			selected = append(selected, lines[span.start:span.end]...)
		}
	}
	return selected, headers
}

// landmarkSpans returns merged context windows, clipped to a step group and
// to nested GitHub action boundaries. Action markers often delimit failure
// output from later setup/cleanup boilerplate within the same step group.
func landmarkSpans(lines []string, group stepGroup, landmarks []int, before, after int) []lineSpan {
	segments := actionSegments(lines, group)
	segmentIndex := 0
	var spans []lineSpan
	for _, landmark := range landmarks {
		for segmentIndex+1 < len(segments) && landmark >= segments[segmentIndex].end {
			segmentIndex++
		}
		segment := segments[segmentIndex]
		span := lineSpan{
			start: max(segment.start, landmark-before),
			end:   min(segment.end, landmark+after+1),
		}
		if len(spans) > 0 && span.start <= spans[len(spans)-1].end {
			spans[len(spans)-1].end = max(spans[len(spans)-1].end, span.end)
		} else {
			spans = append(spans, span)
		}
	}
	return spans
}

// actionSegments partitions a step group at nested action boundaries. End
// markers stay with the action they close; the next action starts a new span.
func actionSegments(lines []string, group stepGroup) []lineSpan {
	var segments []lineSpan
	start := group.start
	for i := group.start; i < group.end; i++ {
		c := content(lines[i])
		switch {
		case strings.HasPrefix(c, "##[start-action"):
			if i > start {
				segments = append(segments, lineSpan{start, i})
			}
			start = i
		case strings.HasPrefix(c, "##[end-action"):
			segments = append(segments, lineSpan{start, i + 1})
			start = i + 1
		}
	}
	if start < group.end {
		segments = append(segments, lineSpan{start, group.end})
	}
	return segments
}

func selectFailureWindows(lines []string, groups []stepGroup, landmarks [][]int, limit int) ([]string, []GroupHeader) {
	selected := make([]string, 0, limit)
	var headers []GroupHeader
	remainingGroups := 0
	for _, group := range landmarks {
		if len(group) > 0 {
			remainingGroups++
		}
	}
	remaining := limit
	for i, group := range groups {
		if len(landmarks[i]) == 0 || remaining <= 0 {
			continue
		}
		headerCost := 0
		if group.name != "" && remaining > 1 {
			headerCost = 1
		}
		budget := max(1, (remaining-headerCost)/remainingGroups)
		localLandmarks := make([]int, len(landmarks[i]))
		for j, landmark := range landmarks[i] {
			localLandmarks[j] = landmark - group.start
		}
		window := windowAround(lines[group.start:group.end], localLandmarks, budget)
		if len(window)+headerCost > remaining {
			budget = max(1, remaining-headerCost)
			window = windowAround(lines[group.start:group.end], localLandmarks, budget)
		}
		if headerCost > 0 {
			headers = append(headers, GroupHeader{Index: len(selected), Name: group.name})
		}
		selected = append(selected, window...)
		remaining -= len(window) + headerCost
		remainingGroups--
	}
	return selected, headers
}

// dropPostSteps removes post-job step groups that start after the last
// landmark. Groups that might still hold failure context are kept.
func dropPostSteps(lines []string, landmarks []int) ([]string, bool) {
	last := landmarks[len(landmarks)-1]
	keep := make([]bool, len(lines))
	for i := range keep {
		keep[i] = true
	}
	dropped := false
	for _, g := range stepGroups(lines) {
		if g.start > last && isPostStepName(g.name) {
			for i := g.start; i < g.end; i++ {
				keep[i] = false
			}
			dropped = true
		}
	}
	if !dropped {
		return lines, false
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if keep[i] {
			out = append(out, line)
		}
	}
	return out, true
}

// windowAround picks at most n consecutive lines anchored on landmarks:
// each landmark gets roughly a quarter of the window as leading context.
// When landmarks span more than n lines, the window covering the most
// landmarks wins.
func windowAround(lines []string, landmarks []int, n int) []string {
	if n >= len(lines) {
		return lines
	}
	before := n / 4

	type span struct{ start, end int }
	var spans []span
	for _, lm := range landmarks {
		s := max(0, lm-before)
		e := min(len(lines), lm+1+(n-before-1))
		if len(spans) > 0 && s <= spans[len(spans)-1].end {
			spans[len(spans)-1].end = max(spans[len(spans)-1].end, e)
		} else {
			spans = append(spans, span{s, e})
		}
	}
	if len(spans) == 1 && spans[0].end-spans[0].start <= n {
		// A span cut short by either end of the log would show far fewer
		// than n lines; shift it to use the full budget.
		s := min(spans[0].start, len(lines)-n)
		s = max(s, spans[0].end-n)
		s = max(s, 0)
		return lines[s : s+n]
	}

	// Landmarks span more than n lines: keep the densest window.
	bestStart, bestCount := 0, -1
	for _, lm := range landmarks {
		s := min(max(0, lm-before), len(lines)-n)
		lo := sort.SearchInts(landmarks, s)
		hi := sort.SearchInts(landmarks, s+n)
		if count := hi - lo; count > bestCount {
			bestStart, bestCount = s, count
		}
	}
	return lines[bestStart : bestStart+n]
}
