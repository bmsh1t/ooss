package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/core"
)

func shouldUseResponsesAPI(provider *config.LLMProvider, llmConfig *MergedLLMConfig) bool {
	if providerUsesResponsesEndpoint(provider.BaseURL) {
		return true
	}
	if llmConfig == nil || llmConfig.ExtraParams == nil {
		return false
	}
	if mode, ok := llmConfig.ExtraParams["api_mode"].(string); ok && strings.EqualFold(strings.TrimSpace(mode), "responses") {
		return true
	}
	if enabled, ok := llmConfig.ExtraParams["use_responses_api"].(bool); ok && enabled {
		return true
	}
	if rawTools, ok := llmConfig.ExtraParams["tools"].([]interface{}); ok {
		for _, rawTool := range rawTools {
			obj, ok := rawTool.(map[string]interface{})
			if !ok {
				continue
			}
			toolType, _ := obj["type"].(string)
			if toolType != "" && toolType != "function" {
				return true
			}
		}
	}
	return false
}

func providerUsesResponsesEndpoint(baseURL string) bool {
	return strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/responses")
}

func determineResponsesURL(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/responses") {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return strings.TrimSuffix(trimmed, "/chat/completions") + "/responses"
	}
	return trimmed + "/responses"
}

func applyExecutorLLMHeaders(req *http.Request, provider *config.LLMProvider, llmConfig *MergedLLMConfig) {
	req.Header.Set("Content-Type", "application/json")
	if provider.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+provider.AuthToken)
	}
	for key, value := range llmConfig.CustomHeaders {
		req.Header.Set(key, value)
	}
}

func (e *LLMExecutor) sendResponsesRequest(
	ctx context.Context,
	provider *config.LLMProvider,
	request *ChatCompletionRequest,
	llmConfig *MergedLLMConfig,
) (*ChatCompletionResponse, error) {
	bodyMap := buildResponsesRequestBody(request, llmConfig)
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal responses request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, determineResponsesURL(provider.BaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create responses request: %w", err)
	}
	applyExecutorLLMHeaders(req, provider, llmConfig)

	client := &http.Client{Timeout: executorTimeout(llmConfig.Timeout)}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("responses request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read responses body: %w", err)
	}

	chatResp, apiErr, parseErr := parseResponsesBodyToChatResponse(respBody)
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse responses body: %w", parseErr)
	}

	if resp.StatusCode >= 400 {
		if apiErr != nil {
			return chatResp, fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiErr.Message)
		}
		return chatResp, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return chatResp, nil
}

