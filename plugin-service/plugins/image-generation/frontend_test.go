package imagegeneration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		`data-testid="history-delete-button"`,
		`setAttribute("data-testid", "history-delete-confirm-overlay")`,
		`modal-overlay image-delete-confirm-overlay`,
		`openDeleteConfirmDialog`,
		`deleteRemoteHistoryItems(targetIds)`,
		`DELETE`,
		`deletedLocalHistoryKeys`,
		`deleteLocalHistoryItem`,
		`history-remote-`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("frontend html missing auth bridge marker %q", needle)
		}
	}

	if count := strings.Count(body, `function deleteLoadedRemoteHistory(ids)`); count != 1 {
		t.Fatalf("frontend html contains %d deleteLoadedRemoteHistory implementations, want 1", count)
	}
	for _, obsolete := range []string{`window.confirm(`, `window.location.reload()`} {
		if strings.Contains(body, obsolete) {
			t.Fatalf("frontend html still contains obsolete remote history deletion code %q", obsolete)
		}
	}
}

func TestFrontendContainsResponsiveAppleButtonStyles(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("frontend status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	for _, needle := range []string{
		`--image-button-primary: #0d9488`,
		`--image-button-secondary-text: #0f172a`,
		`#app button:not([data-testid="new-image-session"])`,
		`:focus-visible`,
		`transform: scale(0.97)`,
		`min-height: 44px`,
		`backdrop-filter: blur(14px) saturate(160%)`,
		`@media (prefers-reduced-motion: reduce)`,
		`@media (max-width: 767px) and (prefers-reduced-motion: reduce)`,
	} {
		if !strings.Contains(rec.Body.String(), needle) {
			t.Fatalf("frontend html missing responsive button style %q", needle)
		}
	}

	if strings.Contains(rec.Body.String(), "\n    #app [data-testid=\"new-image-session\"],") {
		t.Fatal("new session button still receives custom Apple surface styles")
	}
}

func TestFrontendServesBundledImageGenerationHistoryRecordFixes(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation/assets/app.js", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("frontend asset status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	assertBundledHistoryBehavior(t, rec.Body.String())
}

func TestBundledFrontendAssetContainsHistoryRecordFixes(t *testing.T) {
	bodyBytes, err := os.ReadFile(filepath.Join(webRoot(), "assets", "app.js"))
	if err != nil {
		t.Fatal(err)
	}

	assertBundledHistoryBehavior(t, string(bodyBytes))
}

func assertBundledHistoryBehavior(t *testing.T, body string) {
	t.Helper()

	for _, needle := range []string{
		`Nt(T.value.filter(D=>D.id!=="conversation-live"||D.messages.length>0),v=>`,
		`conversation_id:((m=A.value)==null?void 0:m.conversationId)||I`,
		`const w=String($.conversation_id||((m=$.request)==null?void 0:m.conversation_id)||$.id)`,
		`Array.from(f.reduce((D,$)=>`,
		`map(([D,$])=>{const m=$.slice().reverse(),d=m[0],w=$[0];return{id:` + "`history-remote-${$.map(C=>C.id).join(\",\")}`" + `,conversationId:D,title:d.prompt||t("imageGeneration.historyTitle"),preview:ot(w.result)||w.prompt||"",lastUsedAt:ce(w.updated_at),messages:m.flatMap(tt),referenceImages:[]}})`,
		"function M(){const f=`conversation-live-${Date.now()}`,p=new Date().toLocaleString();T.value.unshift({id:f,title:t(\"imageGeneration.conversationFallbackTitle\"),preview:\"\",lastUsedAt:p,messages:[],referenceImages:[]}),L.value=f,y.value=\"\"}",
		`W(I,w=>({...w,title:w.messages.length===0?p.slice(0,24):w.title,preview:t("imageGeneration.generationWaiting"),lastUsedAt:N,messages:[...w.messages,v,$]})),y.value="",g.value=!0;try{`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("frontend asset missing bundled fix %q", needle)
		}
	}

	if strings.Contains(body, `id:"conversation-remote"`) {
		t.Fatal("frontend asset still aggregates all remote history into a single conversation")
	}

	if strings.Contains(body, `f.slice().reverse().flatMap(tt)`) {
		t.Fatal("frontend asset still flattens all remote history into one conversation")
	}
}
