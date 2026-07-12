package backend

import (
	"errors"
	"fmt"
)

var ErrTooManyReferenceImages = errors.New("too many reference images")

type ImageModelCapability struct {
	MaxReferenceImages int `json:"max_reference_images"`
}

var imageModelCapabilities = map[string]ImageModelCapability{
	"gpt-image-2":            {MaxReferenceImages: 16},
	"gpt-image-1":            {MaxReferenceImages: 16},
	"gemini-2.5-flash-image": {MaxReferenceImages: 10},
}

func imageModelCapability(modelName string) ImageModelCapability {
	if capability, ok := imageModelCapabilities[modelName]; ok {
		return capability
	}
	return ImageModelCapability{MaxReferenceImages: 1}
}

func validateReferenceImageCount(modelName string, references []ReferenceImage) error {
	limit := imageModelCapability(modelName).MaxReferenceImages
	if len(references) <= limit {
		return nil
	}
	return fmt.Errorf("%w: model %s supports at most %d reference images", ErrTooManyReferenceImages, modelName, limit)
}
