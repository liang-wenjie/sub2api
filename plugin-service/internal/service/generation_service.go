package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

var (
	ErrPromptRequired      = errors.New("prompt is required")
	ErrProviderKeyRequired = errors.New("provider api key is required")
	ErrProviderBaseURL     = errors.New("provider base url is required")
)

const (
	defaultImageModel          = "gpt-image-1"
	defaultImageSize           = "1024x1024"
	defaultImageResponseFormat = "b64_json"
)

type GenerationServiceOptions struct {
	ProviderBaseURL string
	HTTPClient      *http.Client
}

type GenerationService struct {
	history         *HistoryService
	providerBaseURL string
	httpClient      *http.Client
}

type openAIImagesResponse struct {
	Created int64                     `json:"created"`
	Data    []openAIImageResponseItem `json:"data"`
}

type openAIImageResponseItem struct {
	B64JSON       string `json:"b64_json"`
	URL           string `json:"url"`
	RevisedPrompt string `json:"revised_prompt"`
}

func NewGenerationService(history *HistoryService, opts GenerationServiceOptions) *GenerationService {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &GenerationService{
		history:         history,
		providerBaseURL: strings.TrimRight(strings.TrimSpace(opts.ProviderBaseURL), "/"),
		httpClient:      client,
	}
}

func (s *GenerationService) Generate(ctx context.Context, principal model.CurrentPrincipal, req model.GenerateRequest) (*model.GenerateResponse, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		return nil, ErrPromptRequired
	}
	req.ProviderAPIKey = strings.TrimSpace(req.ProviderAPIKey)
	if req.ProviderAPIKey == "" {
		return nil, ErrProviderKeyRequired
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		req.Model = defaultImageModel
	}
	req.Size = strings.TrimSpace(req.Size)
	if req.Size == "" {
		req.Size = defaultImageSize
	}
	req.ResponseFormat = strings.TrimSpace(req.ResponseFormat)
	if req.ResponseFormat == "" {
		req.ResponseFormat = defaultImageResponseFormat
	}

	record, err := s.history.Create(ctx, principal, req)
	if err != nil {
		return nil, err
	}

	result, err := s.generateWithProvider(ctx, req)
	if err != nil {
		record.Status = model.HistoryStatusFailed
		record.ErrorMessage = err.Error()
		_ = s.history.Update(ctx, record)
		return nil, err
	}

	record.Status = model.HistoryStatusSucceeded
	record.Result = result
	record.ErrorMessage = ""
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
	retryReq := model.GenerateRequest{
		Prompt:          record.Prompt,
		ProviderAPIKey:  stringValue(record.Request["provider_api_key"]),
		Model:           stringValue(record.Request["model"]),
		Size:            stringValue(record.Request["size"]),
		ResponseFormat:  stringValue(record.Request["response_format"]),
		ReferenceImages: referenceImagesValue(record.Request["reference_images"]),
	}
	return s.Generate(ctx, principal, retryReq)
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

func (s *GenerationService) ListCreations(ctx context.Context, principal model.CurrentPrincipal, query model.HistoryQuery) ([]model.CreationRecord, error) {
	records, err := s.history.List(ctx, principal, query)
	if err != nil {
		return nil, err
	}
	creations := make([]model.CreationRecord, 0, len(records))
	for _, record := range records {
		if record.Status != model.HistoryStatusSucceeded {
			continue
		}
		if stringValue(record.Result["type"]) != "image_generation" {
			continue
		}
		images := imageMapsValue(record.Result["images"])
		if len(images) == 0 {
			continue
		}
		for index, image := range images {
			creationID := record.ID + "-" + strconv.Itoa(index)
			creations = append(creations, model.CreationRecord{
				ID:        creationID,
				HistoryID: record.ID,
				UserID:    record.UserID,
				UserEmail: record.UserEmail,
				PluginKey: record.PluginKey,
				Prompt:    record.Prompt,
				Model:     stringValue(record.Result["model"]),
				Size:      stringValue(record.Result["size"]),
				ImageURL:  stringValue(image["url"]),
				B64JSON:   stringValue(image["b64_json"]),
				Result:    copyMap(image),
				CreatedAt: record.CreatedAt,
				UpdatedAt: record.UpdatedAt,
			})
		}
	}
	return creations, nil
}

func (s *GenerationService) generateWithProvider(ctx context.Context, req model.GenerateRequest) (map[string]any, error) {
	baseURL := s.providerBaseURL
	if baseURL == "" {
		return nil, ErrProviderBaseURL
	}

	var (
		httpReq *http.Request
		err     error
	)
	if len(req.ReferenceImages) > 0 {
		httpReq, err = s.newEditRequest(ctx, baseURL, req)
	} else {
		httpReq, err = s.newGenerationRequest(ctx, baseURL, req)
	}
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.ProviderAPIKey)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New(strings.TrimSpace(string(body)))
	}

	var payload openAIImagesResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return normalizeImagesResult(req, payload), nil
}

