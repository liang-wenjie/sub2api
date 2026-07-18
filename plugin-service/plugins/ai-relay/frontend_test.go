package airelay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendServesRelayConfigurationPage(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plugins/ai-relay", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	for _, marker := range []string{
		`/admin/ai-relay`,
		`window.location.replace`,
		`window.top !== window`,
	} {
		if !strings.Contains(rec.Body.String(), marker) {
			t.Fatalf("page is missing %q", marker)
		}
	}
}
