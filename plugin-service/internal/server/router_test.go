package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	hostprincipal "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
)

func TestRouterLaunchRedirectsAfterAuthenticatingWithMainSite(t *testing.T) {
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/v1/auth/me")
		}
		if got := r.Header.Get("Authorization"); got != "Bearer launch-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer launch-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":42,"email":"user@example.com","username":"launch-user","role":"user"}}`))
	}))
	defer mainSite.Close()

	restoreMainSiteResolver(t, mainSite.URL)
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/launch?token=launch-token&plugin=image-generation", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("launch status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	location := rec.Result().Header.Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got := parsed.Path; got != "/plugins/image-generation" {
		t.Fatalf("launch redirect path = %q, want %q", got, "/plugins/image-generation")
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("launch should not set plugin cookies, got %d", len(rec.Result().Cookies()))
	}
}

func TestRouterLaunchPreservesTokenForPluginPageRequests(t *testing.T) {
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer launch-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer launch-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":42,"email":"user@example.com","username":"launch-user","role":"user"}}`))
	}))
	defer mainSite.Close()

	restoreMainSiteResolver(t, mainSite.URL)
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/launch?token=launch-token&plugin=image-generation&path=/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("launch status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}

	location := rec.Result().Header.Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got := parsed.Path; got != "/plugins/image-generation" {
		t.Fatalf("launch redirect path = %q, want %q", got, "/plugins/image-generation")
	}
	if got := parsed.Query().Get("token"); got != "launch-token" {
		t.Fatalf("launch redirect token = %q, want %q", got, "launch-token")
	}
}

func TestRouterLaunchIgnoresRequestControlledMainSiteSignals(t *testing.T) {
	mainSiteHit := false
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainSiteHit = true
		if got := r.Header.Get("Authorization"); got != "Bearer attack-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer attack-token")
		}
		if got := r.Header.Get("Cookie"); got != "sub2api_session=session-abc" {
			t.Fatalf("cookie = %q, want %q", got, "sub2api_session=session-abc")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":51,"email":"user51@example.com","username":"legit-user","role":"user"}}`))
	}))
	defer mainSite.Close()

	restoreMainSiteResolver(t, mainSite.URL)
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/launch?plugin=image-generation&path=/plugins/image-generation&src_host=https%3A%2F%2Fevil.example", nil)
	req.Header.Set("Authorization", "Bearer attack-token")
	req.Header.Set("Cookie", "sub2api_session=session-abc")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Referer", "https://evil.example/path")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("launch status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if !mainSiteHit {
		t.Fatal("main site was not contacted")
	}
}

func TestRouterMeRequiresMainSiteAuth(t *testing.T) {
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"unauthorized"}`))
	}))
	defer mainSite.Close()

	restoreMainSiteResolver(t, mainSite.URL)
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "main site authentication required") {
		t.Fatalf("me body = %q", rec.Body.String())
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

	body := bytes.NewBufferString(`{"prompt":"make a poster","provider_api_key":"provider-secret","model":"gpt-image-1","size":"1024x1024"}`)
	generateReq := httptest.NewRequest(http.MethodPost, "/api/plugins/image-generation/generate", body)
	generateReq.Header.Set("Authorization", "Bearer launch-token")
	addForwardedProviderOrigin(generateReq, upstream.URL)
	generateRec := httptest.NewRecorder()
	router.ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusCreated {
		t.Fatalf("generate status = %d, want %d; body=%s", generateRec.Code, http.StatusCreated, generateRec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/history", nil)
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
	if len(payload.Items) != 1 {
		t.Fatalf("history item count = %d, want 1", len(payload.Items))
	}
	if payload.Items[0].UserID != 42 {
		t.Fatalf("history user_id = %d, want 42", payload.Items[0].UserID)
	}
	if _, ok := payload.Items[0].Request["provider_api_key"]; ok {
		t.Fatal("history exposed provider_api_key")
	}
}

func TestRouterPluginMetadataEndpoint(t *testing.T) {
	router := NewRouter(config.Config{ListenAddr: ":0"})

	req := httptest.NewRequest(http.MethodGet, "/api/plugins", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("plugins status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Items []model.PluginMetadata `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) == 0 {
		t.Fatal("plugin metadata items = 0, want at least 1")
	}
	if payload.Items[0].DefaultEntryPath != "/plugins/image-generation" {
		t.Fatalf("plugin default_entry_path = %q, want %q", payload.Items[0].DefaultEntryPath, "/plugins/image-generation")
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
