package functions

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/core"
	"github.com/j3ssie/osmedeus/v5/internal/retry"
)

func useResponsesAPIForFunc(llmCfg *llmFuncConfig, body map[string]interface{}) bool {
	if strings.EqualFold(strings.TrimSpace(llmCfg.APIMode), "responses") {
		return true
	}
	if strings.HasSuffix(strings.TrimRight(llmCfg.BaseURL, "/"), "/responses") {
		return true
	}
	_, hasInput := body["input"]
	_, hasInstructions := body["instructions"]
	if hasInput || hasInstructions {
		return true
	}
	for _, key := range []string{
		"text",
		"max_output_tokens",
		"previous_response_id",
		"reasoning",
		"store",
		"max_tool_calls",
		"parallel_tool_calls",
		"truncation",
		"conversation",
		"background",
	} {
		if _, ok := body[key]; ok {
			return true
		}
	}
	if rawTools, ok := body["tools"].([]interface{}); ok {
		for _, rawTool := range rawTools {
			obj, ok := rawTool.(map[string]interface{})
			if !ok {
				continue
			}
			toolType := strings.TrimSpace(asStringValue(obj["type"]))
			if toolType != "" && toolType != "function" {
				return true
			}
		}
	}
	return false
}

func determineFuncRequestURL(baseURL string, useResponses bool) string {
	trimmed := strings.TrimRight(baseURL, "/")
	if !useResponses {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/responses") {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return strings.TrimSuffix(trimmed, "/chat/completions") + "/responses"
	}
	return trimmed + "/responses"
}

func prepareFuncRequestBody(llmCfg *llmFuncConfig, body map[string]interface{}) (map[string]interface{}, bool, error) {
	useResponses := useResponsesAPIForFunc(llmCfg, body)
	if !useResponses {
		return body, false, nil
	}
	if _, ok := body["input"]; ok {
		converted := normalizeFuncResponsesBody(body)
		if _, exists := converted["model"]; !exists && llmCfg.Model != "" {
			converted["model"] = llmCfg.Model
		}
		return converted, true, nil
	}
	if messages, ok := body["messages"].([]llmMessage); ok {
		converted := normalizeFuncResponsesBody(body)
		delete(converted, "messages")
		converted["input"] = convertFuncMessagesToResponsesInput(messages)
		if _, exists := converted["model"]; !exists && llmCfg.Model != "" {
			converted["model"] = llmCfg.Model
		}
		return converted, true, nil
	}
	if rawMessages, ok := body["messages"].([]interface{}); ok {
		input, err := convertGenericMessagesToResponsesInput(rawMessages)
		if err != nil {
			return nil, false, err
		}
		converted := normalizeFuncResponsesBody(body)
		delete(converted, "messages")
		converted["input"] = input
		if _, exists := converted["model"]; !exists && llmCfg.Model != "" {
			converted["model"] = llmCfg.Model
		}
		return converted, true, nil
	}
	converted := normalizeFuncResponsesBody(body)
	if _, exists := converted["model"]; !exists && llmCfg.Model != "" {
		converted["model"] = llmCfg.Model
	}
	return converted, true, nil
}

func convertFuncMessagesToResponsesInput(messages []llmMessage) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		items = append(items, map[string]interface{}{
			"type":    "message",
			"role":    normalizeFuncRole(msg.Role),
			"content": msg.Content,
		})
	}
	return items
}

func convertGenericMessagesToResponsesInput(messages []interface{}) ([]map[string]interface{}, error) {
	items := make([]map[string]interface{}, 0, len(messages))
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("messages must be objects")
		}
		item := map[string]interface{}{
			"type": "message",
			"role": normalizeFuncRole(asStringValue(msg["role"])),
		}
		item["content"] = msg["content"]
		items = append(items, item)
	}
	return items, nil
}

func normalizeFuncRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return "user"
	}
	if role == "developer" {
		return "system"
	}
	return role
}

func normalizeFuncResponsesBody(body map[string]interface{}) map[string]interface{} {
	converted := cloneMap(body)
	if responseFormat, ok := converted["response_format"]; ok {
		if _, hasText := converted["text"]; !hasText {
			converted["text"] = map[string]interface{}{"format": responseFormat}
		}
		delete(converted, "response_format")
	}
	return converted
}

