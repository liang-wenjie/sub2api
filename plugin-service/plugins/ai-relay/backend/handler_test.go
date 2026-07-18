package backend

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
)

func TestRelayForwardsAccountBearerKeyAndReturnsOpenAIImages(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer agnes-account-key" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := readRequestBody(t, r); got != `{"model":"gpt-image-1","prompt":"poster","size":"1K","ratio":"1:1","extra_body":{"response_format":"url"}}` {
			t.Fatalf("body = %s", got)
		}
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://cdn.example/image.png"}]}`))
	}))
	defer upstream.Close()

	repository := NewMemoryRouteRepository()
	repository.routes["agnes:team-a"] = RouteConfig{
		Platform: "agnes", Slug: "team-a", BaseURL: upstream.URL + "/v1",
	}
	handler := NewRelayHandler(repository, NewDefaultAdapterRegistry(), upstream.Client())
	req := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"model":"gpt-image-1","prompt":"poster","size":"1024x1024","response_format":"url"}`))
	req.Header.Set("Authorization", "Bearer agnes-account-key")
	rec := httptest.NewRecorder()

	handler.Relay(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestRelayConvertsOpenAIImageEditMultipartRequestForAgnes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := readRequestBody(t, r); got != `{"model":"agnes-image-2.1-flash","prompt":"make it blue","size":"1K","ratio":"1:1","extra_body":{"response_format":"url","image":["data:image/png;base64,cG5nLWJ5dGVz"]}}` {
			t.Fatalf("body = %s", got)
		}
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://cdn.example/edited.png"}]}`))
	}))
	defer upstream.Close()

	repository := NewMemoryRouteRepository()
	repository.routes["agnes:team-a"] = RouteConfig{Platform: "agnes", Slug: "team-a", BaseURL: upstream.URL + "/v1"}
	mux := http.NewServeMux()
	RegisterRoutes(mux, nil, NewRelayHandler(repository, NewDefaultAdapterRegistry(), upstream.Client()))

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("model", "agnes-image-2.1-flash")
	_ = writer.WriteField("prompt", "make it blue")
	_ = writer.WriteField("size", "1024x1024")
	_ = writer.WriteField("response_format", "url")
	file, err := writer.CreateFormFile("image", "source.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("png-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a/v1/images/edits", body)
	req.Header.Set("Authorization", "Bearer account-key")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestV1ModelsAndChatProxyAccountBearerKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer account-key" {
			t.Fatalf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"agnes-image-2.1-flash","object":"model"}]}`))
		case "/v1/chat/completions":
			if got := readRequestBody(t, r); got != `{"model":"agnes-chat","messages":[{"role":"user","content":"hello"}]}` {
				t.Fatalf("chat body = %s", got)
			}
			_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[]}`))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	defer upstream.Close()

	repository := NewMemoryRouteRepository()
	repository.routes["agnes:team-a"] = RouteConfig{Platform: "agnes", Slug: "team-a", BaseURL: upstream.URL + "/v1"}
	mux := http.NewServeMux()
	RegisterRoutes(mux, nil, NewRelayHandler(repository, NewDefaultAdapterRegistry(), upstream.Client()))

	for _, test := range []struct{ method, path, body string }{
		{http.MethodGet, "/plugins/ai-relay/agnes/team-a/v1/models", ""},
		{http.MethodPost, "/plugins/ai-relay/agnes/team-a/v1/chat/completions", `{"model":"agnes-chat","messages":[{"role":"user","content":"hello"}]}`},
	} {
		req := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
		req.Header.Set("Authorization", "Bearer account-key")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d; body=%s", test.method, test.path, rec.Code, rec.Body.String())
		}
	}
}

func TestRelayRejectsMissingBearerAndUnknownSlug(t *testing.T) {
	handler := NewRelayHandler(NewMemoryRouteRepository(), NewDefaultAdapterRegistry(), http.DefaultClient)

	missingKey := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"prompt":"poster"}`))
	missingKeyRec := httptest.NewRecorder()
	handler.Relay(missingKeyRec, missingKey)
	if missingKeyRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d", missingKeyRec.Code)
	}

	unknownSlug := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"prompt":"poster"}`))
	unknownSlug.Header.Set("Authorization", "Bearer account-key")
	unknownSlugRec := httptest.NewRecorder()
	handler.Relay(unknownSlugRec, unknownSlug)
	if unknownSlugRec.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d", unknownSlugRec.Code)
	}
}

func TestRegisterRoutesMountsRelayEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, nil, NewRelayHandler(NewMemoryRouteRepository(), NewDefaultAdapterRegistry(), http.DefaultClient))

	req := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/agnes/team-a", bytes.NewBufferString(`{"prompt":"poster"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminCanManageAgnesRoutes(t *testing.T) {
	repository := NewMemoryRouteRepository()
	mux := http.NewServeMux()
	RegisterRoutes(mux, principal.NewMiddleware(), NewRelayHandler(repository, NewDefaultAdapterRegistry(), http.DefaultClient))

	request := httptest.NewRequest(http.MethodPut, "/plugins/ai-relay/api/routes/agnes/team-a", bytes.NewBufferString(`{"base_url":"https://apihub.agnes-ai.com/v1"}`))
	request.Header.Set("X-Sub2api-User-Id", "7")
	request.Header.Set("X-Sub2api-User-Role", "admin")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admin upsert status = %d; body=%s", response.Code, response.Body.String())
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/plugins/ai-relay/api/routes", nil)
	listRequest.Header.Set("X-Sub2api-User-Id", "7")
	listRequest.Header.Set("X-Sub2api-User-Role", "admin")
	listResponse := httptest.NewRecorder()
	mux.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !bytes.Contains(listResponse.Body.Bytes(), []byte(`"team-a"`)) {
		t.Fatalf("admin list = %d; body=%s", listResponse.Code, listResponse.Body.String())
	}
}

