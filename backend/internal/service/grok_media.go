package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func (s *OpenAIGatewayService) SelectAccountWithSchedulerForGrokMedia(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return s.selectAccountWithSchedulerForPlatform(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", "", false, PlatformGrok)
}

func (s *OpenAIGatewayService) selectAccountWithSchedulerForPlatform(ctx context.Context, groupID *int64, previousResponseID string, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, requiredTransport OpenAIUpstreamTransport, requiredCapability OpenAIEndpointCapability, requiredImageCapability OpenAIImagesCapability, requireCompact bool, platform string) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	decision := OpenAIAccountScheduleDecision{}
	scheduler := s.getOpenAIAccountScheduler(ctx)
	if scheduler != nil {
		var stickyAccountID int64
		if sessionHash != "" && s.cache != nil {
			if accountID, err := s.getStickySessionAccountID(ctx, groupID, sessionHash); err == nil && accountID > 0 {
				stickyAccountID = accountID
			}
		}
		return scheduler.Select(ctx, OpenAIAccountScheduleRequest{GroupID: groupID, TargetPlatform: platform, SessionHash: sessionHash, StickyAccountID: stickyAccountID, PreviousResponseID: previousResponseID, RequestedModel: requestedModel, RequiredTransport: requiredTransport, RequiredCapability: requiredCapability, RequiredImageCapability: requiredImageCapability, RequireCompact: requireCompact, ExcludedIDs: excludedIDs})
	}
	selection, err := s.selectAccountForModelFromPlatform(ctx, groupID, sessionHash, requestedModel, excludedIDs, platform)
	return selection, decision, err
}

func (s *OpenAIGatewayService) selectAccountForModelFromPlatform(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, platform string) (*AccountSelectionResult, error) {
	accounts, err := s.listSchedulableAccountsForPlatform(ctx, groupID, platform)
	if err != nil {
		return nil, err
	}
	for i := range accounts {
		account := &accounts[i]
		if excludedIDs != nil {
			if _, excluded := excludedIDs[account.ID]; excluded {
				continue
			}
		}
		if !account.IsSchedulable() || account.Platform != platform {
			continue
		}
		if account.IsGrok() {
			if paused, _ := shouldAutoPauseGrokAccountByQuota(account); paused {
				continue
			}
		}
		if requestedModel != "" && !account.IsModelSupported(requestedModel) {
			continue
		}
		result, acquireErr := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
		if acquireErr != nil {
			return nil, acquireErr
		}
		if result != nil && result.Acquired {
			if sessionHash != "" {
				_ = s.BindStickySession(ctx, groupID, sessionHash, account.ID)
			}
			return &AccountSelectionResult{Account: account, Acquired: true, ReleaseFunc: result.ReleaseFunc}, nil
		}
	}
	return nil, noAvailableOpenAISelectionErrorForRequest(requestedModel, "", false, false)
}

type GrokMediaEndpoint string

const (
	GrokMediaEndpointImagesGenerations GrokMediaEndpoint = "images_generations"
	GrokMediaEndpointImagesEdits       GrokMediaEndpoint = "images_edits"
	GrokMediaEndpointVideosGenerations GrokMediaEndpoint = "videos_generations"
	GrokMediaEndpointVideoStatus       GrokMediaEndpoint = "video_status"
)

func (e GrokMediaEndpoint) RequiresRequestBody() bool { return e != GrokMediaEndpointVideoStatus }

func (e GrokMediaEndpoint) IsGenerationRequest() bool {
	switch e {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits, GrokMediaEndpointVideosGenerations:
		return true
	default:
		return false
	}
}

func (e GrokMediaEndpoint) httpMethod() string {
	if e == GrokMediaEndpointVideoStatus {
		return http.MethodGet
	}
	return http.MethodPost
}

type GrokMediaRequestInfo struct {
	Model          string
	Prompt         string
	N              int
	Size           string
	SizeTier       string
	InputImageURLs []string
	MaskImageURL   string
	Uploads        []OpenAIImagesUpload
	MaskUpload     *OpenAIImagesUpload
}

