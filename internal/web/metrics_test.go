package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrometheusMetrics(t *testing.T) {
	recordRunStart("test")
	recordRunFinish("test", true)
	rec := httptest.NewRecorder()
	handlePrometheusMetrics(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{"autofilm_uptime_seconds", "autofilm_module_runs_total{module=\"test\"} 1", "autofilm_module_failures_total{module=\"test\"} 1"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}
