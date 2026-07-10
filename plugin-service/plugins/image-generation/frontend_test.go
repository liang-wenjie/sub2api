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
		`确认删除当前历史记录吗？`,
		`DELETE`,
		`deletedLocalHistoryKeys`,
		`deleteLocalHistoryItem`,
		`history-remote-`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("frontend html missing auth bridge marker %q", needle)
		}
	}
}

func TestFrontendContainsCompactMobileHistoryDrawerWidth(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)

	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("frontend status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `@media (max-width: 767px)`) {
		t.Fatal("frontend html missing phone-width media query")
	}
	if !strings.Contains(body, `width: min(76vw, 280px);`) {
		t.Fatal("frontend html missing compact phone history drawer width")
	}
}

func TestFrontendContainsDirectionalHistoryControls(t *testing.T) {
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
		`topbar.appendChild(inlineButton);`,
		`topbar.appendChild(title);`,
		`border-radius: 0.5rem;`,
		`#app [data-testid="history-inline-collapse"]:hover`,
		`#app [data-testid="history-inline-collapse"]:focus-visible`,
		`html.dark #app [data-testid="history-inline-collapse"]`,
		`#app [data-testid="history-drawer-toggle"]:hover`,
		`#app [data-testid="history-drawer-toggle"]:focus-visible`,
		`html.dark #app [data-testid="history-drawer-toggle"]`,
		`<span class="image-history-drawer-handle-text">侧边栏</span>`,
		`transform: rotate(-45deg);`,
		`transform: rotate(45deg);`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("frontend html missing history control marker %q", needle)
		}
	}

	if strings.Index(body, `topbar.appendChild(inlineButton);`) > strings.Index(body, `topbar.appendChild(title);`) {
		t.Fatal("history collapse button is not inserted before the title")
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
