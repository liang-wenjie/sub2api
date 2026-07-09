package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

const historyRecordColumns = `
		id, user_id, user_email, plugin_key, prompt, status,
		request_payload, result_payload, error_message, created_at, updated_at
`

type SQLHistoryRepository struct {
	db    *sql.DB
	now   func() time.Time
	newID func() string
}

func NewSQLHistoryRepository(db *sql.DB) *SQLHistoryRepository {
	return &SQLHistoryRepository{
		db:    db,
		now:   time.Now,
		newID: randomID,
	}
}

func EnsureHistorySchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("nil sql db")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS plugin_generation_history (
			id TEXT PRIMARY KEY,
			user_id BIGINT NOT NULL,
			user_email TEXT NOT NULL,
			plugin_key TEXT NOT NULL,
			prompt TEXT NOT NULL,
			status TEXT NOT NULL,
			request_payload TEXT NOT NULL,
			result_payload TEXT,
			error_message TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_generation_history_user_created
			ON plugin_generation_history(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_plugin_generation_history_created
			ON plugin_generation_history(created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLHistoryRepository) Create(ctx context.Context, principal model.CurrentPrincipal, prompt string, request map[string]any) (*model.HistoryRecord, error) {
	now := r.now().UTC()
	record := model.HistoryRecord{
		ID:        r.newID(),
		UserID:    principal.UserID,
		UserEmail: principal.Email,
		PluginKey: principal.Plugin,
		Prompt:    strings.TrimSpace(prompt),
		Status:    model.HistoryStatusPending,
		Request:   copyMap(request),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if record.Request == nil {
		record.Request = map[string]any{}
	}

	requestPayload, err := marshalPayload(record.Request)
	if err != nil {
		return nil, err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO plugin_generation_history (
			id, user_id, user_email, plugin_key, prompt, status,
			request_payload, result_payload, error_message, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULL, '', $8, $8)
	`,
		record.ID,
		record.UserID,
		record.UserEmail,
		record.PluginKey,
		record.Prompt,
		record.Status,
		requestPayload,
		now,
	)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (r *SQLHistoryRepository) Update(ctx context.Context, record *model.HistoryRecord) error {
	if record == nil {
		return errors.New("nil history record")
	}
	requestPayload, err := marshalPayload(record.Request)
	if err != nil {
		return err
	}
	resultPayload, err := marshalNullablePayload(record.Result)
	if err != nil {
		return err
	}
	updatedAt := r.now().UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE plugin_generation_history
		SET status = $2,
			request_payload = $3,
			result_payload = $4,
			error_message = $5,
			updated_at = $6
		WHERE id = $1
	`,
		record.ID,
		record.Status,
		requestPayload,
		resultPayload,
		record.ErrorMessage,
		updatedAt,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return ErrNotFound
	}
	record.UpdatedAt = updatedAt
	return nil
}

func (r *SQLHistoryRepository) Get(ctx context.Context, id string) (*model.HistoryRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, user_email, plugin_key, prompt, status,
			request_payload, result_payload, error_message, created_at, updated_at
		FROM plugin_generation_history
		WHERE id = $1
	`, id)
	record, err := scanHistoryRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

func (r *SQLHistoryRepository) ListAll(ctx context.Context, query model.HistoryQuery) ([]model.HistoryRecord, error) {
	limit, offset := normalizePagination(query)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, user_email, plugin_key, prompt, status,
			request_payload, result_payload, error_message, created_at, updated_at
		FROM plugin_generation_history
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHistoryRows(rows)
}

func (r *SQLHistoryRepository) ListByUser(ctx context.Context, userID int64, query model.HistoryQuery) ([]model.HistoryRecord, error) {
	limit, offset := normalizePagination(query)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, user_email, plugin_key, prompt, status,
			request_payload, result_payload, error_message, created_at, updated_at
		FROM plugin_generation_history
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHistoryRows(rows)
}

type historyScanner interface {
	Scan(dest ...any) error
}

func scanHistoryRecord(scanner historyScanner) (*model.HistoryRecord, error) {
	var record model.HistoryRecord
	var requestPayload string
	var resultPayload sql.NullString
	if err := scanner.Scan(
		&record.ID,
		&record.UserID,
		&record.UserEmail,
		&record.PluginKey,
		&record.Prompt,
		&record.Status,
		&requestPayload,
		&resultPayload,
		&record.ErrorMessage,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	request, err := unmarshalPayload(requestPayload)
	if err != nil {
		return nil, err
	}
	record.Request = request
	if resultPayload.Valid && strings.TrimSpace(resultPayload.String) != "" {
		result, err := unmarshalPayload(resultPayload.String)
		if err != nil {
			return nil, err
		}
		record.Result = result
	}
	return &record, nil
}

func scanHistoryRows(rows *sql.Rows) ([]model.HistoryRecord, error) {
	records := make([]model.HistoryRecord, 0)
	for rows.Next() {
		record, err := scanHistoryRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func marshalPayload(payload map[string]any) (string, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func marshalNullablePayload(payload map[string]any) (sql.NullString, error) {
	if payload == nil {
		return sql.NullString{}, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}

func unmarshalPayload(payload string) (map[string]any, error) {
	output := map[string]any{}
	if strings.TrimSpace(payload) == "" {
		return output, nil
	}
	if err := json.Unmarshal([]byte(payload), &output); err != nil {
		return nil, fmt.Errorf("decode history payload: %w", err)
	}
	return output, nil
}

func normalizePagination(query model.HistoryQuery) (int, int) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.PageSize <= 0 || query.PageSize > 100 {
		query.PageSize = 20
	}
	return query.PageSize, (query.Page - 1) * query.PageSize
}
