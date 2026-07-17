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
		`/plugins/ai-relay/assets/app.js`,
		`/plugins/ai-relay/assets/app.css`,
		`data-plugin-api-base="/plugins/ai-relay/api"`,
	} {
		if !strings.Contains(rec.Body.String(), marker) {
			t.Fatalf("page is missing %q", marker)
		}
	}
}
