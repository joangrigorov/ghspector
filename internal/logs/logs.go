// Package logs reads GitHub Actions job logs.
//
// A job log is one text stream with two structures layered on it: an RFC3339
// timestamp on most lines, and the runner's own group markers
// (##[group]Title ... ##[endgroup]) that fence off the output of each step. The
// jobs API already reports the steps and their time windows, so the two can be
// matched up.
//
// This is a package rather than a few functions in the UI because it is pure:
// text in, values out, no terminal and no network. The previous TUI carried the
// same logic inside its 1,975-line update switch, where it could only be
// exercised by driving the whole model.
package logs

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/joangrigorov/ghspector/internal/sanitize"
)

// Markers the runner writes into the stream. They carry meaning in the Actions
// web UI (folding, annotations) and are noise once the text is on a screen.
const (
	groupMarker   = "##[group]"
	sectionMarker = "##[section]"
	commandMarker = "[command]"
)

// controlMarkers are removed from every displayed line.
var controlMarkers = []string{
	groupMarker, "##[endgroup]", sectionMarker, "##[command]", "##[error]",
	"##[warning]", "##[debug]", "##[notice]",
}

// groupGrace is the window either side of a step's own timestamps in which a
// group marker is treated as belonging to that step. The marker's timestamp and
// the step record's timestamps come from different clocks and never agree
// exactly.
const groupGrace = 2 * time.Second

// postGrace is how far past a step's completion a line may arrive and still
// belong to it, so the cleanup phase is attributed to the step that produced it.
const postGrace = time.Second

// Line is one log line, kept in both forms the reader needs: Display for the
// screen, Payload for matching group titles against step names.
type Line struct {
	Display string
	Payload string
	At      time.Time
	HasTime bool
}

// Step is the part of a job step that segmentation needs. It is deliberately
// not the API type: this package stays free of the transport.
type Step struct {
	Name        string
	Conclusion  string
	StartedAt   time.Time
	CompletedAt time.Time
}

// ParseTimestamp reads the RFC3339 timestamp GitHub prefixes to a log line.
func ParseTimestamp(line string) (time.Time, bool) {
	_, _, at, ok := splitTimestamp(ansi.Strip(line))
	return at, ok
}

// Clean returns the display form of a whole raw log: control markers and ANSI
// colour removed, lines with nothing left to show dropped. Timestamps are kept,
// because they are the only record of when a step produced its output.
func Clean(raw string) string {
	parsed := Parse(raw)
	out := make([]string, 0, len(parsed))
	for _, l := range parsed {
		out = append(out, l.Display)
	}
	return strings.Join(out, "\n")
}

// Parse splits a raw log into its readable lines.
func Parse(raw string) []Line {
	if raw == "" {
		return nil
	}
	var out []Line
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		l, ok := parse(line)
		if !ok {
			continue
		}
		out = append(out, l)
	}
	return out
}

// parse reduces one raw line to its display and payload forms. It reports false
// when the line carries nothing to show.
func parse(line string) (Line, bool) {
	// Logs from the API carry ANSI colour; nothing downstream has to reason
	// about escape sequences once they are gone, and the layout measures plain
	// text. Strip removes the sequences and leaves the control characters, and a
	// log is the other place a terminal instruction can arrive: a job's output
	// is written by whatever the job ran.
	line = sanitize.Text(ansi.Strip(line))

	prefix, payload, at, hasTime := splitTimestamp(line)

	// A bare "[command]" prefix is the runner's annotation for a shell command
	// it is about to run. It is punctuation, not content, and the indentation of
	// the command itself is worth keeping.
	body := payload
	if rest, ok := strings.CutPrefix(body, commandMarker); ok {
		body = strings.TrimPrefix(rest, " ")
	}

	// A line that held nothing but the marker closing a group is the fold point
	// itself, not output.
	display := stripMarkers(body)
	if strings.TrimSpace(display) == "" {
		return Line{}, false
	}

	// Payload keeps its markers: group titles are matched against step names.
	return Line{Display: prefix + display, Payload: payload, At: at, HasTime: hasTime}, true
}

