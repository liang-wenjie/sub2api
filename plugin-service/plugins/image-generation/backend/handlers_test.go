package backend

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

func TestHandlerConfigIncludesImageModelCapabilities(t *testing.T) {
	handler := NewHandler(HandlerDeps{})
	recorder := httptest.NewRecorder()
	handler.Config(recorder, httptest.NewRequest("GET", "/config", nil), model.CurrentPrincipal{UserID: 7, Role: model.RoleUser})

	var response struct {
		ImageModelCapabilities map[string]ImageModelCapability `json:"image_model_capabilities"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if got := response.ImageModelCapabilities["gpt-image-2"].MaxReferenceImages; got != 16 {
		t.Fatalf("gpt-image-2 max_reference_images = %d, want 16", got)
	}
}

func TestCompactJobResponseOmitsPendingRequestAndResult(t *testing.T) {
	record := &model.HistoryRecord{
		ID:     "job-1",
		Status: model.HistoryStatusPending,
		Request: map[string]any{
			"prompt":     "cat",
			"api_key_id": int64(42),
		},
		Result: map[string]any{"batch_status": "queued"},
	}

	encoded, err := json.Marshal(compactJobResponse(record))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); got != `{"job_id":"job-1","status":"pending"}` {
		t.Fatalf("response = %s", got)
	}
}

func TestCompactJobResponseIncludesOnlyTerminalPayload(t *testing.T) {
	succeeded := compactJobResponse(&model.HistoryRecord{ID: "job-2", Status: model.HistoryStatusSucceeded, Result: map[string]any{"images": []any{"image"}}})
	if succeeded.Result == nil || succeeded.ErrorMessage != "" {
		t.Fatalf("succeeded response = %#v", succeeded)
	}

	failed := compactJobResponse(&model.HistoryRecord{ID: "job-3", Status: model.HistoryStatusFailed, Result: map[string]any{"batch_status": "failed"}, ErrorMessage: "provider failed"})
	if failed.Result != nil || failed.ErrorMessage != "provider failed" {
		t.Fatalf("failed response = %#v", failed)
	}
}