func sendLLMRequest(llmCfg *llmFuncConfig, bodyBytes []byte) (string, error) {
	var bodyMap map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &bodyMap); err != nil {
		return "", fmt.Errorf("failed to parse request body: %w", err)
	}

	preparedBody, useResponses, err := prepareFuncRequestBody(llmCfg, bodyMap)
	if err != nil {
		return "", err
	}
	finalBody, err := json.Marshal(preparedBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", determineFuncRequestURL(llmCfg.BaseURL, useResponses), bytes.NewReader(finalBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", core.DefaultUA)
	if llmCfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+llmCfg.AuthToken)
	}

	client := &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	var resp *http.Response
	ctx := context.Background()
	retryCfg := retry.Config{
		MaxAttempts:  3,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     5 * time.Second,
		Multiplier:   2.0,
	}

	err = retry.Do(ctx, retryCfg, func() error {
		var reqErr error
		req.Body = io.NopCloser(bytes.NewReader(finalBody))
		resp, reqErr = client.Do(req)
		if reqErr != nil {
			return retry.Retryable(reqErr)
		}
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			_ = resp.Body.Close()
			return retry.Retryable(fmt.Errorf("server error: %d", resp.StatusCode))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("request failed after retries: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	if useResponses {
		return parseResponsesText(respBody)
	}
	return parseChatResponseText(respBody)
}

func parseChatResponseText(respBody []byte) (string, error) {
	var llmResp llmChatResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if llmResp.Error != nil {
		return "", fmt.Errorf("API error: %s", llmResp.Error.Message)
	}
	if len(llmResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return llmResp.Choices[0].Message.Content, nil
}

func parseResponsesText(respBody []byte) (string, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", fmt.Errorf("failed to parse responses body: %w", err)
	}
	if rawErr, ok := payload["error"].(map[string]interface{}); ok && rawErr != nil {
		return "", fmt.Errorf("API error: %s", asStringValue(rawErr["message"]))
	}
	if text, ok := payload["output_text"].(string); ok && text != "" {
		return text, nil
	}
	rawOutput, ok := payload["output"].([]interface{})
	if !ok {
		return "", fmt.Errorf("no output in responses payload")
	}
	parts := extractFuncResponsesPrimaryTextParts(rawOutput)
	if len(parts) == 0 {
		parts = extractFuncResponsesReasoningTextParts(rawOutput)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no message text in responses payload")
	}
	return strings.Join(parts, "\n"), nil
}

func extractFuncResponsesPrimaryTextParts(output []interface{}) []string {
	parts := make([]string, 0)
	for _, item := range output {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		switch asStringValue(obj["type"]) {
		case "message":
			parts = append(parts, extractFuncResponsesMessageContentTextParts(obj["content"])...)
		case "refusal":
			parts = append(parts, extractFuncResponsesRefusalText(obj)...)
		}
	}
	return parts
}

func extractFuncResponsesReasoningTextParts(output []interface{}) []string {
	parts := make([]string, 0)
	for _, item := range output {
		obj, ok := item.(map[string]interface{})
		if !ok || asStringValue(obj["type"]) != "reasoning" {
			continue
		}
		parts = append(parts, extractFuncResponsesSummaryText(obj["summary"])...)
		if len(parts) == 0 {
			parts = append(parts, extractFuncResponsesMessageContentTextParts(obj["content"])...)
		}
	}
	return parts
}

func extractFuncResponsesMessageContentTextParts(raw interface{}) []string {
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
		switch asStringValue(contentObj["type"]) {
		case "output_text", "text", "summary_text", "reasoning_summary_text":
			if text := asStringValue(contentObj["text"]); text != "" {
				parts = append(parts, text)
			}
		case "refusal":
			parts = append(parts, extractFuncResponsesRefusalText(contentObj)...)
		}
	}
	return parts
}

func extractFuncResponsesSummaryText(raw interface{}) []string {
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
		switch asStringValue(obj["type"]) {
		case "summary_text", "reasoning_summary_text", "output_text", "text":
			if text := asStringValue(obj["text"]); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return parts
}

func extractFuncResponsesRefusalText(obj map[string]interface{}) []string {
	parts := make([]string, 0, 1)
	if refusal := asStringValue(obj["refusal"]); refusal != "" {
		parts = append(parts, refusal)
	}
	if len(parts) == 0 {
		if text := asStringValue(obj["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return parts
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func asStringValue(raw interface{}) string {
	value, _ := raw.(string)
	return value
}
