package imagegeneration

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const imageHistoryListNeedle = `Nt(T.value,v=>(re(),ae("button",{key:v.id,type:"button","data-testid":"history-item"`
const imageHistoryListReplacement = `Nt(T.value.filter(D=>D.id!=="conversation-live"||D.messages.length>0),v=>(re(),ae("button",{key:v.id,type:"button","data-testid":"history-item"`
const imageHistoryTimestampNeedle = `V("div",{class:mt(["mt-1 truncate text-xs",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.preview||Le(t)("imageGeneration.justNow")),3)`
const imageHistoryTimestampReplacement = `V("div",{class:"mt-1 flex items-center justify-between gap-3 text-xs"},[V("div",{class:mt(["truncate",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.preview||Le(t)("imageGeneration.justNow")),3),V("div",{class:mt(["shrink-0 tabular-nums",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.lastUsedAt||Le(t)("imageGeneration.justNow")),3)])`
const imageRemoteConversationNeedle = `h=f.length===0?[]:[{id:"conversation-remote",title:((I=f[0])==null?void 0:I.prompt)||t("imageGeneration.historyTitle"),preview:((N=f[0])==null?void 0:N.prompt)||"",messages:f.slice().reverse().flatMap(tt),referenceImages:[]}];`
const imageRemoteConversationReplacement = `h=f.length===0?[]:[{id:"conversation-remote",title:((I=f[0])==null?void 0:I.prompt)||t("imageGeneration.historyTitle"),preview:((N=f[0])==null?void 0:N.prompt)||"",lastUsedAt:ce(f.updated_at),messages:f.slice().reverse().flatMap(tt),referenceImages:[]}];`
const imageLocalSendNeedle = `W(I,w=>({...w,title:w.messages.length===0?p.slice(0,24):w.title,preview:t("imageGeneration.generationWaiting"),messages:[...w.messages,v,$]})),y.value="",g.value=!0;try{`
const imageLocalSendReplacement = `W(I,w=>({...w,title:w.messages.length===0?p.slice(0,24):w.title,preview:t("imageGeneration.generationWaiting"),lastUsedAt:N,messages:[...w.messages,v,$]})),y.value="",g.value=!0;try{`

func RegisterFrontend(mux *http.ServeMux) {
	webRoot := webRoot()
	assetRoot := filepath.Join(webRoot, "assets")
	indexPath := indexPath(webRoot)

	mux.HandleFunc("GET /plugins/image-generation", func(w http.ResponseWriter, r *http.Request) {
		disableFrontendCache(w)
		body, err := os.ReadFile(indexPath)
		if err != nil {
			http.Error(w, "plugin frontend not found", http.StatusNotFound)
			return
		}
		html := string(body)
		html = strings.ReplaceAll(html, "/plugins/image-generation/assets/app.js", "/plugins/image-generation/assets/app.js?v="+assetVersion(assetRoot, "app.js"))
		html = strings.ReplaceAll(html, "/plugins/image-generation/assets/app.css", "/plugins/image-generation/assets/app.css?v="+assetVersion(assetRoot, "app.css"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})

	assets := http.StripPrefix("/plugins/image-generation/assets/", http.FileServer(http.Dir(assetRoot)))
	mux.Handle("GET /plugins/image-generation/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		disableFrontendCache(w)
		if strings.HasSuffix(r.URL.Path, "/app.js") {
			servePatchedAppJS(w, assetRoot)
			return
		}
		assets.ServeHTTP(w, r)
	}))
}

func servePatchedAppJS(w http.ResponseWriter, assetRoot string) {
	body, err := os.ReadFile(filepath.Join(assetRoot, "app.js"))
	if err != nil {
		http.Error(w, "plugin asset not found", http.StatusNotFound)
		return
	}
	js := strings.Replace(string(body), imageHistoryListNeedle, imageHistoryListReplacement, 1)
	js = strings.Replace(js, imageHistoryTimestampNeedle, imageHistoryTimestampReplacement, 1)
	js = strings.Replace(js, imageRemoteConversationNeedle, imageRemoteConversationReplacement, 1)
	js = strings.Replace(js, imageLocalSendNeedle, imageLocalSendReplacement, 1)
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

func webRoot() string {
	for _, candidate := range []string{
		filepath.Join("plugins", "image-generation", "web"),
		filepath.Join("plugin-service", "plugins", "image-generation", "web"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("plugins", "image-generation", "web")
	}

	pluginRoot := filepath.Clean(filepath.Dir(currentFile))
	return filepath.Join(pluginRoot, "web")
}
