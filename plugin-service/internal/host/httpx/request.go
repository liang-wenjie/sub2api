package httpx

import (
	"net/http"
	"net/url"
	"strings"
)

func ResolveRequestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}

	if srcHost := normalizeAbsoluteBaseURL(r.URL.Query().Get("src_host")); srcHost != "" {
		return srcHost
	}

	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		if normalized := normalizeAbsoluteBaseURL(origin); normalized != "" {
			return normalized
		}
	}

	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		if normalized := normalizeAbsoluteBaseURL(referer); normalized != "" {
			return normalized
		}
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

func normalizeAbsoluteBaseURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/")
}
