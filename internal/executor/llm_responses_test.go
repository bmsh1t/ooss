package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildResponsesRequestBodyNormalizesRolesAndResponseFormatExtraParam(t *testing.T) {
	body := buildResponsesRequestBody(
		&ChatCompletionRequest{
			Model: "gpt-5.4",
			Messages: []ChatMessage{
				{Role: "developer", Content: "be concise"},
				{Role: "", Content: "hello"},
			},
			ResponseFormat: &core.LLMResponseFormat{Type: "json_object"},
		},
		&MergedLLMConfig{
			ExtraParams: map[string]interface{}{
				"api_mode":        "responses",
				"response_format": map[string]interface{}{"type": "json_schema"},
			},
		},
	)

	inputItems, ok := body["input"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, inputItems, 2)
	assert.Equal(t, "system", inputItems[0]["role"])
	assert.Equal(t, "user", inputItems[1]["role"])

	textConfig, ok := body["text"].(map[string]interface{})
	require.True(t, ok)
	format, ok := textConfig["format"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "json_schema", format["type"])

	_, hasRawResponseFormat := body["response_format"]
	assert.False(t, hasRawResponseFormat)
}

func TestConvertResponsesMapToChatResponseExtractsRefusalAndReasoningSummary(t *testing.T) {
	refusalResp := convertResponsesMapToChatResponse(map[string]interface{}{
		"id":         "resp_refusal",
		"object":     "response",
		"created_at": 1710001999,
		"model":      "gpt-5.4",
		"output": []interface{}{
			map[string]interface{}{
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []interface{}{
					map[string]interface{}{
						"type":    "refusal",
						"refusal": "Cannot comply with that request.",
					},
				},
			},
		},
	})
	assert.Equal(t, "Cannot comply with that request.", refusalResp.Choices[0].Message.Content)
	assert.Equal(t, "stop", refusalResp.Choices[0].FinishReason)

	reasoningResp := convertResponsesMapToChatResponse(map[string]interface{}{
		"id":         "resp_reasoning",
		"object":     "response",
		"created_at": 1710002000,
		"model":      "gpt-5.4",
		"output": []interface{}{
			map[string]interface{}{
				"type": "reasoning",
				"summary": []interface{}{
					map[string]interface{}{
						"type": "summary_text",
						"text": "Reasoning summary text.",
					},
				},
			},
		},
	})
	assert.Equal(t, "Reasoning summary text.", reasoningResp.Choices[0].Message.Content)
}

func TestConvertResponsesMapToChatResponseMapsIncompleteToLengthFinishReason(t *testing.T) {
	resp := convertResponsesMapToChatResponse(map[string]interface{}{
		"id":         "resp_incomplete",
		"object":     "response",
		"created_at": 1710002001,
		"model":      "gpt-5.4",
		"status":     "incomplete",
		"incomplete_details": map[string]interface{}{
			"reason": "max_output_tokens",
		},
		"output": []interface{}{
			map[string]interface{}{
				"type":   "message",
				"role":   "assistant",
				"status": "incomplete",
				"content": []interface{}{
					map[string]interface{}{
						"type": "output_text",
						"text": "Partial answer",
					},
				},
			},
		},
	})

	assert.Equal(t, "Partial answer", resp.Choices[0].Message.Content)
	assert.Equal(t, "length", resp.Choices[0].FinishReason)
}

