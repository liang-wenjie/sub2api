package repository

import (
	"context"
	"sync"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

type SessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]model.Session
}

func NewSessionRepository() *SessionRepository {
	return &SessionRepository{sessions: make(map[string]model.Session)}
}

func (r *SessionRepository) Save(_ context.Context, session *model.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = *session
	return nil
}

func (r *SessionRepository) Get(_ context.Context, id string) (*model.Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.sessions[id]
	if !ok {
		return nil, false
	}
	return &session, true
}

func (r *SessionRepository) Delete(_ context.Context, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}