func TestAdminListsRoutesWithPagination(t *testing.T) {
	repository := NewMemoryRouteRepository()
	for _, slug := range []string{"one", "two", "three"} {
		repository.routes["agnes:"+slug] = RouteConfig{Platform: "agnes", Slug: slug, Name: slug, BaseURL: "https://apihub.agnes-ai.com/v1"}
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, principal.NewMiddleware(), NewRelayHandler(repository, NewDefaultAdapterRegistry(), http.DefaultClient))

	req := httptest.NewRequest(http.MethodGet, "/plugins/ai-relay/api/routes?page=2&page_size=2", nil)
	req.Header.Set("X-Sub2api-User-Id", "7")
	req.Header.Set("X-Sub2api-User-Role", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Items      []RouteConfig `json:"items"`
		Pagination struct {
			Page       int `json:"page"`
			PageSize   int `json:"page_size"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].Slug != "two" {
		t.Fatalf("items = %#v", response.Items)
	}
	if response.Pagination.Page != 2 || response.Pagination.PageSize != 2 || response.Pagination.Total != 3 || response.Pagination.TotalPages != 2 {
		t.Fatalf("pagination = %#v", response.Pagination)
	}
}

func TestAdminBatchDeletesRoutesAtomically(t *testing.T) {
	repository := NewMemoryRouteRepository()
	for _, slug := range []string{"one", "two", "three"} {
		repository.routes["agnes:"+slug] = RouteConfig{Platform: "agnes", Slug: slug, Name: slug, BaseURL: "https://apihub.agnes-ai.com/v1"}
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, principal.NewMiddleware(), NewRelayHandler(repository, NewDefaultAdapterRegistry(), http.DefaultClient))

	req := httptest.NewRequest(http.MethodDelete, "/plugins/ai-relay/api/routes", bytes.NewBufferString(`{"items":[{"platform":"agnes","slug":"one"},{"platform":"agnes","slug":"two"}]}`))
	req.Header.Set("X-Sub2api-User-Id", "7")
	req.Header.Set("X-Sub2api-User-Role", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if _, found, _ := repository.Get(req.Context(), "agnes", "one"); found {
		t.Fatal("first selected route was not deleted")
	}
	if _, found, _ := repository.Get(req.Context(), "agnes", "two"); found {
		t.Fatal("second selected route was not deleted")
	}
	if _, found, _ := repository.Get(req.Context(), "agnes", "three"); !found {
		t.Fatal("unselected route must remain")
	}
}

func TestAdminListsRegisteredPlatforms(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, principal.NewMiddleware(), NewRelayHandler(NewMemoryRouteRepository(), NewDefaultAdapterRegistry(), http.DefaultClient))
	req := httptest.NewRequest(http.MethodGet, "/plugins/ai-relay/api/platforms", nil)
	req.Header.Set("X-Sub2api-User-Id", "7")
	req.Header.Set("X-Sub2api-User-Role", "admin")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"agnes"`)) {
		t.Fatalf("response = %d; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminCreatesOnlyRegisteredRelayPlatforms(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux, principal.NewMiddleware(), NewRelayHandler(NewMemoryRouteRepository(), NewDefaultAdapterRegistry(), http.DefaultClient))

	create := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/api/routes", bytes.NewBufferString(`{"platform":"agnes","slug":"primary","name":"Primary Agnes","base_url":"https://apihub.agnes-ai.com/v1"}`))
	create.Header.Set("X-Sub2api-User-Id", "7")
	create.Header.Set("X-Sub2api-User-Role", "admin")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusCreated || !bytes.Contains(createRec.Body.Bytes(), []byte(`"name":"Primary Agnes"`)) {
		t.Fatalf("create = %d; body=%s", createRec.Code, createRec.Body.String())
	}

	unsupported := httptest.NewRequest(http.MethodPost, "/plugins/ai-relay/api/routes", bytes.NewBufferString(`{"platform":"opencode","slug":"primary","name":"OpenCode","base_url":"https://example.test/v1"}`))
	unsupported.Header.Set("X-Sub2api-User-Id", "7")
	unsupported.Header.Set("X-Sub2api-User-Role", "admin")
	unsupportedRec := httptest.NewRecorder()
	mux.ServeHTTP(unsupportedRec, unsupported)
	if unsupportedRec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported status = %d; body=%s", unsupportedRec.Code, unsupportedRec.Body.String())
	}
}

func readRequestBody(t *testing.T, r *http.Request) string {
	t.Helper()
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(r.Body); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}
