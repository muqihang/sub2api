package service

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func DecodeCodexGatewayResponsesCreateRequest(body []byte) (CodexGatewayResponsesCreateRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return CodexGatewayResponsesCreateRequest{}, err
	}

	req := CodexGatewayResponsesCreateRequest{
		RawFields: make(map[string]json.RawMessage, len(raw)),
	}
	for key, value := range raw {
		req.RawFields[key] = append(json.RawMessage(nil), value...)
	}
	if v, ok := raw["model"]; ok {
		if err := json.Unmarshal(v, &req.Model); err != nil {
			return CodexGatewayResponsesCreateRequest{}, err
		}
	}
	req.Instructions = cloneCodexGatewayRawJSON(raw["instructions"])
	req.Input = cloneCodexGatewayRawJSON(raw["input"])
	req.Tools = cloneCodexGatewayRawJSON(raw["tools"])
	req.ToolChoice = cloneCodexGatewayRawJSON(raw["tool_choice"])
	req.Reasoning = cloneCodexGatewayRawJSON(raw["reasoning"])
	req.Text = cloneCodexGatewayRawJSON(raw["text"])
	req.Include = cloneCodexGatewayRawJSON(raw["include"])
	req.ClientMetadata = cloneCodexGatewayRawJSON(raw["client_metadata"])
	if v, ok := raw["prompt_cache_key"]; ok {
		if err := json.Unmarshal(v, &req.PromptCacheKey); err != nil {
			return CodexGatewayResponsesCreateRequest{}, err
		}
	}
	if v, ok := raw["parallel_tool_calls"]; ok {
		if err := json.Unmarshal(v, &req.ParallelToolCalls); err != nil {
			return CodexGatewayResponsesCreateRequest{}, err
		}
	}
	if v, ok := raw["store"]; ok {
		if err := json.Unmarshal(v, &req.Store); err != nil {
			return CodexGatewayResponsesCreateRequest{}, err
		}
	}
	if v, ok := raw["max_output_tokens"]; ok {
		if err := json.Unmarshal(v, &req.MaxOutputTokens); err != nil {
			return CodexGatewayResponsesCreateRequest{}, err
		}
	}
	if v, ok := raw["previous_response_id"]; ok {
		if err := json.Unmarshal(v, &req.PreviousResponseID); err != nil {
			return CodexGatewayResponsesCreateRequest{}, err
		}
	}
	if v, ok := raw["stream"]; ok {
		if err := json.Unmarshal(v, &req.Stream); err != nil {
			return CodexGatewayResponsesCreateRequest{}, err
		}
	}
	return req, nil
}

func ValidateCodexGatewayResponsesCreateRequest(req CodexGatewayResponsesCreateRequest) error {
	if strings.TrimSpace(req.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "" {
		return fmt.Errorf("previous_response_id is not supported on the HTTP gateway path")
	}
	return nil
}

type CodexGatewayResponseEventWriter struct {
	w io.Writer
}

func NewCodexGatewayResponseEventWriter(w io.Writer) *CodexGatewayResponseEventWriter {
	return &CodexGatewayResponseEventWriter{w: w}
}

func (r CodexGatewayResponse) MarshalJSON() ([]byte, error) {
	payload := make(map[string]json.RawMessage, len(r.RawFields)+8)
	for key, value := range r.RawFields {
		payload[key] = cloneCodexGatewayRawJSON(value)
	}

	set := func(key string, value any, include bool) error {
		if !include {
			return nil
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		payload[key] = raw
		return nil
	}

	if err := set("id", r.ID, r.ID != ""); err != nil {
		return nil, err
	}
	if err := set("object", r.Object, r.Object != ""); err != nil {
		return nil, err
	}
	if err := set("model", r.Model, r.Model != ""); err != nil {
		return nil, err
	}
	if err := set("status", r.Status, r.Status != ""); err != nil {
		return nil, err
	}
	if err := set("output", r.Output, len(r.Output) > 0); err != nil {
		return nil, err
	}
	if err := set("usage", json.RawMessage(r.Usage), len(r.Usage) > 0); err != nil {
		return nil, err
	}
	if err := set("error", r.Error, r.Error != nil); err != nil {
		return nil, err
	}
	if err := set("incomplete_details", json.RawMessage(r.IncompleteDetails), len(r.IncompleteDetails) > 0); err != nil {
		return nil, err
	}

	return json.Marshal(payload)
}

func (e CodexGatewayResponseError) MarshalJSON() ([]byte, error) {
	payload := make(map[string]json.RawMessage, len(e.RawFields)+2)
	for key, value := range e.RawFields {
		payload[key] = cloneCodexGatewayRawJSON(value)
	}

	set := func(key string, value any, include bool) error {
		if !include {
			return nil
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		payload[key] = raw
		return nil
	}

	if err := set("code", e.Code, e.Code != ""); err != nil {
		return nil, err
	}
	if err := set("message", e.Message, e.Message != ""); err != nil {
		return nil, err
	}

	return json.Marshal(payload)
}

func (w *CodexGatewayResponseEventWriter) WriteResponseCreated(response CodexGatewayResponse) error {
	return w.write("response.created", map[string]any{
		"type":     "response.created",
		"response": response,
	})
}

func (w *CodexGatewayResponseEventWriter) WriteOutputItemAdded(responseID string, outputIndex int, item json.RawMessage) error {
	return w.write("response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"response_id":  responseID,
		"output_index": outputIndex,
		"item":         json.RawMessage(item),
	})
}

func (w *CodexGatewayResponseEventWriter) WriteOutputTextDelta(responseID, itemID string, outputIndex, contentIndex int, delta string) error {
	return w.write("response.output_text.delta", map[string]any{
		"type":          "response.output_text.delta",
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"delta":         delta,
	})
}

func (w *CodexGatewayResponseEventWriter) WriteFunctionCallArgumentsDelta(responseID, callID, name, delta string) error {
	return w.write("response.function_call_arguments.delta", map[string]any{
		"type":        "response.function_call_arguments.delta",
		"response_id": responseID,
		"call_id":     callID,
		"name":        name,
		"delta":       delta,
	})
}

func (w *CodexGatewayResponseEventWriter) WriteFunctionCallArgumentsDone(responseID, callID, name, arguments string) error {
	return w.write("response.function_call_arguments.done", map[string]any{
		"type":        "response.function_call_arguments.done",
		"response_id": responseID,
		"call_id":     callID,
		"name":        name,
		"arguments":   arguments,
	})
}

func (w *CodexGatewayResponseEventWriter) WriteOutputItemDone(responseID string, outputIndex int, item json.RawMessage) error {
	return w.write("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"response_id":  responseID,
		"output_index": outputIndex,
		"item":         json.RawMessage(item),
	})
}

func (w *CodexGatewayResponseEventWriter) WriteResponseCompleted(response CodexGatewayResponse) error {
	return w.write("response.completed", map[string]any{
		"type":     "response.completed",
		"response": response,
	})
}

func (w *CodexGatewayResponseEventWriter) WriteResponseFailed(response CodexGatewayResponse) error {
	return w.write("response.failed", map[string]any{
		"type":     "response.failed",
		"response": response,
	})
}

func (w *CodexGatewayResponseEventWriter) WriteResponseIncomplete(response CodexGatewayResponse) error {
	return w.write("response.incomplete", map[string]any{
		"type":     "response.incomplete",
		"response": response,
	})
}

func (w *CodexGatewayResponseEventWriter) write(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w.w, "event: %s\ndata: %s\n\n", name, data)
	return err
}

func cloneCodexGatewayRawJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}
