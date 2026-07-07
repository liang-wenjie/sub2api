package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
)

func TestGenerationService_GenerateRecordsRealImageResult(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Fatalf("path = %s, want /v1/images/generations", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer provider-secret" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer provider-secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/generated.png","revised_prompt":"bright poster"}]}`))
	}))
	defer upstream.Close()

	historyRepo := repository.NewHistoryRepository()
	history := NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})

	principal := model.CurrentPrincipal{
		UserID:   7,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "gen",
	}
	resp, err := svc.Generate(ctx, principal, upstream.URL, model.GenerateRequest{
		Prompt:         "make a poster",
		ProviderAPIKey: "provider-secret",
		Model:          "gpt-image-1",
		Size:           "1024x1024",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != model.HistoryStatusSucceeded {
		t.Fatalf("status = %q, want %q", resp.Status, model.HistoryStatusSucceeded)
	}
	if resp.Result["type"] != "image_generation" {
		t.Fatalf("result.type = %#v, want %q", resp.Result["type"], "image_generation")
	}
	images, ok := resp.Result["images"].([]map[string]any)
	if !ok {
		t.Fatalf("result.images type = %T, want []map[string]any", resp.Result["images"])
	}
	if len(images) != 1 {
		t.Fatalf("image count = %d, want 1", len(images))
	}
	if images[0]["url"] != "https://cdn.example.com/generated.png" {
		t.Fatalf("image url = %#v, want %q", images[0]["url"], "https://cdn.example.com/generated.png")
	}
}

func TestGenerationService_GenerateUploadsReferenceImageAsMultipartFile(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/edits" {
			t.Fatalf("path = %s, want /v1/images/edits", r.URL.Path)
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse content type: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Fatalf("content type = %q, want multipart/form-data", mediaType)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		var foundImage bool
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("read multipart part: %v", err)
			}
			if part.FormName() != "image" {
				continue
			}
			foundImage = true
			if part.FileName() != "reference.png" {
				t.Fatalf("filename = %q, want reference.png", part.FileName())
			}
			if got := part.Header.Get("Content-Type"); got != "image/png" {
				t.Fatalf("image content type = %q, want image/png", got)
			}
			data, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("read image part: %v", err)
			}
			if string(data) != "png-bytes" {
				t.Fatalf("image bytes = %q, want png-bytes", string(data))
			}
		}
		if !foundImage {
			t.Fatal("multipart image part was not uploaded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"b64_json":"abc123"}]}`))
	}))
	defer upstream.Close()

	historyRepo := repository.NewHistoryRepository()
	history := NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})
	principal := model.CurrentPrincipal{UserID: 7, Role: model.RoleUser, Email: "user@example.com", Username: "user", Plugin: "gen"}

	_, err := svc.Generate(ctx, principal, upstream.URL, model.GenerateRequest{
		Prompt:         "use this reference",
		ProviderAPIKey: "provider-secret",
		Model:          "gpt-image-1",
		Size:           "1024x1024",
		ReferenceImages: []model.ReferenceImage{{
			Name:     "reference.png",
			MimeType: "image/png",
			DataURL:  "data:image/png;base64,cG5nLWJ5dGVz",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenerationService_ListCreationsForAdminAndUser(t *testing.T) {
	ctx := context.Background()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer user-a-key":
			_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/a.png","revised_prompt":"image a"}]}`))
		case "Bearer user-b-key":
			_, _ = w.Write([]byte(`{"created":1783000001,"data":[{"url":"https://cdn.example.com/b.png","revised_prompt":"image b"}]}`))
		default:
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
	}))
	defer upstream.Close()

	historyRepo := repository.NewHistoryRepository()
	history := NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})

	userA := model.CurrentPrincipal{UserID: 1, Role: model.RoleUser, Email: "a@example.com", Username: "a", Plugin: "gen"}
	userB := model.CurrentPrincipal{UserID: 2, Role: model.RoleUser, Email: "b@example.com", Username: "b", Plugin: "gen"}
	admin := model.CurrentPrincipal{UserID: 99, Role: model.RoleAdmin, Email: "admin@example.com", Username: "admin", Plugin: "gen"}

	_, err := svc.Generate(ctx, userA, upstream.URL, model.GenerateRequest{
		Prompt:         "first image",
		ProviderAPIKey: "user-a-key",
		Model:          "gpt-image-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Generate(ctx, userB, upstream.URL, model.GenerateRequest{
		Prompt:         "second image",
		ProviderAPIKey: "user-b-key",
		Model:          "gpt-image-1",
	})
	if err != nil {
		t.Fatal(err)
	}

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

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		prompts = append(prompts, payload.Prompt)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"created":1783000000,"data":[{"url":"https://cdn.example.com/generated.png","revised_prompt":"refined"}]}`))
	}))
	defer upstream.Close()

	historyRepo := repository.NewHistoryRepository()
	history := NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})

	principal := model.CurrentPrincipal{
		UserID:   7,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "image-generation",
	}

	resp, err := svc.Generate(ctx, principal, upstream.URL, model.GenerateRequest{
		Prompt:         "Follow the user request.\nUser request: draw a camera",
		ProviderAPIKey: "provider-secret",
		Model:          "gpt-image-1",
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

	if _, err := svc.Retry(ctx, principal, upstream.URL, record.ID); err != nil {
		t.Fatal(err)
	}

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

func TestGenerationServiceGenerateRequiresProviderBaseURL(t *testing.T) {
	ctx := context.Background()
	historyRepo := repository.NewHistoryRepository()
	history := NewHistoryService(historyRepo)
	svc := NewGenerationService(history, GenerationServiceOptions{})
	principal := model.CurrentPrincipal{
		UserID:   7,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "image-generation",
	}

	_, err := svc.Generate(ctx, principal, "", model.GenerateRequest{
		Prompt:         "draw a city",
		ProviderAPIKey: "provider-key",
	})
	if !errors.Is(err, ErrProviderBaseURL) {
		t.Fatalf("Generate() err = %v, want ErrProviderBaseURL", err)
	}
}
