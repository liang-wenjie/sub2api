package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
)

func TestHistoryService_ListForUser_OnlyOwnRecords(t *testing.T) {
	ctx := context.Background()
	svc := NewHistoryService(repository.NewHistoryRepository())

	userA := model.CurrentPrincipal{UserID: 1, Role: model.RoleUser, Email: "a@example.com", Plugin: "gen"}
	userB := model.CurrentPrincipal{UserID: 2, Role: model.RoleUser, Email: "b@example.com", Plugin: "gen"}
	if _, err := svc.Create(ctx, userA, "a", map[string]any{"prompt": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, userB, "b", map[string]any{"prompt": "b"}); err != nil {
		t.Fatal(err)
	}

	records, err := svc.List(ctx, userA, model.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	if records[0].UserID != userA.UserID {
		t.Fatalf("record user_id = %d, want %d", records[0].UserID, userA.UserID)
	}
}

func TestHistoryService_ListForAdmin_AllRecords(t *testing.T) {
	ctx := context.Background()
	svc := NewHistoryService(repository.NewHistoryRepository())

	user := model.CurrentPrincipal{UserID: 1, Role: model.RoleUser, Email: "u@example.com", Plugin: "gen"}
	admin := model.CurrentPrincipal{UserID: 99, Role: model.RoleAdmin, Email: "admin@example.com", Plugin: "gen"}
	if _, err := svc.Create(ctx, user, "u", map[string]any{"prompt": "u"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, admin, "admin", map[string]any{"prompt": "admin"}); err != nil {
		t.Fatal(err)
	}

	records, err := svc.List(ctx, admin, model.HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records len = %d, want 2", len(records))
	}
}

func TestHistoryService_GetForUser_BlocksOtherUsersRecord(t *testing.T) {
	ctx := context.Background()
	svc := NewHistoryService(repository.NewHistoryRepository())

	owner := model.CurrentPrincipal{UserID: 1, Role: model.RoleUser, Email: "owner@example.com", Plugin: "gen"}
	other := model.CurrentPrincipal{UserID: 2, Role: model.RoleUser, Email: "other@example.com", Plugin: "gen"}
	record, err := svc.Create(ctx, owner, "private", map[string]any{"prompt": "private"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Get(ctx, other, record.ID); err == nil {
		t.Fatal("expected forbidden error")
	}
}
