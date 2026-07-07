package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestResolveRequestBaseURLPrefersSourceHostQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "http://plugin-service/launch?src_host=https%3A%2F%2Fapp.example.com", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8091")

	got := ResolveRequestBaseURL(req)
	if got != "https://app.example.com" {
		t.Fatalf("ResolveRequestBaseURL() = %q, want %q", got, "https://app.example.com")
	}
}

func TestResolveRequestBaseURLFallsBackToReferer(t *testing.T) {
	req := httptest.NewRequest("GET", "http://plugin-service/launch", nil)
	req.Header.Set("Referer", "https://main.example.com/custom/image?id=1")

	got := ResolveRequestBaseURL(req)
	if got != "https://main.example.com" {
		t.Fatalf("ResolveRequestBaseURL() = %q, want %q", got, "https://main.example.com")
	}
}

func TestResolveRequestBaseURLFallsBackToForwardedHost(t *testing.T) {
	req := httptest.NewRequest("GET", "http://plugin-service/launch", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "edge.example.com")

	got := ResolveRequestBaseURL(req)
	if got != "https://edge.example.com" {
		t.Fatalf("ResolveRequestBaseURL() = %q, want %q", got, "https://edge.example.com")
	}
}
