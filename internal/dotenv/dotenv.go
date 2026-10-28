// Package dotenv parses .env files while keeping every untouched line
// byte-for-byte, so editing one key never reformats the rest of the file.
package dotenv

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/dumbprism19/secdrop/internal/secret"
)

type kind int

const (
	kindOther kind = iota // blank lines and comments
	kindEntry
	kindInvalid
)

type line struct {
	kind   kind
	raw    string // original text, including continuation lines for multi-line values
	key    string
	export bool
	value  secret.Value
	dirty  bool
}

// Doc is a parsed .env file.
type Doc struct {
	lines   []line
	trailNL bool
}

// Entry is one KEY=value pair.
type Entry struct {
	Key   string
	Value secret.Value
	Index int // position in file order
}

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]*$`)

// ValidKey reports whether k can be used as a key.
func ValidKey(k string) bool { return keyRe.MatchString(k) }

// Parse reads a .env document. It never fails: lines it can't read are kept
// verbatim and reported by IsEnv.
func Parse(b []byte) *Doc {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	d := &Doc{trailNL: strings.HasSuffix(text, "\n")}
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return d
	}
	src := strings.Split(text, "\n")
	for i := 0; i < len(src); i++ {
		raw := src[i]
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			d.lines = append(d.lines, line{kind: kindOther, raw: raw})
			continue
		}
		body, export := t, false
		if strings.HasPrefix(body, "export ") {
			body, export = strings.TrimSpace(body[len("export "):]), true
		}
		eq := strings.IndexByte(body, '=')
		if eq <= 0 || !ValidKey(strings.TrimSpace(body[:eq])) {
			d.lines = append(d.lines, line{kind: kindInvalid, raw: raw})
			continue
		}
		key := strings.TrimSpace(body[:eq])
		rest := strings.TrimLeft(body[eq+1:], " \t")

		var val string
		switch {
		case strings.HasPrefix(rest, `"`):
			// Double quotes may span lines and support escapes.
			acc := rest[1:]
			j := i
			for closingQuote(acc) < 0 && j+1 < len(src) {
				j++
				acc += "\n" + src[j]
				raw += "\n" + src[j]
			}
			if end := closingQuote(acc); end >= 0 {
				val = unescape(acc[:end])
			} else {
				val = rest // unterminated: keep as written
			}
			i = j
		case strings.HasPrefix(rest, `'`):
			if end := strings.IndexByte(rest[1:], '\''); end >= 0 {
				val = rest[1 : end+1]
			} else {
				val = rest
			}
		default:
			val = strings.TrimRight(rest, " \t")
		}
		d.lines = append(d.lines, line{kind: kindEntry, raw: raw, key: key, export: export, value: secret.FromString(val)})
	}
	return d
}

func closingQuote(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

func unescape(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\r`, "\r", `\t`, "\t", `\"`, `"`, `\\`, `\`)
	return r.Replace(s)
}

// IsEnv reports whether the document looks like a .env file: at least one
// entry and no unreadable lines.
func (d *Doc) IsEnv() bool {
	n := 0
	for _, l := range d.lines {
		switch l.kind {
		case kindInvalid:
			return false
		case kindEntry:
			n++
		}
	}
	return n > 0
}

// Entries returns all keys in file order. If a key repeats, the last one wins,
// matching how most loaders behave.
func (d *Doc) Entries() []Entry {
	seen := map[string]int{}
	var out []Entry
	for _, l := range d.lines {
		if l.kind != kindEntry {
			continue
		}
		if i, ok := seen[l.key]; ok {
			out[i].Value = l.value
			continue
		}
		seen[l.key] = len(out)
		out = append(out, Entry{Key: l.key, Value: l.value, Index: len(out)})
	}
	return out
}

func (d *Doc) Has(key string) bool {
	for _, l := range d.lines {
		if l.kind == kindEntry && l.key == key {
			return true
		}
	}
	return false
}

// Set updates every occurrence of key, or appends it if missing.
func (d *Doc) Set(key string, v secret.Value) {
	found := false
	for i := range d.lines {
		if d.lines[i].kind == kindEntry && d.lines[i].key == key {
			d.lines[i].value, d.lines[i].dirty, found = v, true, true
		}
	}
	if !found {
		d.lines = append(d.lines, line{kind: kindEntry, key: key, value: v, dirty: true})
	}
}

// Delete removes every occurrence of key.
func (d *Doc) Delete(key string) {
	out := d.lines[:0]
	for _, l := range d.lines {
		if l.kind == kindEntry && l.key == key {
			continue
		}
		out = append(out, l)
	}
	d.lines = out
}

// Clone returns an independent copy, so edits can be discarded if a save fails.
func (d *Doc) Clone() *Doc {
	c := &Doc{trailNL: d.trailNL, lines: make([]line, len(d.lines))}
	copy(c.lines, d.lines)
	return c
}

// Bytes renders the document. Unchanged lines are written exactly as read.
func (d *Doc) Bytes() []byte {
	var b bytes.Buffer
	for i, l := range d.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if l.kind != kindEntry || !l.dirty {
			b.WriteString(l.raw)
			continue
		}
		if l.export {
			b.WriteString("export ")
		}
		b.WriteString(l.key)
		b.WriteByte('=')
		b.WriteString(quote(l.value.Reveal()))
	}
	if d.trailNL || len(d.lines) > 0 {
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func quote(v string) string {
	if v == "" {
		return ""
	}
	if !strings.ContainsAny(v, " \t\n\r#\"'\\$`") {
		return v
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(v) + `"`
}
