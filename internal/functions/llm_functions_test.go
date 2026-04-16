package functions

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendLLMRequestResponsesModeDerivesResponsesURL(t *testing.T) {
	var seenPath string
	var requestBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_fn_1",
			"object":"response",
			"created_at":1710002000,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"function responses text"}]}],
			"output_text":"function responses text"
		}`)
	}))
	defer server.Close()

	body, err := json.Marshal(map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	})
	require.NoError(t, err)

	text, err := sendLLMRequest(&llmFuncConfig{
		BaseURL: server.URL + "/v1/chat/completions",
		Model:   "gpt-5.4",
		APIMode: "responses",
	}, body)
	require.NoError(t, err)
	assert.Equal(t, "/v1/responses", seenPath)
	assert.Equal(t, "function responses text", text)
	_, hasMessages := requestBody["messages"]
	assert.False(t, hasMessages)
	_, hasInput := requestBody["input"]
	assert.True(t, hasInput)
}

func TestPrepareFuncRequestBodyConvertsMessagesToResponses(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-5.4",
		"messages": []interface{}{
			map[string]interface{}{"role": "developer", "content": "be concise"},
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"response_format": map[string]interface{}{"type": "json_schema"},
	}

	prepared, useResponses, err := prepareFuncRequestBody(&llmFuncConfig{
		BaseURL: "http://example.test/v1/chat/completions",
		Model:   "gpt-5.4",
		APIMode: "responses",
	}, body)
	require.NoError(t, err)
	assert.True(t, useResponses)

	inputItems, ok := prepared["input"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, inputItems, 2)
	assert.Equal(t, "system", inputItems[0]["role"])
	assert.Equal(t, "hello", inputItems[1]["content"])

	textConfig, ok := prepared["text"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, textConfig, "format")
	_, hasMessages := prepared["messages"]
	assert.False(t, hasMessages)
}

func TestPrepareFuncRequestBodyNormalizesNativeResponsesInputBody(t *testing.T) {
	body := map[string]interface{}{
		"input":           "hello",
		"response_format": map[string]interface{}{"type": "json_schema"},
	}

	prepared, useResponses, err := prepareFuncRequestBody(&llmFuncConfig{
		BaseURL: "http://example.test/v1/chat/completions",
		Model:   "gpt-5.4",
		APIMode: "responses",
	}, body)
	require.NoError(t, err)
	assert.True(t, useResponses)
	assert.Equal(t, "gpt-5.4", prepared["model"])
	assert.Equal(t, "hello", prepared["input"])

	textConfig, ok := prepared["text"].(map[string]interface{})
	require.True(t, ok)
	format, ok := textConfig["format"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "json_schema", format["type"])

	_, hasResponseFormat := prepared["response_format"]
	assert.False(t, hasResponseFormat)
}

func TestConvertFuncMessagesToResponsesInputNormalizesDeveloperRole(t *testing.T) {
	inputItems := convertFuncMessagesToResponsesInput([]llmMessage{
		{Role: "developer", Content: "be concise"},
		{Role: "", Content: "hello"},
	})

	require.Len(t, inputItems, 2)
	assert.Equal(t, "system", inputItems[0]["role"])
	assert.Equal(t, "user", inputItems[1]["role"])
}

func TestUseResponsesAPIForFuncDetectsNativeToolAndResponsesFields(t *testing.T) {
	llmCfg := &llmFuncConfig{
		BaseURL: "http://example.test/v1/chat/completions",
	}

	assert.True(t, useResponsesAPIForFunc(llmCfg, map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"tools": []interface{}{
			map[string]interface{}{"type": "web_search_preview"},
		},
	}))

	assert.True(t, useResponsesAPIForFunc(llmCfg, map[string]interface{}{
		"messages":          []interface{}{map[string]interface{}{"role": "user", "content": "hello"}},
		"max_output_tokens": 256,
	}))

	assert.False(t, useResponsesAPIForFunc(llmCfg, map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"tools": []interface{}{
			map[string]interface{}{"type": "function", "name": "dns_lookup"},
		},
	}))
}

func TestParseResponsesTextHandlesRefusalAndReasoningSummary(t *testing.T) {
	refusalText, err := parseResponsesText([]byte(`{
		"output":[
			{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"refusal","refusal":"Cannot comply with that request."}
				]
			}
		]
	}`))
	require.NoError(t, err)
	assert.Equal(t, "Cannot comply with that request.", refusalText)

	reasoningText, err := parseResponsesText([]byte(`{
		"output":[
			{
				"type":"reasoning",
				"summary":[
					{"type":"summary_text","text":"Reasoning summary text."}
				]
			}
		]
	}`))
	require.NoError(t, err)
	assert.Equal(t, "Reasoning summary text.", reasoningText)
}
