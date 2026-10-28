package dotenv

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dumbprism19/secdrop/internal/secret"
)

const sample = `# billing service
export DATABASE_URL=postgres://u:p@10.0.0.4:5432/billing
REDIS_URL = redis://10.0.0.3:6379

STRIPE_KEY="sk_test_abc # not a comment"
PEM="-----BEGIN-----
line2
-----END-----"
SINGLE='it''s'
EMPTY=
`

func TestParseKeepsUntouchedLines(t *testing.T) {
	d := Parse([]byte(sample))
	if !d.IsEnv() {
		t.Fatal("expected env")
	}
	if got := string(d.Bytes()); got != sample {
		t.Fatalf("round trip changed file:\n%s", got)
	}
	want := map[string]string{
		"DATABASE_URL": "postgres://u:p@10.0.0.4:5432/billing",
		"REDIS_URL":    "redis://10.0.0.3:6379",
		"STRIPE_KEY":   "sk_test_abc # not a comment",
		"PEM":          "-----BEGIN-----\nline2\n-----END-----",
		"SINGLE":       "it",
		"EMPTY":        "",
	}
	es := d.Entries()
	if len(es) != len(want) {
		t.Fatalf("got %d entries", len(es))
	}
	for _, e := range es {
		if e.Value.Reveal() != want[e.Key] {
			t.Errorf("%s = %q", e.Key, e.Value.Reveal())
		}
	}
}

func TestSetOnlyRewritesThatLine(t *testing.T) {
	d := Parse([]byte(sample))
	d.Set("REDIS_URL", secret.FromString("redis://new host"))
	d.Set("NEW_KEY", secret.FromString("plain"))
	d.Delete("EMPTY")
	got := string(d.Bytes())
	want := `# billing service
export DATABASE_URL=postgres://u:p@10.0.0.4:5432/billing
REDIS_URL="redis://new host"

STRIPE_KEY="sk_test_abc # not a comment"
PEM="-----BEGIN-----
line2
-----END-----"
SINGLE='it''s'
NEW_KEY=plain
`
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
	if v := Parse([]byte(got)).Entries()[1].Value.Reveal(); v != "redis://new host" {
		t.Fatalf("reparse = %q", v)
	}
}

func TestNotEnv(t *testing.T) {
	if Parse([]byte(`{"type":"service_account"}`)).IsEnv() {
		t.Fatal("json should not be env")
	}
}

func TestValueNeverPrints(t *testing.T) {
	v := secret.FromString("hunter2")
	for _, s := range []string{fmt.Sprint(v), fmt.Sprintf("%v %+v %#v %s %q %x", v, v, v, v, v, v)} {
		if strings.Contains(s, "hunter2") {
			t.Fatalf("leaked: %s", s)
		}
	}
}