func (r GrokMediaRequestInfo) ModerationBody() []byte {
	payload := map[string]any{}
	if prompt := strings.TrimSpace(r.Prompt); prompt != "" {
		payload["prompt"] = prompt
	}
	images := make([]map[string]string, 0, len(r.InputImageURLs)+len(r.Uploads)+1)
	for _, imageURL := range r.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, upload := range r.Uploads {
		if dataURL := upload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if maskURL := strings.TrimSpace(r.MaskImageURL); maskURL != "" {
		images = append(images, map[string]string{"image_url": maskURL})
	}
	if r.MaskUpload != nil {
		if dataURL := r.MaskUpload.ModerationDataURL(); dataURL != "" {
			images = append(images, map[string]string{"image_url": dataURL})
		}
	}
	if len(images) > 0 {
		payload["images"] = images
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}

func ExtractGrokMediaModel(contentType string, body []byte) string {
	return ParseGrokMediaRequest(contentType, body).Model
}

func ParseGrokMediaRequest(contentType string, body []byte) GrokMediaRequestInfo {
	info := GrokMediaRequestInfo{N: 1}
	if gjson.ValidBytes(body) {
		parseGrokMediaJSONRequest(body, &info)
	} else {
		parseGrokMediaMultipartRequest(contentType, body, &info)
	}
	info.Model = normalizeGrokMediaModelAlias(strings.TrimSpace(info.Model))
	info.Prompt = strings.TrimSpace(info.Prompt)
	info.Size = strings.TrimSpace(info.Size)
	info.SizeTier = NormalizeImageBillingTierOrDefault(info.Size)
	if info.N <= 0 {
		info.N = 1
	}
	return info
}

func parseGrokMediaJSONRequest(body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	info.Model = strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info.Prompt = strings.TrimSpace(gjson.GetBytes(body, "prompt").String())
	info.Size = strings.TrimSpace(gjson.GetBytes(body, "size").String())
	if n := gjson.GetBytes(body, "n"); n.Exists() && n.Type == gjson.Number {
		info.N = int(n.Int())
	}
	appendJSONImageURLs := func(value gjson.Result) {
		if !value.Exists() {
			return
		}
		if value.IsArray() {
			for _, item := range value.Array() {
				appendGrokMediaImageURL(info, item)
			}
			return
		}
		appendGrokMediaImageURL(info, value)
	}
	appendJSONImageURLs(gjson.GetBytes(body, "image"))
	appendJSONImageURLs(gjson.GetBytes(body, "images"))
	info.MaskImageURL = strings.TrimSpace(gjson.GetBytes(body, "mask.image_url").String())
}

func appendGrokMediaImageURL(info *GrokMediaRequestInfo, value gjson.Result) {
	if info == nil {
		return
	}
	if imageURL := strings.TrimSpace(value.Get("image_url").String()); imageURL != "" {
		info.InputImageURLs = append(info.InputImageURLs, imageURL)
		return
	}
	if value.Type == gjson.String {
		if imageURL := strings.TrimSpace(value.String()); imageURL != "" {
			info.InputImageURLs = append(info.InputImageURLs, imageURL)
		}
	}
}

func parseGrokMediaMultipartRequest(contentType string, body []byte, info *GrokMediaRequestInfo) {
	if info == nil {
		return
	}
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			return
		}
		name := strings.TrimSpace(part.FormName())
		if name == "" {
			_ = part.Close()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, openAIImageMaxUploadPartSize))
		_ = part.Close()
		if err != nil {
			return
		}
		fileName := strings.TrimSpace(part.FileName())
		partContentType := strings.TrimSpace(part.Header.Get("Content-Type"))
		if fileName != "" {
			upload := OpenAIImagesUpload{FieldName: name, FileName: fileName, ContentType: partContentType, Data: data}
			if name == "mask" {
				info.MaskUpload = &upload
				continue
			}
			if name == "image" || strings.HasPrefix(name, "image[") {
				info.Uploads = append(info.Uploads, upload)
			}
			continue
		}
		value := strings.TrimSpace(string(data))
		switch name {
		case "model":
			info.Model = value
		case "prompt":
			info.Prompt = value
		case "size":
			info.Size = value
		case "n":
			if n, err := strconv.Atoi(value); err == nil {
				info.N = n
			}
		case "image", "image_url":
			if value != "" {
				info.InputImageURLs = append(info.InputImageURLs, value)
			}
		case "mask", "mask_image_url":
			info.MaskImageURL = value
		}
	}
}

func normalizeGrokMediaModelAlias(model string) string {
	if strings.TrimSpace(model) == "grok-imagine" {
		return "grok-imagine-image-quality"
	}
	return strings.TrimSpace(model)
}

func (e GrokMediaEndpoint) upstreamURL(baseURL, requestID string) (string, error) {
	switch e {
	case GrokMediaEndpointImagesGenerations:
		return xai.BuildImagesGenerationsURL(baseURL)
	case GrokMediaEndpointImagesEdits:
		return xai.BuildImagesEditsURL(baseURL)
	case GrokMediaEndpointVideosGenerations:
		return xai.BuildVideosGenerationsURL(baseURL)
	case GrokMediaEndpointVideoStatus:
		return xai.BuildVideoURL(baseURL, requestID)
	default:
		return "", fmt.Errorf("unsupported grok media endpoint: %s", e)
	}
}

