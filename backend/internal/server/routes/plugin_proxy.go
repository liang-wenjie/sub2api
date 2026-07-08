package routes

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

const defaultPluginServiceURL = "http://plugin-service:8091"

// RegisterPluginProxyRoutes mounts the plugin-service reverse proxy on the main site.
func RegisterPluginProxyRoutes(r *gin.Engine, upstreamBaseURL string) {
	if r == nil {
		return
	}

	proxy, err := newPluginReverseProxy(resolvePluginServiceBaseURL(upstreamBaseURL))
	if err != nil {
		return
	}

	handler := gin.WrapH(proxy)
	r.Any("/plugins/*path", handler)
}

func resolvePluginServiceBaseURL(explicit string) string {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(os.Getenv("PLUGIN_SERVICE_BASE_URL")); trimmed != "" {
		return trimmed
	}
	if port := strings.TrimSpace(os.Getenv("PLUGIN_SERVER_PORT")); port != "" {
		return "http://plugin-service:" + port
	}
	return defaultPluginServiceURL
}

func newPluginReverseProxy(baseURL string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalHost := forwardedHost(req)
		originalProto := forwardedProto(req)
		clientIP := realIP(req.RemoteAddr)
		originalDirector(req)
		req.Host = target.Host
		req.Header.Set("X-Forwarded-Host", originalHost)
		req.Header.Set("X-Forwarded-Proto", originalProto)
		req.Header.Set("X-Real-IP", clientIP)
		appendForwardedFor(req, clientIP)
	}
	return proxy, nil
}

func forwardedHost(req *http.Request) string {
	if req == nil {
		return ""
	}
	if host := strings.TrimSpace(req.Header.Get("X-Forwarded-Host")); host != "" {
		return host
	}
	return req.Host
}

func forwardedProto(req *http.Request) string {
	if req == nil {
		return "http"
	}
	if proto := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")); proto != "" {
		return proto
	}
	if req.TLS != nil {
		return "https"
	}
	return "http"
}

func realIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return host
}

func appendForwardedFor(req *http.Request, clientIP string) {
	if req == nil {
		return
	}
	if clientIP == "" {
		return
	}
	if prior := strings.TrimSpace(req.Header.Get("X-Forwarded-For")); prior != "" {
		req.Header.Set("X-Forwarded-For", prior+", "+clientIP)
		return
	}
	req.Header.Set("X-Forwarded-For", clientIP)
}
