package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

func TestRouter_LaunchGenerateAndListHistory(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer provider-secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/generated.png","revised_prompt":"make a poster"}]}`))
	}))
	defer upstream.Close()

	cfg := config.Config{
		ListenAddr:           ":0",
		BaseURL:              "http://plugin.test",
		LaunchSharedSecret:   "secret",
		MainSiteOrigin:       "http://main.test",
		SessionTTL:           time.Hour,
		HistoryEnabled:       true,
		PluginKey:            "gen",
		ImageProviderBaseURL: upstream.URL,
	}
	router := NewRouter(cfg)
	tickets := service.NewTicketService(cfg.LaunchSharedSecret)
	ticket, err := tickets.CreateTicket(model.LaunchClaims{
		UserID:   42,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "gen",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	launchReq := httptest.NewRequest(http.MethodGet, "/launch?ticket="+ticket+"&path=/app", nil)
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
	generateReq := httptest.NewRequest(http.MethodPost, "/api/generate", body)
	generateReq.AddCookie(cookies[0])
	generateRec := httptest.NewRecorder()
	router.ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusCreated {
		t.Fatalf("generate status = %d, want %d; body=%s", generateRec.Code, http.StatusCreated, generateRec.Body.String())
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/history", nil)
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

func TestRouter_DevLoginAndMe(t *testing.T) {
	cfg := config.Config{
		ListenAddr:         ":0",
		BaseURL:            "http://plugin.test",
		LaunchSharedSecret: "secret",
		MainSiteOrigin:     "http://main.test",
		SessionTTL:         time.Hour,
		HistoryEnabled:     true,
		PluginKey:          "gen",
		DevLoginEnabled:    true,
	}
	router := NewRouter(cfg)

	loginReq := httptest.NewRequest(http.MethodGet, "/dev/login?user_id=7&role=admin&email=admin%40example.com&username=dev-admin&path=/app", nil)
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

func TestRouter_AppPageRendersAfterDevLoginRedirect(t *testing.T) {
	cfg := config.Config{
		ListenAddr:         ":0",
		BaseURL:            "http://plugin.test",
		LaunchSharedSecret: "secret",
		MainSiteOrigin:     "http://main.test",
		SessionTTL:         time.Hour,
		HistoryEnabled:     true,
		PluginKey:          "gen",
		DevLoginEnabled:    true,
	}
	router := NewRouter(cfg)

	appReq := httptest.NewRequest(http.MethodGet, "/app", nil)
	appRec := httptest.NewRecorder()
	router.ServeHTTP(appRec, appReq)
	if appRec.Code != http.StatusOK {
		t.Fatalf("app status = %d, want %d; body=%s", appRec.Code, http.StatusOK, appRec.Body.String())
	}
	if got := appRec.Body.String(); got == "" {
		t.Fatal("expected non-empty app page body")
	}
}

func TestRouter_AppPageIncludesImageWorkspace(t *testing.T) {
	cfg := config.Config{
		ListenAddr:           ":0",
		BaseURL:              "http://plugin.test",
		LaunchSharedSecret:   "secret",
		MainSiteOrigin:       "http://main.test",
		SessionTTL:           time.Hour,
		HistoryEnabled:       true,
		PluginKey:            "gen",
		DevLoginEnabled:      true,
		ImageProviderBaseURL: "http://provider.test",
	}
	router := NewRouter(cfg)

	appReq := httptest.NewRequest(http.MethodGet, "/app", nil)
	appRec := httptest.NewRecorder()
	router.ServeHTTP(appRec, appReq)
	if appRec.Code != http.StatusOK {
		t.Fatalf("app status = %d, want %d; body=%s", appRec.Code, http.StatusOK, appRec.Body.String())
	}

	body := appRec.Body.String()
	for _, needle := range []string{
		`data-testid="image-workspace"`,
		`data-testid="image-history"`,
		`data-testid="image-creation-grid"`,
		`data-testid="image-composer"`,
		`data-testid="reference-file-input"`,
		`/api/creations`,
		`/api/generate`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("app page missing %q", needle)
		}
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
		ListenAddr:           ":0",
		BaseURL:              "http://plugin.test",
		LaunchSharedSecret:   "secret",
		MainSiteOrigin:       "http://main.test",
		SessionTTL:           time.Hour,
		HistoryEnabled:       true,
		PluginKey:            "gen",
		DevLoginEnabled:      true,
		ImageProviderBaseURL: upstream.URL,
	}
	router := NewRouter(cfg)

	adminCookie := devLoginCookie(t, router, "/dev/login?user_id=99&role=admin&email=admin%40example.com&username=admin&path=/app")
	userCookie := devLoginCookie(t, router, "/dev/login?user_id=7&role=user&email=user%40example.com&username=user&path=/app")
	otherCookie := devLoginCookie(t, router, "/dev/login?user_id=8&role=user&email=other%40example.com&username=other&path=/app")

	for _, tc := range []struct {
		cookie      *http.Cookie
		prompt      string
		providerKey string
	}{
		{cookie: userCookie, prompt: "user image", providerKey: "user-a-key"},
		{cookie: otherCookie, prompt: "other image", providerKey: "user-b-key"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/generate", bytes.NewBufferString(`{"prompt":"`+tc.prompt+`","provider_api_key":"`+tc.providerKey+`","model":"gpt-image-1"}`))
		req.AddCookie(tc.cookie)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("generate status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
	}

	userCreationsReq := httptest.NewRequest(http.MethodGet, "/api/creations", nil)
	userCreationsReq.AddCookie(userCookie)
	userCreationsRec := httptest.NewRecorder()
	router.ServeHTTP(userCreationsRec, userCreationsReq)
	if userCreationsRec.Code != http.StatusOK {
		t.Fatalf("user creations status = %d, want %d; body=%s", userCreationsRec.Code, http.StatusOK, userCreationsRec.Body.String())
	}
	var userPayload struct {
		Items []model.CreationRecord `json:"items"`
	}
	if err := json.NewDecoder(userCreationsRec.Body).Decode(&userPayload); err != nil {
		t.Fatal(err)
	}
	if len(userPayload.Items) != 1 {
		t.Fatalf("user creation count = %d, want 1", len(userPayload.Items))
	}
	if userPayload.Items[0].UserID != 7 {
		t.Fatalf("user creation user_id = %d, want 7", userPayload.Items[0].UserID)
	}

	adminCreationsReq := httptest.NewRequest(http.MethodGet, "/api/creations", nil)
	adminCreationsReq.AddCookie(adminCookie)
	adminCreationsRec := httptest.NewRecorder()
	router.ServeHTTP(adminCreationsRec, adminCreationsReq)
	if adminCreationsRec.Code != http.StatusOK {
		t.Fatalf("admin creations status = %d, want %d; body=%s", adminCreationsRec.Code, http.StatusOK, adminCreationsRec.Body.String())
	}
	var adminPayload struct {
		Items []model.CreationRecord `json:"items"`
	}
	if err := json.NewDecoder(adminCreationsRec.Body).Decode(&adminPayload); err != nil {
		t.Fatal(err)
	}
	if len(adminPayload.Items) != 2 {
		t.Fatalf("admin creation count = %d, want 2", len(adminPayload.Items))
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
