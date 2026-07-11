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
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

var (
	ErrPromptRequired        = errors.New("prompt is required")
	ErrProviderKeyRequired   = errors.New("provider api key is required")
	ErrProviderBaseURL       = errors.New("provider base url is required")
	ErrImageModelUnsupported = errors.New("image generation model is not supported")
)

const (
	defaultImageModel          = "gemini-2.5-flash-image"
	defaultImageSize           = "1024x1024"
	defaultImageResponseFormat = "b64_json"
)

type GenerationServiceOptions struct {
	HTTPClient  *http.Client
	BatchClient *BatchClient
}

type GenerationService struct {
	history     *service.HistoryService
	httpClient  *http.Client
	batchClient *BatchClient
	tasksMu     sync.Mutex
	localTasks  map[string]context.CancelFunc
}

type UpstreamHTTPError struct {
	StatusCode int
	Message    string
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
	batchClient := opts.BatchClient
	if batchClient == nil {
		batchClient = NewBatchClient(client)
	}
	return &GenerationService{
		history:     history,
		httpClient:  client,
		batchClient: batchClient,
		localTasks:  make(map[string]context.CancelFunc),
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
	if !supportsBatchGeneration(req.Model) && !supportsSynchronousGPTGeneration(req.Model) {
		return nil, ErrImageModelUnsupported
	}
	req.Size = strings.TrimSpace(req.Size)
	if req.Size == "" {
		req.Size = defaultImageSize
	}
	req.ResponseFormat = strings.TrimSpace(req.ResponseFormat)
	if req.ResponseFormat == "" {
		req.ResponseFormat = defaultImageResponseFormat
	}
	if strings.TrimSpace(providerBaseURL) == "" {
		return nil, ErrProviderBaseURL
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

	if supportsBatchGeneration(req.Model) {
		return s.submitBatch(ctx, record, providerBaseURL, req)
	}

	return s.submitLocalTask(record, providerBaseURL, req), nil
}

func supportsBatchGeneration(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(modelName, "gemini-") && strings.Contains(modelName, "image")
}

func supportsSynchronousGPTGeneration(modelName string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelName)), "gpt-image-")
}

func (s *GenerationService) submitLocalTask(record *model.HistoryRecord, providerBaseURL string, req GenerateRequest) *GenerateResponse {
	record.Request["local_task"] = true
	record.Result = map[string]any{
		"type":     "image_generation",
		"provider": "openai-compatible",
		"model":    req.Model,
		"size":     req.Size,
	}
	_ = s.history.Update(context.Background(), record)

	taskCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	s.tasksMu.Lock()
	s.localTasks[record.ID] = cancel
	s.tasksMu.Unlock()
	go s.runLocalTask(taskCtx, cancel, record.ID, providerBaseURL, req)
	return &GenerateResponse{JobID: record.ID, Status: model.HistoryStatusPending, Result: record.Result}
}

func (s *GenerationService) runLocalTask(ctx context.Context, cancel context.CancelFunc, historyID, providerBaseURL string, req GenerateRequest) {
	defer func() {
		cancel()
		s.tasksMu.Lock()
		delete(s.localTasks, historyID)
		s.tasksMu.Unlock()
	}()

	result, generationErr := s.generateWithProvider(ctx, providerBaseURL, req)
	record, err := s.history.Get(context.Background(), model.CurrentPrincipal{Role: model.RoleAdmin}, historyID)
	if err != nil || isTerminalHistoryStatus(record.Status) {
		return
	}
	if generationErr != nil {
		record.Status = model.HistoryStatusFailed
		record.ErrorMessage = generationErr.Error()
	} else {
		record.Status = model.HistoryStatusSucceeded
		record.Result = result
		record.ErrorMessage = ""
	}
	_ = s.history.Update(context.Background(), record)
}