func (s *GenerationService) newGenerationRequest(ctx context.Context, baseURL string, req model.GenerateRequest) (*http.Request, error) {
	payload := map[string]any{
		"model":           req.Model,
		"prompt":          req.Prompt,
		"response_format": req.ResponseFormat,
		"size":            req.Size,
		"n":               1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	requestURL, err := joinProviderPath(baseURL, "/v1/images/generations")
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}

func (s *GenerationService) newEditRequest(ctx context.Context, baseURL string, req model.GenerateRequest) (*http.Request, error) {
	hasRemoteOnly := true
	for _, image := range req.ReferenceImages {
		if strings.TrimSpace(image.DataURL) != "" {
			hasRemoteOnly = false
			break
		}
	}

	requestURL, err := joinProviderPath(baseURL, "/v1/images/edits")
	if err != nil {
		return nil, err
	}
	if hasRemoteOnly {
		images := make([]map[string]string, 0, len(req.ReferenceImages))
		for _, image := range req.ReferenceImages {
			remoteURL := strings.TrimSpace(image.RemoteURL)
			if remoteURL == "" {
				remoteURL = strings.TrimSpace(image.DataURL)
			}
			if remoteURL == "" {
				continue
			}
			images = append(images, map[string]string{"image_url": remoteURL})
		}
		payload := map[string]any{
			"model":           req.Model,
			"prompt":          req.Prompt,
			"response_format": req.ResponseFormat,
			"size":            req.Size,
			"n":               1,
			"input_fidelity":  "high",
			"images":          images,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, nil
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", req.Model)
	_ = writer.WriteField("prompt", req.Prompt)
	_ = writer.WriteField("size", req.Size)
	_ = writer.WriteField("n", "1")
	_ = writer.WriteField("response_format", req.ResponseFormat)
	_ = writer.WriteField("input_fidelity", "high")

	for index, image := range req.ReferenceImages {
		dataURL := strings.TrimSpace(image.DataURL)
		if dataURL == "" {
			continue
		}
		fileName, _, fileBytes, err := decodeDataURLImage(dataURL, image.Name, image.MimeType)
		if err != nil {
			return nil, err
		}
		fieldName := "image"
		if index > 0 {
			fieldName = "image[" + strconv.Itoa(index) + "]"
		}
		part, err := writer.CreateFormFile(fieldName, fileName)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(fileBytes); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, &body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())
	return httpReq, nil
}

func normalizeImagesResult(req model.GenerateRequest, payload openAIImagesResponse) map[string]any {
	images := make([]map[string]any, 0, len(payload.Data))
	for _, item := range payload.Data {
		images = append(images, map[string]any{
			"url":            item.URL,
			"b64_json":       item.B64JSON,
			"revised_prompt": item.RevisedPrompt,
		})
	}
	return map[string]any{
		"type":            "image_generation",
		"provider":        "openai-compatible",
		"model":           req.Model,
		"size":            req.Size,
		"response_format": req.ResponseFormat,
		"created":         payload.Created,
		"images":          images,
	}
}

func joinProviderPath(baseURL string, endpoint string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + endpoint
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func decodeDataURLImage(dataURL string, fileName string, fallbackMimeType string) (string, string, []byte, error) {
	meta, raw, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(meta, "data:") {
		return "", "", nil, errors.New("invalid reference image data url")
	}
	mimeType := fallbackMimeType
	if mimeType == "" {
		mimeType = "image/png"
	}
	if parsedMime, _, ok := strings.Cut(strings.TrimPrefix(meta, "data:"), ";"); ok && strings.TrimSpace(parsedMime) != "" {
		mimeType = strings.TrimSpace(parsedMime)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", "", nil, err
	}
	if strings.TrimSpace(fileName) == "" {
		fileName = "reference.png"
	}
	return fileName, mimeType, decoded, nil
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func copyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func referenceImagesValue(value any) []model.ReferenceImage {
	items, ok := value.([]map[string]any)
	if !ok {
		rawItems, ok := value.([]any)
		if !ok {
			return nil
		}
		items = make([]map[string]any, 0, len(rawItems))
		for _, raw := range rawItems {
			item, ok := raw.(map[string]any)
			if ok {
				items = append(items, item)
			}
		}
	}
	references := make([]model.ReferenceImage, 0, len(items))
	for _, item := range items {
		references = append(references, model.ReferenceImage{
			Name:      stringValue(item["name"]),
			MimeType:  stringValue(item["mime_type"]),
			DataURL:   stringValue(item["data_url"]),
			RemoteURL: stringValue(item["remote_url"]),
		})
	}
	return references
}

func imageMapsValue(value any) []map[string]any {
	items, ok := value.([]map[string]any)
	if ok {
		return items
	}
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	images := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if ok {
			images = append(images, item)
		}
	}
	return images
}
