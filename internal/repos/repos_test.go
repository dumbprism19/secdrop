package repos

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitInit(t *testing.T, dir, remote string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		if args[0] == "remote" && remote == "" {
			continue
		}
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	gitInit(t, filepath.Join(root, "orders-svc"), "")                                         // folder name
	gitInit(t, filepath.Join(root, "work", "checkout"), "git@github.com:acme/Orders_Svc.git") // remote, case and _ differ
	gitInit(t, filepath.Join(root, "billing"), "https://github.com/acme/billing-svc")         // other service
	gitInit(t, filepath.Join(root, "a", "b", "c", "d", "orders-svc"), "")                     // too deep
	gitInit(t, filepath.Join(root, "node_modules", "orders-svc"), "")                         // skipped folder
	gitInit(t, filepath.Join(root, "orders-svc", "nested", "orders-svc"), "")                 // inside a found repo

	got, err := Find(context.Background(), "orders-svc", []string{root, root})
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(root)
	want := map[string]string{
		filepath.Join(real, "orders-svc"):       "folder name",
		filepath.Join(real, "work", "checkout"): "origin remote",
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, r := range got {
		if want[r.Path] != r.Why {
			t.Errorf("unexpected %+v", r)
		}
	}
}

func TestRemoteName(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:acme/orders-svc.git":    "orders-svc",
		"https://github.com/acme/orders-svc":    "orders-svc",
		"https://github.com/acme/orders-svc/":   "orders-svc",
		"ssh://git@host:22/acme/orders-svc.git": "orders-svc",
	} {
		if got := remoteName(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}

func TestTarget(t *testing.T) {
	dir := t.TempDir()
	if got := Target(dir, "prod"); got != filepath.Join(dir, ".env") {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(dir, ".env"), nil, 0o600)
	if got := Target(dir, "prod"); got != filepath.Join(dir, ".env.prod") {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(dir, ".env.prod"), nil, 0o600)
	if got := Target(dir, "prod"); got != "" {
		t.Fatal(got)
	}
}

func TestTracked(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir, "")
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".env\n"), 0o644)
	ctx := context.Background()
	if in, ign := Tracked(ctx, filepath.Join(dir, ".env")); !in || !ign {
		t.Errorf(".env: in=%v ignored=%v", in, ign)
	}
	if in, ign := Tracked(ctx, filepath.Join(dir, ".env.prod")); !in || ign {
		t.Errorf(".env.prod: in=%v ignored=%v", in, ign)
	}
	if in, _ := Tracked(ctx, filepath.Join(t.TempDir(), "x.env")); in {
		t.Error("plain temp dir is not a repo")
	}
}
