package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLLMResponsesStringInputCompatibility(t *testing.T) {
	var upstream map[string]interface{}
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&upstream))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-string",
			"object":"chat.completion",
			"created":1710000000,
			"model":"gpt-5.4-mini",
			"choices":[
				{
					"index":0,
					"message":{"role":"assistant","content":"Pong"},
					"finish_reason":"stop"
				}
			],
			"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}
		}`)
	}))
	defer mockProvider.Close()

	cfg := &config.Config{
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					BaseURL: mockProvider.URL,
					Model:   "gpt-5.4",
				},
			},
			MaxTokens: 4096,
		},
	}

	app := fiber.New()
	app.Post("/llm/v1/responses", LLMResponses(cfg))

	body, err := json.Marshal(map[string]interface{}{
		"model":             "gpt-5.4-mini",
		"input":             "Ping target",
		"instructions":      "You are a pentest assistant.",
		"max_output_tokens": 256,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/llm/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	messages, ok := upstream["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, messages, 2)

	firstMsg, ok := messages[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "system", firstMsg["role"])
	assert.Equal(t, "You are a pentest assistant.", firstMsg["content"])

	secondMsg, ok := messages[1].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "user", secondMsg["role"])
	assert.Equal(t, "Ping target", secondMsg["content"])
	assert.Equal(t, "gpt-5.4-mini", upstream["model"])
	assert.Equal(t, float64(256), upstream["max_tokens"])

	var payload LLMResponsesResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, "chatcmpl-string", payload.ID)
	assert.Equal(t, "response", payload.Object)
	assert.Equal(t, "completed", payload.Status)
	assert.Equal(t, "Pong", payload.OutputText)
	require.NotNil(t, payload.Usage)
	assert.Equal(t, 12, payload.Usage.InputTokens)
	assert.Equal(t, 4, payload.Usage.OutputTokens)
	assert.Len(t, payload.Output, 1)
	assert.Equal(t, "message", payload.Output[0]["type"])
}

func TestLLMResponsesMessageArrayAndFunctionToolCompatibility(t *testing.T) {
	var upstream map[string]interface{}
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&upstream))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-tools",
			"object":"chat.completion",
			"created":1710000001,
			"model":"gpt-5.4",
			"choices":[
				{
					"index":0,
					"message":{
						"role":"assistant",
						"content":"",
						"tool_calls":[
							{
								"id":"call_1",
								"type":"function",
								"function":{
									"name":"dns_lookup",
									"arguments":"{\"domain\":\"example.com\"}"
								}
							}
						]
					},
					"finish_reason":"tool_calls"
				}
			],
			"usage":{"prompt_tokens":20,"completion_tokens":7,"total_tokens":27}
		}`)
	}))
	defer mockProvider.Close()

	cfg := &config.Config{
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					BaseURL: mockProvider.URL,
					Model:   "gpt-5.4",
				},
			},
			MaxTokens: 4096,
		},
	}

	app := fiber.New()
	app.Post("/llm/v1/responses", LLMResponses(cfg))

	body, err := json.Marshal(map[string]interface{}{
		"model":        "gpt-5.4",
		"instructions": "Top-level instruction",
		"input": []interface{}{
			map[string]interface{}{
				"role":    "developer",
				"content": "Focus on exposed APIs",
				"type":    "message",
			},
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "input_text",
						"text": "Enumerate routes",
					},
				},
			},
		},
		"tools": []interface{}{
			map[string]interface{}{
				"type":        "function",
				"name":        "dns_lookup",
				"description": "Look up DNS records",
				"parameters": map[string]interface{}{
					"type": "object",
				},
			},
		},
		"tool_choice": "auto",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/llm/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	messages, ok := upstream["messages"].([]interface{})
	require.True(t, ok)
	require.Len(t, messages, 3)

	firstMsg := messages[0].(map[string]interface{})
	secondMsg := messages[1].(map[string]interface{})
	thirdMsg := messages[2].(map[string]interface{})
	assert.Equal(t, "system", firstMsg["role"])
	assert.Equal(t, "Top-level instruction", firstMsg["content"])
	assert.Equal(t, "system", secondMsg["role"])
	assert.Equal(t, "Focus on exposed APIs", secondMsg["content"])
	assert.Equal(t, "user", thirdMsg["role"])
	assert.Equal(t, "Enumerate routes", thirdMsg["content"])

	tools, ok := upstream["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	toolObj := tools[0].(map[string]interface{})
	assert.Equal(t, "function", toolObj["type"])
	functionObj := toolObj["function"].(map[string]interface{})
	assert.Equal(t, "dns_lookup", functionObj["name"])
	assert.Equal(t, "auto", upstream["tool_choice"])

	var payload LLMResponsesResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, "chatcmpl-tools", payload.ID)
	assert.Equal(t, "", payload.OutputText)
	require.Len(t, payload.Output, 1)
	assert.Equal(t, "function_call", payload.Output[0]["type"])
	assert.Equal(t, "dns_lookup", payload.Output[0]["name"])
	assert.Equal(t, "{\"domain\":\"example.com\"}", payload.Output[0]["arguments"])
}

