// Package sanitize removes terminal instructions from text the program did not
// write.
//
// A pull request title, an issue body, a comment, a label, a branch name and a
// job's output are all written by somebody else, and all of them reach the
// terminal as cells. Terminal escape sequences are instructions rather than
// text: OSC 52 writes the user's clipboard, OSC 8 forges a link that points
// anywhere, and CSI can move the cursor, clear the screen or set the window
// title. GitHub does not filter them out of a body, so the boundary where an
// API response becomes domain data is where they have to go — every screen
// downstream treats those strings as text.
//
// What is kept is layout: newlines and tabs. Everything else in the control
// ranges goes, including the two-byte forms of CSI and OSC (U+009B and U+009D),
// which a terminal reads exactly like the escape forms.
package sanitize

import (
	"reflect"
	"strings"
	"unicode/utf8"
)

const (
	esc = '\x1b'
	bel = '\x07'
	// csi and osc are the C1 spellings of the two introducers this package
	// removes. Their escape forms are ESC [ and ESC ].
	csi = '\u009b'
	osc = '\u009d'
)

// Text returns s without the control characters and escape sequences a terminal
// would read as instructions. Valid UTF-8 that is not a control character is
// returned unchanged, so an emoji or an accented letter survives intact.
func Text(s string) string {
	if !needsCleaning(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == esc:
			i += 1 + escapeBodyLen(s[i+1:])
		case r == csi:
			i += size + csiBodyLen(s[i+size:])
		case r == osc:
			i += size + stringBodyLen(s[i+size:])
		case r == utf8.RuneError && size == 1:
			// An invalid byte is not text either. Dropping it keeps the rest of
			// the string decodable.
			i++
		case r == '\n' || r == '\t':
			b.WriteString(s[i : i+size])
			i += size
		case r < 0x20 || r == 0x7f:
			i += size
		case r >= 0x80 && r <= 0x9f:
			i += size
		default:
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return b.String()
}

// needsCleaning reports whether s holds anything Text would remove. It is the
// fast path: a body without control characters is returned without a copy.
func needsCleaning(s string) bool {
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\x1b' || c == 0x7f:
			return true
		case c < 0x20:
			if c != '\n' && c != '\t' {
				return true
			}
			i++
		case c < utf8.RuneSelf:
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				return true
			}
			if r >= 0x80 && r <= 0x9f {
				return true
			}
			i += size
		}
	}
	return false
}

// escapeBodyLen returns the length of an escape sequence after its ESC, so the
// caller can skip the whole thing. An unterminated sequence runs to the end of
// the string, because a terminal would swallow everything after it.
func escapeBodyLen(s string) int {
	if s == "" {
		return 0
	}
	switch s[0] {
	case '[':
		return 1 + csiBodyLen(s[1:])
	case ']', 'P', 'X', '^', '_':
		return 1 + stringBodyLen(s[1:])
	case '(', ')', '*', '+', '-', '.', '/', '#', '%', '&':
		// A character-set or font designator takes one more byte. The
		// alternative is leaving that byte behind as visible junk.
		if len(s) < 2 {
			return len(s)
		}
		return 2
	default:
		// A single-byte escape, such as ESC 7 or ESC =.
		return 1
	}
}

// csiBodyLen returns the length of a CSI sequence after its introducer:
// parameters and intermediates, then a final byte in 0x40..0x7e.
func csiBodyLen(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x40 && s[i] <= 0x7e {
			return i + 1
		}
	}
	return len(s)
}

// stringBodyLen returns the length of a control string after its introducer.
// It ends at BEL or at ST, which is ESC followed by a backslash.
func stringBodyLen(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == bel {
			return i + 1
		}
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
	}
	return len(s)
}

// Texts cleans every string reachable from v, in place.
//
// It is called on a decoded API response, which is the one place that knows the
// text came from somebody else: the transports call it, so no screen has to
// remember to. Fields that cannot be set are skipped, and []byte is left alone
// because it is payload rather than cells. Maps are not walked — no response
// type holds one — and a string that is not reachable through a settable field
// keeps its bytes, which is why the markdown renderer cleans its own input too.
func Texts(v any) {
	if v == nil {
		return
	}
	walk(reflect.ValueOf(v), 0)
}

// maxDepth bounds the walk. API responses are shallow; the bound exists so that
// no value could ever spin here.
const maxDepth = 12

func walk(v reflect.Value, depth int) {
	if depth > maxDepth || !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return
		}
		walk(v.Elem(), depth+1)
	case reflect.String:
		// A string reached through a slice or array has no struct field to
		// check, and a value that cannot be set (an unexported field, or a
		// response passed by value) is left as it is.
		if v.CanSet() {
			v.SetString(Text(v.String()))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !field.CanSet() {
				// Unexported, such as the fields inside a time.Time.
				continue
			}
			walk(field, depth+1)
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for i := 0; i < v.Len(); i++ {
			walk(v.Index(i), depth+1)
		}
	}
}
