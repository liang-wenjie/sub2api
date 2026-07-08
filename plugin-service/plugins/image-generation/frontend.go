package imagegeneration

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/frontendhost"
)

const imageHistoryListNeedle = `Nt(T.value,v=>(re(),ae("button",{key:v.id,type:"button","data-testid":"history-item"`
const imageHistoryListReplacement = `Nt(T.value.filter(D=>D.id!=="conversation-live"||D.messages.length>0),v=>(re(),ae("button",{key:v.id,type:"button","data-testid":"history-item"`
const imageHistoryTimestampNeedle = `V("div",{class:mt(["mt-1 truncate text-xs",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.preview||Le(t)("imageGeneration.justNow")),3)`
const imageHistoryTimestampReplacement = `V("div",{class:"mt-1 flex items-center justify-between gap-3 text-xs"},[V("div",{class:mt(["truncate",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.preview||"（无内容）"),3),V("div",{class:mt(["shrink-0 tabular-nums",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.lastUsedAt||Le(t)("imageGeneration.justNow")),3)])`
const imageRemoteConversationNeedle = `h=f.length===0?[]:[{id:"conversation-remote",title:((I=f[0])==null?void 0:I.prompt)||t("imageGeneration.historyTitle"),preview:((N=f[0])==null?void 0:N.prompt)||"",messages:f.slice().reverse().flatMap(tt),referenceImages:[]}];`
const imageRemoteConversationReplacement = `h=f.length===0?[]:[{id:"conversation-remote",title:((I=f[0])==null?void 0:I.prompt)||t("imageGeneration.historyTitle"),preview:((N=f[0])==null?void 0:N.prompt)||"",lastUsedAt:ce(f.updated_at),messages:f.slice().reverse().flatMap(tt),referenceImages:[]}];`
const imageLocalSendNeedle = `W(I,w=>({...w,title:w.messages.length===0?p.slice(0,24):w.title,preview:t("imageGeneration.generationWaiting"),messages:[...w.messages,v,$]})),y.value="",g.value=!0;try{`
const imageLocalSendReplacement = `W(I,w=>({...w,title:w.messages.length===0?p.slice(0,24):w.title,preview:t("imageGeneration.generationWaiting"),lastUsedAt:N,messages:[...w.messages,v,$]})),y.value="",g.value=!0;try{`
const imageNewConversationNeedle = "function M(){const f=`conversation-live-${Date.now()}`;T.value.unshift({id:f,title:t(\"imageGeneration.conversationFallbackTitle\"),preview:\"\",messages:[],referenceImages:[]}),L.value=f,y.value=\"\"}"
const imageNewConversationReplacement = "function M(){const f=`conversation-live-${Date.now()}`,p=new Date().toLocaleString();T.value.unshift({id:f,title:t(\"imageGeneration.conversationFallbackTitle\"),preview:\"\",lastUsedAt:p,messages:[],referenceImages:[]}),L.value=f,y.value=\"\"}"

func RegisterFrontend(mux *http.ServeMux) {
	frontendhost.RegisterHostedPlugin(mux, frontendhost.HostedPluginOptions{
		PluginKey: "image-generation",
		WebRoot:   webRoot(),
		PatchAppJS: func(input string) string {
			js := strings.Replace(input, imageHistoryListNeedle, imageHistoryListReplacement, 1)
			js = strings.Replace(js, imageHistoryTimestampNeedle, imageHistoryTimestampReplacement, 1)
			js = strings.Replace(js, imageRemoteConversationNeedle, imageRemoteConversationReplacement, 1)
			js = strings.Replace(js, imageLocalSendNeedle, imageLocalSendReplacement, 1)
			js = strings.Replace(js, imageNewConversationNeedle, imageNewConversationReplacement, 1)
			return js
		},
	})
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
