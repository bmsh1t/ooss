package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLLMResponsesSettings(t *testing.T, providerURL string) string {
	t.Helper()

	settingsPath := filepath.Join(t.TempDir(), "osm-settings.yaml")
	settingsContent := fmt.Sprintf(`llm_config:
  llm_providers:
    - provider: mock
      base_url: %q
      auth_token: ""
      model: "gpt-5.4"
  max_retries: 1
  timeout: "30s"
`, providerURL)

	require.NoError(t, os.WriteFile(settingsPath, []byte(settingsContent), 0o644))
	return settingsPath
}

func writeStreamingLLMResponsesWorkflow(t *testing.T) string {
	t.Helper()

	workflowDir := t.TempDir()
	workflowPath := filepath.Join(workflowDir, "test-llm-responses-streaming.yaml")
	workflowContent := `kind: module
name: test-llm-responses-streaming
description: Test streaming LLM Responses API mode execution
tags: test,llm,responses,streaming

params:
  - name: target
    required: true
    default: example.com

steps:
  - name: responses-chat-stream
    type: llm
    log: "Running streaming LLM via Responses API"
    messages:
      - role: user
        content: "Search recent reconnaissance guidance for {{Target}}"
    llm_config:
      stream: true
    extra_llm_parameters:
      api_mode: responses
      tools:
        - type: web_search_preview
    timeout: 60
    exports:
      responses_text: "{{responses_chat_stream_content}}"

  - name: verify-responses-output
    type: bash
    log: "Printing Responses API result"
    command: 'echo "Responses summary: {{responses_text}}"'
`

	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))
	return workflowDir
}

func runCLIWithLogEnv(t *testing.T, log *TestLogger, extraEnv []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	binary := getBinaryPath(t)
	baseDir := t.TempDir()
	workspacesDir := filepath.Join(baseDir, "workspaces")
	require.NoError(t, os.MkdirAll(workspacesDir, 0o755))

	args = append([]string{"--base-folder", baseDir}, args...)
	log.Command(args...)

	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"OSM_SKIP_PATH_SETUP=1",
		"OSM_WORKSPACES="+workspacesDir,
	)
	cmd.Env = append(cmd.Env, extraEnv...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	log.Result(stdout, stderr)
	if err != nil {
		log.Error("Command failed: %v", err)
	}

	return stdout, stderr, err
}

func TestRun_LLMResponsesModule_WorkflowValidate(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing LLM Responses workflow validation")

	settingsPath := writeLLMResponsesSettings(t, "http://127.0.0.1:65535/v1/chat/completions")
	workflowPath := getAgentTestdataPath(t)

	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"workflow", "validate", "test-llm-responses",
		"-F", workflowPath,
	)
	require.NoError(t, err, stderr)

	assert.Contains(t, stdout, "test-llm-responses")
}

func TestRun_LLMResponsesModule_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing LLM Responses workflow against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_e2e_1",
			"object":"response",
			"created_at":1710003000,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Native workflow response"}]}],
			"output_text":"Native workflow response",
			"usage":{"input_tokens":14,"output_tokens":4,"total_tokens":18}
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")
	workflowPath := getAgentTestdataPath(t)

	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"run", "-m", "test-llm-responses",
		"-t", "example.com",
		"-F", workflowPath,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Responses summary: Native workflow response")

	_, hasMessages := requestBody["messages"]
	assert.False(t, hasMessages)
	_, hasInput := requestBody["input"]
	assert.True(t, hasInput)

	tools, ok := requestBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool, ok := tools[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "web_search_preview", tool["type"])
}

func TestRun_LLMResponsesModule_LiveMock_OutputItemFallback(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing LLM Responses workflow with output_item.done fallback against mock provider")

	var seenPath string

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_e2e_fallback_1\",\"object\":\"response\",\"created_at\":1710003007,\"status\":\"in_progress\",\"model\":\"gpt-5.4\",\"output\":[]}}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Fallback \"}\n\n")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"workflow response\"}\n\n")
		_, _ = io.WriteString(w, "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_fallback\",\"type\":\"message\",\"status\":\"completed\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Fallback workflow response\"}]}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")
	workflowPath := writeStreamingLLMResponsesWorkflow(t)

	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"run", "-m", "test-llm-responses-streaming",
		"-t", "example.com",
		"-F", workflowPath,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Responses summary: Fallback workflow response")
}

