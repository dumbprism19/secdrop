package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Commands shown to people who need to update by hand, matching how they
// installed secdrop.
const (
	GoInstallCmd = "go install " + Command + "@latest"
	CurlCmd      = "curl -fsSL https://raw.githubusercontent.com/dumbprism19/secdrop/main/install.sh | sh"
)

// ReleaseURL is where release archives are downloaded from (the same files
// install.sh uses); tests point it elsewhere.
var ReleaseURL = "https://github.com/dumbprism19/secdrop/releases/download"

// ErrNotPublished means the release is tagged but its files aren't attached
// yet (the release build takes a few minutes after the tag is pushed).
var ErrNotPublished = errors.New("release files aren't published yet")

// maxArchive caps how much is downloaded for one release archive.
const maxArchive = 200 << 20

// AssetName is the release archive for one platform, as named by .goreleaser.yaml.
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("secdrop_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch)
}

// InstallRelease downloads version's archive for this platform, verifies it
// against the release's checksums.txt and replaces the program at exe with
// the binary inside. The swap is a single rename, so exe is either the old
// program or the new one, never a partial file.
func InstallRelease(ctx context.Context, version, exe string) error {
	version = "v" + strings.TrimPrefix(version, "v")
	asset := AssetName(version, runtime.GOOS, runtime.GOARCH)
	base := ReleaseURL + "/" + version

	sums, err := fetch(ctx, base+"/checksums.txt", 1<<20)
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return err
	}
	archive, err := fetch(ctx, base+"/"+asset, maxArchive)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("checksum mismatch for %s, not installing it", asset)
	}
	bin, err := binaryFrom(archive)
	if err != nil {
		return fmt.Errorf("%s: %w", asset, err)
	}
	return replace(exe, bin)
}

// fetch downloads url; a 404 means the release files aren't there yet.
func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotPublished
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("download is larger than expected (over %d MB)", limit>>20)
	}
	return b, nil
}

// checksumFor finds asset's SHA-256 in a checksums.txt ("<hex>  <name>" lines).
func checksumFor(sums []byte, asset string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	// The tag exists but this platform's build isn't listed.
	return "", ErrNotPublished
}

// binaryFrom pulls the secdrop program out of a release .tar.gz.
func binaryFrom(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, errors.New("no secdrop binary in the archive")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "secdrop" {
			return io.ReadAll(io.LimitReader(tr, maxArchive))
		}
	}
}

// replace writes bin next to exe and renames it over exe.
func replace(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	f, err := os.CreateTemp(dir, ".secdrop-update-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("can't write to %s", dir)
		}
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once renamed
	if _, err := f.Write(bin); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, exe)
}