func (e *LLMExecutor) sendResponsesRequestStreaming(
	ctx context.Context,
	provider *config.LLMProvider,
	request *ChatCompletionRequest,
	llmConfig *MergedLLMConfig,
	onToken func(token string),
) (*ChatCompletionResponse, error) {
	bodyMap := buildResponsesRequestBody(request, llmConfig)
	bodyMap["stream"] = true
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal responses streaming request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, determineResponsesURL(provider.BaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create responses streaming request: %w", err)
	}
	applyExecutorLLMHeaders(req, provider, llmConfig)
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: executorTimeout(llmConfig.Timeout)}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("responses stream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		chatResp, apiErr, _ := parseResponsesBodyToChatResponse(respBody)
		if apiErr != nil {
			return chatResp, fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiErr.Message)
		}
		return chatResp, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var finalResponse *ChatCompletionResponse
	var streamErr *ChatError
	var contentBuilder strings.Builder
	streamPayload := make(map[string]interface{})
	var streamOutputItems []interface{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "" || data == "[DONE]" {
			continue
		}

		var envelope map[string]interface{}
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			continue
		}

		eventType, _ := envelope["type"].(string)
		switch eventType {
		case "response.created", "response.in_progress":
			if rawResp, ok := envelope["response"].(map[string]interface{}); ok {
				mergeResponsesStreamEnvelope(streamPayload, rawResp)
			}
		case "response.output_text.delta":
			if delta, _ := envelope["delta"].(string); delta != "" {
				contentBuilder.WriteString(delta)
				if onToken != nil {
					onToken(delta)
				}
			}
		case "response.output_item.done":
			item, ok := envelope["item"].(map[string]interface{})
			if !ok {
				continue
			}
			index := asIntMap(envelope, "output_index")
			streamOutputItems = setResponsesStreamOutputItem(streamOutputItems, index, item)
		case "response.error":
			if rawErr, ok := envelope["error"].(map[string]interface{}); ok {
				streamErr = parseChatError(rawErr)
			}
		case "response.completed":
			rawResp, ok := envelope["response"].(map[string]interface{})
			if !ok {
				continue
			}
			finalResponse = convertResponsesMapToChatResponse(rawResp)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading responses stream: %w", err)
	}
	if streamErr != nil {
		return &ChatCompletionResponse{Error: streamErr}, fmt.Errorf("streaming error: %s", streamErr.Message)
	}
	if onToken != nil && contentBuilder.Len() > 0 {
		onToken("\n")
	}
	if finalResponse != nil {
		return finalResponse, nil
	}
	if len(streamOutputItems) > 0 {
		if len(streamPayload) == 0 {
			streamPayload = make(map[string]interface{})
		}
		if streamPayload["object"] == nil {
			streamPayload["object"] = "response"
		}
		streamPayload["output"] = streamOutputItems
		if contentBuilder.Len() > 0 {
			streamPayload["output_text"] = contentBuilder.String()
		}
		return convertResponsesMapToChatResponse(streamPayload), nil
	}

	// Fallback when upstream never sent response.completed.
	return &ChatCompletionResponse{
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: contentBuilder.String(),
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

func mergeResponsesStreamEnvelope(target map[string]interface{}, source map[string]interface{}) {
	if target == nil || source == nil {
		return
	}
	for _, key := range []string{"id", "object", "created_at", "model"} {
		if value, ok := source[key]; ok && value != nil {
			target[key] = value
		}
	}
	if usage, ok := source["usage"].(map[string]interface{}); ok && usage != nil {
		target["usage"] = usage
	}
}

func setResponsesStreamOutputItem(items []interface{}, index int, item map[string]interface{}) []interface{} {
	if index < 0 {
		index = len(items)
	}
	if index >= len(items) {
		expanded := make([]interface{}, index+1)
		copy(expanded, items)
		items = expanded
	}
	items[index] = item
	return items
}

func buildResponsesRequestBody(request *ChatCompletionRequest, llmConfig *MergedLLMConfig) map[string]interface{} {
	body := map[string]interface{}{
		"model": request.Model,
		"input": convertChatMessagesToResponsesInput(request.Messages),
	}
	if request.MaxTokens > 0 {
		body["max_output_tokens"] = request.MaxTokens
	}
	if request.Temperature != 0 {
		body["temperature"] = request.Temperature
	}
	if request.TopP > 0 {
		body["top_p"] = request.TopP
	}
	if request.Stream {
		body["stream"] = true
	}
	if request.ToolChoice != nil {
		body["tool_choice"] = request.ToolChoice
	}
	if len(request.Tools) > 0 {
		body["tools"] = convertCoreToolsToResponsesTools(request.Tools)
	}
	if request.ResponseFormat != nil {
		body["text"] = map[string]interface{}{
			"format": request.ResponseFormat,
		}
	}
	var extraResponseFormat interface{}
	var hasExtraResponseFormat bool
	for key, value := range llmConfig.ExtraParams {
		switch key {
		case "api_mode", "use_responses_api":
			continue
		case "response_format":
			extraResponseFormat = value
			hasExtraResponseFormat = true
			continue
		default:
			body[key] = value
		}
	}
	if hasExtraResponseFormat {
		if _, hasExplicitText := llmConfig.ExtraParams["text"]; !hasExplicitText {
			body["text"] = map[string]interface{}{
				"format": extraResponseFormat,
			}
		}
	}
	return body
}

func convertChatMessagesToResponsesInput(messages []ChatMessage) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		item := map[string]interface{}{
			"type": "message",
			"role": normalizeExecutorResponsesRole(msg.Role),
		}
		item["content"] = convertChatMessageContentToResponses(msg.Content)
		items = append(items, item)
	}
	return items
}

func normalizeExecutorResponsesRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	switch role {
	case "", "user":
		return "user"
	case "developer":
		return "system"
	default:
		return role
	}
}

func convertChatMessageContentToResponses(content interface{}) interface{} {
	switch value := content.(type) {
	case string:
		return value
	case []core.LLMContentPart:
		parts := make([]map[string]interface{}, 0, len(value))
		for _, part := range value {
			switch part.Type {
			case core.LLMContentTypeImageURL:
				entry := map[string]interface{}{
					"type": "input_image",
				}
				if part.ImageURL != nil {
					entry["image_url"] = part.ImageURL.URL
					if part.ImageURL.Detail != "" {
						entry["detail"] = part.ImageURL.Detail
					}
				}
				parts = append(parts, entry)
			default:
				parts = append(parts, map[string]interface{}{
					"type": "input_text",
					"text": part.Text,
				})
			}
		}
		return parts
	default:
		return content
	}
}

func convertCoreToolsToResponsesTools(tools []core.LLMTool) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		entry := map[string]interface{}{
			"type": tool.Type,
		}
		if tool.Type == "function" {
			entry["name"] = tool.Function.Name
			if tool.Function.Description != "" {
				entry["description"] = tool.Function.Description
			}
			if tool.Function.Parameters != nil {
				entry["parameters"] = tool.Function.Parameters
			}
		}
		result = append(result, entry)
	}
	return result
}

func parseResponsesBodyToChatResponse(body []byte) (*ChatCompletionResponse, *ChatError, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, err
	}
	resp := convertResponsesMapToChatResponse(payload)
	var apiErr *ChatError
	if rawErr, ok := payload["error"].(map[string]interface{}); ok && rawErr != nil {
		apiErr = parseChatError(rawErr)
		resp.Error = apiErr
	}
	return resp, apiErr, nil
}

