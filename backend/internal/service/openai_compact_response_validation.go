package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// openAICompactResponseSemantics mirrors the Codex remote compaction v2
// contract: a successful response must contain exactly one compaction item.
func openAICompactResponseSemantics(body []byte) (valid bool, compactCount, outputCount int) {
	seen := make(map[string]struct{})
	appendItem := func(item gjson.Result) {
		if !item.IsObject() {
			return
		}
		key := strings.TrimSpace(item.Get("id").String())
		if key == "" {
			key = item.Raw
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		outputCount++
		if isResponsesCompactionItemType(item.Get("type").String()) {
			compactCount++
		}
	}
	appendOutput := func(output gjson.Result) {
		if !output.IsArray() {
			return
		}
		for _, item := range output.Array() {
			appendItem(item)
		}
	}

	if gjson.ValidBytes(body) && gjson.ParseBytes(body).IsObject() {
		root := gjson.ParseBytes(body)
		appendOutput(root.Get("output"))
		appendOutput(root.Get("response.output"))
	} else {
		forEachOpenAISSEDataPayload(string(body), func(data []byte) {
			root := gjson.ParseBytes(data)
			switch strings.TrimSpace(root.Get("type").String()) {
			case "response.output_item.added", "response.output_item.done":
				appendItem(root.Get("item"))
			case "response.completed", "response.done":
				appendOutput(root.Get("response.output"))
			}
		})
	}
	return compactCount == 1, compactCount, outputCount
}

func (s *OpenAIGatewayService) inspectOpenAICompactResponse(resp *http.Response, c *gin.Context) (bool, int, int, error) {
	if resp == nil || resp.Body == nil {
		return false, 0, 0, fmt.Errorf("compact response body is missing")
	}
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return false, 0, 0, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	valid, compactCount, outputCount := openAICompactResponseSemantics(body)
	return valid, compactCount, outputCount, nil
}

func newOpenAICompactSemanticMismatchFailover(compactCount, outputCount int) error {
	message := fmt.Sprintf(
		"remote compaction expected exactly one compaction output item, got %d from %d output items",
		compactCount,
		outputCount,
	)
	return &UpstreamFailoverError{
		StatusCode: http.StatusBadGateway,
		ResponseBody: []byte(fmt.Sprintf(
			`{"error":{"type":"upstream_error","code":"compact_semantic_mismatch","message":%q}}`,
			message,
		)),
		RetryableOnSameAccount: false,
	}
}