func (s *OpenAIGatewayService) ForwardGrokMedia(ctx context.Context, c *gin.Context, account *Account, endpoint GrokMediaEndpoint, requestID string, body []byte, contentType string) (*OpenAIForwardResult, error) {
	startTime := time.Now()
	if account == nil {
		return nil, fmt.Errorf("grok account is required")
	}
	if account.Platform != PlatformGrok {
		return nil, fmt.Errorf("account platform %s is not supported for grok media", account.Platform)
	}
	token, err := grokMediaAccessToken(account)
	if err != nil {
		return nil, err
	}
	targetURL, err := endpoint.upstreamURL(grokMediaBaseURL(account), requestID)
	if err != nil {
		return nil, err
	}
	body, contentType, err = prepareGrokMediaForwardBody(endpoint, body, contentType)
	if err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if endpoint.RequiresRequestBody() {
		bodyReader = bytes.NewReader(body)
	}
	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	defer releaseUpstreamCtx()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, endpoint.httpMethod(), targetURL, bodyReader)
	if err != nil {
		return nil, err
	}
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	upstreamReq.Header.Set("Accept", "application/json")
	upstreamReq.Header.Set("User-Agent", "sub2api-grok/1.0")
	if endpoint.RequiresRequestBody() {
		if strings.TrimSpace(contentType) == "" {
			contentType = "application/json"
		}
		upstreamReq.Header.Set("Content-Type", contentType)
	}
	account.ApplyHeaderOverrides(upstreamReq.Header)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()
	requestIDHeader := firstNonEmptyString(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
	requestInfo := ParseGrokMediaRequest(contentType, body)
	requestModel := requestInfo.Model
	if resp.StatusCode >= 400 {
		return s.handleGrokMediaErrorResponse(ctx, resp, c, account, requestIDHeader, requestModel)
	}
	respBody, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	writeGrokMediaResponse(c, resp, respBody, s.responseHeaderFilter)
	usage := grokMediaUsageFromResponse(endpoint, requestInfo, respBody)
	return &OpenAIForwardResult{RequestID: requestIDHeader, ResponseID: usage.ResponseID, Usage: usage.Usage, Model: requestModel, BillingModel: requestModel, UpstreamModel: requestModel, ResponseHeaders: resp.Header.Clone(), Duration: time.Since(startTime), ImageCount: usage.ImageCount, ImageSize: usage.ImageSize, ImageInputSize: usage.ImageInputSize, ImageOutputSizes: usage.ImageOutputSizes}, nil
}

func grokMediaAccessToken(account *Account) (string, error) {
	if account == nil {
		return "", fmt.Errorf("grok account is required")
	}
	if token := strings.TrimSpace(account.GetCredential("api_key")); token != "" {
		return token, nil
	}
	if token := strings.TrimSpace(account.GetCredential("access_token")); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("grok account missing api_key or access_token")
}

func grokMediaBaseURL(account *Account) string {
	if account == nil {
		return ""
	}
	return strings.TrimSpace(account.GetCredential("base_url"))
}

func prepareGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	if endpoint != GrokMediaEndpointImagesEdits || gjson.ValidBytes(body) {
		if gjson.ValidBytes(body) {
			return normalizeGrokMediaForwardBody(endpoint, body, contentType)
		}
		return body, contentType, nil
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return body, contentType, nil
	}
	info := ParseGrokMediaRequest(contentType, body)
	payload := make(map[string]any)
	if info.Model != "" {
		payload["model"] = info.Model
	}
	if info.Prompt != "" {
		payload["prompt"] = info.Prompt
	}
	if info.N > 1 {
		payload["n"] = info.N
	}
	if info.Size != "" {
		payload["size"] = info.Size
	}
	images := make([]map[string]string, 0, len(info.InputImageURLs)+len(info.Uploads))
	for _, imageURL := range info.InputImageURLs {
		if imageURL = strings.TrimSpace(imageURL); imageURL != "" {
			images = append(images, map[string]string{"image_url": imageURL})
		}
	}
	for _, upload := range info.Uploads {
		dataURL, err := openAIImageUploadToDataURL(upload)
		if err != nil {
			return nil, "", err
		}
		images = append(images, map[string]string{"image_url": dataURL})
	}
	if len(images) > 0 {
		payload["image"] = images[0]
		if len(images) > 1 {
			payload["images"] = images
		}
	}
	maskImageURL := strings.TrimSpace(info.MaskImageURL)
	if info.MaskUpload != nil {
		dataURL, err := openAIImageUploadToDataURL(*info.MaskUpload)
		if err != nil {
			return nil, "", err
		}
		maskImageURL = dataURL
	}
	if maskImageURL != "" {
		payload["mask"] = map[string]string{"image_url": maskImageURL}
	}
	out, err := marshalOpenAIUpstreamJSON(payload)
	if err != nil {
		return nil, "", err
	}
	return out, "application/json", nil
}