func TestRun_LLMResponsesModule_LiveMock_RefusalFallback(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing LLM Responses workflow with refusal fallback against mock provider")

	var seenPath string

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_e2e_refusal_1",
			"object":"response",
			"created_at":1710003009,
			"model":"gpt-5.4",
			"output":[
				{
					"id":"msg_refusal",
					"type":"message",
					"status":"completed",
					"role":"assistant",
					"content":[
						{"type":"refusal","refusal":"Cannot comply with that request."}
					]
				}
			]
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")
	workflowPath := getAgentTestdataPath(t)

	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"run", "-m", "test-llm-responses",
		"-t", "example.com",
		"-F", workflowPath,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Responses summary: Cannot comply with that request.")
}

func TestRun_LLMResponsesAliasModule_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing LLM Responses alias workflow against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_e2e_alias_1",
			"object":"response",
			"created_at":1710003001,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Alias workflow response"}]}],
			"output_text":"Alias workflow response",
			"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")
	workflowPath := getAgentTestdataPath(t)

	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"run", "-m", "test-llm-responses-alias",
		"-t", "example.com",
		"-F", workflowPath,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Responses alias summary: Alias workflow response")
	_, hasFlag := requestBody["use_responses_api"]
	assert.False(t, hasFlag)

	tools, ok := requestBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
	tool, ok := tools[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "web_search_preview", tool["type"])
}

func TestFunctionEval_LLMInvoke_ResponsesEnvMode_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing llm_invoke via Responses env mode against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_func_env_1",
			"object":"response",
			"created_at":1710003002,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Function env response"}]}],
			"output_text":"Function env response"
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")

	stdout, stderr, err := runCLIWithLogEnv(t, log,
		[]string{"OSM_LLM_API_MODE=responses"},
		"--settings-file", settingsPath,
		"func", "e", `llm_invoke("hello responses")`,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Function env response")

	_, hasMessages := requestBody["messages"]
	assert.False(t, hasMessages)
	assert.Equal(t, "hello responses", requestBody["input"])
}

func TestFunctionEval_LLMInvokeCustom_ResponsesInput_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing llm_invoke_custom with native Responses body against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_func_custom_1",
			"object":"response",
			"created_at":1710003003,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Function custom response"}]}],
			"output_text":"Function custom response"
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")

	script := `llm_invoke_custom("latest recon news", "{\"model\":\"gpt-5.4\",\"input\":\"{{message}}\",\"tools\":[{\"type\":\"web_search_preview\"}]}")`
	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"func", "e", script,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Function custom response")
	assert.Equal(t, "latest recon news", requestBody["input"])

	tools, ok := requestBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
}

func TestFunctionEval_LLMInvokeCustom_ResponsesInputWithInstructionsAndFormat_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing llm_invoke_custom Responses body with instructions and response_format against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_func_custom_2",
			"object":"response",
			"created_at":1710003006,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Function custom formatted response"}]}],
			"output_text":"Function custom formatted response"
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")

	script := `llm_invoke_custom("latest recon news", "{\"model\":\"gpt-5.4\",\"instructions\":\"Be brief\",\"input\":\"{{message}}\",\"response_format\":{\"type\":\"json_schema\"}}")`
	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"func", "e", script,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Function custom formatted response")
	assert.Equal(t, "latest recon news", requestBody["input"])
	assert.Equal(t, "Be brief", requestBody["instructions"])

	textConfig, ok := requestBody["text"].(map[string]interface{})
	require.True(t, ok)
	format, ok := textConfig["format"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "json_schema", format["type"])

	_, hasRawResponseFormat := requestBody["response_format"]
	assert.False(t, hasRawResponseFormat)
}

