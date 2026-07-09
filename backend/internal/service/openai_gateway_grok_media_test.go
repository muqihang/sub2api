package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGrokMediaRequestParsingAndModeration(t *testing.T) {
	body := []byte(`{"model":" grok-imagine ","prompt":" make a cat ","n":2,"size":"1024x1024","image":[{"image_url":"https://example.test/a.png"}],"mask":{"image_url":"https://example.test/mask.png"}}`)

	info := ParseGrokMediaRequest("application/json", body)

	require.Equal(t, "grok-imagine-image-quality", info.Model)
	require.Equal(t, "make a cat", info.Prompt)
	require.Equal(t, 2, info.N)
	require.Equal(t, "1024x1024", info.Size)
	require.Equal(t, []string{"https://example.test/a.png"}, info.InputImageURLs)
	require.Equal(t, "https://example.test/mask.png", info.MaskImageURL)
	require.JSONEq(t, `{"images":[{"image_url":"https://example.test/a.png"},{"image_url":"https://example.test/mask.png"}],"prompt":"make a cat"}`, string(info.ModerationBody()))
}

func TestPrepareGrokMediaForwardBody_NormalizesImagineAliasAndConvertsEditMultipart(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "grok-imagine"))
	require.NoError(t, writer.WriteField("prompt", "edit it"))
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="image"; filename="input.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	out, contentType, err := prepareGrokMediaForwardBody(GrokMediaEndpointImagesEdits, body.Bytes(), writer.FormDataContentType())
	require.NoError(t, err)
	require.Equal(t, "application/json", contentType)
	require.True(t, json.Valid(out))
	require.Equal(t, "grok-imagine-image-quality", strings.TrimSpace(grokMediaTestGJSON(out, "model")))
	require.Equal(t, "edit it", strings.TrimSpace(grokMediaTestGJSON(out, "prompt")))
	imageURL := grokMediaTestGJSON(out, "image.image_url")
	require.True(t, strings.HasPrefix(imageURL, "data:image/"), "image upload should be converted to a data URL, got %q", imageURL)
}

func TestForwardGrokMedia_SendsImagesGenerationsToXAIAndRecordsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &grokMediaHTTPUpstreamStub{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"x-request-id": []string{"req-upstream"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"img_resp","data":[{"url":"https://example.test/out.png"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}`)),
		},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{
		ID:       42,
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "xai-key",
			"base_url": "https://api.x.ai/v1",
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"grok-imagine","prompt":"draw","n":1}`)

	result, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointImagesGenerations, "", body, "application/json")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/v1/images/generations", upstream.request.URL.Path)
	require.Equal(t, http.MethodPost, upstream.request.Method)
	require.Equal(t, "Bearer xai-key", upstream.request.Header.Get("Authorization"))
	require.Equal(t, "grok-imagine-image-quality", grokMediaTestGJSON(upstream.body, "model"))
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "grok-imagine-image-quality", result.UpstreamModel)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
}

func TestForwardGrokMedia_VideoStatusUsesGETWithoutBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &grokMediaHTTPUpstreamStub{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"status":"succeeded"}`))}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 43, Platform: PlatformGrok, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "xai-key", "base_url": "https://api.x.ai/v1"}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	_, err := svc.ForwardGrokMedia(context.Background(), c, account, GrokMediaEndpointVideoStatus, "video_123", nil, "")

	require.NoError(t, err)
	require.Equal(t, http.MethodGet, upstream.request.Method)
	require.Equal(t, "/v1/videos/video_123", upstream.request.URL.Path)
	require.Empty(t, string(upstream.body))
}

type grokMediaHTTPUpstreamStub struct {
	request *http.Request
	body    []byte
	resp    *http.Response
	err     error
}

func (s *grokMediaHTTPUpstreamStub) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	s.request = req
	if req.Body != nil {
		s.body, _ = io.ReadAll(req.Body)
	}
	return s.resp, s.err
}

func (s *grokMediaHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

func grokMediaTestGJSON(body []byte, path string) string {
	return gjson.GetBytes(body, path).String()
}
