package backend

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/media"
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
	maxPersistedImageBytes     = 25 << 20
)

type GenerationServiceOptions struct {
	HTTPClient     *http.Client
	BatchClient    *BatchClient
	APIKeyResolver APIKeyResolver
	MediaStorage   media.Storage
}

type GenerationService struct {
	history      *service.HistoryService
	httpClient   *http.Client
	batchClient  *BatchClient
	keyResolver  APIKeyResolver
	mediaStorage media.Storage
	tasksMu      sync.Mutex
	localTasks   map[string]context.CancelFunc
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
	APIKeyID        int64            `json:"api_key_id"`
	ProviderAPIKey  string           `json:"-"`
	Model           string           `json:"model,omitempty"`
	Size            string           `json:"size,omitempty"`
	ResponseFormat  string           `json:"response_format,omitempty"`
	OutputCount     int              `json:"output_count,omitempty"`
	ReferenceImages []ReferenceImage `json:"reference_images,omitempty"`
	Inputs          map[string]any   `json:"inputs,omitempty"`
}

type GenerateResponse struct {
	JobID  string         `json:"job_id"`
	Status string         `json:"status"`
	Result map[string]any `json:"result,omitempty"`
}

type ReferenceImage struct {
	Name              string `json:"name,omitempty"`
	MimeType          string `json:"mime_type,omitempty"`
	DataURL           string `json:"data_url,omitempty"`
	RemoteURL         string `json:"remote_url,omitempty"`
	StorageKey        string `json:"storage_key,omitempty"`
	PreviewStorageKey string `json:"preview_storage_key,omitempty"`
	PreviewURL        string `json:"preview_url,omitempty"`
}

type UploadedReference struct {
	ReferenceImage
	OriginalURL string `json:"original_url"`
}

