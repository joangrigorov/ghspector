package logs

import (
	"strings"
	"testing"
	"time"
)

// base is the fixed instant every test log is built around.
var base = time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)

// ts renders an instant the way the runner prefixes a log line.
func ts(t time.Time) string { return t.Format(time.RFC3339Nano) }

// logLines joins the given lines the way the runner writes them.
func logLines(lines ...string) string { return strings.Join(lines, "\n") }

func TestParseTimestampReadsBothPrecisions(t *testing.T) {
	cases := []struct {
		name string
		line string
		want time.Time
	}{
		{"no sub-second digits", "2024-05-01T10:00:00Z running", base},
		{"nanoseconds", "2024-05-01T10:00:00.123456789Z running", base.Add(123456789 * time.Nanosecond)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseTimestamp(tc.line)
			if !ok {
				t.Fatalf("ParseTimestamp(%q) did not find a timestamp", tc.line)
			}
			if !got.Equal(tc.want) {
				t.Errorf("ParseTimestamp(%q) = %s, want %s", tc.line, got, tc.want)
			}
		})
	}
}

func TestParseTimestampRejectsLinesWithoutOne(t *testing.T) {
	for _, line := range []string{"", "no timestamp here", "12:00:00 short", "##[group]Run build"} {
		if got, ok := ParseTimestamp(line); ok {
			t.Errorf("ParseTimestamp(%q) = %s, true; want false", line, got)
		}
	}
}

func TestParseStripsMarkersAndColour(t *testing.T) {
	line := ts(base) + " \x1b[32m##[group]Run tests\x1b[0m"
	lines := Parse(line)
	if len(lines) != 1 {
		t.Fatalf("Parse returned %d lines, want 1", len(lines))
	}
	if want := ts(base) + " Run tests"; lines[0].Display != want {
		t.Errorf("Display = %q, want %q", lines[0].Display, want)
	}
}

func TestParseKeepsTimestampsAndIndentation(t *testing.T) {
	line := ts(base) + "     indented output"
	lines := Parse(line)
	if len(lines) != 1 {
		t.Fatalf("Parse returned %d lines, want 1", len(lines))
	}
	if want := ts(base) + "     indented output"; lines[0].Display != want {
		t.Errorf("Display = %q, want %q", lines[0].Display, want)
	}
	if lines[0].Payload != "    indented output" {
		t.Errorf("Payload = %q, want the message without the timestamp", lines[0].Payload)
	}
}

func TestParseDropsLinesWithNothingToShow(t *testing.T) {
	raw := logLines(
		ts(base)+" ##[endgroup]",
		ts(base),
		"",
		ts(base)+" real output",
	)
	lines := Parse(raw)
	if len(lines) != 1 {
		t.Fatalf("Parse kept %d lines, want only the one with output: %+v", len(lines), lines)
	}
	if !strings.Contains(lines[0].Display, "real output") {
		t.Errorf("the surviving line is %q, want the one carrying output", lines[0].Display)
	}
}

func TestParseUnwrapsTheBareCommandMarker(t *testing.T) {
	line := ts(base) + " [command]/usr/bin/git status"
	lines := Parse(line)
	if len(lines) != 1 {
		t.Fatalf("Parse returned %d lines, want 1", len(lines))
	}
	if want := ts(base) + " /usr/bin/git status"; lines[0].Display != want {
		t.Errorf("Display = %q, want %q", lines[0].Display, want)
	}
}

func TestCleanJoinsTheReadableLines(t *testing.T) {
	raw := logLines(
		ts(base)+" ##[group]Build",
		ts(base)+" ##[endgroup]",
		ts(base)+" ##[error]boom",
	)
	got := Clean(raw)
	want := ts(base) + " Build\n" + ts(base) + " boom"
	if got != want {
		t.Errorf("Clean() = %q, want %q", got, want)
	}
}

// twoStepLog is a log whose group markers name the steps, which is the signal
// segmentation keys on.
func twoStepLog() string {
	return logLines(
		ts(base)+" ##[group]Set up job",
		ts(base.Add(time.Second))+" checked out",
		ts(base.Add(time.Second))+" ##[endgroup]",
		ts(base.Add(2*time.Second))+" ##[group]Run tests",
		ts(base.Add(3*time.Second))+" ok 1 - works",
		ts(base.Add(4*time.Second))+" ##[endgroup]",
	)
}

func TestSegmentAssignsLinesToTheStepThatProducedThem(t *testing.T) {
	steps := []Step{
		{Name: "Set up job", StartedAt: base, CompletedAt: base.Add(2 * time.Second)},
		{Name: "Run tests", StartedAt: base.Add(2 * time.Second), CompletedAt: base.Add(10 * time.Second)},
	}
	segments := Segment(twoStepLog(), steps)

	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2: %#v", len(segments), segments)
	}
	setup := segments[0]
	if !strings.Contains(setup, "Set up job") || !strings.Contains(setup, "checked out") {
		t.Errorf("segment 0 = %q, want the setup group and its output", setup)
	}
	if strings.Contains(setup, "ok 1 - works") {
		t.Errorf("segment 0 = %q, want no output from the test step", setup)
	}
	tests := segments[1]
	if !strings.Contains(tests, "Run tests") || !strings.Contains(tests, "ok 1 - works") {
		t.Errorf("segment 1 = %q, want the test group and its output", tests)
	}
}

