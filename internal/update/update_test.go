package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsReleaseAndNewer(t *testing.T) {
	for v, want := range map[string]bool{
		"0.3.0": true, "v1.2.3": true, "dev": false,
		"0.2.1-0.20261029085543-ab2754482aae+dirty": false, "0.4.0-rc.1": false,
	} {
		if IsRelease(v) != want {
			t.Errorf("IsRelease(%q) = %v", v, !want)
		}
	}
	if !Newer("0.3.0", "v0.4.0") || Newer("0.4.0", "v0.4.0") || Newer("dev", "v9.0.0") || Newer("0.10.0", "v0.9.0") {
		t.Fatal("Newer is wrong")
	}
}

// fakeProxy serves a version list and the newest go.mod, like proxy.golang.org.
func fakeProxy(t *testing.T, list, newest, gomod string) *int {
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		switch r.URL.Path {
		case "/" + Module + "/@v/list":
			w.Write([]byte(list))
		case "/" + Module + "/@v/" + newest + ".mod":
			w.Write([]byte(gomod))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := ProxyURL
	ProxyURL = srv.URL
	t.Cleanup(func() { ProxyURL = old })
	return hits
}

func TestLatestSkipsRetracted(t *testing.T) {
	fakeProxy(t, "v0.1.0\nv0.2.0\nv0.3.0\nv0.4.0-rc.1\n", "v0.3.0",
		"module "+Module+"\n\ngo 1.26\n\nretract v0.2.0\n")
	if v, err := Latest(context.Background()); err != nil || v != "v0.3.0" {
		t.Fatalf("got %s %v", v, err)
	}

	// If the newest itself is retracted, fall back to the one before.
	fakeProxy(t, "v0.1.0\nv0.2.0\nv0.3.0\n", "v0.3.0",
		"module "+Module+"\n\nretract [v0.2.0, v0.3.0]\n")
	if v, err := Latest(context.Background()); err != nil || v != "v0.1.0" {
		t.Fatalf("got %s %v", v, err)
	}
}

func TestCheckUsesCache(t *testing.T) {
	hits := fakeProxy(t, "v0.3.0\n", "v0.3.0", "module "+Module+"\n")
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if v, err := Check(context.Background(), dir); err != nil || v != "v0.3.0" {
			t.Fatalf("got %s %v", v, err)
		}
	}
	if *hits != 2 { // list + go.mod, once
		t.Fatalf("network hit %d times, want only the first check", *hits)
	}
	// An old cache is refreshed.
	os.WriteFile(filepath.Join(dir, "update.json"), []byte(`{"checked":"`+time.Now().Add(-2*time.Hour).Format(time.RFC3339)+`","latest":"v0.1.0"}`), 0o600)
	if v, _ := Check(context.Background(), dir); v != "v0.3.0" {
		t.Fatalf("stale cache not refreshed: %s", v)
	}
}

func TestCheckOfflineFailsQuickly(t *testing.T) {
	old := ProxyURL
	ProxyURL = "http://127.0.0.1:1" // nothing listens here
	defer func() { ProxyURL = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := Check(ctx, t.TempDir()); err == nil {
		t.Fatal("expected an error offline")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("offline check must not hang")
	}
}