func TestLLMResponsesRejectsUnsupportedInputType(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					BaseURL: "http://127.0.0.1:1",
					Model:   "gpt-5.4",
				},
			},
		},
	}

	app := fiber.New()
	app.Post("/llm/v1/responses", LLMResponses(cfg))

	body, err := json.Marshal(map[string]interface{}{
		"input": 123,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/llm/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	var payload map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, true, payload["error"])
	assert.Contains(t, payload["message"], "input must be a string")
}

func TestLLMResponsesNativeBuiltinToolUsesResponsesEndpoint(t *testing.T) {
	var seenPath string
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp-native",
			"object":"response",
			"created_at":1710000002,
			"status":"completed",
			"model":"gpt-5.4",
			"output":[
				{"id":"ws_1","type":"web_search_call","status":"completed"},
				{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Native response text","annotations":[]}]}
			],
			"output_text":"Native response text",
			"parallel_tool_calls":true,
			"tool_choice":"auto",
			"tools":[{"type":"web_search_preview"}],
			"usage":{"input_tokens":8,"output_tokens":4,"total_tokens":12}
		}`)
	}))
	defer mockProvider.Close()

	cfg := &config.Config{
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					BaseURL: mockProvider.URL + "/v1/chat/completions",
					Model:   "gpt-5.4",
				},
			},
		},
	}

	app := fiber.New()
	app.Post("/llm/v1/responses", LLMResponses(cfg))

	body, err := json.Marshal(map[string]interface{}{
		"input": "latest security news",
		"tools": []interface{}{
			map[string]interface{}{
				"type": "web_search_preview",
			},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/llm/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, "/v1/responses", seenPath)

	var payload LLMResponsesResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, "resp-native", payload.ID)
	assert.Equal(t, "Native response text", payload.OutputText)
	assert.Len(t, payload.Output, 2)
	assert.Equal(t, "web_search_call", payload.Output[0]["type"])
}

func TestExtractResponsesOutputTextHandlesRefusalAndReasoningSummary(t *testing.T) {
	refusalText := extractResponsesOutputText([]map[string]interface{}{
		{
			"type": "message",
			"content": []interface{}{
				map[string]interface{}{
					"type":    "refusal",
					"refusal": "Cannot comply with that request.",
				},
			},
		},
	})
	assert.Equal(t, "Cannot comply with that request.", refusalText)

	reasoningText := extractResponsesOutputText([]map[string]interface{}{
		{
			"type": "reasoning",
			"summary": []interface{}{
				map[string]interface{}{
					"type": "summary_text",
					"text": "Reasoning summary text.",
				},
			},
		},
	})
	assert.Equal(t, "Reasoning summary text.", reasoningText)
}

func TestLLMResponsesStreamingCompatibility(t *testing.T) {
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-stream\",\"object\":\"chat.completion.chunk\",\"created\":1710000003,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-stream\",\"object\":\"chat.completion.chunk\",\"created\":1710000003,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-stream\",\"object\":\"chat.completion.chunk\",\"created\":1710000003,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer mockProvider.Close()

	cfg := &config.Config{
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					BaseURL: mockProvider.URL,
					Model:   "gpt-5.4",
				},
			},
		},
	}

	app := fiber.New()
	app.Post("/llm/v1/responses", LLMResponses(cfg))

	body, err := json.Marshal(map[string]interface{}{
		"input":  "hello",
		"stream": true,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/llm/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")

	streamBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyText := string(streamBody)
	assert.Contains(t, bodyText, "event: response.created")
	assert.Contains(t, bodyText, "event: response.output_text.delta")
	assert.Contains(t, bodyText, "\"delta\":\"Hel\"")
	assert.Contains(t, bodyText, "event: response.completed")
	assert.Contains(t, bodyText, "\"output_text\":\"Hello\"")
}

func TestLLMResponsesStreamingNativeProxy(t *testing.T) {
	var seenPath string
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-stream-native\",\"object\":\"response\",\"created_at\":1710000004,\"status\":\"in_progress\",\"model\":\"gpt-5.4\",\"output\":[]}}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-stream-native\",\"object\":\"response\",\"created_at\":1710000004,\"status\":\"completed\",\"model\":\"gpt-5.4\",\"output\":[],\"output_text\":\"native stream\"}}\n\n")
	}))
	defer mockProvider.Close()

	cfg := &config.Config{
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					BaseURL: mockProvider.URL + "/v1/chat/completions",
					Model:   "gpt-5.4",
				},
			},
		},
	}

	app := fiber.New()
	app.Post("/llm/v1/responses", LLMResponses(cfg))

	body, err := json.Marshal(map[string]interface{}{
		"input":  "news",
		"stream": true,
		"tools": []interface{}{
			map[string]interface{}{
				"type": "web_search_preview",
			},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/llm/v1/responses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, "/v1/responses", seenPath)

	streamBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	bodyText := string(streamBody)
	assert.Contains(t, bodyText, "event: response.created")
	assert.Contains(t, bodyText, "event: response.completed")
	assert.Contains(t, bodyText, "native stream")
}
