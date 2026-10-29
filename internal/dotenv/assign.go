package dotenv

import (
	"fmt"
	"strings"
)

// Assignment is one KEY=value pair read from pasted text.
type Assignment struct {
	Key   string
	Value string
}

// ParseAssignments reads what people typically paste: lines such as
//
//	export KEY=value
//	export KEY = value
//	KEY="two words"
//	KEY='it'\''s'      (what secdrop's own "copy export" produces)
//
// Shell quoting is understood, so a quoted value may span lines. Blank lines
// and # comments are skipped. Any other line is an error naming the line.
func ParseAssignments(text string) ([]Assignment, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []Assignment
	line := 1
	for i := 0; i < len(text); {
		// Skip blank lines and leading spaces.
		if text[i] == '\n' {
			line++
			i++
			continue
		}
		if text[i] == ' ' || text[i] == '\t' {
			i++
			continue
		}
		if text[i] == '#' {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			continue
		}

		start := line
		rest := text[i:]
		if strings.HasPrefix(rest, "export ") || strings.HasPrefix(rest, "export\t") {
			i += len("export")
			for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
				i++
			}
		}
		eq := strings.IndexAny(text[i:], "=\n")
		if eq < 0 || text[i+eq] != '=' {
			return nil, fmt.Errorf("line %d isn't KEY=value", start)
		}
		key := strings.TrimSpace(text[i : i+eq])
		if !ValidKey(key) {
			return nil, fmt.Errorf("line %d: %q isn't a valid key name", start, key)
		}
		i += eq + 1
		for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
			i++
		}

		val, n, lines, err := shellValue(text[i:])
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", start, err)
		}
		i += n
		line += lines
		out = append(out, Assignment{Key: key, Value: val})
	}
	return out, nil
}

// shellValue reads one value the way a shell would, up to an unquoted end of
// line or an unquoted " #" comment. It returns the value, the bytes consumed
// and how many newlines were inside quotes.
func shellValue(s string) (val string, n, lines int, err error) {
	var b strings.Builder
	pendingSpace := ""
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\n':
			return b.String(), i, lines, nil
		case c == ' ' || c == '\t':
			// Spaces only count if more value follows; a following # starts a comment.
			j := i
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			if j == len(s) || s[j] == '\n' || s[j] == '#' {
				for j < len(s) && s[j] != '\n' {
					j++
				}
				return b.String(), j, lines, nil
			}
			pendingSpace = s[i:j]
			i = j
			continue
		case c == '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return "", 0, 0, fmt.Errorf("a single quote is never closed")
			}
			seg := s[i+1 : i+1+end]
			lines += strings.Count(seg, "\n")
			b.WriteString(pendingSpace + seg)
			i += end + 2
		case c == '"':
			j := i + 1
			var seg strings.Builder
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
					switch s[j] {
					case 'n':
						seg.WriteByte('\n')
					case 't':
						seg.WriteByte('\t')
					case '\n':
						lines++ // a line continuation adds nothing
					default:
						seg.WriteByte(s[j])
					}
					continue
				}
				if s[j] == '\n' {
					lines++
				}
				seg.WriteByte(s[j])
			}
			if j == len(s) {
				return "", 0, 0, fmt.Errorf("a double quote is never closed")
			}
			b.WriteString(pendingSpace + seg.String())
			i = j + 1
		case c == '\\' && i+1 < len(s) && s[i+1] != '\n':
			b.WriteString(pendingSpace + string(s[i+1]))
			i += 2
		default:
			b.WriteString(pendingSpace + string(c))
			i++
		}
		pendingSpace = ""
	}
	return b.String(), i, lines, nil
}
