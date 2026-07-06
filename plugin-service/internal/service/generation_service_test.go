package service

import (
	"context"
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
	svc := NewGenerationService(history, GenerationServiceOptions{
		ProviderBaseURL: upstream.URL,
	})

	principal := model.CurrentPrincipal{
		UserID:   7,
		Role:     model.RoleUser,
		Email:    "user@example.com",
		Username: "user",
		Plugin:   "gen",
	}
	resp, err := svc.Generate(ctx, principal, model.GenerateRequest{
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
	svc := NewGenerationService(history, GenerationServiceOptions{
		ProviderBaseURL: upstream.URL,
	})

	userA := model.CurrentPrincipal{UserID: 1, Role: model.RoleUser, Email: "a@example.com", Username: "a", Plugin: "gen"}
	userB := model.CurrentPrincipal{UserID: 2, Role: model.RoleUser, Email: "b@example.com", Username: "b", Plugin: "gen"}
	admin := model.CurrentPrincipal{UserID: 99, Role: model.RoleAdmin, Email: "admin@example.com", Username: "admin", Plugin: "gen"}

	_, err := svc.Generate(ctx, userA, model.GenerateRequest{
		Prompt:         "first image",
		ProviderAPIKey: "user-a-key",
		Model:          "gpt-image-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Generate(ctx, userB, model.GenerateRequest{
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
