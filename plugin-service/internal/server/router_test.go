package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

func TestRouter_LaunchGenerateAndListHistory(t *testing.T) {
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

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer provider-secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/generated.png","revised_prompt":"make a poster"}]}`))
	}))
	defer upstream.Close()

	cfg := config.Config{
		ListenAddr:     ":0",
		SessionTTL:     time.Hour,
		HistoryEnabled: true,
	}
	router := NewRouter(cfg)

	launchReq := httptest.NewRequest(http.MethodGet, "/launch?token=launch-token&plugin=image-generation&path=/plugins/image-generation", nil)
	addPublicBaseHeaders(launchReq, mainSite.URL)
	launchRec := httptest.NewRecorder()
	router.ServeHTTP(launchRec, launchReq)
	if launchRec.Code != http.StatusFound {
		t.Fatalf("launch status = %d, want %d", launchRec.Code, http.StatusFound)
	}
	cookies := launchRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("launch did not set a session cookie")
	}

	body := bytes.NewBufferString(`{"prompt":"make a poster","provider_api_key":"provider-secret","model":"gpt-image-1","size":"1024x1024"}`)
	generateReq := httptest.NewRequest(http.MethodPost, "/api/plugins/image-generation/generate", body)
	generateReq.AddCookie(cookies[0])
	addForwardedProviderOrigin(generateReq, upstream.URL)
	generateRec := httptest.NewRecorder()
	router.ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusCreated {
		t.Fatalf("generate status = %d, want %d; body=%s", generateRec.Code, http.StatusCreated, generateRec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/history", nil)
	historyReq.AddCookie(cookies[0])
	historyRec := httptest.NewRecorder()
	router.ServeHTTP(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history status = %d, want %d", historyRec.Code, http.StatusOK)
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
}

func TestRouter_HealthDoesNotExposeSpecificPlugin(t *testing.T) {
	router := NewRouter(config.Config{
		ListenAddr:     ":0",
		SessionTTL:     time.Hour,
		HistoryEnabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["plugin"]; ok {
		t.Fatalf("health payload exposes a specific plugin: %#v", payload)
	}
	if got := payload["status"]; got != "ok" {
		t.Fatalf("health status payload = %#v, want %q", got, "ok")
	}
}

func TestRouter_LaunchCreatesSessionFromMainSiteAuthToken(t *testing.T) {
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

	cfg := config.Config{
		ListenAddr:     ":0",
		SessionTTL:     time.Hour,
		HistoryEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/launch?session=launch-token&plugin=image-generation&path=/plugins/image-generation", nil)
	addPublicBaseHeaders(req, mainSite.URL)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("launch status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Result().Header.Get("Location"); got != "/plugins/image-generation" {
		t.Fatalf("launch redirect location = %q, want %q", got, "/plugins/image-generation")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("launch did not set a session cookie")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body=%s", meRec.Code, http.StatusOK, meRec.Body.String())
	}

	var principal model.CurrentPrincipal
	if err := json.NewDecoder(meRec.Body).Decode(&principal); err != nil {
		t.Fatal(err)
	}
	if principal.UserID != 42 {
		t.Fatalf("user_id = %d, want 42", principal.UserID)
	}
	if principal.Username != "launch-user" {
		t.Fatalf("username = %q, want %q", principal.Username, "launch-user")
	}
	if principal.Plugin != "image-generation" {
		t.Fatalf("plugin = %q, want %q", principal.Plugin, "image-generation")
	}
}

func TestRouter_ImageGenerationNamespacedGenerateAndList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer provider-secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/generated.png","revised_prompt":"make a poster"}]}`))
	}))
	defer upstream.Close()

	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)
	cookie := devLoginCookie(t, router, "/dev/login?user_id=7&role=user&email=user%40example.com&username=user&path=/plugins/image-generation")

	configReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/config", nil)
	configReq.AddCookie(cookie)
	configRec := httptest.NewRecorder()
	router.ServeHTTP(configRec, configReq)
	if configRec.Code != http.StatusOK {
		t.Fatalf("namespaced config status = %d, want %d; body=%s", configRec.Code, http.StatusOK, configRec.Body.String())
	}
	var configPayload map[string]any
	if err := json.NewDecoder(configRec.Body).Decode(&configPayload); err != nil {
		t.Fatal(err)
	}
	if got := configPayload["plugin_key"]; got != "image-generation" {
		t.Fatalf("namespaced config plugin_key = %#v, want %q", got, "image-generation")
	}

	body := bytes.NewBufferString(`{"prompt":"make a poster","provider_api_key":"provider-secret","model":"gpt-image-1","size":"1024x1024"}`)
	generateReq := httptest.NewRequest(http.MethodPost, "/api/plugins/image-generation/generate", body)
	generateReq.AddCookie(cookie)
	addForwardedProviderOrigin(generateReq, upstream.URL)
	generateRec := httptest.NewRecorder()
	router.ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusCreated {
		t.Fatalf("namespaced generate status = %d, want %d; body=%s", generateRec.Code, http.StatusCreated, generateRec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/history", nil)
	historyReq.AddCookie(cookie)
	historyRec := httptest.NewRecorder()
	router.ServeHTTP(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("namespaced history status = %d, want %d; body=%s", historyRec.Code, http.StatusOK, historyRec.Body.String())
	}
	var historyPayload struct {
		Items []model.HistoryRecord `json:"items"`
	}
	if err := json.NewDecoder(historyRec.Body).Decode(&historyPayload); err != nil {
		t.Fatal(err)
	}
	if len(historyPayload.Items) != 1 {
		t.Fatalf("namespaced history item count = %d, want 1", len(historyPayload.Items))
	}
	if _, ok := historyPayload.Items[0].Request["provider_api_key"]; ok {
		t.Fatal("namespaced history exposed provider_api_key")
	}

	creationsReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/creations", nil)
	creationsReq.AddCookie(cookie)
	creationsRec := httptest.NewRecorder()
	router.ServeHTTP(creationsRec, creationsReq)
	if creationsRec.Code != http.StatusOK {
		t.Fatalf("namespaced creations status = %d, want %d; body=%s", creationsRec.Code, http.StatusOK, creationsRec.Body.String())
	}
	var creationsPayload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(creationsRec.Body).Decode(&creationsPayload); err != nil {
		t.Fatal(err)
	}
	if len(creationsPayload.Items) != 1 {
		t.Fatalf("namespaced creation count = %d, want 1", len(creationsPayload.Items))
	}
	if got := creationsPayload.Items[0]["plugin_key"]; got != "image-generation" {
		t.Fatalf("namespaced creation plugin_key = %#v, want %q", got, "image-generation")
	}
}

func TestRouter_DevLoginAndMe(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	loginReq := httptest.NewRequest(http.MethodGet, "/dev/login?user_id=7&role=admin&email=admin%40example.com&username=dev-admin&path=/plugins/image-generation", nil)
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("dev login status = %d, want %d", loginRec.Code, http.StatusFound)
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("dev login did not set a session cookie")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want %d; body=%s", meRec.Code, http.StatusOK, meRec.Body.String())
	}

	var principal model.CurrentPrincipal
	if err := json.NewDecoder(meRec.Body).Decode(&principal); err != nil {
		t.Fatal(err)
	}
	if principal.UserID != 7 {
		t.Fatalf("user_id = %d, want 7", principal.UserID)
	}
	if principal.Role != model.RoleAdmin {
		t.Fatalf("role = %q, want %q", principal.Role, model.RoleAdmin)
	}
}

func TestRouter_MeRequiresSession(t *testing.T) {
	cfg := config.Config{
		ListenAddr:     ":0",
		SessionTTL:     time.Hour,
		HistoryEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without session status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "plugin session is required") {
		t.Fatalf("me without session body = %q, want error containing %q", rec.Body.String(), "plugin session is required")
	}
}

func TestRouter_DevLoginDisabled(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: false,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/dev/login?path=/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("dev login disabled status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRouter_DevLoginRejectsUnknownPluginSelection(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/dev/login?plugin=unknown&path=/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("dev login unknown plugin status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRouter_PluginMetadataEndpoint(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

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

func TestRouter_PluginDetailEndpoint(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("plugin detail status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload model.PluginMetadata
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Key != "image-generation" {
		t.Fatalf("plugin key = %q, want %q", payload.Key, "image-generation")
	}
	if payload.DefaultEntryPath != "/plugins/image-generation" {
		t.Fatalf("plugin default_entry_path = %q, want %q", payload.DefaultEntryPath, "/plugins/image-generation")
	}
}

func TestRouter_PluginDetailEndpointNotFound(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/plugins/unknown", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown plugin detail status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRouter_LaunchAcceptsCanonicalPluginKey(t *testing.T) {
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "sub2api_session=session-abc" {
			t.Fatalf("cookie = %q, want %q", got, "sub2api_session=session-abc")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":42,"email":"user@example.com","username":"cookie-user","role":"user"}}`))
	}))
	defer mainSite.Close()

	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/launch?plugin=image-generation&path=/plugins/image-generation", nil)
	req.Header.Set("Cookie", "sub2api_session=session-abc")
	addPublicBaseHeaders(req, mainSite.URL)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("launch canonical plugin status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Result().Header.Get("Location"); got != "/plugins/image-generation" {
		t.Fatalf("launch canonical plugin redirect = %q, want %q", got, "/plugins/image-generation")
	}

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("launch canonical plugin did not set a session cookie")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(cookies[0])
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me after canonical launch status = %d, want %d; body=%s", meRec.Code, http.StatusOK, meRec.Body.String())
	}

	var principal model.CurrentPrincipal
	if err := json.NewDecoder(meRec.Body).Decode(&principal); err != nil {
		t.Fatal(err)
	}
	if principal.Plugin != "image-generation" {
		t.Fatalf("principal plugin = %q, want %q", principal.Plugin, "image-generation")
	}
	if principal.Username != "cookie-user" {
		t.Fatalf("principal username = %q, want %q", principal.Username, "cookie-user")
	}
}

func TestRouter_LaunchRedirectsToPluginEntryByDefault(t *testing.T) {
	mainSite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer launch-token" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer launch-token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":42,"email":"user@example.com","username":"user","role":"user"}}`))
	}))
	defer mainSite.Close()

	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/launch?token=launch-token&plugin=image-generation", nil)
	addPublicBaseHeaders(req, mainSite.URL)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("launch status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Result().Header.Get("Location"); got != "/plugins/image-generation" {
		t.Fatalf("launch redirect location = %q, want %q", got, "/plugins/image-generation")
	}
}

func TestRouter_RedirectNormalizationBlocksSchemeRelativePath(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/dev/login?path=%2F%2Fevil.example%2Fpath", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("dev login redirect status = %d, want %d; body=%s", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Result().Header.Get("Location"); got != "/plugins/image-generation" {
		t.Fatalf("redirect location = %q, want %q", got, "/plugins/image-generation")
	}
}

func TestRouter_LegacyCompatibilityRoutesRemoved(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/app"},
		{method: http.MethodGet, path: "/api/config"},
		{method: http.MethodPost, path: "/api/generate"},
		{method: http.MethodGet, path: "/api/creations"},
		{method: http.MethodGet, path: "/api/history"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want %d", tc.method, tc.path, rec.Code, http.StatusNotFound)
		}
	}
}

func TestRouter_ImageGenerationHostedPage(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	appReq := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	appRec := httptest.NewRecorder()
	router.ServeHTTP(appRec, appReq)
	if appRec.Code != http.StatusOK {
		t.Fatalf("hosted page status = %d, want %d; body=%s", appRec.Code, http.StatusOK, appRec.Body.String())
	}

	body := appRec.Body.String()
	for _, needle := range []string{
		`<div id="app" data-plugin-api-base="/api/plugins/image-generation"></div>`,
		`<title>Image Generation Plugin</title>`,
		`/plugins/image-generation/assets/app.js`,
		`/plugins/image-generation/assets/app.css`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("hosted page missing %q", needle)
		}
	}
}

func TestRouter_ImageGenerationHostedPageVersionBumpsAssets(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hosted page status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	for _, needle := range []string{
		`/plugins/image-generation/assets/app.js?v=`,
		`/plugins/image-generation/assets/app.css?v=`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("hosted page missing cache-busted asset %q", needle)
		}
	}
}

func TestRouter_ImageGenerationFrontendResponsesDisableBrowserCache(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	for _, path := range []string{
		"/plugins/image-generation",
		"/plugins/image-generation/assets/app.js",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d; body=%s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}

func TestRouter_ImageGenerationHostedAssets(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation/assets/app.js", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hosted asset status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-plugin-api-base`) && !strings.Contains(rec.Body.String(), `/api/plugins/image-generation`) {
		t.Fatalf("hosted asset body missing namespaced api base; body=%s", rec.Body.String())
	}
}

