package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
)

func TestSessionService_CreateFromLaunchClaims(t *testing.T) {
	svc := NewSessionService(repository.NewSessionRepository(), time.Hour)
	session, err := svc.CreateFromLaunchClaims(context.Background(), model.LaunchClaims{
		UserID:   12,
		Role:     model.RoleAdmin,
		Email:    "admin@example.com",
		Username: "admin",
		Plugin:   "gen",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID == "" {
		t.Fatal("session id is empty")
	}
	if session.Principal.UserID != 12 {
		t.Fatalf("user_id = %d, want 12", session.Principal.UserID)
	}
}
