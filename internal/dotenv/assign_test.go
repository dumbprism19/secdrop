package dotenv

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParseAssignments(t *testing.T) {
	cases := []struct {
		in   string
		want []Assignment
	}{
		{"export KEY=value", []Assignment{{"KEY", "value"}}},
		{"export KEY = value", []Assignment{{"KEY", "value"}}},
		{"KEY=value", []Assignment{{"KEY", "value"}}},
		{"  export   KEY  =  two words  ", []Assignment{{"KEY", "two words"}}},
		{`export GREETING='hello world'`, []Assignment{{"GREETING", "hello world"}}},
		{`export NOTE='it'\''s here'`, []Assignment{{"NOTE", "it's here"}}},
		{`KEY="a \"quoted\" word"`, []Assignment{{"KEY", `a "quoted" word`}}},
		{`URL=postgres://u:p@10.0.0.4:5432/db?ssl=true`, []Assignment{{"URL", "postgres://u:p@10.0.0.4:5432/db?ssl=true"}}},
		{"EMPTY=", []Assignment{{"EMPTY", ""}}},
		{"EMPTY=''", []Assignment{{"EMPTY", ""}}},
		{"PORT=8080 # the http port", []Assignment{{"PORT", "8080"}}},
		{"HASH=abc#def", []Assignment{{"HASH", "abc#def"}}},
		{"PEM='-----BEGIN-----\nline2\n-----END-----'", []Assignment{{"PEM", "-----BEGIN-----\nline2\n-----END-----"}}},
		{"# comment\n\nexport A=1\nB = 2\r\nexport C='x y'\n", []Assignment{{"A", "1"}, {"B", "2"}, {"C", "x y"}}},
	}
	for _, c := range cases {
		got, err := ParseAssignments(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v", c.in, got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %+v, want %+v", c.in, got[i], c.want[i])
			}
		}
	}
}

func TestParseAssignmentsErrors(t *testing.T) {
	for _, in := range []string{
		"just a value",
		"export 1KEY=x",
		"KEY='never closed",
		"A=1\nnot an assignment",
	} {
		if _, err := ParseAssignments(in); err == nil {
			t.Errorf("%q should fail", in)
		}
	}
}

// Whatever a real shell makes of an export line, the parser must agree.
func TestParseAssignmentsMatchesShell(t *testing.T) {
	for _, line := range []string{
		`export K='it'\''s here'`,
		`export K="a \"b\" c"`,
		`export K=plain`,
		`export K='$HOME and spaces'`,
		`export K=a\ b`,
	} {
		got, err := ParseAssignments(line)
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %v %v", line, got, err)
		}
		out, err := exec.Command("sh", "-c", line+`; printf %s "$K"`).Output()
		if err != nil {
			t.Fatal(err)
		}
		if got[0].Value != string(out) {
			t.Errorf("%s: parser %q, shell %q", line, got[0].Value, strings.TrimSpace(string(out)))
		}
	}
}