func (s *GenerationService) UploadReference(ctx context.Context, principal model.CurrentPrincipal, name, contentType string, body io.Reader) (*UploadedReference, error) {
	if s.mediaStorage == nil {
		return nil, errors.New("image storage unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(body, maxPersistedImageBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxPersistedImageBytes {
		return nil, errors.New("reference image has invalid size")
	}
	if detected := http.DetectContentType(data); !strings.HasPrefix(contentType, "image/") {
		contentType = detected
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, errors.New("reference file is not an image")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	uploadID := hex.EncodeToString(random)
	prefix := "image-generation/uploads/" + strconv.FormatInt(principal.UserID, 10) + "/" + uploadID
	originalKey := prefix + "/original"
	if err := s.mediaStorage.Put(ctx, originalKey, contentType, bytes.NewReader(data), int64(len(data))); err != nil {
		return nil, err
	}
	preview, previewType, err := createCompressedPreview(data)
	if err != nil {
		_ = s.mediaStorage.Delete(ctx, originalKey)
		return nil, fmt.Errorf("unsupported reference image format: %w", err)
	}
	previewKey := prefix + "/preview"
	if err := s.mediaStorage.Put(ctx, previewKey, previewType, bytes.NewReader(preview), int64(len(preview))); err != nil {
		_ = s.mediaStorage.Delete(ctx, originalKey)
		return nil, err
	}
	base := apiBasePath + "/references/" + uploadID
	return &UploadedReference{ReferenceImage: ReferenceImage{Name: name, MimeType: contentType, StorageKey: originalKey, PreviewStorageKey: previewKey, PreviewURL: base + "/preview"}, OriginalURL: base + "/original"}, nil
}

func (s *GenerationService) GetUploadedReference(ctx context.Context, principal model.CurrentPrincipal, uploadID, variant string) (*media.Object, error) {
	if s.mediaStorage == nil || (variant != "original" && variant != "preview") {
		return nil, media.ErrNotFound
	}
	prefix := "image-generation/uploads/" + strconv.FormatInt(principal.UserID, 10) + "/" + uploadID
	if variant == "original" {
		return s.mediaStorage.Get(ctx, prefix+"/original")
	}
	return s.mediaStorage.Get(ctx, prefix+"/preview")
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
	keyResolver := opts.APIKeyResolver
	if keyResolver == nil {
		keyResolver = NewMainServiceAPIKeyResolver(client)
	}
	return &GenerationService{
		history:      history,
		httpClient:   client,
		batchClient:  batchClient,
		keyResolver:  keyResolver,
		mediaStorage: opts.MediaStorage,
		localTasks:   make(map[string]context.CancelFunc),
	}
}

func (s *GenerationService) GetMedia(ctx context.Context, key string) (*media.Object, error) {
	if s.mediaStorage == nil || strings.TrimSpace(key) == "" {
		return nil, media.ErrNotFound
	}
	return s.mediaStorage.Get(ctx, key)
}

func (s *GenerationService) DeleteMedia(ctx context.Context, record *model.HistoryRecord) {
	if s.mediaStorage == nil || record == nil {
		return
	}
	keys := make([]string, 0)
	for _, reference := range referenceImagesValue(record.Request["reference_images"]) {
		if reference.StorageKey != "" {
			keys = append(keys, reference.StorageKey)
		}
		if reference.PreviewStorageKey != "" {
			keys = append(keys, reference.PreviewStorageKey)
		}
	}
	for _, image := range imageMapsValue(record.Result["images"]) {
		if key := stringValue(image["object_key"]); key != "" {
			keys = append(keys, key)
		}
		if key := stringValue(image["preview_object_key"]); key != "" {
			keys = append(keys, key)
		}
	}
	for _, key := range keys {
		if err := s.mediaStorage.Delete(ctx, key); err != nil {
			log.Printf("[plugin-service] failed to delete media object history_id=%s err=%v", record.ID, err)
		}
	}
}

func (s *GenerationService) Generate(ctx context.Context, principal model.CurrentPrincipal, providerBaseURL string, req GenerateRequest) (*GenerateResponse, error) {
	return s.generate(ctx, nil, principal, providerBaseURL, req)
}

func (s *GenerationService) GenerateWithRequest(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, req GenerateRequest) (*GenerateResponse, error) {
	return s.generate(ctx, source, principal, providerBaseURL, req)
}

func (s *GenerationService) generate(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, req GenerateRequest) (*GenerateResponse, error) {
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		return nil, ErrPromptRequired
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		req.Model = defaultImageModel
	}
	if !supportsBatchGeneration(req.Model) && !supportsSynchronousGPTGeneration(req.Model) {
		return nil, ErrImageModelUnsupported
	}
	if err := validateReferenceImageCount(req.Model, req.ReferenceImages); err != nil {
		return nil, err
	}
	outputCount, err := validateOutputCount(req.Model, req.OutputCount)
	if err != nil {
		return nil, err
	}
	req.OutputCount = outputCount
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
	if req.APIKeyID > 0 {
		secret, err := s.keyResolver.Resolve(ctx, source, principal, providerBaseURL, req.APIKeyID, req.Model)
		if err != nil {
			return nil, err
		}
		req.ProviderAPIKey = secret
	} else {
		req.ProviderAPIKey = strings.TrimSpace(req.ProviderAPIKey)
		if req.ProviderAPIKey == "" {
			return nil, ErrProviderKeyRequired
		}
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
	if s.mediaStorage != nil && len(req.ReferenceImages) > 0 {
		if err := s.archiveReferenceImages(ctx, principal, record.ID, req.ReferenceImages); err != nil {
			record.Status = model.HistoryStatusFailed
			record.ErrorMessage = err.Error()
			_ = s.history.Update(ctx, record)
			return nil, err
		}
		record.Request = requestPayload(req)
		if err := s.history.Update(ctx, record); err != nil {
			return nil, err
		}
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
	if generationErr == nil {
		generationErr = s.archiveResultImages(ctx, historyID, result)
	}
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
			OutputCount:     req.OutputCount,
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
	return s.retry(ctx, nil, principal, providerBaseURL, id)
}

func (s *GenerationService) RetryWithRequest(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, id string) (*GenerateResponse, error) {
	return s.retry(ctx, source, principal, providerBaseURL, id)
}

func (s *GenerationService) retry(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, id string) (*GenerateResponse, error) {
	record, err := s.history.Get(ctx, principal, id)
	if err != nil {
		return nil, err
	}
	retryReq := GenerateRequest{
		Prompt:          stringValue(record.Request["prompt"]),
		APIKeyID:        int64(intValue(record.Request["api_key_id"])),
		Model:           stringValue(record.Request["model"]),
		Size:            stringValue(record.Request["size"]),
		ResponseFormat:  stringValue(record.Request["response_format"]),
		OutputCount:     intValue(record.Request["output_count"]),
		ReferenceImages: referenceImagesValue(record.Request["reference_images"]),
		Inputs:          copyMap(record.Request),
	}
	if retryReq.APIKeyID <= 0 {
		return nil, ErrProviderKeyRequired
	}
	if err := s.restoreReferenceImages(ctx, retryReq.ReferenceImages); err != nil {
		return nil, err
	}
	if strings.TrimSpace(retryReq.Prompt) == "" {
		retryReq.Prompt = record.Prompt
	}
	return s.generate(ctx, source, principal, providerBaseURL, retryReq)
}

func (s *GenerationService) Status(ctx context.Context, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
	return s.status(ctx, nil, principal, providerBaseURL, id)
}

func (s *GenerationService) StatusWithRequest(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
	return s.status(ctx, source, principal, providerBaseURL, id)
}

func (s *GenerationService) status(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
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
	return s.reconcileBatch(ctx, source, principal, record, providerBaseURL)
}

func (s *GenerationService) reconcileBatch(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, record *model.HistoryRecord, providerBaseURL string) (*model.HistoryRecord, error) {
	batchID := stringValue(record.Request["batch_id"])
	apiKey, err := s.resolveStoredAPIKey(ctx, source, principal, providerBaseURL, record)
	if err != nil {
		return nil, err
	}
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
	images := make([]map[string]any, 0, found.ImageCount)
	for imageIndex := 0; imageIndex < found.ImageCount; imageIndex++ {
		content, mimeType, err := s.batchClient.GetItemContent(ctx, providerBaseURL, apiKey, batchID, customID, imageIndex)
		if err != nil {
			return err
		}
		image := map[string]any{
			"url":            "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(content),
			"b64_json":       base64.StdEncoding.EncodeToString(content),
			"revised_prompt": "",
		}
		if s.mediaStorage != nil {
			stored, err := s.archiveImage(ctx, record.ID, imageIndex, mimeType, content)
			if err != nil {
				return err
			}
			image = stored
		}
		images = append(images, image)
	}
	record.Result = map[string]any{
		"type":         "image_generation",
		"provider":     "batch",
		"model":        stringValue(record.Request["model"]),
		"size":         stringValue(record.Request["size"]),
		"batch_status": "completed",
		"images":       images,
	}
	return nil
}

func isTerminalHistoryStatus(status string) bool {
	return status == model.HistoryStatusSucceeded || status == model.HistoryStatusFailed || status == model.HistoryStatusCanceled
}

func (s *GenerationService) Cancel(ctx context.Context, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
	return s.cancel(ctx, nil, principal, providerBaseURL, id)
}

func (s *GenerationService) CancelWithRequest(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
	return s.cancel(ctx, source, principal, providerBaseURL, id)
}

func (s *GenerationService) cancel(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, id string) (*model.HistoryRecord, error) {
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
		apiKey, err := s.resolveStoredAPIKey(ctx, source, principal, providerBaseURL, record)
		if err != nil {
			return nil, err
		}
		if _, err := s.batchClient.Cancel(ctx, providerBaseURL, apiKey, batchID); err != nil {
			return nil, err
		}
	}
	record.Status = model.HistoryStatusCanceled
	if err := s.history.Update(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *GenerationService) resolveStoredAPIKey(ctx context.Context, source *http.Request, principal model.CurrentPrincipal, providerBaseURL string, record *model.HistoryRecord) (string, error) {
	if keyID := int64(intValue(record.Request["api_key_id"])); keyID > 0 {
		return s.keyResolver.Resolve(ctx, source, principal, providerBaseURL, keyID, stringValue(record.Request["model"]))
	}
	if legacy := strings.TrimSpace(stringValue(record.Request["provider_api_key"])); legacy != "" {
		return legacy, nil
	}
	return "", ErrProviderKeyRequired
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
	count := req.OutputCount
	if count < 1 {
		count = 1
	}
	if supportsSynchronousGPTGeneration(req.Model) && count > 1 {
		var merged map[string]any
		images := make([]map[string]any, 0, count)
		single := req
		single.OutputCount = 1
		for index := 0; index < count; index++ {
			result, err := s.generateSingleWithProvider(ctx, providerBaseURL, single)
			if err != nil {
				return nil, err
			}
			if merged == nil {
				merged = result
			}
			images = append(images, imageMapsValue(result["images"])...)
		}
		merged["images"] = images
		return merged, nil
	}
	return s.generateSingleWithProvider(ctx, providerBaseURL, req)
}

func (s *GenerationService) generateSingleWithProvider(ctx context.Context, providerBaseURL string, req GenerateRequest) (map[string]any, error) {
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
		"model": req.Model, "prompt": req.Prompt, "response_format": req.ResponseFormat, "size": req.Size, "n": req.OutputCount,
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
			"n":               req.OutputCount,
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
	_ = writer.WriteField("n", strconv.Itoa(req.OutputCount))
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

func (s *GenerationService) archiveResultImages(ctx context.Context, historyID string, result map[string]any) error {
	if s.mediaStorage == nil {
		return nil
	}
	images := imageMapsValue(result["images"])
	archived := make([]map[string]any, 0, len(images))
	for index, image := range images {
		contentType := "image/png"
		var data []byte
		var err error
		if encoded := stringValue(image["b64_json"]); encoded != "" {
			data, err = base64.StdEncoding.DecodeString(encoded)
		} else if rawURL := stringValue(image["url"]); rawURL != "" {
			data, contentType, err = s.downloadImage(ctx, rawURL)
		} else {
			err = errors.New("generated image has no data")
		}
		if err != nil {
			return err
		}
		stored, err := s.archiveImage(ctx, historyID, index, contentType, data)
		if err != nil {
			return err
		}
		stored["revised_prompt"] = stringValue(image["revised_prompt"])
		archived = append(archived, stored)
	}
	result["images"] = archived
	return nil
}

func (s *GenerationService) downloadImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", errors.New("generated image URL must use HTTP or HTTPS")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", errors.New("failed to download generated image")
	}
	limited := io.LimitReader(resp.Body, maxPersistedImageBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || len(data) > maxPersistedImageBytes {
		return nil, "", errors.New("generated image has invalid size")
	}
	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.HasPrefix(contentType, "image/") {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", errors.New("generated result is not an image")
	}
	return data, contentType, nil
}

func (s *GenerationService) archiveImage(ctx context.Context, historyID string, index int, contentType string, data []byte) (map[string]any, error) {
	digest := sha256.Sum256(data)
	extension := ".img"
	if extensions, _ := mime.ExtensionsByType(contentType); len(extensions) > 0 {
		extension = extensions[0]
	}
	key := "image-generation/" + historyID + "/result/" + strconv.Itoa(index) + "-" + hex.EncodeToString(digest[:]) + extension
	if err := s.mediaStorage.Put(ctx, key, contentType, bytes.NewReader(data), int64(len(data))); err != nil {
		return nil, err
	}
	stored := map[string]any{
		"url":          apiBasePath + "/assets/" + historyID + "/result/" + strconv.Itoa(index),
		"object_key":   key,
		"content_type": contentType,
		"byte_size":    len(data),
	}
	s.addPreview(ctx, stored, historyID, "result", index, data)
	return stored, nil
}

func (s *GenerationService) addPreview(ctx context.Context, stored map[string]any, historyID, kind string, index int, data []byte) ([]byte, string) {
	preview, contentType, err := createCompressedPreview(data)
	if err != nil {
		return nil, ""
	}
	extension := ".jpg"
	if contentType == "image/webp" {
		extension = ".webp"
	}
	digest := sha256.Sum256(preview)
	key := "image-generation/" + historyID + "/" + kind + "/preview-" + strconv.Itoa(index) + "-" + hex.EncodeToString(digest[:]) + extension
	if err := s.mediaStorage.Put(ctx, key, contentType, bytes.NewReader(preview), int64(len(preview))); err != nil {
		return nil, ""
	}
	stored["preview_object_key"] = key
	stored["preview_url"] = apiBasePath + "/assets/" + historyID + "/" + kind + "/" + strconv.Itoa(index) + "/preview"
	stored["preview_content_type"] = contentType
	stored["preview_byte_size"] = len(preview)
	return preview, contentType
}

func (s *GenerationService) archiveReferenceImages(ctx context.Context, principal model.CurrentPrincipal, historyID string, images []ReferenceImage) error {
	for index := range images {
		name, contentType, data, err := s.referenceImageBytes(ctx, principal, images[index])
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		extension := ".img"
		if extensions, _ := mime.ExtensionsByType(contentType); len(extensions) > 0 {
			extension = extensions[0]
		}
		key := "image-generation/" + historyID + "/reference/" + strconv.Itoa(index) + "-" + hex.EncodeToString(digest[:]) + extension
		if err := s.mediaStorage.Put(ctx, key, contentType, bytes.NewReader(data), int64(len(data))); err != nil {
			return err
		}
		images[index].Name = name
		images[index].MimeType = contentType
		images[index].StorageKey = key
		previewMetadata := map[string]any{}
		s.addPreview(ctx, previewMetadata, historyID, "reference", index, data)
		images[index].PreviewStorageKey = stringValue(previewMetadata["preview_object_key"])
		images[index].PreviewURL = stringValue(previewMetadata["preview_url"])
		images[index].DataURL = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return nil
}

func (s *GenerationService) referenceImageBytes(ctx context.Context, principal model.CurrentPrincipal, image ReferenceImage) (string, string, []byte, error) {
	expectedUploadPrefix := "image-generation/uploads/" + strconv.FormatInt(principal.UserID, 10) + "/"
	if strings.HasPrefix(image.StorageKey, expectedUploadPrefix) {
		object, err := s.mediaStorage.Get(ctx, image.StorageKey)
		if err != nil {
			return "", "", nil, err
		}
		defer object.Body.Close()
		data, err := io.ReadAll(io.LimitReader(object.Body, maxPersistedImageBytes+1))
		if err != nil || len(data) > maxPersistedImageBytes {
			return "", "", nil, errors.New("failed to read uploaded reference image")
		}
		return image.Name, object.ContentType, data, nil
	}
	if strings.HasPrefix(strings.TrimSpace(image.DataURL), "data:") {
		return decodeDataURLImage(image.DataURL, image.Name, image.MimeType)
	}
	rawURL := strings.TrimSpace(image.DataURL)
	if rawURL == "" {
		rawURL = strings.TrimSpace(image.RemoteURL)
	}
	prefix := apiBasePath + "/assets/"
	parsedURL, parseErr := url.Parse(rawURL)
	storedPath := rawURL
	if parseErr == nil {
		storedPath = parsedURL.Path
	}
	if strings.HasPrefix(storedPath, prefix) {
		parts := strings.Split(strings.TrimPrefix(storedPath, prefix), "/")
		if len(parts) != 3 || (parts[1] != "result" && parts[1] != "reference") {
			return "", "", nil, errors.New("invalid stored image URL")
		}
		index, err := strconv.Atoi(parts[2])
		if err != nil || index < 0 {
			return "", "", nil, errors.New("invalid stored image index")
		}
		record, err := s.history.Get(ctx, principal, parts[0])
		if err != nil {
			return "", "", nil, err
		}
		var objectKey string
		if parts[1] == "result" {
			results := imageMapsValue(record.Result["images"])
			if index < len(results) {
				objectKey = stringValue(results[index]["object_key"])
			}
		} else {
			references := referenceImagesValue(record.Request["reference_images"])
			if index < len(references) {
				objectKey = references[index].StorageKey
			}
		}
		if objectKey == "" {
			return "", "", nil, media.ErrNotFound
		}
		object, err := s.mediaStorage.Get(ctx, objectKey)
		if err != nil {
			return "", "", nil, err
		}
		defer object.Body.Close()
		data, err := io.ReadAll(io.LimitReader(object.Body, maxPersistedImageBytes+1))
		if err != nil || len(data) > maxPersistedImageBytes {
			return "", "", nil, errors.New("failed to read stored reference image")
		}
		return image.Name, object.ContentType, data, nil
	}
	data, contentType, err := s.downloadImage(ctx, rawURL)
	return image.Name, contentType, data, err
}

func (s *GenerationService) restoreReferenceImages(ctx context.Context, images []ReferenceImage) error {
	if s.mediaStorage == nil {
		return nil
	}
	for index := range images {
		if images[index].DataURL != "" || images[index].StorageKey == "" {
			continue
		}
		object, err := s.mediaStorage.Get(ctx, images[index].StorageKey)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(object.Body, maxPersistedImageBytes+1))
		_ = object.Body.Close()
		if err != nil {
			return err
		}
		if len(data) > maxPersistedImageBytes {
			return errors.New("stored reference image is too large")
		}
		images[index].MimeType = object.ContentType
		images[index].DataURL = "data:" + object.ContentType + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return nil
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

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := strconv.Atoi(typed.String())
		return parsed
	default:
		return 0
	}
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
	if req.APIKeyID > 0 {
		payload["api_key_id"] = req.APIKeyID
	} else if req.ProviderAPIKey != "" {
		payload["provider_api_key"] = req.ProviderAPIKey
	}
	payload["model"] = req.Model
	payload["size"] = req.Size
	payload["response_format"] = req.ResponseFormat
	payload["output_count"] = req.OutputCount
	if len(req.ReferenceImages) > 0 {
		references := make([]map[string]any, 0, len(req.ReferenceImages))
		for _, reference := range req.ReferenceImages {
			stored := map[string]any{
				"name":                reference.Name,
				"mime_type":           reference.MimeType,
				"remote_url":          reference.RemoteURL,
				"storage_key":         reference.StorageKey,
				"preview_storage_key": reference.PreviewStorageKey,
				"preview_url":         reference.PreviewURL,
			}
			if reference.StorageKey == "" {
				stored["data_url"] = reference.DataURL
			}
			references = append(references, stored)
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
			Name:              stringValue(item["name"]),
			MimeType:          stringValue(item["mime_type"]),
			DataURL:           stringValue(item["data_url"]),
			RemoteURL:         stringValue(item["remote_url"]),
			StorageKey:        stringValue(item["storage_key"]),
			PreviewStorageKey: stringValue(item["preview_storage_key"]),
			PreviewURL:        stringValue(item["preview_url"]),
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