func TestSendChatRequestResponsesByProviderURL(t *testing.T) {
	var seenPath string
	var requestBody map[string]interface{}
	server := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_exec_1",
			"object":"response",
			"created_at":1710001000,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"executor response text"}]}],
			"output_text":"executor response text",
			"usage":{"input_tokens":11,"output_tokens":4,"total_tokens":15}
		}`)
	})
	defer server.Close()

	executor := &LLMExecutor{config: &config.Config{}, silent: true}
	response, err := executor.sendChatRequest(
		context.Background(),
		&config.LLMProvider{BaseURL: server.URL + "/v1/responses", Model: "gpt-5.4"},
		&ChatCompletionRequest{
			Model:    "gpt-5.4",
			Messages: []ChatMessage{{Role: "user", Content: "hello"}},
		},
		&MergedLLMConfig{Timeout: "30s", ExtraParams: map[string]interface{}{}},
	)

	require.NoError(t, err)
	assert.Equal(t, "/v1/responses", seenPath)
	assert.Equal(t, "gpt-5.4", requestBody["model"])
	_, hasMessages := requestBody["messages"]
	assert.False(t, hasMessages)
	inputItems, ok := requestBody["input"].([]interface{})
	require.True(t, ok)
	require.Len(t, inputItems, 1)
	assert.Equal(t, "executor response text", response.Choices[0].Message.Content)
	assert.Equal(t, 11, response.Usage.PromptTokens)
}

func TestSendChatRequestResponsesByExtraParams(t *testing.T) {
	var seenPath string
	var requestBody map[string]interface{}
	server := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_exec_2",
			"object":"response",
			"created_at":1710001001,
			"model":"gpt-5.4",
			"output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"dns_lookup","arguments":"{\"domain\":\"example.com\"}"}],
			"usage":{"input_tokens":9,"output_tokens":3,"total_tokens":12}
		}`)
	})
	defer server.Close()

	executor := &LLMExecutor{config: &config.Config{}, silent: true}
	response, err := executor.sendChatRequest(
		context.Background(),
		&config.LLMProvider{BaseURL: server.URL + "/v1/chat/completions", Model: "gpt-5.4"},
		&ChatCompletionRequest{
			Model:    "gpt-5.4",
			Messages: []ChatMessage{{Role: "user", Content: "lookup"}},
			Tools: []core.LLMTool{
				{
					Type: "function",
					Function: core.LLMToolFunction{
						Name: "legacy_tool",
					},
				},
			},
		},
		&MergedLLMConfig{
			Timeout: "30s",
			ExtraParams: map[string]interface{}{
				"api_mode": "responses",
				"tools": []interface{}{
					map[string]interface{}{"type": "web_search_preview"},
				},
			},
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "/v1/responses", seenPath)
	tools, ok := requestBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]interface{})
	assert.Equal(t, "web_search_preview", tool["type"])
	require.Len(t, response.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "dns_lookup", response.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Equal(t, "tool_calls", response.Choices[0].FinishReason)
}

func TestSendChatRequestStreamingResponses(t *testing.T) {
	server := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream_exec\",\"object\":\"response\",\"created_at\":1710001002,\"status\":\"in_progress\",\"model\":\"gpt-5.4\",\"output\":[]}}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hel\"}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream_exec\",\"object\":\"response\",\"created_at\":1710001002,\"status\":\"completed\",\"model\":\"gpt-5.4\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello\"}]}],\"output_text\":\"Hello\",\"usage\":{\"input_tokens\":5,\"output_tokens\":2,\"total_tokens\":7}}}\n\n")
	})
	defer server.Close()

	executor := &LLMExecutor{config: &config.Config{}, silent: true}
	var tokens []string
	response, err := executor.sendResponsesRequestStreaming(
		context.Background(),
		&config.LLMProvider{BaseURL: server.URL + "/v1/responses", Model: "gpt-5.4"},
		&ChatCompletionRequest{
			Model:    "gpt-5.4",
			Messages: []ChatMessage{{Role: "user", Content: "hello"}},
			Stream:   true,
		},
		&MergedLLMConfig{Timeout: "30s", ExtraParams: map[string]interface{}{}},
		func(token string) { tokens = append(tokens, token) },
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"Hel", "lo", "\n"}, tokens)
	assert.Equal(t, "Hello", response.Choices[0].Message.Content)
	assert.Equal(t, 7, response.Usage.TotalTokens)
}

