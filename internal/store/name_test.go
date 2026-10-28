package store

import "testing"

func TestSplitName(t *testing.T) {
	cases := []struct{ in, svc, env string }{
		{"payments-api-prod-env", "payments-api", "prod"},
		{"payments-api-production-env", "payments-api", "prod"},
		{"auth-service-staging-env", "auth-service", "staging"},
		{"notifier/dev.env", "notifier", "dev"},
		{"search_uat", "search", "uat"},
		{"shared-config", "shared-config", ""},
		{"prod-env", "prod-env", "prod"},
		{"bootstrap-test-nonprod-env", "bootstrap-test", "nonprod"},
		{"assets-svc-nonprod-env", "assets-svc", "nonprod"},
		{"env-file", "env-file", ""},
	}
	for _, c := range cases {
		s, e := SplitName(c.in)
		if s != c.svc || e != c.env {
			t.Errorf("%s → %q %q, want %q %q", c.in, s, e, c.svc, c.env)
		}
	}
}