func normalizeGrokMediaForwardBody(endpoint GrokMediaEndpoint, body []byte, contentType string) ([]byte, string, error) {
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	info := ParseGrokMediaRequest(contentType, body)
	normalized := normalizeGrokMediaModelForEndpoint(endpoint, model, info.HasInputImage())
	if normalized == model || normalized == "" {
		return body, contentType, nil
	}
	rewritten, err := sjsonSetBytes(body, "model", normalized)
	if err != nil {
		return nil, "", err
	}
	return rewritten, contentType, nil
}

func (r GrokMediaRequestInfo) HasInputImage() bool {
	return len(r.InputImageURLs) > 0 || len(r.Uploads) > 0
}

func normalizeGrokMediaModelForEndpoint(endpoint GrokMediaEndpoint, model string, hasInputImage bool) string {
	model = strings.TrimSpace(model)
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		return normalizeGrokMediaModelAlias(model)
	case GrokMediaEndpointVideosGenerations:
		if model == "grok-imagine-video-1.5" && !hasInputImage {
			return "grok-imagine-video"
		}
	}
	return model
}

func sjsonSetBytes(body []byte, path, value string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload[path] = value
	return marshalOpenAIUpstreamJSON(payload)
}

type grokMediaUsageMetadata struct {
	ResponseID       string
	Usage            OpenAIUsage
	ImageCount       int
	ImageSize        string
	ImageInputSize   string
	ImageOutputSizes []string
}

func grokMediaUsageFromResponse(endpoint GrokMediaEndpoint, requestInfo GrokMediaRequestInfo, responseBody []byte) grokMediaUsageMetadata {
	usage, _ := extractOpenAIUsageFromJSONBytes(responseBody)
	meta := grokMediaUsageMetadata{Usage: usage}
	switch endpoint {
	case GrokMediaEndpointImagesGenerations, GrokMediaEndpointImagesEdits:
		imageCount := countOpenAIResponseImageOutputsFromJSONBytes(responseBody)
		if imageCount <= 0 {
			imageCount = requestInfo.N
		}
		if imageCount <= 0 {
			imageCount = 1
		}
		meta.ImageCount = imageCount
		meta.ImageSize = requestInfo.SizeTier
		meta.ImageInputSize = requestInfo.Size
		meta.ImageOutputSizes = collectOpenAIResponseImageOutputSizesFromJSONBytes(responseBody)
	case GrokMediaEndpointVideosGenerations:
		meta.ResponseID = extractGrokMediaVideoRequestID(responseBody)
		meta.ImageCount = 1
		meta.ImageSize = requestInfo.SizeTier
		meta.ImageInputSize = requestInfo.Size
	}
	return meta
}

func extractGrokMediaVideoRequestID(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	for _, path := range []string{"request_id", "id", "data.request_id", "data.id", "video.request_id", "video.id"} {
		if id := strings.TrimSpace(gjson.GetBytes(body, path).String()); id != "" {
			return id
		}
	}
	return ""
}

func (s *OpenAIGatewayService) handleGrokMediaErrorResponse(ctx context.Context, resp *http.Response, c *gin.Context, account *Account, requestIDHeader string, requestedModel string) (*OpenAIForwardResult, error) {
	body := s.readUpstreamErrorBody(resp)
	upstreamMsg := sanitizeUpstreamErrorMessage(strings.TrimSpace(extractUpstreamErrorMessage(body)))
	if upstreamMsg == "" {
		upstreamMsg = fmt.Sprintf("xAI upstream returned status %d", resp.StatusCode)
	}
	setOpsUpstreamError(c, resp.StatusCode, upstreamMsg, "")
	if s.shouldFailoverUpstreamError(resp.StatusCode) {
		s.handleFailoverSideEffects(ctx, resp, account, body, requestedModel)
		return nil, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: body, RetryableOnSameAccount: account.IsPoolMode() && account.IsPoolModeRetryableStatus(resp.StatusCode)}
	}
	MarkResponseCommitted(c)
	writeGrokMediaErrorResponse(c, resp.StatusCode, grokMediaErrorType(resp.StatusCode), upstreamMsg)
	return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
}

func grokMediaErrorType(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		return "upstream_error"
	}
}

func writeGrokMediaErrorResponse(c *gin.Context, statusCode int, errType, message string) {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	c.JSON(statusCode, gin.H{"error": gin.H{"type": strings.TrimSpace(errType), "message": strings.TrimSpace(message)}})
}

func writeGrokMediaResponse(c *gin.Context, resp *http.Response, body []byte, filter *responseheaders.CompiledHeaderFilter) {
	if c == nil || resp == nil {
		return
	}
	writeOpenAIPassthroughResponseHeaders(c.Writer.Header(), resp.Header, filter)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, body)
}
