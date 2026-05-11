package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type codexGatewayOpenAIResponsesAdapter struct {
	gateway *OpenAIGatewayService
}

func (a *codexGatewayOpenAIResponsesAdapter) Complete(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayDeepSeekAdapterResult, error) {
	if a == nil || a.gateway == nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("codex gateway openai adapter is not configured")
	}
	resp, err := a.gateway.DoNativeResponsesRequest(ctx, account, req.Request.Headers, req.Request.Body, false)
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, err
	}
	serviceResp := CodexGatewayServiceResponse{
		StatusCode: resp.StatusCode,
		Headers:    a.gateway.FilterNativeResponsesResponseHeaders(resp.Header),
		Body:       append([]byte(nil), body...),
	}
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(extractUpstreamErrorMessage(body))
		if a.gateway.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, msg, body) {
			return CodexGatewayDeepSeekAdapterResult{}, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: append([]byte(nil), body...)}
		}
		return CodexGatewayDeepSeekAdapterResult{ServiceResponse: serviceResp}, nil
	}
	if isEventStreamResponse(resp.Header) || (account != nil && account.Type == AccountTypeOAuth && codexGatewayLooksLikeSSEBody(body)) {
		convertedResp, err := codexGatewayOpenAIStreamJSONResponse(body)
		if err != nil {
			return CodexGatewayDeepSeekAdapterResult{}, err
		}
		serviceResp.Body = convertedResp
		serviceResp.Headers.Set("Content-Type", "application/json; charset=utf-8")
		body = convertedResp
	}

	var parsed CodexGatewayResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return CodexGatewayDeepSeekAdapterResult{}, fmt.Errorf("decode codex gateway native openai response: %w", err)
	}
	usage, _ := extractOpenAIUsageFromJSONBytes(body)
	return CodexGatewayDeepSeekAdapterResult{
		ServiceResponse: serviceResp,
		ProviderResult: CodexGatewayProviderResult{
			ResponseID:        strings.TrimSpace(parsed.ID),
			UpstreamRequestID: strings.TrimSpace(resp.Header.Get("x-request-id")),
			UpstreamModel:     firstNonBlankString(parsed.Model, strings.TrimSpace(req.Model.UpstreamModel)),
			Response:          parsed,
			Usage: CodexGatewayProviderUsage{
				InputTokens:          usage.InputTokens,
				OutputTokens:         usage.OutputTokens,
				TotalTokens:          usage.InputTokens + usage.OutputTokens,
				CacheReadInputTokens: usage.CacheReadInputTokens,
			},
		},
	}, nil
}

