package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
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
	HTTPClient *http.Client
}

type GenerationService struct {
	history    *service.HistoryService
	httpClient *http.Client
}

type UpstreamHTTPError struct {
	StatusCode int
	Message    string
}

func (e *UpstreamHTTPError) Error() string {
	if e == nil {
		return "upstream request failed"
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.StatusCode > 0 {
		return "upstream request failed with status " + strconv.Itoa(e.StatusCode)
	}
	return "upstream request failed"
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

type GenerateRequest struct {
	Prompt          string           `json:"prompt"`
	ProviderAPIKey  string           `json:"provider_api_key,omitempty"`
	Model           string           `json:"model,omitempty"`
	Size            string           `json:"size,omitempty"`
	ResponseFormat  string           `json:"response_format,omitempty"`
	ReferenceImages []ReferenceImage `json:"reference_images,omitempty"`
	Inputs          map[string]any   `json:"inputs,omitempty"`
}

type GenerateResponse struct {
	JobID  string         `json:"job_id"`
	Status string         `json:"status"`
	Result map[string]any `json:"result,omitempty"`
}

type ReferenceImage struct {
	Name      string `json:"name,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	DataURL   string `json:"data_url,omitempty"`
	RemoteURL string `json:"remote_url,omitempty"`
}

type CreationRecord struct {
	ID        string         `json:"id"`
	HistoryID string         `json:"history_id"`
	UserID    int64          `json:"user_id"`
	UserEmail string         `json:"user_email"`
	PluginKey string         `json:"plugin_key"`
	Prompt    string         `json:"prompt"`
	Model     string         `json:"model,omitempty"`
	Size      string         `json:"size,omitempty"`
	ImageURL  string         `json:"image_url,omitempty"`
	B64JSON   string         `json:"b64_json,omitempty"`
	Result    map[string]any `json:"result,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func NewGenerationService(history *service.HistoryService, opts GenerationServiceOptions) *GenerationService {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &GenerationService{
		history:    history,
		httpClient: client,
	}
}

func (s *GenerationService) Generate(ctx context.Context, principal model.CurrentPrincipal, providerBaseURL string, req GenerateRequest) (*GenerateResponse, error) {
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

	log.Printf(
		"[plugin-service] image generation start user_id=%d role=%s plugin=%s model=%s size=%s has_reference_images=%t provider_base_url=%s",
		principal.UserID,
		principal.Role,
		principal.Plugin,
		req.Model,
		req.Size,
		len(req.ReferenceImages) > 0,
		strings.TrimSpace(providerBaseURL),
	)

	record, err := s.history.Create(ctx, principal, displayPrompt(req), requestPayload(req))
	if err != nil {
		log.Printf("[plugin-service] image generation create history failed user_id=%d err=%v", principal.UserID, err)
		return nil, err
	}

	result, err := s.generateWithProvider(ctx, providerBaseURL, req)
	if err != nil {
		record.Status = model.HistoryStatusFailed
		record.ErrorMessage = err.Error()
		_ = s.history.Update(ctx, record)
		log.Printf("[plugin-service] image generation failed user_id=%d history_id=%s err=%v", principal.UserID, record.ID, err)
		return nil, err
	}

	record.Status = model.HistoryStatusSucceeded
	record.Result = result
	record.ErrorMessage = ""
	if err := s.history.Update(ctx, record); err != nil {
		log.Printf("[plugin-service] image generation update history failed user_id=%d history_id=%s err=%v", principal.UserID, record.ID, err)
		return nil, err
	}

	log.Printf("[plugin-service] image generation succeeded user_id=%d history_id=%s", principal.UserID, record.ID)

	return &GenerateResponse{
		JobID:  record.ID,
		Status: record.Status,
		Result: record.Result,
	}, nil
}

func (s *GenerationService) Retry(ctx context.Context, principal model.CurrentPrincipal, providerBaseURL string, id string) (*GenerateResponse, error) {
	record, err := s.history.Get(ctx, principal, id)
	if err != nil {
		return nil, err
	}
	retryReq := GenerateRequest{
		Prompt:          stringValue(record.Request["prompt"]),
		ProviderAPIKey:  stringValue(record.Request["provider_api_key"]),
		Model:           stringValue(record.Request["model"]),
		Size:            stringValue(record.Request["size"]),
		ResponseFormat:  stringValue(record.Request["response_format"]),
		ReferenceImages: referenceImagesValue(record.Request["reference_images"]),
		Inputs:          copyMap(record.Request),
	}
	if strings.TrimSpace(retryReq.Prompt) == "" {
		retryReq.Prompt = record.Prompt
	}
	return s.Generate(ctx, principal, providerBaseURL, retryReq)
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

func (s *GenerationService) ListCreations(ctx context.Context, principal model.CurrentPrincipal, query model.HistoryQuery) ([]CreationRecord, error) {
	records, err := s.history.List(ctx, principal, query)
	if err != nil {
		return nil, err
	}
	creations := make([]CreationRecord, 0, len(records))
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
			creations = append(creations, CreationRecord{
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

func (s *GenerationService) generateWithProvider(ctx context.Context, providerBaseURL string, req GenerateRequest) (map[string]any, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(providerBaseURL), "/")
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
		message := extractUpstreamErrorMessage(body)
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
		if message == "" {
			message = "upstream request failed"
		}
		log.Printf("[plugin-service] upstream image request failed status=%d message=%s", resp.StatusCode, message)
		return nil, &UpstreamHTTPError{
			StatusCode: resp.StatusCode,
			Message:    message,
		}
	}

	var payload openAIImagesResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return normalizeImagesResult(req, payload), nil
}

func (s *GenerationService) newGenerationRequest(ctx context.Context, baseURL string, req GenerateRequest) (*http.Request, error) {
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

func (s *GenerationService) newEditRequest(ctx context.Context, baseURL string, req GenerateRequest) (*http.Request, error) {
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
		fileName, mimeType, fileBytes, err := decodeDataURLImage(dataURL, image.Name, image.MimeType)
		if err != nil {
			return nil, err
		}
		fieldName := "image"
		if index > 0 {
			fieldName = "image[" + strconv.Itoa(index) + "]"
		}
		part, err := createImageFormFile(writer, fieldName, fileName, mimeType)
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

func createImageFormFile(writer *multipart.Writer, fieldName string, fileName string, mimeType string) (io.Writer, error) {
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "image/png"
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="`+escapeQuotes(fieldName)+`"; filename="`+escapeQuotes(fileName)+`"`)
	header.Set("Content-Type", mimeType)
	return writer.CreatePart(header)
}

func escapeQuotes(value string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(value)
}

func normalizeImagesResult(req GenerateRequest, payload openAIImagesResponse) map[string]any {
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

func displayPrompt(req GenerateRequest) string {
	text := strings.TrimSpace(stringValue(req.Inputs["display_prompt"]))
	if text != "" {
		return text
	}
	return req.Prompt
}

func requestPayload(req GenerateRequest) map[string]any {
	payload := make(map[string]any)
	for key, value := range req.Inputs {
		payload[key] = value
	}
	payload["prompt"] = req.Prompt
	payload["provider_api_key"] = req.ProviderAPIKey
	payload["model"] = req.Model
	payload["size"] = req.Size
	payload["response_format"] = req.ResponseFormat
	if len(req.ReferenceImages) > 0 {
		references := make([]map[string]any, 0, len(req.ReferenceImages))
		for _, reference := range req.ReferenceImages {
			references = append(references, map[string]any{
				"name":       reference.Name,
				"mime_type":  reference.MimeType,
				"data_url":   reference.DataURL,
				"remote_url": reference.RemoteURL,
			})
		}
		payload["reference_images"] = references
	}
	return payload
}

func referenceImagesValue(value any) []ReferenceImage {
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
	references := make([]ReferenceImage, 0, len(items))
	for _, item := range items {
		references = append(references, ReferenceImage{
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

func extractUpstreamErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return trimmed
	}

	if msg := nestedStringValue(payload["error"], "message"); msg != "" {
		return msg
	}
	if msg := stringValue(payload["error"]); msg != "" {
		return msg
	}
	if msg := stringValue(payload["message"]); msg != "" {
		return msg
	}
	return trimmed
}

func nestedStringValue(value any, key string) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return stringValue(object[key])
}