func convertResponsesMapToChatResponse(payload map[string]interface{}) *ChatCompletionResponse {
	text := extractResponsesText(payload)
	toolCalls := extractResponsesFunctionCalls(payload)
	finishReason := determineResponsesFinishReason(payload, toolCalls)

	response := &ChatCompletionResponse{
		ID:      asStringMap(payload, "id"),
		Object:  asStringMap(payload, "object"),
		Created: asInt64Map(payload, "created_at"),
		Model:   asStringMap(payload, "model"),
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:      "assistant",
					Content:   text,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
	}

	if rawUsage, ok := payload["usage"].(map[string]interface{}); ok {
		response.Usage = ChatUsage{
			PromptTokens:     asIntMap(rawUsage, "input_tokens"),
			CompletionTokens: asIntMap(rawUsage, "output_tokens"),
			TotalTokens:      asIntMap(rawUsage, "total_tokens"),
		}
	}
	return response
}

func determineResponsesFinishReason(payload map[string]interface{}, toolCalls []core.LLMToolCall) string {
	if len(toolCalls) > 0 {
		return "tool_calls"
	}
	if asStringMap(payload, "status") != "incomplete" {
		return "stop"
	}
	incompleteDetails, ok := payload["incomplete_details"].(map[string]interface{})
	if !ok {
		return "stop"
	}
	switch asStringMap(incompleteDetails, "reason") {
	case "max_output_tokens":
		return "length"
	case "content_filter":
		return "content_filter"
	default:
		return "stop"
	}
}

func extractResponsesText(payload map[string]interface{}) string {
	if text, ok := payload["output_text"].(string); ok && text != "" {
		return text
	}
	rawOutput, ok := payload["output"].([]interface{})
	if !ok {
		return ""
	}
	if parts := extractResponsesPrimaryTextParts(rawOutput); len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return strings.Join(extractResponsesReasoningTextParts(rawOutput), "\n")
}

func extractResponsesPrimaryTextParts(output []interface{}) []string {
	parts := make([]string, 0)
	for _, item := range output {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		switch asStringMap(obj, "type") {
		case "message":
			parts = append(parts, extractResponsesMessageContentTextParts(obj["content"])...)
		case "refusal":
			parts = append(parts, extractResponsesRefusalText(obj)...)
		}
	}
	return parts
}

func extractResponsesReasoningTextParts(output []interface{}) []string {
	parts := make([]string, 0)
	for _, item := range output {
		obj, ok := item.(map[string]interface{})
		if !ok || asStringMap(obj, "type") != "reasoning" {
			continue
		}
		parts = append(parts, extractResponsesSummaryText(obj["summary"])...)
		if len(parts) == 0 {
			parts = append(parts, extractResponsesMessageContentTextParts(obj["content"])...)
		}
	}
	return parts
}

func extractResponsesMessageContentTextParts(raw interface{}) []string {
	contentItems, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	parts := make([]string, 0)
	for _, content := range contentItems {
		contentObj, ok := content.(map[string]interface{})
		if !ok {
			continue
		}
		switch asStringMap(contentObj, "type") {
		case "output_text", "text":
			if text, ok := contentObj["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		case "refusal":
			parts = append(parts, extractResponsesRefusalText(contentObj)...)
		case "summary_text", "reasoning_summary_text":
			if text, ok := contentObj["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
	}
	return parts
}

func extractResponsesSummaryText(raw interface{}) []string {
	summaryItems, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	parts := make([]string, 0)
	for _, item := range summaryItems {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		switch asStringMap(obj, "type") {
		case "summary_text", "reasoning_summary_text", "output_text", "text":
			if text, ok := obj["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
	}
	return parts
}

func extractResponsesRefusalText(obj map[string]interface{}) []string {
	parts := make([]string, 0, 1)
	if refusal, ok := obj["refusal"].(string); ok && refusal != "" {
		parts = append(parts, refusal)
	}
	if len(parts) == 0 {
		if text, ok := obj["text"].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	return parts
}

func extractResponsesFunctionCalls(payload map[string]interface{}) []core.LLMToolCall {
	rawOutput, ok := payload["output"].([]interface{})
	if !ok {
		return nil
	}
	toolCalls := make([]core.LLMToolCall, 0)
	for _, item := range rawOutput {
		obj, ok := item.(map[string]interface{})
		if !ok || asStringMap(obj, "type") != "function_call" {
			continue
		}
		toolCalls = append(toolCalls, core.LLMToolCall{
			ID:   asStringMap(obj, "call_id"),
			Type: "function",
			Function: core.LLMToolCallFunction{
				Name:      asStringMap(obj, "name"),
				Arguments: asStringMap(obj, "arguments"),
			},
		})
	}
	return toolCalls
}

func parseChatError(raw map[string]interface{}) *ChatError {
	return &ChatError{
		Message: asStringMap(raw, "message"),
		Type:    asStringMap(raw, "type"),
		Code:    asStringMap(raw, "code"),
	}
}

func executorTimeout(raw string) time.Duration {
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return 120 * time.Second
	}
	return timeout
}

func asStringMap(m map[string]interface{}, key string) string {
	value, _ := m[key].(string)
	return value
}

func asIntMap(m map[string]interface{}, key string) int {
	switch value := m[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func asInt64Map(m map[string]interface{}, key string) int64 {
	switch value := m[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}
