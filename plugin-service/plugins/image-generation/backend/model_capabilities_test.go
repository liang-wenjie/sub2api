package backend

import (
	"errors"
	"testing"
)

func TestImageModelCapability(t *testing.T) {
	tests := []struct {
		name           string
		model          string
		wantReferences int
		wantOutputs    int
	}{
		{name: "gpt image 2", model: "gpt-image-2", wantReferences: 16, wantOutputs: 10},
		{name: "gpt image 1", model: "gpt-image-1", wantReferences: 16, wantOutputs: 10},
		{name: "gemini flash image", model: "gemini-2.5-flash-image", wantReferences: 10, wantOutputs: 4},
		{name: "unknown model", model: "custom-image-model", wantReferences: 1, wantOutputs: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capability := imageModelCapability(tt.model)
			if capability.MaxReferenceImages != tt.wantReferences || capability.MaxOutputImages != tt.wantOutputs {
				t.Fatalf("capability = %#v, want references=%d outputs=%d", capability, tt.wantReferences, tt.wantOutputs)
			}
		})
	}
}

func TestValidateOutputCount(t *testing.T) {
	if got, err := validateOutputCount("gpt-image-1", 0); err != nil || got != 1 {
		t.Fatalf("default output count = %d, err = %v", got, err)
	}
	if _, err := validateOutputCount("gemini-2.5-flash-image", 5); !errors.Is(err, ErrInvalidOutputCount) {
		t.Fatalf("error = %v, want ErrInvalidOutputCount", err)
	}
}

func TestValidateReferenceImageCountRejectsOverLimit(t *testing.T) {
	err := validateReferenceImageCount("custom-image-model", make([]ReferenceImage, 2))
	if !errors.Is(err, ErrTooManyReferenceImages) {
		t.Fatalf("error = %v, want ErrTooManyReferenceImages", err)
	}
}