// splitTimestamp separates a line's timestamp prefix (kept for display) from
// its payload (used for matching). A line that is nothing but a timestamp
// reports ok with an empty payload, so the caller can drop it as output-free.
func splitTimestamp(line string) (prefix, payload string, at time.Time, ok bool) {
	idx := strings.IndexByte(line, ' ')
	if idx < 0 {
		if t, valid := timestamp(line); valid {
			return "", "", t, true
		}
		return "", line, time.Time{}, false
	}
	t, valid := timestamp(line[:idx])
	if !valid {
		return "", line, time.Time{}, false
	}
	return line[:idx+1], line[idx+1:], t, true
}

// timestamp parses one field as RFC3339 with or without sub-second precision.
func timestamp(field string) (time.Time, bool) {
	// Guard the parse: a short field is never a timestamp, and time.Parse would
	// reject it anyway.
	if len(field) < 19 {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, field); err == nil {
		return t, true
	}
	t, err := time.Parse(time.RFC3339Nano, field)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// stripMarkers removes every runner control marker from a payload.
func stripMarkers(s string) string {
	for _, m := range controlMarkers {
		s = strings.ReplaceAll(s, m, "")
	}
	return s
}

// activeStep is one step that produced output, in the order the runner reported
// it. Skipped steps never appear, so their log lines land on their neighbour.
type activeStep struct {
	// index is the position of the step in the caller's slice, which is what
	// Segment keys its result by.
	index       int
	name        string
	startedAt   time.Time
	completedAt time.Time
	// isPost marks the cleanup steps the runner appends after the job.
	isPost bool
}

// Segment assigns the lines of a raw log to the step that produced them, keyed
// by the step's index in steps. Segments are display-ready text.
func Segment(raw string, steps []Step) map[int]string {
	return segment(Parse(raw), steps)
}

func segment(lines []Line, steps []Step) map[int]string {
	segments := make(map[int]string)

	active := make([]activeStep, 0, len(steps))
	for i, s := range steps {
		if s.Conclusion == "skipped" {
			continue
		}
		active = append(active, activeStep{
			index:       i,
			name:        s.Name,
			startedAt:   s.StartedAt,
			completedAt: s.CompletedAt,
			isPost:      isPostStep(s.Name),
		})
	}
	if len(active) == 0 {
		if text := joinLines(lines); text != "" {
			segments[0] = text
		}
		return segments
	}

	// cursor is the position within active; current is the step index the next
	// line is attributed to.
	cursor := 0
	current := active[0].index

	for _, line := range lines {
		if line.HasTime && cursor < len(active)-1 {
			if advance(line, active[cursor], active[cursor+1]) {
				cursor++
				current = active[cursor].index
			}
		}

		if title, ok := groupTitle(line.Payload); ok {
			if idx, ok := matchGroup(title, line, active, cursor); ok {
				cursor = idx
				current = active[idx].index
			}
		}

		segments[current] += line.Display + "\n"
	}

	return segments
}

// advance reports whether a timestamped line ends the current step and belongs
// to the next one.
func advance(line Line, curr, next activeStep) bool {
	if curr.completedAt.IsZero() {
		return false
	}
	if curr.isPost {
		// Cleanup steps are serial: the next one starts when this one is done.
		return !line.At.Before(curr.completedAt) || !line.At.Before(next.startedAt)
	}
	if !next.isPost && !strings.Contains(line.Payload, postJobCleanup) {
		return false
	}
	// The runner's "Post job cleanup." banner arrives a moment after the last
	// step's completion, which is the signal to move on.
	return strings.Contains(line.Payload, postJobCleanup) ||
		line.At.After(curr.completedAt.Add(postGrace))
}

// postJobCleanup is the banner the runner prints when it starts tearing the job
// down. It is the boundary between the job's own steps and its cleanup.
const postJobCleanup = "Post job cleanup."

// isPostStep reports whether a step name belongs to the cleanup phase.
func isPostStep(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "post ") || lower == "complete job"
}

// groupTitle extracts the title of a group or section marker line.
func groupTitle(payload string) (string, bool) {
	if idx := strings.Index(payload, groupMarker); idx != -1 {
		return payload[idx+len(groupMarker):], true
	}
	if idx := strings.Index(payload, sectionMarker); idx != -1 {
		return payload[idx+len(sectionMarker):], true
	}
	return "", false
}

