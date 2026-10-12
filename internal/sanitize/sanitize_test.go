package sanitize

import (
	"strings"
	"testing"
)

func TestTextRemovesTerminalInstructions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is returned as it is", "fix the thing", "fix the thing"},
		{"a newline and a tab are layout", "one\ntwo\tthree", "one\ntwo\tthree"},
		{"an emoji survives", "ship it 🚀", "ship it 🚀"},
		{"accented text survives", "café del mar", "café del mar"},
		{"a CJK body survives", "修正してください", "修正してください"},
		{
			name: "an OSC 52 clipboard write is removed whole",
			in:   "before\x1b]52;c;aGFja2Vk\x07after",
			want: "beforeafter",
		},
		{
			name: "an OSC 8 link is removed whole",
			in:   "see \x1b]8;;https://evil.example\x07click\x1b]8;;\x07 here",
			want: "see click here",
		},
		{
			name: "an OSC terminated by ST is removed",
			in:   "a\x1b]0;title\x1b\\b",
			want: "ab",
		},
		{
			name: "a CSI sequence is removed",
			in:   "a\x1b[31mred\x1b[0mb",
			want: "aredb",
		},
		{
			name: "a CSI with a private prefix is removed",
			in:   "a\x1b[?1049hb",
			want: "ab",
		},
		{
			name: "a bare escape takes its catch-all byte with it",
			in:   "a\x1b7b\x1b=c",
			want: "abc",
		},
		{
			name: "a charset designator takes its third byte with it",
			in:   "a\x1b(Bb",
			want: "ab",
		},
		{
			name: "a C1 CSI is removed",
			in:   "a\u009b31mb",
			want: "ab",
		},
		{
			name: "the other C0 controls go",
			in:   "a\x07b\x0cc\x7fd",
			want: "abcd",
		},
		{
			name: "a carriage return goes, so CRLF text does not litter the frame",
			in:   "line one\r\nline two",
			want: "line one\nline two",
		},
		{
			name: "an unterminated sequence runs to the end",
			in:   "kept\x1b]52;c;never-finished",
			want: "kept",
		},
		{
			name: "an invalid byte is dropped rather than breaking the rest",
			in:   "ok\xffand more",
			want: "okand more",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.in); got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTextKeepsLongBodiesIntact(t *testing.T) {
	body := strings.Repeat("A paragraph of ordinary prose.\n\n", 200)
	if got := Text(body); got != body {
		t.Errorf("a body with no control characters changed: %d bytes in, %d out", len(body), len(got))
	}
}

type inner struct {
	Title string
	Stats []string
}

type response struct {
	Body   string
	Inner  inner
	Nested *inner
	Items  []inner
	Number int
	Raw    []byte
	Tagged inner
}

func TestTextsCleansEveryReachableString(t *testing.T) {
	r := response{
		Body:   "read \x1b]52;c;x\x07this",
		Inner:  inner{Title: "a\x1b[0mb", Stats: []string{"ok", "c\x07d"}},
		Nested: &inner{Title: "e\x1b[1mf"},
		Items:  []inner{{Title: "g\x1b[0mh"}, {Title: "plain"}},
		Number: 7,
		Raw:    []byte("raw \x1b[0m stays"),
	}
	Texts(&r)

	if r.Body != "read this" {
		t.Errorf("body = %q", r.Body)
	}
	if r.Inner.Title != "ab" || r.Inner.Stats[1] != "cd" {
		t.Errorf("inner = %+v", r.Inner)
	}
	if r.Nested.Title != "ef" {
		t.Errorf("nested = %+v", r.Nested)
	}
	if r.Items[0].Title != "gh" || r.Items[1].Title != "plain" {
		t.Errorf("items = %+v", r.Items)
	}
	if string(r.Raw) != "raw \x1b[0m stays" {
		t.Errorf("bytes were rewritten: %q", r.Raw)
	}
	if r.Number != 7 {
		t.Errorf("number = %d", r.Number)
	}
}

func TestTextsAcceptsAValueOrAPointer(t *testing.T) {
	byValue := response{Body: "x\x07y"}
	Texts(byValue)
	if byValue.Body != "x\x07y" {
		t.Error("a value copy was rewritten in place, which cannot happen")
	}

	byPointer := &response{Body: "x\x07y"}
	Texts(byPointer)
	if byPointer.Body != "xy" {
		t.Errorf("pointer body = %q, want the controls gone", byPointer.Body)
	}

	Texts(nil)
}
