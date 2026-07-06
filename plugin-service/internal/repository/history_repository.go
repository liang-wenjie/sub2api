package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

var ErrNotFound = errors.New("record not found")

type HistoryRepository struct {
	mu      sync.RWMutex
	records map[string]model.HistoryRecord
	now     func() time.Time
}

func NewHistoryRepository() *HistoryRepository {
	return &HistoryRepository{
		records: make(map[string]model.HistoryRecord),
		now:     time.Now,
	}
}

func (r *HistoryRepository) Create(_ context.Context, principal model.CurrentPrincipal, req model.GenerateRequest) (*model.HistoryRecord, error) {
	now := r.now().UTC()
	record := model.HistoryRecord{
		ID:        randomID(),
		UserID:    principal.UserID,
		UserEmail: principal.Email,
		PluginKey: principal.Plugin,
		Prompt:    req.Prompt,
		Status:    model.HistoryStatusPending,
		Request:   req.Inputs,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if record.Request == nil {
		record.Request = map[string]any{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[record.ID] = record
	return &record, nil
}

func (r *HistoryRepository) Update(_ context.Context, record *model.HistoryRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.records[record.ID]; !ok {
		return ErrNotFound
	}
	record.UpdatedAt = r.now().UTC()
	r.records[record.ID] = *record
	return nil
}

func (r *HistoryRepository) Get(_ context.Context, id string) (*model.HistoryRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, ok := r.records[id]
	if !ok {
		return nil, false
	}
	return &record, true
}

func (r *HistoryRepository) ListAll(_ context.Context, query model.HistoryQuery) []model.HistoryRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	records := make([]model.HistoryRecord, 0, len(r.records))
	for _, record := range r.records {
		records = append(records, record)
	}
	return paginate(sortRecords(records), query)
}

func (r *HistoryRepository) ListByUser(_ context.Context, userID int64, query model.HistoryQuery) []model.HistoryRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	records := make([]model.HistoryRecord, 0)
	for _, record := range r.records {
		if record.UserID == userID {
			records = append(records, record)
		}
	}
	return paginate(sortRecords(records), query)
}

func sortRecords(records []model.HistoryRecord) []model.HistoryRecord {
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records
}

func paginate(records []model.HistoryRecord, query model.HistoryQuery) []model.HistoryRecord {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 || query.PageSize > 100 {
		query.PageSize = 20
	}
	start := (query.Page - 1) * query.PageSize
	if start >= len(records) {
		return []model.HistoryRecord{}
	}
	end := start + query.PageSize
	if end > len(records) {
		end = len(records)
	}
	return records[start:end]
}

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf)
}
