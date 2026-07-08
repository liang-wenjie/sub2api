package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	ch := make(chan bool, 1)
	return ch
}

func TestPluginProxyRoutesForwardRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type upstreamResult struct {
		method         string
		path           string
		query          string
		forwardedHost  string
		forwardedProto string
		realIP         string
		auth           string
		cookie         string
		body           string
	}

	results := make(chan upstreamResult, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		results <- upstreamResult{
			method:         r.Method,
			path:           r.URL.Path,
			query:          r.URL.RawQuery,
			forwardedHost:  r.Header.Get("X-Forwarded-Host"),
			forwardedProto: r.Header.Get("X-Forwarded-Proto"),
			realIP:         r.Header.Get("X-Real-IP"),
			auth:           r.Header.Get("Authorization"),
			cookie:         r.Header.Get("Cookie"),
			body:           string(payload),
		}
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self' https://main.example.com")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("proxied"))
	}))
	defer upstream.Close()

	router := gin.New()
	RegisterPluginProxyRoutes(router, upstream.URL)

	tests := []struct {
		name      string
		method    string
		target    string
		body      string
		wantPath  string
		wantQuery string
	}{
		{
			name:      "plugin page route",
			method:    http.MethodGet,
			target:    "/plugins/image-generation",
			wantPath:  "/plugins/image-generation",
			wantQuery: "",
		},
		{
			name:      "plugin scoped api route",
			method:    http.MethodPost,
			target:    "/plugins/image-generation/api/generate?mode=fast",
			body:      `{"prompt":"hello"}`,
			wantPath:  "/plugins/image-generation/api/generate",
			wantQuery: "mode=fast",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.Host = "main.example.com"
			req.RemoteAddr = "203.0.113.10:4567"
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Cookie", "session=abc")
			req.Header.Set("X-Forwarded-Proto", "https")
			w := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder()}

			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusCreated, w.Code)
			require.Equal(t, "proxied", w.Body.String())
			require.Equal(t, "frame-ancestors 'self' https://main.example.com", w.Header().Get("Content-Security-Policy"))

			got := <-results
			require.Equal(t, tc.method, got.method)
			require.Equal(t, tc.wantPath, got.path)
			require.Equal(t, tc.wantQuery, got.query)
			require.Equal(t, "main.example.com", got.forwardedHost)
			require.Equal(t, "https", got.forwardedProto)
			require.Equal(t, "203.0.113.10", got.realIP)
			require.Equal(t, "Bearer test-token", got.auth)
			require.Equal(t, "session=abc", got.cookie)
			require.Equal(t, tc.body, got.body)
		})
	}
}

func TestResolvePluginServiceBaseURL(t *testing.T) {
	t.Setenv("PLUGIN_SERVICE_BASE_URL", "")
	t.Setenv("PLUGIN_SERVER_PORT", "")
	require.Equal(t, "http://plugin-service:8091", resolvePluginServiceBaseURL(""))

	t.Setenv("PLUGIN_SERVER_PORT", "18091")
	require.Equal(t, "http://plugin-service:18091", resolvePluginServiceBaseURL(""))

	t.Setenv("PLUGIN_SERVICE_BASE_URL", "http://custom-plugin:19091")
	require.Equal(t, "http://custom-plugin:19091", resolvePluginServiceBaseURL(""))

	require.Equal(t, "http://explicit-plugin:28091", resolvePluginServiceBaseURL("http://explicit-plugin:28091"))
}
