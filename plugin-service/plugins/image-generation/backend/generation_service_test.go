package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

func TestGenerationService_GPTCreatesLocalTaskAndReturnsResultFromStatus(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		once.Do(func() { close(started) })
		<-release
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/gpt.png"}]}`))
	}))
	defer upstream.Close()

	history := service.NewHistoryService(repository.NewHistoryRepository())
	svc := NewGenerationService(history, GenerationServiceOptions{HTTPClient: upstream.Client()})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Plugin: "image-generation"}

	response, err := svc.Generate(context.Background(), principal, upstream.URL, GenerateRequest{
		Prompt: "draw a cat", ProviderAPIKey: "provider-key", Model: "gpt-image-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != model.HistoryStatusPending {
		t.Fatalf("status = %q, want pending", response.Status)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("GPT task did not start")
	}
	close(release)

	var record *model.HistoryRecord
	deadline := time.Now().Add(2 * time.Second)
	for {
		record, err = svc.Status(context.Background(), principal, upstream.URL, response.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == model.HistoryStatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task status = %q", record.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	images := imageMapsValue(record.Result["images"])
	if len(images) != 1 || images[0]["url"] != "https://cdn.example.com/gpt.png" {
		t.Fatalf("images = %#v", images)
	}
}

func TestGenerationService_CancelGPTLocalTaskKeepsCanceledStatus(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		close(stopped)
		return nil, r.Context().Err()
	})}

	history := service.NewHistoryService(repository.NewHistoryRepository())
	svc := NewGenerationService(history, GenerationServiceOptions{HTTPClient: client})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Plugin: "image-generation"}
	created, err := svc.Generate(context.Background(), principal, "http://provider.example", GenerateRequest{
		Prompt: "cat", ProviderAPIKey: "provider-key", Model: "gpt-image-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	canceled, err := svc.Cancel(context.Background(), principal, "http://provider.example", created.JobID)
	if err != nil || canceled.Status != model.HistoryStatusCanceled {
		t.Fatalf("Cancel() = %#v, %v", canceled, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("GPT provider request was not interrupted")
	}
	record, err := svc.Status(context.Background(), principal, "http://provider.example", created.JobID)
	if err != nil || record.Status != model.HistoryStatusCanceled {
		t.Fatalf("Status() = %#v, %v", record, err)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestGenerationService_GenerateReferenceImageSubmitsBatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/batches" {
			t.Fatalf("request = %s %s, want POST /v1/images/batches", r.Method, r.URL.Path)
		}
		var payload batchSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Items) != 1 || len(payload.Items[0].ReferenceImages) != 1 {
			t.Fatalf("payload items = %#v", payload.Items)
		}
		ref := payload.Items[0].ReferenceImages[0]
		if ref.MimeType != "image/png" || string(ref.Data) != "png-bytes" {
			t.Fatalf("reference = %#v", ref)
		}
		_, _ = w.Write([]byte(`{"id":"imgbatch_reference","status":"queued","model":"gemini-2.5-flash-image"}`))
	}))
	defer upstream.Close()

	history := service.NewHistoryService(repository.NewHistoryRepository())
	svc := NewGenerationService(history, GenerationServiceOptions{HTTPClient: upstream.Client()})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Plugin: "image-generation"}

	resp, err := svc.Generate(context.Background(), principal, upstream.URL, GenerateRequest{
		Prompt: "use this reference", ProviderAPIKey: "provider-key", Model: "gemini-2.5-flash-image",
		ReferenceImages: []ReferenceImage{{
			Name: "reference.png", MimeType: "image/png", DataURL: "data:image/png;base64,cG5nLWJ5dGVz",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != model.HistoryStatusPending {
		t.Fatalf("status = %q, want pending", resp.Status)
	}
}

func TestGenerationService_NewEditRequestUsesJSONForRemoteReferenceImage(t *testing.T) {
	history := service.NewHistoryService(repository.NewHistoryRepository())
	svc := NewGenerationService(history, GenerationServiceOptions{})
	req, err := svc.newEditRequest(context.Background(), "https://provider.example", GenerateRequest{
		Prompt: "restyle this image", Model: "gpt-image-1", Size: "1024x1024", ResponseFormat: "b64_json",
		ReferenceImages: []ReferenceImage{{RemoteURL: "https://cdn.example.com/reference.png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/v1/images/edits" {
		t.Fatalf("path = %q", req.URL.Path)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q, want application/json", req.Header.Get("Content-Type"))
	}
	var payload struct {
		Images []struct {
			URL string `json:"image_url"`
		} `json:"images"`
	}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Images) != 1 || payload.Images[0].URL != "https://cdn.example.com/reference.png" {
		t.Fatalf("images = %#v", payload.Images)
	}
}

func TestGenerationService_GenerateSubmitsSingleBatch(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/batches" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var payload batchSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Items) != 1 || payload.Items[0].OutputCount != 1 {
			t.Fatalf("payload = %#v", payload)
		}
		_, _ = w.Write([]byte(`{"id":"imgbatch_async","status":"queued","model":"gemini-2.5-flash-image"}`))
	}))
	defer upstream.Close()

	history := service.NewHistoryService(repository.NewHistoryRepository())
	svc := NewGenerationService(history, GenerationServiceOptions{HTTPClient: upstream.Client()})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Email: "user@example.com", Plugin: "image-generation"}

	resp, err := svc.Generate(ctx, principal, upstream.URL, GenerateRequest{
		Prompt: "draw a cat", ProviderAPIKey: "api-key", Model: "gemini-2.5-flash-image",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != model.HistoryStatusPending || resp.Result["batch_status"] != "queued" {
		t.Fatalf("response = %#v", resp)
	}
	record, err := history.Get(ctx, principal, resp.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Request["batch_id"] != "imgbatch_async" {
		t.Fatalf("batch_id = %#v", record.Request["batch_id"])
	}
	if record.Request["batch_custom_id"] != "plugin-image-"+record.ID {
		t.Fatalf("batch_custom_id = %#v", record.Request["batch_custom_id"])
	}
}

func TestGenerationService_ReconcileCompletedBatch(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/batches":
			_, _ = w.Write([]byte(`{"id":"imgbatch_done","status":"queued","model":"gemini-2.5-flash-image"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/batches/imgbatch_done":
			_, _ = w.Write([]byte(`{"id":"imgbatch_done","status":"completed","model":"gemini-2.5-flash-image"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/images/batches/imgbatch_done/items":
			_, _ = w.Write([]byte(`{"data":[{"custom_id":"plugin-image-placeholder","status":"completed","image_count":1}]}`))
		default:
			if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content") {
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte("png"))
				return
			}
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	history := service.NewHistoryService(repository.NewHistoryRepository())
	svc := NewGenerationService(history, GenerationServiceOptions{HTTPClient: server.Client()})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Plugin: "image-generation"}
	created, err := svc.Generate(ctx, principal, server.URL, GenerateRequest{Prompt: "cat", ProviderAPIKey: "api-key", Model: "gemini-2.5-flash-image"})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := history.Get(ctx, principal, created.JobID)
	// Make the fake item match the generated stable custom ID without coupling the server to repository internals.
	record.Request["batch_custom_id"] = "plugin-image-placeholder"
	_ = history.Update(ctx, record)
	completed, err := svc.Status(ctx, principal, server.URL, created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.HistoryStatusSucceeded {
		t.Fatalf("status = %q", completed.Status)
	}
	images := imageMapsValue(completed.Result["images"])
	if len(images) != 1 || images[0]["url"] != "data:image/png;base64,cG5n" {
		t.Fatalf("images = %#v", images)
	}
}

func TestGenerationService_CancelBatch(t *testing.T) {
	ctx := context.Background()
	cancelCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/batches":
			_, _ = w.Write([]byte(`{"id":"imgbatch_cancel","status":"queued"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/images/batches/imgbatch_cancel/cancel":
			cancelCalls++
			_, _ = w.Write([]byte(`{"id":"imgbatch_cancel","status":"cancelled"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	history := service.NewHistoryService(repository.NewHistoryRepository())
	svc := NewGenerationService(history, GenerationServiceOptions{HTTPClient: server.Client()})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Plugin: "image-generation"}
	created, err := svc.Generate(ctx, principal, server.URL, GenerateRequest{Prompt: "cat", ProviderAPIKey: "api-key", Model: "gemini-2.5-flash-image"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.Cancel(ctx, principal, server.URL, created.JobID)
	if err != nil || cancelled.Status != model.HistoryStatusCanceled || cancelCalls != 1 {
		t.Fatalf("Cancel() = %#v, %v, calls=%d", cancelled, err, cancelCalls)
	}
}

func TestGenerationService_ListCreationsForAdminAndUser(t *testing.T) {
	ctx := context.Background()
	historyRepo := repository.NewHistoryRepository()
	history := service.NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})

	userA := model.CurrentPrincipal{UserID: 1, Role: model.RoleUser, Email: "a@example.com", Username: "a", Plugin: "gen"}
	userB := model.CurrentPrincipal{UserID: 2, Role: model.RoleUser, Email: "b@example.com", Username: "b", Plugin: "gen"}
	admin := model.CurrentPrincipal{UserID: 99, Role: model.RoleAdmin, Email: "admin@example.com", Username: "admin", Plugin: "gen"}

	createSucceededImageHistory(t, history, userA, "first image", "https://cdn.example.com/a.png")
	createSucceededImageHistory(t, history, userB, "second image", "https://cdn.example.com/b.png")

	userCreations, err := svc.ListCreations(ctx, userA, model.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(userCreations) != 1 {
		t.Fatalf("user creation count = %d, want 1", len(userCreations))
	}
	if userCreations[0].UserID != userA.UserID {
		t.Fatalf("user creation user_id = %d, want %d", userCreations[0].UserID, userA.UserID)
	}

	adminCreations, err := svc.ListCreations(ctx, admin, model.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(adminCreations) != 2 {
		t.Fatalf("admin creation count = %d, want 2", len(adminCreations))
	}
}

func TestGenerationService_RetryUsesStoredPromptWhileHistoryKeepsDisplayPrompt(t *testing.T) {
	ctx := context.Background()
	var prompts []string
	var promptsMu sync.Mutex

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload batchSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		if len(payload.Items) != 1 {
			t.Fatalf("batch items = %#v", payload.Items)
		}
		promptsMu.Lock()
		prompts = append(prompts, payload.Items[0].Prompt)
		batchNumber := len(prompts)
		promptsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"imgbatch_retry_` + strconv.Itoa(batchNumber) + `","status":"queued","model":"gemini-2.5-flash-image"}`))
	}))
	defer upstream.Close()

	historyRepo := repository.NewHistoryRepository()
	history := service.NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})

	principal := model.CurrentPrincipal{
		UserID:   7,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "image-generation",
	}

	resp, err := svc.Generate(ctx, principal, upstream.URL, GenerateRequest{
		Prompt:         "Follow the user request.\nUser request: draw a camera",
		ProviderAPIKey: "provider-secret",
		Model:          "gemini-2.5-flash-image",
		Inputs: map[string]any{
			"display_prompt": "draw a camera",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	record, err := history.Get(ctx, principal, resp.JobID)
	if err != nil {
		t.Fatal(err)
	}

	if record.Prompt != "draw a camera" {
		t.Fatalf("history prompt = %q, want %q", record.Prompt, "draw a camera")
	}
	if got := record.Request["prompt"]; got != "Follow the user request.\nUser request: draw a camera" {
		t.Fatalf("stored request prompt = %#v", got)
	}
	if record.ConversationID != "" {
		t.Fatalf("conversation id = %q, want empty", record.ConversationID)
	}
	retry, err := svc.Retry(ctx, principal, upstream.URL, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.JobID == record.ID {
		t.Fatal("retry reused the original history id")
	}

	promptsMu.Lock()
	defer promptsMu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("provider prompt count = %d, want 2", len(prompts))
	}
	if prompts[0] != "Follow the user request.\nUser request: draw a camera" {
		t.Fatalf("first provider prompt = %q", prompts[0])
	}
	if prompts[1] != "Follow the user request.\nUser request: draw a camera" {
		t.Fatalf("retry provider prompt = %q", prompts[1])
	}
}

func TestGenerationService_GenerateStoresConversationID(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"imgbatch_conversation","status":"queued","model":"gemini-2.5-flash-image"}`))
	}))
	defer upstream.Close()

	historyRepo := repository.NewHistoryRepository()
	history := service.NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Email: "user@example.com", Username: "user", Plugin: "image-generation"}

	resp, err := svc.Generate(ctx, principal, upstream.URL, GenerateRequest{
		Prompt:         "draw a camera",
		ProviderAPIKey: "provider-secret",
		Model:          "gemini-2.5-flash-image",
		Inputs: map[string]any{
			"conversation_id": "conversation-live-123",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	record, err := history.Get(ctx, principal, resp.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ConversationID != "conversation-live-123" {
		t.Fatalf("conversation id = %q, want %q", record.ConversationID, "conversation-live-123")
	}
}

func TestGenerationServiceGenerateRequiresProviderBaseURL(t *testing.T) {
	ctx := context.Background()
	historyRepo := repository.NewHistoryRepository()
	history := service.NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})
	principal := model.CurrentPrincipal{
		UserID:   7,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "image-generation",
	}

	_, err := svc.Generate(ctx, principal, "", GenerateRequest{
		Prompt:         "draw a city",
		ProviderAPIKey: "provider-key",
	})
	if !errors.Is(err, ErrProviderBaseURL) {
		t.Fatalf("Generate() err = %v, want ErrProviderBaseURL", err)
	}
}

func TestGenerationService_GenerateReturnsUpstreamStatusAndMessage(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API key","type":"invalid_request_error"}}`))
	}))
	defer upstream.Close()

	historyRepo := repository.NewHistoryRepository()
	history := service.NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})
	principal := model.CurrentPrincipal{
		UserID:   7,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "image-generation",
	}

	resp, err := svc.Generate(ctx, principal, upstream.URL, GenerateRequest{
		Prompt:         "draw a city",
		ProviderAPIKey: "provider-key",
		Model:          "gemini-2.5-flash-image",
	})
	if resp != nil {
		t.Fatalf("Generate() response = %#v, want nil", resp)
	}
	var upstreamErr *UpstreamHTTPError
	if !errors.As(err, &upstreamErr) || upstreamErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Generate() err = %v, want upstream 401", err)
	}

	records, listErr := history.List(ctx, principal, model.HistoryQuery{})
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(records) != 1 {
		t.Fatalf("history records = %d, want 1", len(records))
	}
	if records[0].Status != model.HistoryStatusFailed {
		t.Fatalf("history status = %q, want %q", records[0].Status, model.HistoryStatusFailed)
	}
	if records[0].ErrorMessage != "Invalid API key" {
		t.Fatalf("history error message = %q, want %q", records[0].ErrorMessage, "Invalid API key")
	}
}

func createSucceededImageHistory(t *testing.T, history *service.HistoryService, principal model.CurrentPrincipal, prompt, imageURL string) {
	t.Helper()
	record, err := history.Create(context.Background(), principal, prompt, map[string]any{
		"prompt": prompt,
		"model":  "gemini-2.5-flash-image",
		"size":   "1024x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	record.Status = model.HistoryStatusSucceeded
	record.Result = map[string]any{
		"type":  "image_generation",
		"model": "gemini-2.5-flash-image",
		"size":  "1024x1024",
		"images": []map[string]any{{
			"url": imageURL,
		}},
	}
	if err := history.Update(context.Background(), record); err != nil {
		t.Fatal(err)
	}
}

func waitForHistoryStatus(t *testing.T, history *service.HistoryService, principal model.CurrentPrincipal, id, expected string) *model.HistoryRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		record, err := history.Get(context.Background(), principal, id)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == expected {
			return record
		}
		if isTerminalHistoryStatus(record.Status) && record.Status != expected {
			t.Fatalf("history status = %q, want %q; error=%q", record.Status, expected, record.ErrorMessage)
		}
		if time.Now().After(deadline) {
			t.Fatalf("history status = %q, want %q", record.Status, expected)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
