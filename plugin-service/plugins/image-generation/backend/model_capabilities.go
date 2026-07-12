package backend

import (
	"errors"
	"fmt"
)

var ErrTooManyReferenceImages = errors.New("too many reference images")
var ErrInvalidOutputCount = errors.New("invalid output image count")

type ImageModelCapability struct {
	MaxReferenceImages int `json:"max_reference_images"`
	MaxOutputImages    int `json:"max_output_images"`
}

var imageModelCapabilities = map[string]ImageModelCapability{
	"gpt-image-2":            {MaxReferenceImages: 16, MaxOutputImages: 10},
	"gpt-image-1":            {MaxReferenceImages: 16, MaxOutputImages: 10},
	"gemini-2.5-flash-image": {MaxReferenceImages: 10, MaxOutputImages: 4},
}

func imageModelCapability(modelName string) ImageModelCapability {
	if capability, ok := imageModelCapabilities[modelName]; ok {
		return capability
	}
	return ImageModelCapability{MaxReferenceImages: 1, MaxOutputImages: 1}
}

func validateOutputCount(modelName string, count int) (int, error) {
	if count == 0 {
		count = 1
	}
	limit := imageModelCapability(modelName).MaxOutputImages
	if count < 1 || count > limit {
		return 0, fmt.Errorf("%w: model %s supports between 1 and %d output images", ErrInvalidOutputCount, modelName, limit)
	}
	return count, nil
}

func validateReferenceImageCount(modelName string, references []ReferenceImage) error {
	limit := imageModelCapability(modelName).MaxReferenceImages
	if len(references) <= limit {
		return nil
	}
	return fmt.Errorf("%w: model %s supports at most %d reference images", ErrTooManyReferenceImages, modelName, limit)
}
