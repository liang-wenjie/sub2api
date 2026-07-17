package backend

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
)

func TestRelayForwardsAccountBearerKeyAndReturnsOpenAIImages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer agnes-account-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := readRequestBody(t, r); got != `{"model":"agnes-image-2.1-flash","prompt":"poster","size":"1K","ratio":"1:1","extra_body":{"response_format":"url"}}` {
			t.Fatalf("body = %s", got)
		}
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://cdn.example/image.png"}]}`))
	}))
	defer upstream.Close()

	repository := NewMemoryRouteRepository()
	repository.routes["agnes:team-a"] = RouteConfig{
		Platform: "agnes", Slug: "team-a", BaseURL: upstream.URL + "/v1", DefaultModel: "agnes-image-2.1-flash", Enabled: true, MaxN: 1,
	}
	handler := NewRelayHandler(repository, NewDefaultAdapterRegistry(), upstream.Client())
	req := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"model":"gpt-image-1","prompt":"poster","size":"1024x1024","response_format":"url"}`))
	req.Header.Set("Authorization", "Bearer agnes-account-key")
	rec := httptest.NewRecorder()

	handler.Relay(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestRelayRejectsMissingBearerAndUnknownSlug(t *testing.T) {
	handler := NewRelayHandler(NewMemoryRouteRepository(), NewDefaultAdapterRegistry(), http.DefaultClient)

	missingKey := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"prompt":"poster"}`))
	missingKeyRec := httptest.NewRecorder()
	handler.Relay(missingKeyRec, missingKey)
	if missingKeyRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d", missingKeyRec.Code)
	}

	unknownSlug := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"prompt":"poster"}`))
	unknownSlug.Header.Set("Authorization", "Bearer account-key")
	unknownSlugRec := httptest.NewRecorder()
	handler.Relay(unknownSlugRec, unknownSlug)
	if unknownSlugRec.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d", unknownSlugRec.Code)
	}
}

func TestRegisterRoutesMountsRelayEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, nil, NewRelayHandler(NewMemoryRouteRepository(), NewDefaultAdapterRegistry(), http.DefaultClient))

	req := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"prompt":"poster"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminCanManageAgnesRoutes(t *testing.T) {
	repository := NewMemoryRouteRepository()
	mux := http.NewServeMux()
	RegisterRoutes(mux, principal.NewMiddleware(), NewRelayHandler(repository, NewDefaultAdapterRegistry(), http.DefaultClient))

	request := httptest.NewRequest(http.MethodPut, "/plugins/ai-relay/api/routes/agnes/team-a", bytes.NewBufferString(`{"base_url":"https://apihub.agnes-ai.com/v1","default_model":"agnes-image-2.1-flash","enabled":true}`))
	request.Header.Set("X-Sub2api-User-Id", "7")
	request.Header.Set("X-Sub2api-User-Role", "admin")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admin upsert status = %d; body=%s", response.Code, response.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/plugins/ai-relay/api/routes", nil)
	listRequest.Header.Set("X-Sub2api-User-Id", "7")
	listRequest.Header.Set("X-Sub2api-User-Role", "admin")
	listResponse := httptest.NewRecorder()
	mux.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"team-a"`)) {
		t.Fatalf("admin list = %d; body=%s", listResponse.Code, listResponse.Body.String())
	}
}

func readRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(r.Body); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}
