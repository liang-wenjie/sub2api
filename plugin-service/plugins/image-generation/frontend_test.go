package imagegeneration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFrontendInjectsPluginAuthBridgeScript(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("frontend status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	for _, needle := range []string{
		`localStorage.getItem("auth_token")`,
		`window.location.search`,
		`Authorization`,
		`/plugins/image-generation/api`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("frontend html missing auth bridge marker %q", needle)
		}
	}
}

func TestFrontendPatchesImageGenerationHistoryRecords(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation/assets/app.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("frontend asset status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	for _, needle := range []string{
		`Nt(T.value.filter(D=>D.id!=="conversation-live"||D.messages.length>0),v=>`,
		`V("div",{class:"mt-1 flex items-center justify-between gap-3 text-xs"},[V("div",{class:mt(["truncate",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.preview||"（无内容）"),3),V("div",{class:mt(["shrink-0 tabular-nums",v.id===L.value?"text-slate-300":"text-slate-400"])},ie(v.lastUsedAt||Le(t)("imageGeneration.justNow")),3)])`,
		`lastUsedAt:ce(f.updated_at)`,
		"function M(){const f=`conversation-live-${Date.now()}`,p=new Date().toLocaleString();T.value.unshift({id:f,title:t(\"imageGeneration.conversationFallbackTitle\"),preview:\"\",lastUsedAt:p,messages:[],referenceImages:[]}),L.value=f,y.value=\"\"}",
		`W(I,w=>({...w,title:w.messages.length===0?p.slice(0,24):w.title,preview:t("imageGeneration.generationWaiting"),lastUsedAt:N,messages:[...w.messages,v,$]})),y.value="",g.value=!0;try{`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("patched frontend asset missing %q", needle)
		}
	}

	if strings.Contains(body, `Nt(T.value,v=>`) {
		t.Fatal("patched frontend asset still renders the default live conversation directly in history")
	}
}