// matchGroup picks the step a group title belongs to, by scoring the title
// against every step whose time window contains the marker. It never moves the
// cursor backwards, and on a tie it prefers the later step.
func matchGroup(title string, line Line, active []activeStep, cursor int) (int, bool) {
	best := -1
	bestScore := 0
	for i, cand := range active {
		if i < cursor || !covers(cand, line) {
			continue
		}
		score := MatchScore(title, cand.name)
		if score > bestScore || (score == bestScore && score > 0) {
			best, bestScore = i, score
		}
	}
	return best, best >= 0
}

// covers reports whether a group marker's timestamp falls inside a step's
// window, allowing for the skew between the two clocks.
func covers(step activeStep, line Line) bool {
	if !line.HasTime {
		return true
	}
	if line.At.Before(step.startedAt.Add(-groupGrace)) {
		return false
	}
	if step.completedAt.IsZero() {
		return true
	}
	return !line.At.After(step.completedAt.Add(groupGrace))
}

// MatchScore rates how well a log group title names a step. Zero means the two
// cannot be the same step; a higher score is a better match.
func MatchScore(groupTitle, stepName string) int {
	g := strings.ToLower(groupTitle)
	s := strings.ToLower(stepName)

	// A cleanup group never describes a build step, and a pre-step group never
	// describes a post-step: the words are the only reliable distinction when
	// names otherwise look alike.
	if strings.Contains(g, "post") != strings.Contains(s, "post") {
		return 0
	}
	if strings.Contains(g, "pre") != strings.Contains(s, "pre") {
		return 0
	}

	if strings.Contains(g, s) || strings.Contains(s, g) {
		return 1000
	}
	if tightG, tightS := noSeparators(g), noSeparators(s); strings.Contains(tightG, tightS) ||
		strings.Contains(tightS, tightG) {
		return 950
	}

	// Word-level matching: the two names are written by different tools, so
	// "Run actions/checkout@v4" and "Checkout code" have to meet in the middle.
	groupWords := words(g)
	joined := strings.Join(groupWords, " ")

	score := 0
	for _, w := range words(s) {
		if len(w) < 2 || noiseWords[w] {
			continue
		}
		if wordIn(w, groupWords) {
			score += len(w) * 10
			continue
		}
		// A word buried inside a longer group word is a weaker signal than a
		// whole-word match.
		if len(w) >= 4 && strings.Contains(joined, w) {
			score += len(w) * 5
		}
	}
	return score
}

// noiseWords are the words that appear in so many step names that matching on
// them says nothing.
var noiseWords = map[string]bool{
	"run": true, "post": true, "code": true, "job": true, "action": true,
	"in": true, "to": true, "of": true, "for": true, "on": true, "at": true,
	"by": true, "with": true, "the": true, "a": true, "an": true, "and": true,
	"or": true, "is": true, "it": true, "as": true, "be": true,
}

// wordIn reports whether word matches any candidate, counting a simple plural
// as a match.
func wordIn(word string, candidates []string) bool {
	singular := strings.TrimSuffix(word, "s")
	for _, c := range candidates {
		if c == word {
			return true
		}
		if len(word) > 3 && c == singular {
			return true
		}
	}
	return false
}

// separators is the punctuation that varies between a group title and a step
// name for the same thing.
var separators = strings.NewReplacer(" ", "", "-", "", "_", "", "/", "", "@", "", ":", "", ".", "")

// noSeparators removes that punctuation, leaving one unbroken token.
func noSeparators(s string) string { return separators.Replace(s) }

// words splits a name into words, so punctuation reads as a boundary rather
// than gluing two words together.
var wordBreaks = strings.NewReplacer("-", " ", "_", " ", "/", " ", "@", " ", ":", " ")

func words(s string) []string { return strings.Fields(wordBreaks.Replace(s)) }

// joinLines renders segments in order.
func joinLines(lines []Line) string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.Display)
	}
	return strings.Join(out, "\n")
}
