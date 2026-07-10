package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	hostprincipal "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

func TestRouterPluginPageAllowsEmbeddingFromForwardedHost(t *testing.T) {
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "app.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("plugin page status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self' https://app.example.com" {
		t.Fatalf("plugin page CSP = %q, want %q", got, "frame-ancestors 'self' https://app.example.com")
	}
}

func TestRouterPluginPageAllowsEmbeddingFromRefererFallback(t *testing.T) {
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	req.Header.Set("Referer", "https://app.example.com/user/custom-page/plugin")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("plugin page status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self' https://app.example.com" {
		t.Fatalf("plugin page CSP = %q, want %q", got, "frame-ancestors 'self' https://app.example.com")
	}
}

func TestRouterPluginPageAlwaysAllowsSameOriginEmbedding(t *testing.T) {
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("plugin page status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
		t.Fatalf("plugin page CSP = %q, want %q", got, "frame-ancestors 'self'")
	}
}

func TestRouterSharedAuthGenerateAndListHistory(t *testing.T) {
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer launch-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer launch-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":42,"email":"user@example.com","username":"launch-user","role":"user"}}`))
	}))
	defer mainSite.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Fatalf("provider authorization = %q, want %q", got, "Bearer provider-secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/generated.png","revised_prompt":"make a poster"}]}`))
	}))
	defer upstream.Close()

	restoreMainSiteResolver(t, mainSite.URL)
	router := NewRouter(config.Config{ListenAddr: ":0"})

	body := bytes.NewBufferString(`{"prompt":"make a poster","provider_api_key":"provider-secret","model":"gpt-image-1","size":"1024x1024","inputs":{"conversation_id":"conversation-live-test"}}`)
	generateReq := httptest.NewRequest(http.MethodPost, "/plugins/image-generation/api/generate", body)
	generateReq.Header.Set("Authorization", "Bearer launch-token")
	addForwardedProviderOrigin(generateReq, upstream.URL)
	generateRec := httptest.NewRecorder()
	router.ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusCreated {
		t.Fatalf("generate status = %d, want %d; body=%s", generateRec.Code, http.StatusCreated, generateRec.Body.String())
	}
	secondBody := bytes.NewBufferString(`{"prompt":"make another poster","provider_api_key":"provider-secret","model":"gpt-image-1","size":"1024x1024","inputs":{"conversation_id":"conversation-live-test"}}`)
	secondGenerateReq := httptest.NewRequest(http.MethodPost, "/plugins/image-generation/api/generate", secondBody)
	secondGenerateReq.Header.Set("Authorization", "Bearer launch-token")
	addForwardedProviderOrigin(secondGenerateReq, upstream.URL)
	secondGenerateRec := httptest.NewRecorder()
	router.ServeHTTP(secondGenerateRec, secondGenerateReq)
	if secondGenerateRec.Code != http.StatusCreated {
		t.Fatalf("second generate status = %d, want %d; body=%s", secondGenerateRec.Code, http.StatusCreated, secondGenerateRec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/plugins/image-generation/api/history", nil)
	historyReq.Header.Set("Authorization", "Bearer launch-token")
	historyRec := httptest.NewRecorder()
	router.ServeHTTP(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status = %d, want %d; body=%s", historyRec.Code, http.StatusOK, historyRec.Body.String())
	}

	var payload struct {
		Items []model.HistoryRecord `json:"items"`
	}
	if err := json.NewDecoder(historyRec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("history item count = %d, want 2", len(payload.Items))
	}
	for _, item := range payload.Items {
		if item.UserID != 42 {
			t.Fatalf("history user_id = %d, want 42", item.UserID)
		}
		if item.ConversationID != "conversation-live-test" {
			t.Fatalf("history conversation_id = %q, want %q", item.ConversationID, "conversation-live-test")
		}
		if _, ok := item.Request["provider_api_key"]; ok {
			t.Fatal("history exposed provider_api_key")
		}
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/plugins/image-generation/api/history/"+payload.Items[0].ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer launch-token")
	deleteRec := httptest.NewRecorder()
	router.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete history status = %d, want %d; body=%s", deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}

	historyAfterDeleteReq := httptest.NewRequest(http.MethodGet, "/plugins/image-generation/api/history", nil)
	historyAfterDeleteReq.Header.Set("Authorization", "Bearer launch-token")
	historyAfterDeleteRec := httptest.NewRecorder()
	router.ServeHTTP(historyAfterDeleteRec, historyAfterDeleteReq)
	if historyAfterDeleteRec.Code != http.StatusOK {
		t.Fatalf("history after delete status = %d, want %d; body=%s", historyAfterDeleteRec.Code, http.StatusOK, historyAfterDeleteRec.Body.String())
	}

	var afterDeletePayload struct {
		Items []model.HistoryRecord `json:"items"`
	}
	if err := json.NewDecoder(historyAfterDeleteRec.Body).Decode(&afterDeletePayload); err != nil {
		t.Fatal(err)
	}
	if len(afterDeletePayload.Items) != 1 {
		t.Fatalf("history item count after delete = %d, want 1", len(afterDeletePayload.Items))
	}
}

func TestRouterGenerateUsesConfiguredMainServiceBaseURL(t *testing.T) {
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("main service path = %s, want /v1/images/generations", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Fatalf("provider authorization = %q, want %q", got, "Bearer provider-secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/generated.png"}]}`))
	}))
	defer mainSite.Close()

	restoreMainSiteResolver(t, mainSite.URL)
	t.Setenv("PLUGIN_MAIN_SERVICE_BASE_URL", mainSite.URL)
	router := NewRouter(config.Config{ListenAddr: ":0"})

	body := bytes.NewBufferString(`{"prompt":"make a poster","provider_api_key":"provider-secret","model":"gpt-image-1","size":"1024x1024"}`)
	req := httptest.NewRequest(http.MethodPost, "/plugins/image-generation/api/generate", body)
	req.Header.Set("X-Sub2api-User-Id", "42")
	req.Header.Set("X-Sub2api-User-Role", "user")
	req.Header.Set("X-Forwarded-Proto", "http")
	req.Header.Set("X-Forwarded-Host", "192.168.0.230:8080")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("generate status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func restoreMainSiteResolver(t *testing.T, baseURL string) {
	t.Helper()
	previous := hostprincipal.ResolveMainSiteBaseCandidates
	hostprincipal.ResolveMainSiteBaseCandidates = func(_ *http.Request) []string {
		return []string{baseURL}
	}
	t.Cleanup(func() {
		hostprincipal.ResolveMainSiteBaseCandidates = previous
	})
}

func addForwardedProviderOrigin(req *http.Request, upstreamURL string) {
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		panic(err)
	}
	req.Header.Set("X-Forwarded-Proto", parsed.Scheme)
	req.Header.Set("X-Forwarded-Host", parsed.Host)
}
