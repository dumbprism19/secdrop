package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeReleases serves files under /v0.5.0/, like GitHub's release downloads.
func fakeReleases(t *testing.T, files map[string][]byte) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/v0.5.0/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	old := ReleaseURL
	ReleaseURL = srv.URL
	t.Cleanup(func() { ReleaseURL = old })
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func installedExe(t *testing.T) string {
	exe := filepath.Join(t.TempDir(), "secdrop")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestInstallReleaseReplacesBinary(t *testing.T) {
	asset := AssetName("v0.5.0", runtime.GOOS, runtime.GOARCH)
	archive := tarGz(t, map[string]string{"LICENSE": "x", "secdrop": "new binary"})
	fakeReleases(t, map[string][]byte{
		"checksums.txt": []byte(sum([]byte("other")) + "  secdrop_0.5.0_plan9_mips.tar.gz\n" + sum(archive) + "  " + asset + "\n"),
		asset:           archive,
	})
	exe := installedExe(t)
	if err := InstallRelease(context.Background(), "v0.5.0", exe); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(exe)
	fi, _ := os.Stat(exe)
	if string(b) != "new binary" || fi.Mode().Perm() != 0o755 {
		t.Fatalf("got %q mode %v", b, fi.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".secdrop-update-*")); len(left) != 0 {
		t.Fatalf("temp files left behind: %v", left)
	}
}

func TestInstallReleaseRejectsBadChecksum(t *testing.T) {
	asset := AssetName("0.5.0", runtime.GOOS, runtime.GOARCH)
	archive := tarGz(t, map[string]string{"secdrop": "tampered"})
	fakeReleases(t, map[string][]byte{
		"checksums.txt": []byte(sum([]byte("the real one")) + "  " + asset + "\n"),
		asset:           archive,
	})
	exe := installedExe(t)
	err := InstallRelease(context.Background(), "v0.5.0", exe)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("binary must be untouched after a failed check")
	}
}

func TestInstallReleaseNotPublishedYet(t *testing.T) {
	fakeReleases(t, map[string][]byte{}) // tag exists, no files yet
	err := InstallRelease(context.Background(), "v0.5.0", installedExe(t))
	if !errors.Is(err, ErrNotPublished) {
		t.Fatalf("want ErrNotPublished, got %v", err)
	}

	// Checksums are up but this platform's build isn't in them.
	fakeReleases(t, map[string][]byte{"checksums.txt": []byte(sum(nil) + "  secdrop_0.5.0_plan9_mips.tar.gz\n")})
	if err := InstallRelease(context.Background(), "v0.5.0", installedExe(t)); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("want ErrNotPublished, got %v", err)
	}
}

func TestInstallReleaseNoBinaryInArchive(t *testing.T) {
	asset := AssetName("0.5.0", runtime.GOOS, runtime.GOARCH)
	archive := tarGz(t, map[string]string{"README.md": "hi"})
	fakeReleases(t, map[string][]byte{"checksums.txt": []byte(sum(archive) + "  " + asset + "\n"), asset: archive})
	exe := installedExe(t)
	if err := InstallRelease(context.Background(), "v0.5.0", exe); err == nil || !strings.Contains(err.Error(), "no secdrop binary") {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatal("binary must be untouched")
	}
}
