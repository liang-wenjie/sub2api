package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
)

type SessionService struct {
	repo *repository.SessionRepository
	ttl  time.Duration
	now  func() time.Time
}

func randomHex(byteLen int) string {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buf)
}

func NewSessionService(repo *repository.SessionRepository, ttl time.Duration) *SessionService {
	return &SessionService{repo: repo, ttl: ttl, now: time.Now}
}

func (s *SessionService) CreateFromLaunchClaims(ctx context.Context, claims model.LaunchClaims) (*model.Session, error) {
	principal := model.CurrentPrincipal{
		UserID:   claims.UserID,
		Role:     claims.Role,
		Email:    claims.Email,
		Username: claims.Username,
		Plugin:   claims.Plugin,
	}
	session := &model.Session{
		ID:        randomHex(24),
		Principal: principal,
		ExpiresAt: s.now().UTC().Add(s.ttl),
		CreatedAt: s.now().UTC(),
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *SessionService) Get(ctx context.Context, id string) (*model.Session, bool) {
	session, ok := s.repo.Get(ctx, id)
	if !ok {
		return nil, false
	}
	if !session.ExpiresAt.After(s.now().UTC()) {
		s.repo.Delete(ctx, id)
		return nil, false
	}
	return session, true
}