func TestSegmentSkippedStepsGetNothing(t *testing.T) {
	steps := []Step{
		{Name: "Set up job", StartedAt: base, CompletedAt: base.Add(2 * time.Second)},
		{Name: "Cache", Conclusion: "skipped"},
		{Name: "Run tests", StartedAt: base.Add(2 * time.Second), CompletedAt: base.Add(10 * time.Second)},
	}
	segments := Segment(twoStepLog(), steps)

	if _, ok := segments[1]; ok {
		t.Errorf("segment 1 was produced for a skipped step: %q", segments[1])
	}
	if !strings.Contains(segments[2], "ok 1 - works") {
		t.Errorf("segment 2 = %q, want the test output", segments[2])
	}
}

func TestSegmentAttributesCleanupToItsOwnStep(t *testing.T) {
	steps := []Step{
		{Name: "Build", StartedAt: base, CompletedAt: base.Add(2 * time.Second)},
		{Name: "Post Build", StartedAt: base.Add(2 * time.Second)},
	}
	raw := logLines(
		ts(base)+" ##[group]Build",
		ts(base.Add(time.Second))+" compiling",
		ts(base.Add(2500*time.Millisecond))+" Post job cleanup.",
		ts(base.Add(3*time.Second))+" deleted the cache",
	)
	segments := Segment(raw, steps)

	if !strings.Contains(segments[0], "compiling") {
		t.Errorf("segment 0 = %q, want the build output", segments[0])
	}
	if strings.Contains(segments[0], "deleted the cache") {
		t.Errorf("segment 0 = %q, want the cleanup line attributed elsewhere", segments[0])
	}
	if !strings.Contains(segments[1], "deleted the cache") {
		t.Errorf("segment 1 = %q, want the cleanup output", segments[1])
	}
}

func TestSegmentWithoutStepsKeepsTheWholeLog(t *testing.T) {
	segments := Segment(twoStepLog(), nil)
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1: %#v", len(segments), segments)
	}
	if !strings.Contains(segments[0], "ok 1 - works") {
		t.Errorf("segment 0 = %q, want every line of the log", segments[0])
	}
}

func TestSegmentEmptyLog(t *testing.T) {
	if segments := Segment("", nil); len(segments) != 0 {
		t.Errorf("got %d segments for an empty log, want none", len(segments))
	}
}

func TestMatchScoreRanksTheRightStepHighest(t *testing.T) {
	cases := []struct {
		name  string
		group string
		step  string
		want  int
	}{
		{"identical", "Run npm test", "Run npm test", 1000},
		{"step name inside the group title", "Run actions/checkout@v4", "Run actions/checkout@v4", 1000},
		{"post versus build", "Post Build", "Build", 0},
		{"pre versus build", "Pre Build", "Build", 0},
		{"unrelated names", "Run npm ci", "Checkout code", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchScore(tc.group, tc.step); got != tc.want {
				t.Errorf("MatchScore(%q, %q) = %d, want %d", tc.group, tc.step, got, tc.want)
			}
		})
	}
}

func TestMatchScoreCountsSharedWords(t *testing.T) {
	score := MatchScore("Run actions/checkout@v4", "Checkout code")
	if score <= 0 {
		t.Fatalf("MatchScore for a shared word = %d, want a positive score", score)
	}
	if score >= 1000 {
		t.Errorf("MatchScore = %d, want less than a containment match", score)
	}
}

func TestMatchScoreIgnoresNoiseWords(t *testing.T) {
	// "run" carries no signal: two unrelated commands must not match on it.
	if score := MatchScore("Run foo", "Run bar"); score != 0 {
		t.Errorf("MatchScore = %d, want 0 for names sharing only a noise word", score)
	}
}

// TestParseLeavesNoTerminalInstructions covers the second boundary a body from
// somebody else crosses: a job's output is whatever the job ran, and ansi.Strip
// removes the sequences while leaving the control characters behind.
func TestParseLeavesNoTerminalInstructions(t *testing.T) {
	line := ts(base) + " a\x1b]52;c;cGF5bG9hZA==\x07b\x07c\rd"
	lines := Parse(line)
	if len(lines) != 1 {
		t.Fatalf("Parse returned %d lines, want 1", len(lines))
	}
	if want := ts(base) + " abcd"; lines[0].Display != want {
		t.Errorf("Display = %q, want %q", lines[0].Display, want)
	}
}
