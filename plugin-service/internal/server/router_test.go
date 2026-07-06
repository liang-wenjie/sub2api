package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

func TestRouter_LaunchGenerateAndListHistory(t *testing.T) {
	cfg := config.Config{
		ListenAddr:         ":0",
		BaseURL:            "http://plugin.test",
		LaunchSharedSecret: "secret",
		MainSiteOrigin:     "http://main.test",
		SessionTTL:         time.Hour,
		HistoryEnabled:     true,
		PluginKey:          "gen",
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

	body := bytes.NewBufferString(`{"prompt":"make a poster","inputs":{"size":"1024x1024"}}`)
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
