package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

var ErrPromptRequired = errors.New("prompt is required")

type GenerationService struct {
	history *HistoryService
}

func NewGenerationService(history *HistoryService) *GenerationService {
	return &GenerationService{history: history}
}

func (s *GenerationService) Generate(ctx context.Context, principal model.CurrentPrincipal, req model.GenerateRequest) (*model.GenerateResponse, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		return nil, ErrPromptRequired
	}
	record, err := s.history.Create(ctx, principal, req)
	if err != nil {
		return nil, err
	}
	record.Status = model.HistoryStatusSucceeded
	record.Result = map[string]any{
		"type":   "placeholder",
		"title":  "Generated draft",
		"prompt": req.Prompt,
	}
	if err := s.history.Update(ctx, record); err != nil {
		return nil, err
	}
	return &model.GenerateResponse{
		JobID:  record.ID,
		Status: record.Status,
		Result: record.Result,
	}, nil
}

func (s *GenerationService) Retry(ctx context.Context, principal model.CurrentPrincipal, id string) (*model.GenerateResponse, error) {
	record, err := s.history.Get(ctx, principal, id)
	if err != nil {
		return nil, err
	}
	return s.Generate(ctx, principal, model.GenerateRequest{
		Prompt: record.Prompt,
		Inputs: record.Request,
	})
}

func (s *GenerationService) Cancel(ctx context.Context, principal model.CurrentPrincipal, id string) (*model.HistoryRecord, error) {
	record, err := s.history.Get(ctx, principal, id)
	if err != nil {
		return nil, err
	}
	if record.Status != model.HistoryStatusPending {
		return nil, errors.New("only pending jobs can be canceled")
	}
	record.Status = model.HistoryStatusCanceled
	if err := s.history.Update(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}
