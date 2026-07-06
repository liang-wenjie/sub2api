package model

import "time"

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	HistoryStatusPending   = "pending"
	HistoryStatusSucceeded = "succeeded"
	HistoryStatusFailed    = "failed"
	HistoryStatusCanceled  = "canceled"
)

type LaunchClaims struct {
	UserID    int64  `json:"user_id"`
	Role      string `json:"role"`
	Email     string `json:"email"`
	Username  string `json:"username"`
	Plugin    string `json:"plugin"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
	Nonce     string `json:"nonce"`
}

type CurrentPrincipal struct {
	UserID   int64  `json:"user_id"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Plugin   string `json:"plugin"`
}

func (p CurrentPrincipal) IsAdmin() bool {
	return p.Role == RoleAdmin
}

type Session struct {
	ID        string
	Principal CurrentPrincipal
	ExpiresAt time.Time
	CreatedAt time.Time
}

type HistoryRecord struct {
	ID           string         `json:"id"`
	UserID       int64          `json:"user_id"`
	UserEmail    string         `json:"user_email"`
	PluginKey    string         `json:"plugin_key"`
	Prompt       string         `json:"prompt"`
	Status       string         `json:"status"`
	Request      map[string]any `json:"request"`
	Result       map[string]any `json:"result,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type GenerateRequest struct {
	Prompt string         `json:"prompt"`
	Inputs map[string]any `json:"inputs"`
}

type GenerateResponse struct {
	JobID  string         `json:"job_id"`
	Status string         `json:"status"`
	Result map[string]any `json:"result,omitempty"`
}

type HistoryQuery struct {
	Page     int
	PageSize int
}
