package functions

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dop251/goja"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/logger"
	"github.com/j3ssie/osmedeus/v5/internal/terminal"
	"go.uber.org/zap"
)

// llmFuncConfig holds resolved LLM configuration for function calls
type llmFuncConfig struct {
	BaseURL   string
	AuthToken string
	Model     string
	APIMode   string
}

// getLLMConfig resolves LLM config with environment variable overrides (highest priority)
func getLLMConfig() (*llmFuncConfig, error) {
	cfg := config.Get()
	result := &llmFuncConfig{}

	// Start with config file values
	if cfg != nil && len(cfg.LLM.LLMProviders) > 0 {
		provider := cfg.LLM.GetCurrentProvider()
		if provider != nil {
			result.BaseURL = provider.BaseURL
			result.AuthToken = provider.AuthToken
			result.Model = provider.Model
		}
	}

	// Environment overrides (highest priority)
	if v := os.Getenv("OSM_LLM_BASE_URL"); v != "" {
		result.BaseURL = v
	}
	if v := os.Getenv("OSM_LLM_AUTH_TOKEN"); v != "" {
		result.AuthToken = v
	}
	if v := os.Getenv("OSM_LLM_MODEL"); v != "" {
		result.Model = v
	}
	if v := os.Getenv("OSM_LLM_API_MODE"); v != "" {
		result.APIMode = v
	}

	// Validate we have required configuration
	if result.BaseURL == "" {
		return nil, fmt.Errorf("LLM base URL not configured (set OSM_LLM_BASE_URL or configure llm_config in settings)")
	}

	return result, nil
}

// llmChatRequest represents an OpenAI-compatible chat completion request
type llmChatRequest struct {
	Model    string       `json:"model,omitempty"`
	Messages []llmMessage `json:"messages"`
}