func (a *codexGatewayOpenAIResponsesAdapter) Stream(ctx context.Context, account *Account, req CodexGatewayProviderRequest) (CodexGatewayProviderResult, error) {
	if a == nil || a.gateway == nil {
		return CodexGatewayProviderResult{}, fmt.Errorf("codex gateway openai adapter is not configured")
	}
	if req.Request.StreamWriter == nil {
		return CodexGatewayProviderResult{}, fmt.Errorf("codex gateway openai stream requires writer")
	}
	resp, err := a.gateway.DoNativeResponsesRequest(ctx, account, req.Request.Headers, req.Request.Body, true)
	if err != nil {
		return CodexGatewayProviderResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		if readErr != nil {
			return CodexGatewayProviderResult{}, readErr
		}
		msg := strings.TrimSpace(extractUpstreamErrorMessage(body))
		if a.gateway.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, msg, body) {
			return CodexGatewayProviderResult{}, &UpstreamFailoverError{StatusCode: resp.StatusCode, ResponseBody: append([]byte(nil), body...)}
		}
		mapped := codexGatewayDeepSeekMapErrorBody(resp.StatusCode, body)
		status := resp.StatusCode
		errType := gjson.GetBytes(mapped, "error.type").String()
		errCode := gjson.GetBytes(mapped, "error.code").String()
		message := gjson.GetBytes(mapped, "error.message").String()
		_ = status
		if err := writeCodexGatewayStreamFailure(req.Request.StreamWriter, "", errType, errCode, message); err != nil {
			return CodexGatewayProviderResult{}, err
		}
		if req.Request.Flush != nil {
			req.Request.Flush()
		}
		return CodexGatewayProviderResult{}, &codexGatewayStreamingHandledError{}
	}

	if req.Request.ResponseHeader != nil {
		copyCodexGatewayHTTPHeaders(req.Request.ResponseHeader, a.gateway.FilterNativeResponsesResponseHeaders(resp.Header))
		if req.Request.ResponseHeader.Get("Content-Type") == "" {
			req.Request.ResponseHeader.Set("Content-Type", "text/event-stream")
		}
	}
	if req.Request.WriteStatus != nil {
		req.Request.WriteStatus(resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)

	var result CodexGatewayProviderResult
	result.UpstreamRequestID = strings.TrimSpace(resp.Header.Get("x-request-id"))
	result.UpstreamModel = strings.TrimSpace(req.Model.UpstreamModel)
	var currentEvent []string
	var preOutput bytes.Buffer
	var usage OpenAIUsage
	clientOutputStarted := false
	sawTerminal := false
	var finalResponse CodexGatewayResponse
	var finalResponseSet bool

	flushEvent := func() error {
		if len(currentEvent) == 0 {
			return nil
		}
		raw := strings.Join(currentEvent, "\n") + "\n\n"
		dataLines := make([]string, 0, 2)
		eventType := ""
		for _, line := range currentEvent {
			if data, ok := extractOpenAISSEDataLine(line); ok {
				dataLines = append(dataLines, data)
			}
		}
		if len(dataLines) > 0 {
			payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
			if payload != "" && payload != "[DONE]" {
				payloadBytes := []byte(payload)
				a.gateway.parseSSEUsageBytes(payloadBytes, &usage)
				eventType = strings.TrimSpace(gjson.GetBytes(payloadBytes, "type").String())
				if openAIStreamEventIsTerminal(payload) {
					sawTerminal = true
					if responseRaw := gjson.GetBytes(payloadBytes, "response"); responseRaw.Exists() && responseRaw.Raw != "" {
						_ = json.Unmarshal([]byte(responseRaw.Raw), &finalResponse)
						finalResponseSet = true
					}
				}
				if eventType == "response.failed" && !clientOutputStarted {
					msg := extractOpenAISSEErrorMessage(payloadBytes)
					if openAIStreamFailedEventShouldFailover(payloadBytes, msg) {
						return &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: payloadBytes}
					}
				}
				if !clientOutputStarted && openAIStreamDataStartsClientOutput(payload, eventType) {
					if preOutput.Len() > 0 {
						if _, err := io.Copy(req.Request.StreamWriter, &preOutput); err != nil {
							return err
						}
					}
					clientOutputStarted = true
				}
			}
		}
		if clientOutputStarted {
			if _, err := io.WriteString(req.Request.StreamWriter, raw); err != nil {
				return err
			}
			if req.Request.Flush != nil {
				req.Request.Flush()
			}
		} else {
			if _, err := preOutput.WriteString(raw); err != nil {
				return err
			}
		}
		currentEvent = currentEvent[:0]
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if err := flushEvent(); err != nil {
				return CodexGatewayProviderResult{}, err
			}
			continue
		}
		currentEvent = append(currentEvent, line)
	}
	if err := scanner.Err(); err != nil {
		if !clientOutputStarted {
			return CodexGatewayProviderResult{}, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(err.Error())}
		}
		return CodexGatewayProviderResult{}, err
	}
	if err := flushEvent(); err != nil {
		return CodexGatewayProviderResult{}, err
	}
	if !sawTerminal {
		if !clientOutputStarted {
			return CodexGatewayProviderResult{}, &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte("missing terminal event")}
		}
		return CodexGatewayProviderResult{}, fmt.Errorf("codex gateway openai stream ended without terminal event")
	}
	if !clientOutputStarted && preOutput.Len() > 0 {
		if _, err := io.Copy(req.Request.StreamWriter, &preOutput); err != nil {
			return CodexGatewayProviderResult{}, err
		}
		if req.Request.Flush != nil {
			req.Request.Flush()
		}
	}

	result.Usage = CodexGatewayProviderUsage{
		InputTokens:          usage.InputTokens,
		OutputTokens:         usage.OutputTokens,
		TotalTokens:          usage.InputTokens + usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens,
	}
	if finalResponseSet {
		result.Response = finalResponse
		result.ResponseID = strings.TrimSpace(finalResponse.ID)
		if strings.TrimSpace(finalResponse.Model) != "" {
			result.UpstreamModel = strings.TrimSpace(finalResponse.Model)
		}
	}
	return result, nil
}

func codexGatewayOpenAIStreamJSONResponse(body []byte) ([]byte, error) {
	bodyText := string(body)
	var finalResponse []byte
	forEachOpenAISSEDataPayload(bodyText, func(data []byte) {
		if finalResponse != nil {
			return
		}
		if !openAIStreamEventIsTerminal(string(data)) {
			return
		}
		if responseRaw := gjson.GetBytes(data, "response"); responseRaw.Exists() && responseRaw.Raw != "" {
			finalResponse = []byte(responseRaw.Raw)
		}
	})
	if finalResponse != nil {
		if len(gjson.GetBytes(finalResponse, "output").Array()) == 0 {
			if outputJSON, reconstructed := reconstructResponseOutputFromSSE(bodyText); reconstructed {
				if patched, err := sjson.SetRawBytes(finalResponse, "output", outputJSON); err == nil {
					finalResponse = patched
				}
			}
		}
		return finalResponse, nil
	}
	return nil, fmt.Errorf("codex gateway openai sync stream response missing terminal payload")
}

func firstNonBlankString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func codexGatewayLooksLikeSSEBody(body []byte) bool {
	return bytes.Contains(body, []byte("data:")) || bytes.Contains(body, []byte("event:"))
}
