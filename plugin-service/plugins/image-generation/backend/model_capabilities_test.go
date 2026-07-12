package backend

import (
	"errors"
	"testing"
)

func TestImageModelCapability(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  int
	}{
		{name: "gpt image 2", model: "gpt-image-2", want: 16},
		{name: "gpt image 1", model: "gpt-image-1", want: 16},
		{name: "gemini flash image", model: "gemini-2.5-flash-image", want: 10},
		{name: "unknown model", model: "custom-image-model", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageModelCapability(tt.model).MaxReferenceImages; got != tt.want {
				t.Fatalf("MaxReferenceImages = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestValidateReferenceImageCountRejectsOverLimit(t *testing.T) {
	err := validateReferenceImageCount("custom-image-model", make([]ReferenceImage, 2))
	if !errors.Is(err, ErrTooManyReferenceImages) {
		t.Fatalf("error = %v, want ErrTooManyReferenceImages", err)
	}
}
