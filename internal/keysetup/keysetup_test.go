package keysetup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall(t *testing.T) {
	for name, in := range map[string]string{
		"empty":    "[]\n",
		"existing": "[\n  { \"key\": \"cmd+j\", \"command\": \"x\" }\n]\n",
		"trailing": "[\n  { \"key\": \"cmd+j\", \"command\": \"x\" },\n]\n",
		"missing":  "",
	} {
		p := filepath.Join(t.TempDir(), "keybindings.json")
		if in != "" {
			os.WriteFile(p, []byte(in), 0o600)
		}
		if _, err := install(p); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := os.ReadFile(p)
		if name != "trailing" { // trailing commas are valid JSONC, not JSON
			var v []map[string]any
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatalf("%s: not valid JSON: %v\n%s", name, err, b)
			}
		}
		if msg, _ := install(p); !strings.Contains(msg, "already") {
			t.Fatalf("%s: second run should be a no-op", name)
		}
	}
}
