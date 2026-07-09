package service

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
)

var ErrHistoryForbidden = errors.New("history record is not accessible")

type HistoryService struct {
	repo *repository.HistoryRepository
}

func NewHistoryService(repo *repository.HistoryRepository) *HistoryService {
	return &HistoryService{repo: repo}
}

func (s *HistoryService) Create(ctx context.Context, principal model.CurrentPrincipal, prompt string, request map[string]any) (*model.HistoryRecord, error) {
	return s.repo.Create(ctx, principal, prompt, request)
}

func (s *HistoryService) Update(ctx context.Context, record *model.HistoryRecord) error {
	return s.repo.Update(ctx, record)
}

func (s *HistoryService) List(ctx context.Context, principal model.CurrentPrincipal, query model.HistoryQuery) ([]model.HistoryRecord, error) {
	if principal.IsAdmin() {
		return s.repo.ListAll(ctx, query), nil
	}
	return s.repo.ListByUser(ctx, principal.UserID, query), nil
}

func (s *HistoryService) Get(ctx context.Context, principal model.CurrentPrincipal, id string) (*model.HistoryRecord, error) {
	record, ok := s.repo.Get(ctx, id)
	if !ok {
		return nil, repository.ErrNotFound
	}
	if !principal.IsAdmin() && record.UserID != principal.UserID {
		return nil, ErrHistoryForbidden
	}
	return record, nil
}
