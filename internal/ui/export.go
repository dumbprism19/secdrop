package ui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func newExportModal(o *opened) *modal {
	in := newInput("path")
	name := o.sec.Service
	if o.sec.Env != "" {
		name += "." + o.sec.Env
	}
	in.SetValue("./" + name + ".env")
	in.CursorEnd()
	in.Focus()
	return &modal{kind: mExport, o: o, valIn: in}
}

// exportFile writes the decrypted env exactly as stored, readable only by
// the user, and never replaces an existing file.
func exportFile(path string, o *opened) (int, error) {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return 0, errors.New(path + " already exists. Choose another name.")
	}
	if err != nil {
		return 0, err
	}
	if _, err := f.Write(o.raw.Bytes()); err != nil {
		f.Close()
		return 0, err
	}
	return len(o.entries(false)), f.Close()
}
