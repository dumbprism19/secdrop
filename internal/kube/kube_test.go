package kube

import "testing"

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
