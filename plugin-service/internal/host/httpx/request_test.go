package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestResolveRequestBaseURLFallsBackToForwardedHost(t *testing.T) {
	req := httptest.NewRequest("GET", "http://plugin-service/launch", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "edge.example.com")

	got := ResolveRequestBaseURL(req)
	if got != "https://edge.example.com" {
		t.Fatalf("ResolveRequestBaseURL() = %q, want %q", got, "https://edge.example.com")
	}
}

func TestResolveRequestBaseURLReturnsEmptyWithoutForwardedHost(t *testing.T) {
	req := httptest.NewRequest("GET", "http://plugin-service/launch", nil)
	if got := ResolveRequestBaseURL(req); got != "http://plugin-service" {
		t.Fatalf("ResolveRequestBaseURL() = %q, want %q", got, "http://plugin-service")
	}
}
