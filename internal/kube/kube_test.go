package kube

import (
	"encoding/json"
	"testing"
)

func TestBelongsTo(t *testing.T) {
	cases := []struct {
		workload, ns, svc string
		want              bool
	}{
		{"web-chart-orders-svc", "apps", "orders-svc", true},
		{"web-chart-billing-svc-v2", "apps", "billing-svc", false},
		{"web-chart-billing-svc-v2", "apps", "billing-svc-v2", true},
		{"orders-svc", "default", "orders-svc", true},
		{"search-worker", "search", "search", true},
		{"web-chart-ordersvc", "apps", "orders-svc", false},
	}
	for _, c := range cases {
		if got := (Pod{Workload: c.workload, Namespace: c.ns}).BelongsTo(c.svc); got != c.want {
			t.Errorf("%s in %s belongs to %s = %v", c.workload, c.ns, c.svc, got)
		}
	}
}

func TestWithToken(t *testing.T) {
	raw := []byte(`{"apiVersion":"v1","kind":"Config","current-context":"c","clusters":[{"name":"k","cluster":{"server":"https://1.2.3.4"}}],"contexts":[{"name":"c","context":{"cluster":"k","user":"u"}}],"users":[{"name":"u","user":{"exec":{"command":"gke-gcloud-auth-plugin"}}}]}`)
	out, err := withToken(raw, "tok123")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Users []struct {
			Name string
			User map[string]any
		}
		Clusters []struct{ Name string }
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Users) != 1 || cfg.Users[0].Name != "u" || cfg.Users[0].User["token"] != "tok123" || cfg.Users[0].User["exec"] != nil {
		t.Errorf("users not rewritten: %+v", cfg.Users)
	}
	if len(cfg.Clusters) != 1 || cfg.Clusters[0].Name != "k" {
		t.Errorf("clusters changed: %+v", cfg.Clusters)
	}
	if _, err := withToken([]byte(`{"users":[]}`), "t"); err == nil {
		t.Error("expected error for config with no users")
	}
}
