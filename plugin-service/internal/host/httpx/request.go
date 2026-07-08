package httpx

import (
	"net/http"
	"strings"
)

func ResolveRequestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}

	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if proto == "" {
		proto = "http"
	}
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return strings.TrimRight(proto+"://"+host, "/")
}