func (s *GenerationService) submitBatch(ctx context.Context, record *model.HistoryRecord, providerBaseURL string, req GenerateRequest) (*GenerateResponse, error) {
	customID := "plugin-image-" + record.ID
	references, err := batchReferenceInputs(req.ReferenceImages)
	if err != nil {
		record.Status = model.HistoryStatusFailed
		record.ErrorMessage = err.Error()
		_ = s.history.Update(ctx, record)
		return nil, err
	}
	payload := batchSubmitRequest{
		Model:            req.Model,
		TaskName:         "plugin-image-" + record.ID,
		ResponseMimeType: "image/png",
		Items: []batchSubmitItem{{
			CustomID:        customID,
			Prompt:          req.Prompt,
			OutputCount:     1,
			ReferenceImages: references,
		}},
		Metadata: map[string]string{"plugin_history_id": record.ID},
	}
	payload.AspectRatio, payload.ImageSize = batchDimensions(req.Size)
	job, err := s.batchClient.Submit(ctx, providerBaseURL, req.ProviderAPIKey, customID, payload)
	if err != nil {
		record.Status = model.HistoryStatusFailed
		record.ErrorMessage = err.Error()
		_ = s.history.Update(ctx, record)
		return nil, err
	}
	record.Request["batch_id"] = job.ID
	record.Request["batch_custom_id"] = customID
	record.Result = map[string]any{
		"type":         "image_generation",
		"provider":     "batch",
		"model":        req.Model,
		"size":         req.Size,
		"batch_status": job.Status,
	}
	if err := s.history.Update(ctx, record); err != nil {
		return nil, err
	}
	return &GenerateResponse{JobID: record.ID, Status: record.Status, Result: record.Result}, nil
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

func (s *GenerationService) Status(ctx context.Context, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
	record, err := s.history.Get(ctx, principal, id)
	if err != nil {
		return nil, err
	}
	if isTerminalHistoryStatus(record.Status) {
		return record, nil
	}
	if boolValue(record.Request["local_task"]) {
		return record, nil
	}
	return s.reconcileBatch(ctx, record, providerBaseURL)
}

func (s *GenerationService) reconcileBatch(ctx context.Context, record *model.HistoryRecord, providerBaseURL string) (*model.HistoryRecord, error) {
	batchID := stringValue(record.Request["batch_id"])
	apiKey := stringValue(record.Request["provider_api_key"])
	if batchID == "" || apiKey == "" {
		return nil, errors.New("batch tracking data is missing")
	}
	job, err := s.batchClient.Get(ctx, providerBaseURL, apiKey, batchID)
	if err != nil {
		return nil, err
	}
	if record.Result == nil {
		record.Result = map[string]any{}
	}
	record.Result["batch_status"] = job.Status
	switch strings.ToLower(job.Status) {
	case "completed":
		if err := s.completeBatchResult(ctx, record, providerBaseURL, apiKey, batchID); err != nil {
			return nil, err
		}
		record.Status = model.HistoryStatusSucceeded
		record.ErrorMessage = ""
	case "failed", "output_deleted":
		record.Status = model.HistoryStatusFailed
		record.ErrorMessage = "batch image generation failed"
	case "cancelled", "canceled":
		record.Status = model.HistoryStatusCanceled
	default:
		record.Status = model.HistoryStatusPending
	}
	if err := s.history.Update(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *GenerationService) completeBatchResult(ctx context.Context, record *model.HistoryRecord, providerBaseURL, apiKey, batchID string) error {
	items, err := s.batchClient.ListItems(ctx, providerBaseURL, apiKey, batchID)
	if err != nil {
		return err
	}
	customID := stringValue(record.Request["batch_custom_id"])
	var found *batchItem
	for index := range items.Data {
		if items.Data[index].CustomID == customID {
			found = &items.Data[index]
			break
		}
	}
	if found == nil || found.Status != "completed" || found.ImageCount < 1 {
		return errors.New("completed batch image item is missing")
	}
	content, mimeType, err := s.batchClient.GetItemContent(ctx, providerBaseURL, apiKey, batchID, customID, 0)
	if err != nil {
		return err
	}
	dataURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(content)
	record.Result = map[string]any{
		"type":         "image_generation",
		"provider":     "batch",
		"model":        stringValue(record.Request["model"]),
		"size":         stringValue(record.Request["size"]),
		"batch_status": "completed",
		"images": []map[string]any{{
			"url":            dataURL,
			"b64_json":       base64.StdEncoding.EncodeToString(content),
			"revised_prompt": "",
		}},
	}
	return nil
}

func isTerminalHistoryStatus(status string) bool {
	return status == model.HistoryStatusSucceeded || status == model.HistoryStatusFailed || status == model.HistoryStatusCanceled
}

func (s *GenerationService) Cancel(ctx context.Context, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
	record, err := s.history.Get(ctx, principal, id)
	if err != nil {
		return nil, err
	}
	if record.Status != model.HistoryStatusPending {
		return nil, errors.New("only pending jobs can be canceled")
	}
	if boolValue(record.Request["local_task"]) {
		s.tasksMu.Lock()
		cancel := s.localTasks[id]
		s.tasksMu.Unlock()
		record.Status = model.HistoryStatusCanceled
		if err := s.history.Update(ctx, record); err != nil {
			return nil, err
		}
		if cancel != nil {
			cancel()
		}
		return record, nil
	}
	if batchID := stringValue(record.Request["batch_id"]); batchID != "" {
		if _, err := s.batchClient.Cancel(ctx, providerBaseURL, stringValue(record.Request["provider_api_key"]), batchID); err != nil {
			return nil, err
		}
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
			message = "upstream image request failed"
		}
		return nil, &UpstreamHTTPError{StatusCode: resp.StatusCode, Message: message}
	}
	var payload openAIImagesResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return normalizeImagesResult(req, payload), nil
}

func (s *GenerationService) newGenerationRequest(ctx context.Context, baseURL string, req GenerateRequest) (*http.Request, error) {
	payload := map[string]any{
		"model": req.Model, "prompt": req.Prompt, "response_format": req.ResponseFormat, "size": req.Size, "n": 1,
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
	requestURL, err := joinProviderPath(baseURL, "/v1/images/edits")
	if err != nil {
		return nil, err
	}
	hasRemoteOnly := true
	for _, image := range req.ReferenceImages {
		if strings.TrimSpace(image.DataURL) != "" {
			hasRemoteOnly = false
			break
		}
	}
	if hasRemoteOnly {
		images := make([]map[string]string, 0, len(req.ReferenceImages))
		for _, image := range req.ReferenceImages {
			remoteURL := strings.TrimSpace(image.RemoteURL)
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
		if strings.TrimSpace(image.DataURL) == "" {
			continue
		}
		fileName, mimeType, fileBytes, err := decodeDataURLImage(image.DataURL, image.Name, image.MimeType)
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

func createImageFormFile(writer *multipart.Writer, fieldName, fileName, mimeType string) (io.Writer, error) {
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
			"url": item.URL, "b64_json": item.B64JSON, "revised_prompt": item.RevisedPrompt,
		})
	}
	return map[string]any{
		"type": "image_generation", "provider": "openai-compatible", "model": req.Model,
		"size": req.Size, "response_format": req.ResponseFormat, "created": payload.Created, "images": images,
	}
}

func joinProviderPath(baseURL, endpoint string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + endpoint
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func batchReferenceInputs(images []ReferenceImage) ([]batchReferenceInput, error) {
	references := make([]batchReferenceInput, 0, len(images))
	for index, image := range images {
		mimeType := strings.TrimSpace(image.MimeType)
		if mimeType == "" {
			mimeType = "image/png"
		}
		reference := batchReferenceInput{
			ID:       strings.TrimSpace(image.Name),
			MimeType: mimeType,
		}
		if dataURL := strings.TrimSpace(image.DataURL); dataURL != "" {
			_, decodedMimeType, data, err := decodeDataURLImage(dataURL, image.Name, mimeType)
			if err != nil {
				return nil, err
			}
			reference.MimeType = decodedMimeType
			reference.Data = data
		} else if remoteURL := strings.TrimSpace(image.RemoteURL); remoteURL != "" {
			reference.FileURI = remoteURL
		} else {
			return nil, errors.New("reference image " + strconv.Itoa(index+1) + " has no data or file URI")
		}
		references = append(references, reference)
	}
	return references, nil
}

func batchDimensions(size string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "1024x1024", "1:1":
		return "1:1", "1K"
	case "1536x1024", "3:2":
		return "3:2", "1K"
	case "1024x1536", "2:3":
		return "2:3", "1K"
	default:
		return "", ""
	}
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

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
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