func TestSendResponsesRequestStreamingFallsBackToOutputItemDone(t *testing.T) {
	server := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream_fallback\",\"object\":\"response\",\"created_at\":1710001004,\"status\":\"in_progress\",\"model\":\"gpt-5.4\",\"output\":[]}}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hel\"}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}\n\n")
		_, _ = io.WriteString(w, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_fallback\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Hello\"}]}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	defer server.Close()

	executor := &LLMExecutor{config: &config.Config{}, silent: true}
	var tokens []string
	response, err := executor.sendResponsesRequestStreaming(
		context.Background(),
		&config.LLMProvider{BaseURL: server.URL + "/v1/responses", Model: "gpt-5.4"},
		&ChatCompletionRequest{
			Model:    "gpt-5.4",
			Messages: []ChatMessage{{Role: "user", Content: "hello"}},
			Stream:   true,
		},
		&MergedLLMConfig{Timeout: "30s", ExtraParams: map[string]interface{}{}},
		func(token string) { tokens = append(tokens, token) },
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"Hel", "lo", "\n"}, tokens)
	assert.Equal(t, "resp_stream_fallback", response.ID)
	assert.Equal(t, "gpt-5.4", response.Model)
	assert.Equal(t, "Hello", response.Choices[0].Message.Content)
	assert.Equal(t, "stop", response.Choices[0].FinishReason)
}

func TestSendResponsesRequestStreamingFallsBackToToolCallOutputItem(t *testing.T) {
	server := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream_tool\",\"object\":\"response\",\"created_at\":1710001005,\"status\":\"in_progress\",\"model\":\"gpt-5.4\",\"output\":[]}}\n\n")
		_, _ = io.WriteString(w, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fc_tool\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"dns_lookup\",\"arguments\":\"{\\\"domain\\\":\\\"example.com\\\"}\"}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	defer server.Close()

	executor := &LLMExecutor{config: &config.Config{}, silent: true}
	response, err := executor.sendResponsesRequestStreaming(
		context.Background(),
		&config.LLMProvider{BaseURL: server.URL + "/v1/responses", Model: "gpt-5.4"},
		&ChatCompletionRequest{
			Model:    "gpt-5.4",
			Messages: []ChatMessage{{Role: "user", Content: "lookup"}},
			Stream:   true,
		},
		&MergedLLMConfig{Timeout: "30s", ExtraParams: map[string]interface{}{}},
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, "resp_stream_tool", response.ID)
	assert.Equal(t, "tool_calls", response.Choices[0].FinishReason)
	require.Len(t, response.Choices[0].Message.ToolCalls, 1)
	assert.Equal(t, "dns_lookup", response.Choices[0].Message.ToolCalls[0].Function.Name)
	assert.Equal(t, `{"domain":"example.com"}`, response.Choices[0].Message.ToolCalls[0].Function.Arguments)
}

func TestResponsesLLMStepIntegrationViaExtraParams(t *testing.T) {
	var seenPath string
	var requestBody map[string]interface{}
	server := newMockLLMServer(t, func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_exec_step_1",
			"object":"response",
			"created_at":1710001003,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"workflow response text"}]}],
			"output_text":"workflow response text",
			"usage":{"input_tokens":13,"output_tokens":5,"total_tokens":18}
		}`)
	})
	defer server.Close()

	cfg := newMockConfig(t, server.URL+"/v1/chat/completions")
	cfg.LLM.LLMProviders[0].Model = "gpt-5.4"

	dispatcher := NewStepDispatcher()
	llmExec := NewLLMExecutor(dispatcher.GetTemplateEngine())
	llmExec.SetConfig(cfg)
	llmExec.SetSilent(true)

	result, err := llmExec.Execute(
		context.Background(),
		&core.Step{
			Name: "responses-workflow",
			Type: core.StepTypeLLM,
			Messages: []core.LLMMessage{
				{Role: core.LLMRoleUser, Content: "search latest recon guidance"},
			},
			ExtraLLMParams: map[string]interface{}{
				"api_mode": "responses",
				"tools": []interface{}{
					map[string]interface{}{"type": "web_search_preview"},
				},
			},
		},
		core.NewExecutionContext("test", core.KindModule, "run-1", "example.com"),
	)

	require.NoError(t, err)
	assert.Equal(t, core.StepStatusSuccess, result.Status)
	assert.Equal(t, "/v1/responses", seenPath)
	assert.Equal(t, "workflow response text", result.Output)
	assert.Equal(t, "workflow response text", result.Exports["responses_workflow_content"])

	tools, ok := requestBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool, ok := tools[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "web_search_preview", tool["type"])

	_, hasMessages := requestBody["messages"]
	assert.False(t, hasMessages)
	_, hasInput := requestBody["input"]
	assert.True(t, hasInput)
}