func TestFunctionEval_LLMInvokeCustom_NativeToolAutoResponses_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing llm_invoke_custom auto-detects Responses mode from native tool against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_func_custom_3",
			"object":"response",
			"created_at":1710003008,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Function custom auto responses"}]}],
			"output_text":"Function custom auto responses"
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")

	script := `llm_invoke_custom("latest recon news", "{\"model\":\"gpt-5.4\",\"messages\":[{\"role\":\"user\",\"content\":\"{{message}}\"}],\"tools\":[{\"type\":\"web_search_preview\"}]}")`
	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"func", "e", script,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Function custom auto responses")

	inputItems, ok := requestBody["input"].([]interface{})
	require.True(t, ok)
	require.Len(t, inputItems, 1)

	first, ok := inputItems[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "user", first["role"])
	assert.Equal(t, "latest recon news", first["content"])

	tools, ok := requestBody["tools"].([]interface{})
	require.True(t, ok)
	require.Len(t, tools, 1)
}

func TestFunctionEval_LLMInvokeCustom_ResponsesRefusalFallback_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing llm_invoke_custom refusal fallback against mock provider")

	var seenPath string

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_func_custom_4",
			"object":"response",
			"created_at":1710003010,
			"model":"gpt-5.4",
			"output":[
				{
					"id":"msg_refusal",
					"type":"message",
					"status":"completed",
					"role":"assistant",
					"content":[
						{"type":"refusal","refusal":"Cannot comply with that request."}
					]
				}
			]
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")

	script := `llm_invoke_custom("latest recon news", "{\"model\":\"gpt-5.4\",\"input\":\"{{message}}\"}")`
	stdout, stderr, err := runCLIWithLog(t, log,
		"--settings-file", settingsPath,
		"func", "e", script,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Cannot comply with that request.")
}

func TestFunctionEval_LLMConversations_ResponsesEnvMode_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing llm_conversations via Responses env mode against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_func_conv_1",
			"object":"response",
			"created_at":1710003004,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Function conversations response"}]}],
			"output_text":"Function conversations response"
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")

	stdout, stderr, err := runCLIWithLogEnv(t, log,
		[]string{"OSM_LLM_API_MODE=responses"},
		"--settings-file", settingsPath,
		"func", "e", `llm_conversations("system:Be brief", "user:Analyze example.com")`,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Function conversations response")

	inputItems, ok := requestBody["input"].([]interface{})
	require.True(t, ok)
	require.Len(t, inputItems, 2)

	first, ok := inputItems[0].(map[string]interface{})
	require.True(t, ok)
	second, ok := inputItems[1].(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, "system", first["role"])
	assert.Equal(t, "Be brief", first["content"])
	assert.Equal(t, "user", second["role"])
	assert.Equal(t, "Analyze example.com", second["content"])
}

func TestFunctionEval_LLMConversations_DeveloperRoleResponsesEnvMode_LiveMock(t *testing.T) {
	log := NewTestLogger(t)
	log.Step("Testing llm_conversations developer role via Responses env mode against mock provider")

	var seenPath string
	var requestBody map[string]interface{}

	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&requestBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_func_conv_dev_1",
			"object":"response",
			"created_at":1710003005,
			"model":"gpt-5.4",
			"output":[{"id":"msg_1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Function developer conversations response"}]}],
			"output_text":"Function developer conversations response"
		}`)
	}))
	defer server.Close()

	settingsPath := writeLLMResponsesSettings(t, server.URL+"/v1/chat/completions")

	stdout, stderr, err := runCLIWithLogEnv(t, log,
		[]string{"OSM_LLM_API_MODE=responses"},
		"--settings-file", settingsPath,
		"func", "e", `llm_conversations("developer:Be brief", "user:Analyze example.com")`,
	)
	require.NoError(t, err, stderr)

	assert.Equal(t, "/v1/responses", seenPath)
	assert.Contains(t, stdout, "Function developer conversations response")

	inputItems, ok := requestBody["input"].([]interface{})
	require.True(t, ok)
	require.Len(t, inputItems, 2)

	first, ok := inputItems[0].(map[string]interface{})
	require.True(t, ok)
	second, ok := inputItems[1].(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, "system", first["role"])
	assert.Equal(t, "Be brief", first["content"])
	assert.Equal(t, "user", second["role"])
	assert.Equal(t, "Analyze example.com", second["content"])
}
