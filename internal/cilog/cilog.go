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
}

var timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z `)

const groupPrefix = "##[group]"

var landmarkRe = regexp.MustCompile(
	`##\[error\]|^Failed tests\b|^Error:|^Execution halted\b`)

// Select returns the lines to display for a job log:
//   - n <= 0, or a log that fits in n lines, yields the complete log;
//   - otherwise post-job steps are dropped and the remaining lines are
//     windowed around error landmarks;
//   - without landmarks it falls back to the last n lines.
func Select(lines []string, n int) Selection {
	if len(lines) == 0 {
		return Selection{Lines: lines, Mode: ModeFull}
	}
	if n <= 0 || len(lines) <= n {
		return Selection{Lines: lines, Mode: ModeFull}
	}

	landmarks := findLandmarks(lines)
	if len(landmarks) == 0 {
		return Selection{Lines: lines[len(lines)-n:], Mode: ModeTail}
	}

	kept := dropPostSteps(lines, landmarks)
	if len(kept) <= n {
		return Selection{Lines: kept, Mode: ModeFull}
	}

	window := windowAround(kept, findLandmarks(kept), n)
	return Selection{Lines: window, Mode: ModeLandmark}
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
	for i, line := range lines {
		if keep[i] {
			out = append(out, line)
		}
	}
	return out, matched
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
	return timestampRe.ReplaceAllString(line, "")
}

type stepGroup struct {
	name       string
	start, end int // half-open [start, end)
}

// stepGroups parses ##[group] step boundaries. A group extends from its
// marker to the next ##[group] marker (or the end of the log), so it covers
// both the grouped input expansion and the ungrouped step output that
// follows.
func stepGroups(lines []string) []stepGroup {
	var groups []stepGroup
	start := -1
	name := ""
	for i, line := range lines {
		c := content(line)
		if !strings.HasPrefix(c, groupPrefix) {
			continue
		}
		if start >= 0 {
			groups = append(groups, stepGroup{name, start, i})
		}
		start, name = i, strings.TrimSpace(c[len(groupPrefix):])
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

// dropPostSteps removes post-job step groups that start after the last
// landmark. Groups that might still hold failure context are kept.
func dropPostSteps(lines []string, landmarks []int) []string {
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
		return lines
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if keep[i] {
			out = append(out, line)
		}
	}
	return out
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
		return lines[spans[0].start:spans[0].end]
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
