package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

func TestTicketService_CreateAndVerify(t *testing.T) {
	svc := NewTicketService("secret")
	ticket, err := svc.CreateTicket(model.LaunchClaims{
		UserID: 12,
		Role:   model.RoleAdmin,
		Email:  "admin@example.com",
		Plugin: "gen",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.VerifyTicket(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 12 {
		t.Fatalf("user_id = %d, want 12", claims.UserID)
	}
}

func TestTicketService_RejectsTamperedTicket(t *testing.T) {
	svc := NewTicketService("secret")
	ticket, err := svc.CreateTicket(model.LaunchClaims{
		UserID: 1,
		Role:   model.RoleUser,
		Email:  "u@example.com",
		Plugin: "gen",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VerifyTicket(ticket + "x"); err == nil {
		t.Fatal("expected invalid ticket")
	}
}