// llmMessage represents a single message in the chat
type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llmChatResponse represents an OpenAI-compatible chat completion response
type llmChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// llmInvoke makes a simple LLM call with a single message
// Usage: llm_invoke(message) -> string
func (vf *vmFunc) llmInvoke(call goja.FunctionCall) goja.Value {
	message := call.Argument(0).String()
	logger.Get().Debug("Calling "+terminal.HiGreen(FnLLMInvoke), zap.Int("messageLength", len(message)))

	if message == "undefined" || message == "" {
		logger.Get().Warn(FnLLMInvoke + ": message is required")
		return vf.vm.ToValue("")
	}

	// Get LLM config
	llmCfg, err := getLLMConfig()
	if err != nil {
		logger.Get().Warn(FnLLMInvoke+": config error", zap.Error(err))
		return vf.vm.ToValue("")
	}

	// Build request body
	body := map[string]interface{}{
		"model": llmCfg.Model,
	}
	if strings.EqualFold(strings.TrimSpace(llmCfg.APIMode), "responses") || strings.HasSuffix(strings.TrimRight(llmCfg.BaseURL, "/"), "/responses") {
		body["input"] = message
	} else {
		body["messages"] = []llmMessage{
			{Role: "user", Content: message},
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		logger.Get().Warn(FnLLMInvoke+": failed to marshal request", zap.Error(err))
		return vf.vm.ToValue("")
	}

	// Send request
	content, err := sendLLMRequest(llmCfg, bodyBytes)
	if err != nil {
		logger.Get().Warn(FnLLMInvoke+": request failed", zap.Error(err))
		return vf.vm.ToValue("")
	}

	logger.Get().Debug(terminal.HiGreen(FnLLMInvoke)+" result", zap.Int("responseLength", len(content)))
	return vf.vm.ToValue(content)
}

// llmInvokeCustom makes an LLM call with a custom request body
// Usage: llm_invoke_custom(message, body_json) -> string
// The body_json can contain {{message}} placeholder that will be replaced with the message
func (vf *vmFunc) llmInvokeCustom(call goja.FunctionCall) goja.Value {
	message := call.Argument(0).String()
	bodyJSON := call.Argument(1).String()
	logger.Get().Debug("Calling "+terminal.HiGreen(FnLLMInvokeCustom), zap.Int("messageLength", len(message)), zap.Int("bodyLength", len(bodyJSON)))

	if message == "undefined" || message == "" {
		logger.Get().Warn(FnLLMInvokeCustom + ": message is required")
		return vf.vm.ToValue("")
	}

	if bodyJSON == "undefined" || bodyJSON == "" {
		logger.Get().Warn(FnLLMInvokeCustom + ": body_json is required")
		return vf.vm.ToValue("")
	}

	// Get LLM config
	llmCfg, err := getLLMConfig()
	if err != nil {
		logger.Get().Warn(FnLLMInvokeCustom+": config error", zap.Error(err))
		return vf.vm.ToValue("")
	}

	// Replace {{message}} placeholder in body
	// Escape the message for JSON string embedding
	escapedMessage, err := json.Marshal(message)
	if err != nil {
		logger.Get().Warn(FnLLMInvokeCustom+": failed to escape message", zap.Error(err))
		return vf.vm.ToValue("")
	}
	// Remove surrounding quotes from JSON encoding
	escapedMessageStr := string(escapedMessage[1 : len(escapedMessage)-1])
	bodyStr := strings.ReplaceAll(bodyJSON, "{{message}}", escapedMessageStr)

	// Validate JSON
	var bodyMap map[string]interface{}
	if err := json.Unmarshal([]byte(bodyStr), &bodyMap); err != nil {
		logger.Get().Warn(FnLLMInvokeCustom+": invalid JSON body", zap.Error(err))
		return vf.vm.ToValue("")
	}

	// Send request
	content, err := sendLLMRequest(llmCfg, []byte(bodyStr))
	if err != nil {
		logger.Get().Warn(FnLLMInvokeCustom+": request failed", zap.Error(err))
		return vf.vm.ToValue("")
	}

	logger.Get().Debug(terminal.HiGreen(FnLLMInvokeCustom)+" result", zap.Int("responseLength", len(content)))
	return vf.vm.ToValue(content)
}

// llmConversations makes an LLM call with multiple messages
// Usage: llm_conversations(msg1, msg2, ...) -> string
// Each message should be in format "role:content" where role is system, developer, user, or assistant
func (vf *vmFunc) llmConversations(call goja.FunctionCall) goja.Value {
	logger.Get().Debug("Calling "+terminal.HiGreen(FnLLMConversations), zap.Int("argCount", len(call.Arguments)))

	if len(call.Arguments) == 0 {
		logger.Get().Warn(FnLLMConversations + ": at least one message is required")
		return vf.vm.ToValue("")
	}

	// Get LLM config
	llmCfg, err := getLLMConfig()
	if err != nil {
		logger.Get().Warn(FnLLMConversations+": config error", zap.Error(err))
		return vf.vm.ToValue("")
	}

	// Parse messages
	var messages []llmMessage
	for i, arg := range call.Arguments {
		msgStr := arg.String()
		if msgStr == "undefined" || msgStr == "" {
			continue
		}

		// Parse "role:content" format
		colonIdx := strings.Index(msgStr, ":")
		if colonIdx == -1 {
			logger.Get().Warn(FnLLMConversations+": invalid message format (expected 'role:content')",
				zap.Int("argIndex", i), zap.String("message", msgStr))
			return vf.vm.ToValue("")
		}

		role := strings.ToLower(strings.TrimSpace(msgStr[:colonIdx]))
		content := strings.TrimSpace(msgStr[colonIdx+1:])

		// Validate role
		validRoles := map[string]bool{"system": true, "developer": true, "user": true, "assistant": true}
		if !validRoles[role] {
			logger.Get().Warn(FnLLMConversations+": invalid role (expected system, developer, user, or assistant)",
				zap.Int("argIndex", i), zap.String("role", role))
			return vf.vm.ToValue("")
		}

		messages = append(messages, llmMessage{
			Role:    normalizeFuncRole(role),
			Content: content,
		})
	}

	if len(messages) == 0 {
		logger.Get().Warn(FnLLMConversations + ": no valid messages provided")
		return vf.vm.ToValue("")
	}

	// Build request body
	body := map[string]interface{}{
		"model": llmCfg.Model,
	}
	if strings.EqualFold(strings.TrimSpace(llmCfg.APIMode), "responses") || strings.HasSuffix(strings.TrimRight(llmCfg.BaseURL, "/"), "/responses") {
		body["input"] = convertFuncMessagesToResponsesInput(messages)
	} else {
		body["messages"] = messages
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		logger.Get().Warn(FnLLMConversations+": failed to marshal request", zap.Error(err))
		return vf.vm.ToValue("")
	}

	// Send request
	content, err := sendLLMRequest(llmCfg, bodyBytes)
	if err != nil {
		logger.Get().Warn(FnLLMConversations+": request failed", zap.Error(err))
		return vf.vm.ToValue("")
	}

	logger.Get().Debug(terminal.HiGreen(FnLLMConversations)+" result", zap.Int("responseLength", len(content)))
	return vf.vm.ToValue(content)
}