func TestRouter_ImageGenerationHostedAssetKeepsEmptyLiveConversation(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation/assets/app.js", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hosted asset status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `D.id===L.value`) {
		t.Fatal("hosted image app does not preserve the selected empty live conversation")
	}
	if strings.Contains(body, `D.id.startsWith("conversation-live")&&D.messages.length>0)`) {
		t.Fatal("hosted image app still drops the empty live conversation when history is empty")
	}
}

func TestRouter_ImageGenerationHostedAssetDoesNotRestoreComposerAfterSend(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation/assets/app.js", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hosted asset status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `W(I,w=>({...w,title:w.messages.length===0?p.slice(0,24):w.title,preview:t("imageGeneration.generationWaiting"),messages:[...w.messages,v,$]})),y.value="",g.value=!0;try{`) {
		t.Fatal("hosted image app no longer clears the composer before sending")
	}
	if strings.Contains(body, `catch(w){y.value=p,`) {
		t.Fatal("hosted image app still restores the composer text after send")
	}
}

func TestRouter_CreationsVisibilityFollowsRole(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer user-a-key":
			_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/a.png","revised_prompt":"image a"}]}`))
		case "Bearer user-b-key":
			_, _ = w.Write([]byte(`{"created":1783000001,"data":[{"url":"https://cdn.example.com/b.png","revised_prompt":"image b"}]}`))
		default:
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
	}))
	defer upstream.Close()

	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)

	adminCookie := devLoginCookie(t, router, "/dev/login?user_id=99&role=admin&email=admin%40example.com&username=admin&path=/plugins/image-generation")
	userCookie := devLoginCookie(t, router, "/dev/login?user_id=7&role=user&email=user%40example.com&username=user&path=/plugins/image-generation")
	otherCookie := devLoginCookie(t, router, "/dev/login?user_id=8&role=user&email=other%40example.com&username=other&path=/plugins/image-generation")

	for _, tc := range []struct {
		cookie      *http.Cookie
		prompt      string
		providerKey string
	}{
		{cookie: userCookie, prompt: "user image", providerKey: "user-a-key"},
		{cookie: otherCookie, prompt: "other image", providerKey: "user-b-key"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/plugins/image-generation/generate", bytes.NewBufferString(`{"prompt":"`+tc.prompt+`","provider_api_key":"`+tc.providerKey+`","model":"gpt-image-1"}`))
		req.AddCookie(tc.cookie)
		addForwardedProviderOrigin(req, upstream.URL)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("generate status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	userCreationsReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/creations", nil)
	userCreationsReq.AddCookie(userCookie)
	userCreationsRec := httptest.NewRecorder()
	router.ServeHTTP(userCreationsRec, userCreationsReq)
	if userCreationsRec.Code != http.StatusOK {
		t.Fatalf("user creations status = %d, want %d; body=%s", userCreationsRec.Code, http.StatusOK, userCreationsRec.Body.String())
	}
	var userPayload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(userCreationsRec.Body).Decode(&userPayload); err != nil {
		t.Fatal(err)
	}
	if len(userPayload.Items) != 1 {
		t.Fatalf("user creation count = %d, want 1", len(userPayload.Items))
	}
	if got := userPayload.Items[0]["user_id"]; got != float64(7) {
		t.Fatalf("user creation user_id = %#v, want 7", got)
	}

	adminCreationsReq := httptest.NewRequest(http.MethodGet, "/api/plugins/image-generation/creations", nil)
	adminCreationsReq.AddCookie(adminCookie)
	adminCreationsRec := httptest.NewRecorder()
	router.ServeHTTP(adminCreationsRec, adminCreationsReq)
	if adminCreationsRec.Code != http.StatusOK {
		t.Fatalf("admin creations status = %d, want %d; body=%s", adminCreationsRec.Code, http.StatusOK, adminCreationsRec.Body.String())
	}
	var adminPayload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(adminCreationsRec.Body).Decode(&adminPayload); err != nil {
		t.Fatal(err)
	}
	if len(adminPayload.Items) != 2 {
		t.Fatalf("admin creation count = %d, want 2", len(adminPayload.Items))
	}
}

func TestRouter_ImageGenerationGeneratePreservesUpstreamHTTPError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`))
	}))
	defer upstream.Close()

	cfg := config.Config{
		ListenAddr:      ":0",
		SessionTTL:      time.Hour,
		HistoryEnabled:  true,
		DevLoginEnabled: true,
	}
	router := NewRouter(cfg)
	cookie := devLoginCookie(t, router, "/dev/login?user_id=7&role=user&email=user%40example.com&username=user&path=/plugins/image-generation")

	req := httptest.NewRequest(http.MethodPost, "/api/plugins/image-generation/generate", bytes.NewBufferString(`{"prompt":"draw a city","provider_api_key":"provider-secret","model":"gpt-image-1"}`))
	req.AddCookie(cookie)
	addForwardedProviderOrigin(req, upstream.URL)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("generate status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}

	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["error"]; got != "Invalid API key" {
		t.Fatalf("error body = %#v, want %q", got, "Invalid API key")
	}
}

func devLoginCookie(t *testing.T, router http.Handler, path string) *http.Cookie {
	t.Helper()
	loginReq := httptest.NewRequest(http.MethodGet, path, nil)
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("dev login status = %d, want %d; body=%s", loginRec.Code, http.StatusFound, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("dev login did not set a session cookie")
	}
	return cookies[0]
}

func addForwardedProviderOrigin(req *http.Request, upstreamURL string) {
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		panic(err)
	}
	req.Header.Set("X-Forwarded-Proto", parsed.Scheme)
	req.Header.Set("X-Forwarded-Host", parsed.Host)
}

func addPublicBaseHeaders(req *http.Request, baseURL string) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		panic(err)
	}
	req.Header.Set("X-Forwarded-Proto", parsed.Scheme)
	req.Header.Set("X-Forwarded-Host", parsed.Host)
	req.Header.Set("Origin", parsed.Scheme+"://"+parsed.Host)
}
