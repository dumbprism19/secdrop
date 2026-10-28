package store

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"
)

// Runs only with SECDROP_LIVE_BUCKET set. Lists names; decrypts nothing.
func TestLiveList(t *testing.T) {
	b := os.Getenv("SECDROP_LIVE_BUCKET")
	if b == "" {
		t.Skip("set SECDROP_LIVE_BUCKET")
	}
	st, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ss, err := st.List(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d secrets", len(ss))
	for i, s := range ss {
		if i < 25 {
			t.Logf("%-45s → %-28s %s", s.Object, s.Service, s.Env)
		}
	}
}

// Runs only with SECDROP_LIVE_DISCOVER=1.
func TestLiveDiscover(t *testing.T) {
	if os.Getenv("SECDROP_LIVE_DISCOVER") == "" {
		t.Skip("set SECDROP_LIVE_DISCOVER=1")
	}
	st, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for ev := range st.Discover(context.Background(), []string{os.Getenv("CLOUDSDK_CORE_PROJECT")}) {
		if ev.Found != nil {
			t.Logf("found gs://%s in %s", ev.Found.Name, ev.Found.Project)
		}
		if ev.Done {
			t.Logf("done: scanned %d/%d err=%v", ev.Scanned, ev.Total, ev.Err)
		}
	}
}

// Runs only with SECDROP_LIVE_BUCKET and SECDROP_LIVE_OBJECT. Lists versions
// and audit events; decrypts nothing.
func TestLiveHistory(t *testing.T) {
	b, o := os.Getenv("SECDROP_LIVE_BUCKET"), os.Getenv("SECDROP_LIVE_OBJECT")
	if b == "" || o == "" {
		t.Skip("set SECDROP_LIVE_BUCKET and SECDROP_LIVE_OBJECT")
	}
	st, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ss, _ := st.List(context.Background(), b)
	sec, err := FindSecret(ss, o, "")
	if err != nil {
		t.Fatal(err)
	}
	bi, err := st.Bucket(context.Background(), b)
	t.Logf("bucket project=%d versioning=%v err=%v", bi.ProjectNumber, bi.Versioning, err)
	vs, err := st.History(context.Background(), sec)
	t.Logf("versions=%d err=%v", len(vs), err)
	for i, v := range vs {
		if i < 5 {
			t.Logf("  gen=%d updated=%s live=%v", v.Generation, v.Updated.Format(time.RFC3339), v.Live)
		}
	}
	evs, err := st.Audit(context.Background(), sec)
	t.Logf("audit events=%d err=%v", len(evs), err)
	for i, e := range evs {
		if i < 6 {
			t.Logf("  %s %s %s denied=%v", e.Time.Format(time.RFC3339), e.Kind, e.Who, e.Denied)
		}
	}
}

func TestLiveVersionCounts(t *testing.T) {
	b := os.Getenv("SECDROP_LIVE_COUNTS")
	if b == "" {
		t.Skip("set SECDROP_LIVE_COUNTS")
	}
	st, _ := New(context.Background())
	ss, _ := st.List(context.Background(), b)
	for _, s := range ss {
		if vs, _ := st.History(context.Background(), s); len(vs) > 1 {
			t.Logf("%-40s %d versions", s.Object, len(vs))
		}
	}
}

func TestLiveMostRecent(t *testing.T) {
	b := os.Getenv("SECDROP_LIVE_RECENT")
	if b == "" {
		t.Skip("set SECDROP_LIVE_RECENT")
	}
	st, _ := New(context.Background())
	ss, err := st.List(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].Updated.After(ss[j].Updated) })
	for _, s := range ss[:5] {
		t.Logf("%-40s updated %s", s.Object, s.Updated.Local().Format("2006-01-02 15:04"))
	}
}
