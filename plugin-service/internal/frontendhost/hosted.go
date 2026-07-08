package frontendhost

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const pluginAuthBridgeScript = `<script>
    (function () {
      function getPluginAuthToken() {
        try {
          var params = new URLSearchParams(window.location.search);
          var urlToken = params.get("token") || params.get("session");
          if (urlToken) {
            return urlToken;
          }
        } catch (error) {}

        try {
          return window.localStorage.getItem("auth_token") || "";
        } catch (error) {
          return "";
        }
      }

      function shouldAttachPluginAuth(url) {
        return /\/plugins\/[^/?#]+\/api(?:\/|$)/.test(url);
      }

      function mergeHeaders(baseHeaders, extraHeaders) {
        var headers = new Headers(baseHeaders || {});
        if (extraHeaders) {
          new Headers(extraHeaders).forEach(function (value, key) {
            headers.set(key, value);
          });
        }
        return headers;
      }

      var originalFetch = window.fetch.bind(window);
      window.fetch = function (input, init) {
        var requestUrl = typeof input === "string" ? input : (input && input.url) || "";
        if (!shouldAttachPluginAuth(requestUrl)) {
          return originalFetch(input, init);
        }

        var token = getPluginAuthToken();
        if (!token) {
          return originalFetch(input, init);
        }

        if (input instanceof Request) {
          var requestHeaders = mergeHeaders(input.headers, init && init.headers);
          if (!requestHeaders.has("Authorization")) {
            requestHeaders.set("Authorization", "Bearer " + token);
          }
          return originalFetch(new Request(input, { headers: requestHeaders }), init);
        }

        var nextInit = init ? Object.assign({}, init) : {};
        var headers = mergeHeaders(null, nextInit.headers);
        if (!headers.has("Authorization")) {
          headers.set("Authorization", "Bearer " + token);
        }
        nextInit.headers = headers;
        return originalFetch(input, nextInit);
      };
    })();
  </script>`

type HostedPluginOptions struct {
	PluginKey   string
	WebRoot     string
	PatchAppJS  func(string) string
	HTMLHeadTag string
}

func RegisterHostedPlugin(mux *http.ServeMux, opts HostedPluginOptions) {
	pluginKey := strings.TrimSpace(opts.PluginKey)
	if mux == nil || pluginKey == "" {
		return
	}

	webRoot := strings.TrimSpace(opts.WebRoot)
	if webRoot == "" {
		return
	}

	assetRoot := filepath.Join(webRoot, "assets")
	indexPath := indexPath(webRoot)
	pagePath := "/plugins/" + pluginKey
	assetPrefix := pagePath + "/assets/"

	mux.HandleFunc("GET "+pagePath, func(w http.ResponseWriter, r *http.Request) {
		disableFrontendCache(w)
		body, err := os.ReadFile(indexPath)
		if err != nil {
			http.Error(w, "plugin frontend not found", http.StatusNotFound)
			return
		}

		html := injectPluginAuthBridge(string(body), opts.HTMLHeadTag)
		html = strings.ReplaceAll(html, assetPrefix+"app.js", assetPrefix+"app.js?v="+assetVersion(assetRoot, "app.js"))
		html = strings.ReplaceAll(html, assetPrefix+"app.css", assetPrefix+"app.css?v="+assetVersion(assetRoot, "app.css"))

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})

	assets := http.StripPrefix(assetPrefix, http.FileServer(http.Dir(assetRoot)))
	mux.Handle("GET "+assetPrefix, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		disableFrontendCache(w)
		if strings.HasSuffix(r.URL.Path, "/app.js") && opts.PatchAppJS != nil {
			servePatchedAppJS(w, filepath.Join(assetRoot, "app.js"), opts.PatchAppJS)
			return
		}
		assets.ServeHTTP(w, r)
	}))
}

func injectPluginAuthBridge(html string, headTag string) string {
	tag := strings.TrimSpace(headTag)
	if tag == "" {
		tag = `<script type="module" crossorigin src="`
	}
	index := strings.Index(html, tag)
	if index < 0 {
		return html
	}
	return html[:index] + pluginAuthBridgeScript + "\n  " + html[index:]
}

func servePatchedAppJS(w http.ResponseWriter, assetPath string, patch func(string) string) {
	body, err := os.ReadFile(assetPath)
	if err != nil {
		http.Error(w, "plugin asset not found", http.StatusNotFound)
		return
	}

	js := string(body)
	if patch != nil {
		js = patch(js)
	}

	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(js))
}

func disableFrontendCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func assetVersion(assetRoot string, name string) string {
	info, err := os.Stat(filepath.Join(assetRoot, name))
	if err != nil {
		return "0"
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

func indexPath(webRoot string) string {
	for _, name := range []string{
		"index.html",
		"plugin-image-generation.html",
		filepath.Join("plugin-image-generation", "index.html"),
	} {
		candidate := filepath.Join(webRoot, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return filepath.Join(webRoot, "index.html")
}
