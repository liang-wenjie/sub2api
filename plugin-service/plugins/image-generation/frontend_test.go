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

func TestFrontendContainsBatchTracking(t *testing.T) {
	mux := http.NewServeMux()
	RegisterFrontend(mux)
	req := httptest.NewRequest(http.MethodGet, "/plugins/image-generation", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("frontend status = %d; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, needle := range []string{
		`installBatchTrackingFetchBridge`,
		`batchActionUrl(tracker.jobId, "status")`,
		`batchActionUrl(tracker.jobId, "cancel")`,
		`addButton("image-generation-stop", "停止生成", ""`,
		`tracker.cancelButton.classList.add("image-generation-stop-button")`,
		`document.querySelector('[data-testid="image-send-button"]')`,
		`sendButton.classList.add("image-generation-send-button-replaced")`,
		`sendButton.parentNode.insertBefore(controls, sendButton)`,
		`tracker.sendButton.classList.remove("image-generation-send-button-replaced")`,
		`.image-generation-send-button-replaced {`,
		`display: none !important;`,
		`@media (prefers-reduced-motion: reduce)`,
		`window.clearTimeout(tracker.timer)`,
		`restoreBatchTrackers(payload.items)`,
		`record.request.batch_id`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("frontend html missing batch tracking marker %q", needle)
		}
	}
	for _, forbidden := range []string{
		`batchActionUrl(tracker.jobId, "pause")`,
		`batchActionUrl(tracker.jobId, "resume")`,
		`image-generation-resume`,
		`image-generation-cancel`,
		`record.status === "paused"`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("frontend html still contains removed pause/resume marker %q", forbidden)
		}
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

func TestBundledFrontendOffersTaskCapableModels(t *testing.T) {
	bodyBytes, err := os.ReadFile(filepath.Join(webRoot(), "assets", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)
	if !strings.Contains(body, `{value:"gpt-image-2",label:"GPT Image 2"}`) ||
		!strings.Contains(body, `{value:"gemini-2.5-flash-image",label:"Gemini 2.5 Flash Image"}`) {
		t.Fatal("frontend asset is missing a supported GPT or Gemini task model")
	}
	if !strings.Contains(body, `p.startsWith("gpt-image-")||p.startsWith("gemini-")&&p.includes("image")`) {
		t.Fatal("frontend asset does not recognize both GPT and Gemini task models")
	}
}

func assertBundledHistoryBehavior(t *testing.T, body string) {
	t.Helper()

	for _, needle := range []string{
		`Nt(T.value.filter(D=>D.id!=="conversation-live"||D.messages.length>0),v=>`,
		`f.status==="failed"`,
		`String(f.error_message||t("imageGeneration.generationFailed"))`,
		`status:"failed"`,
		`id:` + "`assistant-failed-${Date.now()}`" + `,role:"assistant"`,
		`messages:C.messages.map(H=>H.id===D?U:H)`,
		`generationFailed:"Image generation failed"`,
		`generationFailed:"\u56fe\u7247\u751f\u6210\u5931\u8d25"`,
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
