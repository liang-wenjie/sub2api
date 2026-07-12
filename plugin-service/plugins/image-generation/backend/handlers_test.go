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
