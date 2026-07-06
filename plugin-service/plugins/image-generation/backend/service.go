package backend

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/httpx"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

func parseHistoryQuery(values url.Values) model.HistoryQuery {
	return model.HistoryQuery{
		Page:     parsePositiveInt(values.Get("page"), 1),
		PageSize: parsePositiveInt(values.Get("page_size"), 20),
	}
}

func parsePositiveInt(raw string, fallback int) int {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "history record not found")
	case errors.Is(err, service.ErrHistoryForbidden):
		httpx.WriteError(w, http.StatusForbidden, "history record is not accessible")
	default:
		httpx.WriteError(w, http.StatusConflict, err.Error())
	}
}

func sanitizeHistoryRecords(records []model.HistoryRecord) []model.HistoryRecord {
	sanitized := make([]model.HistoryRecord, 0, len(records))
	for _, record := range records {
		current := sanitizeHistoryRecord(&record)
		sanitized = append(sanitized, *current)
	}
	return sanitized
}

func sanitizeHistoryRecord(record *model.HistoryRecord) *model.HistoryRecord {
	if record == nil {
		return nil
	}

	safe := *record
	safe.Request = sanitizeRequestPayload(record.Request)
	return &safe
}

func sanitizeRequestPayload(request map[string]any) map[string]any {
	if request == nil {
		return nil
	}

	safe := make(map[string]any, len(request))
	for key, value := range request {
		if key == "provider_api_key" {
			continue
		}
		safe[key] = value
	}
	return safe
}
