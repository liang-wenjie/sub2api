package imagegeneration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendInjectsPluginAuthBridgeScript(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("frontend status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	for _, needle := range []string{
		`localStorage.getItem("auth_token")`,
		`window.location.search`,
		`Authorization`,
		`/plugins/image-generation/api`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("frontend html missing auth bridge marker %q", needle)
		}
	}
}
