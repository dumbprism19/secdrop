// Package clip copies text to the clipboard and clears it again later.
package clip

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/atotto/clipboard"
)

// overSSH is true when the system clipboard belongs to another machine; then
// we use the OSC 52 escape so the user's own terminal receives the copy.
var overSSH = os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != ""

func Copy(text string) error {
	if overSSH || clipboard.Unsupported {
		fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
		return nil
	}
	return clipboard.WriteAll(text)
}

// ClearAfter empties the clipboard after d, but only if it still holds text,
// so we never wipe something the user copied in the meantime.
func ClearAfter(d time.Duration, text string) {
	go func() {
		time.Sleep(d)
		if overSSH || clipboard.Unsupported {
			fmt.Fprint(os.Stdout, "\x1b]52;c;\x07")
			return
		}
		if cur, err := clipboard.ReadAll(); err == nil && cur == text {
			_ = clipboard.WriteAll("")
		}
	}()
}
