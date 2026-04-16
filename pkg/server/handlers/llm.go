package handlers

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

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/core"
)

// LLMChatRequest represents a direct LLM chat completion request
type LLMChatRequest struct {
	Messages       []core.LLMMessage       `json:"messages"`
	Model          string                  `json:"model,omitempty"`
	MaxTokens      int                     `json:"max_tokens,omitempty"`
	Temperature    *float64                `json:"temperature,omitempty"`
	TopP           *float64                `json:"top_p,omitempty"`
	TopK           *int                    `json:"top_k,omitempty"`
	N              int                     `json:"n,omitempty"`
	Stream         bool                    `json:"stream,omitempty"`
	Tools          []core.LLMTool          `json:"tools,omitempty"`
	ToolChoice     interface{}             `json:"tool_choice,omitempty"`
	ResponseFormat *core.LLMResponseFormat `json:"response_format,omitempty"`
}

// LLMChatResponse represents the chat completion response
type LLMChatResponse struct {
	ID           string             `json:"id"`
	Model        string             `json:"model"`
	Content      interface{}        `json:"content"`
	FinishReason string             `json:"finish_reason"`
	ToolCalls    []core.LLMToolCall `json:"tool_calls,omitempty"`
	Usage        map[string]int     `json:"usage"`
}

// LLMResponsesRequest represents a focused OpenAI-compatible Responses API request.
// This compatibility layer currently targets text inputs and function tools.
type LLMResponsesRequest struct {
	Model              string                   `json:"model,omitempty"`
	Input              interface{}              `json:"input"`
	Instructions       string                   `json:"instructions,omitempty"`
	MaxOutputTokens    int                      `json:"max_output_tokens,omitempty"`
	Temperature        *float64                 `json:"temperature,omitempty"`
	TopP               *float64                 `json:"top_p,omitempty"`
	Tools              []map[string]interface{} `json:"tools,omitempty"`
	ToolChoice         interface{}              `json:"tool_choice,omitempty"`
	Stream             bool                     `json:"stream,omitempty"`
	Background         bool                     `json:"background,omitempty"`
	PreviousResponseID string                   `json:"previous_response_id,omitempty"`
	Conversation       interface{}              `json:"conversation,omitempty"`
	Include            []string                 `json:"include,omitempty"`
	Metadata           map[string]interface{}   `json:"metadata,omitempty"`
	Reasoning          map[string]interface{}   `json:"reasoning,omitempty"`
	Text               map[string]interface{}   `json:"text,omitempty"`
	Truncation         string                   `json:"truncation,omitempty"`
	ParallelToolCalls  *bool                    `json:"parallel_tool_calls,omitempty"`
	Store              *bool                    `json:"store,omitempty"`
	User               interface{}              `json:"user,omitempty"`
	MaxToolCalls       int                      `json:"max_tool_calls,omitempty"`
}

// LLMResponsesResponse represents a focused OpenAI-compatible Responses API response.
type LLMResponsesResponse struct {
	ID                 string                   `json:"id"`
	Object             string                   `json:"object"`
	CreatedAt          int64                    `json:"created_at"`
	Status             string                   `json:"status"`
	CompletedAt        *int64                   `json:"completed_at,omitempty"`
	Error              map[string]interface{}   `json:"error,omitempty"`
	IncompleteDetails  map[string]interface{}   `json:"incomplete_details,omitempty"`
	Instructions       interface{}              `json:"instructions,omitempty"`
	MaxOutputTokens    interface{}              `json:"max_output_tokens,omitempty"`
	MaxToolCalls       interface{}              `json:"max_tool_calls,omitempty"`
	Model              string                   `json:"model"`
	Output             []map[string]interface{} `json:"output"`
	OutputText         string                   `json:"output_text,omitempty"`
	ParallelToolCalls  bool                     `json:"parallel_tool_calls"`
	PreviousResponseID interface{}              `json:"previous_response_id,omitempty"`
	Reasoning          map[string]interface{}   `json:"reasoning,omitempty"`
	ServiceTier        string                   `json:"service_tier,omitempty"`
	Store              interface{}              `json:"store,omitempty"`
	Temperature        *float64                 `json:"temperature,omitempty"`
	Text               map[string]interface{}   `json:"text,omitempty"`
	ToolChoice         interface{}              `json:"tool_choice,omitempty"`
	Tools              []map[string]interface{} `json:"tools,omitempty"`
	TopP               *float64                 `json:"top_p,omitempty"`
	Truncation         string                   `json:"truncation,omitempty"`
	Usage              *LLMResponsesUsage       `json:"usage,omitempty"`
	User               interface{}              `json:"user,omitempty"`
	Metadata           map[string]interface{}   `json:"metadata,omitempty"`
	Background         bool                     `json:"background,omitempty"`
	Conversation       interface{}              `json:"conversation,omitempty"`
}

type LLMResponsesUsage struct {
	InputTokens         int                    `json:"input_tokens"`
	InputTokensDetails  map[string]interface{} `json:"input_tokens_details,omitempty"`
	OutputTokens        int                    `json:"output_tokens"`
	OutputTokensDetails map[string]interface{} `json:"output_tokens_details,omitempty"`
	TotalTokens         int                    `json:"total_tokens"`
}

// LLMEmbeddingRequest represents an embedding request
type LLMEmbeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model,omitempty"`
}

// LLMEmbeddingResponse represents the embedding response
type LLMEmbeddingResponse struct {
	Model      string         `json:"model"`
	Embeddings [][]float64    `json:"embeddings"`
	Usage      map[string]int `json:"usage"`
}

// Internal types for API communication
type llmChatAPIRequest struct {
	Model          string                  `json:"model"`
	Messages       []llmChatMessage        `json:"messages"`
	MaxTokens      int                     `json:"max_tokens,omitempty"`
	Temperature    float64                 `json:"temperature,omitempty"`
	TopP           float64                 `json:"top_p,omitempty"`
	TopK           int                     `json:"top_k,omitempty"`
	N              int                     `json:"n,omitempty"`
	Stream         bool                    `json:"stream,omitempty"`
	Tools          []core.LLMTool          `json:"tools,omitempty"`
	ToolChoice     interface{}             `json:"tool_choice,omitempty"`
	ResponseFormat *core.LLMResponseFormat `json:"response_format,omitempty"`
}

type llmChatMessage struct {
	Role       string             `json:"role"`
	Content    interface{}        `json:"content"`
	Name       string             `json:"name,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolCalls  []core.LLMToolCall `json:"tool_calls,omitempty"`
}

type llmChatAPIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []llmAPIChoice `json:"choices"`
	Usage   llmAPIUsage    `json:"usage"`
	Error   *llmAPIError   `json:"error,omitempty"`
}

type llmAPIChoice struct {
	Index        int            `json:"index"`
	Message      llmChatMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type llmAPIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type llmAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

type llmEmbeddingAPIRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
}

type llmEmbeddingAPIResponse struct {
	Object string               `json:"object"`
	Data   []llmEmbeddingData   `json:"data"`
	Model  string               `json:"model"`
	Usage  llmEmbeddingAPIUsage `json:"usage"`
	Error  *llmAPIError         `json:"error,omitempty"`
}

type llmEmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type llmEmbeddingAPIUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type llmChatStreamChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []llmStreamChoice `json:"choices"`
	Usage   *llmAPIUsage      `json:"usage,omitempty"`
	Error   *llmAPIError      `json:"error,omitempty"`
}

type llmStreamChoice struct {
	Index        int            `json:"index"`
	Delta        llmStreamDelta `json:"delta"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

type llmStreamDelta struct {
	Role      string              `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	ToolCalls []llmStreamToolCall `json:"tool_calls,omitempty"`
}

type llmStreamToolCall struct {
	Index    int                       `json:"index"`
	ID       string                    `json:"id,omitempty"`
	Type     string                    `json:"type,omitempty"`
	Function llmStreamToolCallFunction `json:"function,omitempty"`
}

type llmStreamToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// LLMChat handles direct LLM chat completion requests
// @Summary LLM Chat Completion
// @Description Send a chat completion request to the configured LLM provider (OpenAI-compatible)
// @Tags LLM
// @Accept json
// @Produce json
// @Param request body LLMChatRequest true "Chat request"
// @Success 200 {object} LLMChatResponse "Chat response"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "LLM error"
// @Security BearerAuth
// @Router /osm/api/llm/v1/chat/completions [post]
func LLMChat(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req LLMChatRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body: " + err.Error(),
			})
		}

		// Validate messages
		if len(req.Messages) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Messages field is required",
			})
		}

		// Validate LLM configuration
		if cfg.LLM.GetProviderCount() == 0 {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "No LLM providers configured",
			})
		}

		// Get provider
		provider := cfg.LLM.GetCurrentProvider()
		if provider == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "No LLM provider available",
			})
		}

		// Build API request
		apiReq := buildLLMChatAPIRequest(&req, cfg, provider)

		// Execute request with retry
		ctx, cancel := context.WithTimeout(c.Context(), 120*time.Second)
		defer cancel()

		response, err := executeLLMChatRequest(ctx, cfg, provider, apiReq)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "LLM request failed: " + err.Error(),
			})
		}

		if response.Error != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": fmt.Sprintf("LLM API error: %s (%s)", response.Error.Message, response.Error.Type),
			})
		}

		// Build response
		result := &LLMChatResponse{
			ID:    response.ID,
			Model: response.Model,
			Usage: map[string]int{
				"prompt_tokens":     response.Usage.PromptTokens,
				"completion_tokens": response.Usage.CompletionTokens,
				"total_tokens":      response.Usage.TotalTokens,
			},
		}

		if len(response.Choices) > 0 {
			choice := response.Choices[0]
			result.Content = choice.Message.Content
			result.FinishReason = choice.FinishReason
			if len(choice.Message.ToolCalls) > 0 {
				result.ToolCalls = choice.Message.ToolCalls
			}
		}

		return c.JSON(result)
	}
}

// LLMResponses handles focused OpenAI-compatible Responses API requests.
// @Summary LLM Responses API
// @Description Send a focused OpenAI-compatible responses request to the configured LLM provider
// @Tags LLM
// @Accept json
// @Produce json
// @Param request body LLMResponsesRequest true "Responses request"
// @Success 200 {object} LLMResponsesResponse "Responses API response"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "LLM error"
// @Security BearerAuth
// @Router /osm/api/llm/v1/responses [post]
func LLMResponses(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req LLMResponsesRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body: " + err.Error(),
			})
		}

		if !hasResponsesInput(req.Input) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Input field is required",
			})
		}

		// Validate LLM configuration
		if cfg.LLM.GetProviderCount() == 0 {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "No LLM providers configured",
			})
		}

		// Get provider
		provider := cfg.LLM.GetCurrentProvider()
		if provider == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "No LLM provider available",
			})
		}

		preparedReq := prepareLLMResponsesRequest(&req, cfg, provider)
		if shouldUseNativeResponsesAPI(&req, provider) {
			if preparedReq.Stream {
				return streamNativeResponsesRequest(c, cfg, provider, preparedReq)
			}

			response, err := executeLLMResponsesRequest(c.Context(), cfg, provider, preparedReq)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   true,
					"message": "LLM request failed: " + err.Error(),
				})
			}

			if response.Error != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error":   true,
					"message": fmt.Sprintf("LLM API error: %v", response.Error["message"]),
				})
			}

			if response.OutputText == "" {
				response.OutputText = extractResponsesOutputText(response.Output)
			}
			return c.JSON(response)
		}

		chatReq, err := buildLLMResponsesChatRequest(preparedReq)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		if len(chatReq.Messages) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Input field is required",
			})
		}

		apiReq := buildLLMChatAPIRequest(chatReq, cfg, provider)
		if preparedReq.Stream {
			apiReq.Stream = true
			return streamResponsesViaChatCompletions(c, cfg, provider, preparedReq, apiReq)
		}

		ctx, cancel := context.WithTimeout(c.Context(), 120*time.Second)
		defer cancel()

		response, err := executeLLMChatRequest(ctx, cfg, provider, apiReq)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "LLM request failed: " + err.Error(),
			})
		}

		if response.Error != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": fmt.Sprintf("LLM API error: %s (%s)", response.Error.Message, response.Error.Type),
			})
		}

		return c.JSON(buildLLMResponsesResponse(preparedReq, response))
	}
}

// LLMEmbedding handles embedding generation requests
// @Summary Generate Embeddings
// @Description Generate embeddings for input text using the configured LLM provider
// @Tags LLM
// @Accept json
// @Produce json
// @Param request body LLMEmbeddingRequest true "Embedding request"
// @Success 200 {object} LLMEmbeddingResponse "Embedding response"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 500 {object} map[string]interface{} "LLM error"
// @Security BearerAuth
// @Router /osm/api/llm/v1/embeddings [post]
func LLMEmbedding(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req LLMEmbeddingRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body: " + err.Error(),
			})
		}

		// Validate input
		if len(req.Input) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Input field is required",
			})
		}

		// Validate LLM configuration
		if cfg.LLM.GetProviderCount() == 0 {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "No LLM providers configured",
			})
		}

		// Get provider
		provider := cfg.LLM.GetCurrentProvider()
		if provider == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "No LLM provider available",
			})
		}

		// Build API request
		apiReq := &llmEmbeddingAPIRequest{
			Model: req.Model,
			Input: req.Input,
		}
		if apiReq.Model == "" {
			apiReq.Model = provider.Model
		}

		// Execute request
		ctx, cancel := context.WithTimeout(c.Context(), 120*time.Second)
		defer cancel()

		response, err := executeLLMEmbeddingRequest(ctx, cfg, provider, apiReq)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Embedding request failed: " + err.Error(),
			})
		}

		if response.Error != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": fmt.Sprintf("Embedding API error: %s (%s)", response.Error.Message, response.Error.Type),
			})
		}

		// Build response
		embeddings := make([][]float64, len(response.Data))
		for i, d := range response.Data {
			embeddings[i] = d.Embedding
		}

		result := &LLMEmbeddingResponse{
			Model:      response.Model,
			Embeddings: embeddings,
			Usage: map[string]int{
				"prompt_tokens": response.Usage.PromptTokens,
				"total_tokens":  response.Usage.TotalTokens,
			},
		}

		return c.JSON(result)
	}
}

// buildLLMChatAPIRequest builds the API request from handler request
func buildLLMChatAPIRequest(req *LLMChatRequest, cfg *config.Config, provider *config.LLMProvider) *llmChatAPIRequest {
	apiReq := &llmChatAPIRequest{
		Model:          req.Model,
		MaxTokens:      req.MaxTokens,
		N:              req.N,
		Stream:         req.Stream,
		Tools:          req.Tools,
		ToolChoice:     req.ToolChoice,
		ResponseFormat: req.ResponseFormat,
	}

	// Use defaults from config if not specified
	if apiReq.Model == "" {
		apiReq.Model = provider.Model
	}
	if apiReq.MaxTokens == 0 {
		apiReq.MaxTokens = cfg.LLM.MaxTokens
	}

	// Temperature - use request value, config default, or 0.7
	if req.Temperature != nil {
		apiReq.Temperature = *req.Temperature
	} else if cfg.LLM.Temperature > 0 {
		apiReq.Temperature = cfg.LLM.Temperature
	} else {
		apiReq.Temperature = 0.7
	}

	// TopP
	if req.TopP != nil {
		apiReq.TopP = *req.TopP
	} else {
		apiReq.TopP = cfg.LLM.TopP
	}

	// TopK
	if req.TopK != nil {
		apiReq.TopK = *req.TopK
	} else {
		apiReq.TopK = cfg.LLM.TopK
	}

	// Convert messages
	messages := make([]llmChatMessage, 0, len(req.Messages)+1)

	// Auto-prepend system prompt if configured and no system message exists
	if cfg.LLM.SystemPrompt != "" {
		hasSystem := false
		for _, msg := range req.Messages {
			if msg.Role == core.LLMRoleSystem {
				hasSystem = true
				break
			}
		}
		if !hasSystem {
			messages = append(messages, llmChatMessage{
				Role:    string(core.LLMRoleSystem),
				Content: cfg.LLM.SystemPrompt,
			})
		}
	}

	for _, msg := range req.Messages {
		messages = append(messages, llmChatMessage{
			Role:       string(msg.Role),
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
			ToolCalls:  msg.ToolCalls,
		})
	}

	apiReq.Messages = messages

	return apiReq
}

func prepareLLMResponsesRequest(req *LLMResponsesRequest, cfg *config.Config, provider *config.LLMProvider) *LLMResponsesRequest {
	prepared := *req
	if prepared.Model == "" {
		prepared.Model = provider.Model
	}
	if prepared.MaxOutputTokens == 0 && cfg.LLM.MaxTokens > 0 {
		prepared.MaxOutputTokens = cfg.LLM.MaxTokens
	}
	if prepared.Temperature == nil {
		temperature := cfg.LLM.Temperature
		if temperature == 0 {
			temperature = 0.7
		}
		prepared.Temperature = &temperature
	}
	if prepared.TopP == nil && cfg.LLM.TopP > 0 {
		topP := cfg.LLM.TopP
		prepared.TopP = &topP
	}
	if prepared.Text == nil {
		prepared.Text = map[string]interface{}{
			"format": map[string]interface{}{
				"type": "text",
			},
		}
	}
	if prepared.Metadata == nil {
		prepared.Metadata = map[string]interface{}{}
	}
	return &prepared
}

func shouldUseNativeResponsesAPI(req *LLMResponsesRequest, provider *config.LLMProvider) bool {
	if providerUsesResponsesEndpoint(provider.BaseURL) {
		return true
	}
	if requestContainsNonFunctionTools(req.Tools) {
		return true
	}
	if req.Background || req.PreviousResponseID != "" || req.Conversation != nil || len(req.Include) > 0 ||
		len(req.Metadata) > 0 || req.Reasoning != nil || req.Text != nil || req.Truncation != "" ||
		req.ParallelToolCalls != nil || req.Store != nil || req.User != nil || req.MaxToolCalls > 0 {
		return true
	}
	return false
}

func providerUsesResponsesEndpoint(baseURL string) bool {
	return strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/responses")
}

func requestContainsNonFunctionTools(tools []map[string]interface{}) bool {
	for _, tool := range tools {
		toolType := strings.TrimSpace(asString(tool["type"]))
		if toolType != "" && toolType != "function" {
			return true
		}
	}
	return false
}

func buildLLMResponsesChatRequest(req *LLMResponsesRequest) (*LLMChatRequest, error) {
	messages, err := convertResponsesInputToMessages(req.Input)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(req.Instructions) != "" {
		messages = append([]core.LLMMessage{
			{
				Role:    core.LLMRoleSystem,
				Content: req.Instructions,
			},
		}, messages...)
	}

	chatReq := &LLMChatRequest{
		Model:       req.Model,
		Messages:    messages,
		MaxTokens:   req.MaxOutputTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		ToolChoice:  req.ToolChoice,
		Stream:      req.Stream,
	}

	if len(req.Tools) > 0 {
		tools, err := convertResponsesTools(req.Tools)
		if err != nil {
			return nil, err
		}
		chatReq.Tools = tools
	}

	return chatReq, nil
}

func convertResponsesInputToMessages(input interface{}) ([]core.LLMMessage, error) {
	switch value := input.(type) {
	case string:
		return []core.LLMMessage{
			{
				Role:    core.LLMRoleUser,
				Content: value,
			},
		}, nil
	case map[string]interface{}:
		msg, err := convertResponsesInputItemToMessage(value)
		if err != nil {
			return nil, err
		}
		return []core.LLMMessage{msg}, nil
	case []interface{}:
		messages := make([]core.LLMMessage, 0, len(value))
		for _, item := range value {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("input array items must be message objects")
			}
			msg, err := convertResponsesInputItemToMessage(obj)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		}
		return messages, nil
	default:
		return nil, fmt.Errorf("input must be a string, message object, or message array")
	}
}

func convertResponsesInputItemToMessage(item map[string]interface{}) (core.LLMMessage, error) {
	role, err := normalizeResponsesRole(asString(item["role"]))
	if err != nil {
		return core.LLMMessage{}, err
	}

	content, err := normalizeResponsesMessageContent(item["content"])
	if err != nil {
		return core.LLMMessage{}, err
	}

	return core.LLMMessage{
		Role:    role,
		Content: content,
	}, nil
}

func normalizeResponsesRole(role string) (core.LLMMessageRole, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "user":
		return core.LLMRoleUser, nil
	case "assistant":
		return core.LLMRoleAssistant, nil
	case "system", "developer":
		return core.LLMRoleSystem, nil
	case "tool":
		return core.LLMRoleTool, nil
	default:
		return "", fmt.Errorf("unsupported input role: %s", role)
	}
}

func normalizeResponsesMessageContent(raw interface{}) (interface{}, error) {
	switch content := raw.(type) {
	case string:
		return content, nil
	case map[string]interface{}:
		part, err := normalizeResponsesContentPart(content)
		if err != nil {
			return nil, err
		}
		if part.Type == core.LLMContentTypeText {
			return part.Text, nil
		}
		return []core.LLMContentPart{part}, nil
	case []interface{}:
		parts := make([]core.LLMContentPart, 0, len(content))
		allText := true
		textParts := make([]string, 0, len(content))
		for _, item := range content {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("message content items must be objects")
			}
			part, err := normalizeResponsesContentPart(obj)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
			if part.Type == core.LLMContentTypeText {
				textParts = append(textParts, part.Text)
				continue
			}
			allText = false
		}
		if len(parts) == 0 {
			return "", nil
		}
		if allText {
			return strings.Join(textParts, "\n"), nil
		}
		return parts, nil
	default:
		return nil, fmt.Errorf("message content must be a string or content-part array")
	}
}

func normalizeResponsesContentPart(item map[string]interface{}) (core.LLMContentPart, error) {
	partType := strings.TrimSpace(asString(item["type"]))
	switch partType {
	case "", "input_text", "text":
		text := asString(item["text"])
		return core.LLMContentPart{
			Type: core.LLMContentTypeText,
			Text: text,
		}, nil
	case "input_image", "image_url":
		url := asString(item["image_url"])
		if url == "" {
			if imageURLObj, ok := item["image_url"].(map[string]interface{}); ok {
				url = asString(imageURLObj["url"])
			}
		}
		if url == "" {
			return core.LLMContentPart{}, fmt.Errorf("input_image content requires image_url")
		}
		detail := asString(item["detail"])
		if detail == "" {
			if imageURLObj, ok := item["image_url"].(map[string]interface{}); ok {
				detail = asString(imageURLObj["detail"])
			}
		}
		return core.LLMContentPart{
			Type: core.LLMContentTypeImageURL,
			ImageURL: &core.LLMImageURL{
				URL:    url,
				Detail: detail,
			},
		}, nil
	default:
		return core.LLMContentPart{}, fmt.Errorf("unsupported message content type: %s", partType)
	}
}

func convertResponsesTools(tools []map[string]interface{}) ([]core.LLMTool, error) {
	result := make([]core.LLMTool, 0, len(tools))
	for _, tool := range tools {
		toolType := strings.TrimSpace(asString(tool["type"]))
		if toolType != "function" {
			return nil, fmt.Errorf("unsupported tool type for compatibility mode: %s", toolType)
		}

		if nested, ok := tool["function"].(map[string]interface{}); ok {
			function, err := buildLLMToolFunction(nested)
			if err != nil {
				return nil, err
			}
			result = append(result, core.LLMTool{
				Type:     "function",
				Function: function,
			})
			continue
		}

		function, err := buildLLMToolFunction(tool)
		if err != nil {
			return nil, err
		}
		result = append(result, core.LLMTool{
			Type:     "function",
			Function: function,
		})
	}
	return result, nil
}

func buildLLMToolFunction(source map[string]interface{}) (core.LLMToolFunction, error) {
	name := strings.TrimSpace(asString(source["name"]))
	if name == "" {
		return core.LLMToolFunction{}, fmt.Errorf("function tool requires name")
	}

	function := core.LLMToolFunction{
		Name:        name,
		Description: asString(source["description"]),
	}
	if params, ok := source["parameters"].(map[string]interface{}); ok {
		function.Parameters = params
	}
	return function, nil
}

func buildLLMResponsesResponse(req *LLMResponsesRequest, response *llmChatAPIResponse) *LLMResponsesResponse {
	result := &LLMResponsesResponse{
		ID:                 response.ID,
		Object:             "response",
		CreatedAt:          response.Created,
		Status:             "completed",
		Model:              response.Model,
		Instructions:       stringOrNil(strings.TrimSpace(req.Instructions)),
		MaxOutputTokens:    intOrNil(req.MaxOutputTokens),
		MaxToolCalls:       intOrNil(req.MaxToolCalls),
		Output:             []map[string]interface{}{},
		ParallelToolCalls:  boolOrDefault(req.ParallelToolCalls, true),
		PreviousResponseID: stringOrNil(strings.TrimSpace(req.PreviousResponseID)),
		Reasoning:          cloneMap(req.Reasoning),
		Store:              boolPtrOrNil(req.Store),
		Temperature:        req.Temperature,
		Text:               cloneMap(req.Text),
		ToolChoice:         req.ToolChoice,
		Tools:              cloneMapSlice(req.Tools),
		TopP:               req.TopP,
		Truncation:         defaultString(req.Truncation, "disabled"),
		Usage: &LLMResponsesUsage{
			InputTokens:         response.Usage.PromptTokens,
			InputTokensDetails:  map[string]interface{}{"cached_tokens": 0},
			OutputTokens:        response.Usage.CompletionTokens,
			OutputTokensDetails: map[string]interface{}{"reasoning_tokens": 0},
			TotalTokens:         response.Usage.TotalTokens,
		},
		User:         req.User,
		Metadata:     cloneMap(req.Metadata),
		Background:   req.Background,
		Conversation: req.Conversation,
	}
	completedAt := response.Created
	result.CompletedAt = &completedAt

	if len(response.Choices) == 0 {
		return result
	}

	choice := response.Choices[0]
	result.Output = buildResponsesOutputItems(response.ID, choice.Message.Content, choice.Message.ToolCalls)
	result.OutputText = extractResponseText(choice.Message.Content)
	return result
}

func buildResponsesOutputItems(responseID string, content interface{}, toolCalls []core.LLMToolCall) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(toolCalls)+1)
	text := extractResponseText(content)
	if text != "" || len(toolCalls) == 0 {
		items = append(items, map[string]interface{}{
			"id":     "msg_" + responseID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]interface{}{
				{
					"type":        "output_text",
					"text":        text,
					"annotations": []interface{}{},
				},
			},
		})
	}
	for i, toolCall := range toolCalls {
		callID := toolCall.ID
		if callID == "" {
			callID = fmt.Sprintf("call_%s_%d", responseID, i)
		}
		items = append(items, map[string]interface{}{
			"id":        callID,
			"type":      "function_call",
			"status":    "completed",
			"call_id":   callID,
			"name":      toolCall.Function.Name,
			"arguments": toolCall.Function.Arguments,
		})
	}
	return items
}

func extractResponseText(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			obj, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			partType := asString(obj["type"])
			switch partType {
			case "", "text", "output_text":
				if text := asString(obj["text"]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case []core.LLMContentPart:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if item.Type == core.LLMContentTypeText && item.Text != "" {
				parts = append(parts, item.Text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func extractResponsesOutputText(output []map[string]interface{}) string {
	parts := make([]string, 0)
	for _, item := range output {
		switch asString(item["type"]) {
		case "message":
			parts = append(parts, extractResponsesOutputMessageContentText(item["content"])...)
		case "refusal":
			parts = append(parts, extractResponsesOutputRefusalText(item)...)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	for _, item := range output {
		if asString(item["type"]) != "reasoning" {
			continue
		}
		parts = append(parts, extractResponsesOutputSummaryText(item["summary"])...)
		if len(parts) == 0 {
			parts = append(parts, extractResponsesOutputMessageContentText(item["content"])...)
		}
	}
	return strings.Join(parts, "\n")
}

func extractResponsesOutputMessageContentText(raw interface{}) []string {
	contentItems, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	parts := make([]string, 0)
	for _, content := range contentItems {
		obj, ok := content.(map[string]interface{})
		if !ok {
			continue
		}
		switch asString(obj["type"]) {
		case "output_text", "text", "summary_text", "reasoning_summary_text":
			if text := asString(obj["text"]); text != "" {
				parts = append(parts, text)
			}
		case "refusal":
			parts = append(parts, extractResponsesOutputRefusalText(obj)...)
		}
	}
	return parts
}

func extractResponsesOutputSummaryText(raw interface{}) []string {
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
		switch asString(obj["type"]) {
		case "summary_text", "reasoning_summary_text", "output_text", "text":
			if text := asString(obj["text"]); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return parts
}

func extractResponsesOutputRefusalText(obj map[string]interface{}) []string {
	parts := make([]string, 0, 1)
	if refusal := asString(obj["refusal"]); refusal != "" {
		parts = append(parts, refusal)
	}
	if len(parts) == 0 {
		if text := asString(obj["text"]); text != "" {
			parts = append(parts, text)
		}
	}
	return parts
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

func executeLLMResponsesRequest(ctx context.Context, cfg *config.Config, provider *config.LLMProvider, req *LLMResponsesRequest) (*LLMResponsesResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, determineResponsesURL(provider.BaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	applyLLMHeaders(httpReq, cfg, provider)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response LLMResponsesResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if resp.StatusCode >= 400 {
		if response.Error != nil {
			return &response, fmt.Errorf("HTTP %d: %v", resp.StatusCode, response.Error["message"])
		}
		return &response, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return &response, nil
}

func streamNativeResponsesRequest(c *fiber.Ctx, cfg *config.Config, provider *config.LLMProvider, req *LLMResponsesRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Failed to marshal request: " + err.Error(),
		})
	}

	httpReq, err := http.NewRequestWithContext(c.Context(), http.MethodPost, determineResponsesURL(provider.BaseURL), bytes.NewReader(body))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Failed to create request: " + err.Error(),
		})
	}
	applyLLMHeaders(httpReq, cfg, provider)
	httpReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "LLM request failed: " + err.Error(),
		})
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": fmt.Sprintf("LLM request failed: HTTP %d: %s", resp.StatusCode, string(respBody)),
		})
	}

	c.Status(resp.StatusCode)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(w, resp.Body)
		_ = w.Flush()
	})
	return nil
}

func streamResponsesViaChatCompletions(c *fiber.Ctx, cfg *config.Config, provider *config.LLMProvider, req *LLMResponsesRequest, apiReq *llmChatAPIRequest) error {
	body, err := json.Marshal(apiReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Failed to marshal request: " + err.Error(),
		})
	}

	httpReq, err := http.NewRequestWithContext(c.Context(), http.MethodPost, provider.BaseURL, bytes.NewReader(body))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "Failed to create request: " + err.Error(),
		})
	}
	applyLLMHeaders(httpReq, cfg, provider)
	httpReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": "LLM request failed: " + err.Error(),
		})
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		respBody, _ := io.ReadAll(resp.Body)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   true,
			"message": fmt.Sprintf("LLM request failed: HTTP %d: %s", resp.StatusCode, string(respBody)),
		})
	}

	c.Status(resp.StatusCode)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var responseID, model, role, finishReason string
		var createdAt int64
		var contentBuilder strings.Builder
		usage := llmAPIUsage{}
		toolCallMap := make(map[int]*core.LLMToolCall)
		itemOpened := false
		contentOpened := false

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk llmChatStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			if chunk.Error != nil {
				_ = writeSSEEvent(w, "response.error", map[string]interface{}{
					"type": "response.error",
					"error": map[string]interface{}{
						"message": chunk.Error.Message,
						"type":    chunk.Error.Type,
						"code":    chunk.Error.Code,
					},
				})
				return
			}

			if responseID == "" {
				responseID = chunk.ID
			}
			if responseID == "" {
				responseID = fmt.Sprintf("resp_%d", time.Now().UnixNano())
			}
			if model == "" {
				model = chunk.Model
			}
			if createdAt == 0 {
				createdAt = chunk.Created
				if createdAt == 0 {
					createdAt = time.Now().Unix()
				}
				baseResponse := buildStreamingEnvelope(req, responseID, model, createdAt, "in_progress")
				_ = writeSSEEvent(w, "response.created", map[string]interface{}{
					"type":     "response.created",
					"response": baseResponse,
				})
				_ = writeSSEEvent(w, "response.in_progress", map[string]interface{}{
					"type":     "response.in_progress",
					"response": baseResponse,
				})
			}

			if chunk.Usage != nil {
				usage = *chunk.Usage
			}

			for _, choice := range chunk.Choices {
				if choice.Delta.Role != "" {
					role = choice.Delta.Role
				}
				if choice.Delta.Content != "" {
					if !itemOpened {
						itemOpened = true
						_ = writeSSEEvent(w, "response.output_item.added", map[string]interface{}{
							"type":         "response.output_item.added",
							"output_index": 0,
							"item": map[string]interface{}{
								"id":      "msg_" + responseID,
								"type":    "message",
								"status":  "in_progress",
								"role":    defaultString(role, "assistant"),
								"content": []interface{}{},
							},
						})
					}
					if !contentOpened {
						contentOpened = true
						_ = writeSSEEvent(w, "response.content_part.added", map[string]interface{}{
							"type":          "response.content_part.added",
							"item_id":       "msg_" + responseID,
							"output_index":  0,
							"content_index": 0,
							"part": map[string]interface{}{
								"type":        "output_text",
								"text":        "",
								"annotations": []interface{}{},
							},
						})
					}
					contentBuilder.WriteString(choice.Delta.Content)
					_ = writeSSEEvent(w, "response.output_text.delta", map[string]interface{}{
						"type":          "response.output_text.delta",
						"item_id":       "msg_" + responseID,
						"output_index":  0,
						"content_index": 0,
						"delta":         choice.Delta.Content,
					})
				}
				for _, tc := range choice.Delta.ToolCalls {
					existing, ok := toolCallMap[tc.Index]
					if !ok {
						existing = &core.LLMToolCall{Type: "function"}
						toolCallMap[tc.Index] = existing
					}
					if tc.ID != "" {
						existing.ID = tc.ID
					}
					if tc.Type != "" {
						existing.Type = tc.Type
					}
					if tc.Function.Name != "" {
						existing.Function.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						existing.Function.Arguments += tc.Function.Arguments
					}
				}
				if choice.FinishReason != "" {
					finishReason = choice.FinishReason
				}
			}
		}

		if createdAt == 0 {
			return
		}

		if contentOpened {
			finalText := contentBuilder.String()
			_ = writeSSEEvent(w, "response.output_text.done", map[string]interface{}{
				"type":          "response.output_text.done",
				"item_id":       "msg_" + responseID,
				"output_index":  0,
				"content_index": 0,
				"text":          finalText,
			})
			_ = writeSSEEvent(w, "response.content_part.done", map[string]interface{}{
				"type":          "response.content_part.done",
				"item_id":       "msg_" + responseID,
				"output_index":  0,
				"content_index": 0,
				"part": map[string]interface{}{
					"type":        "output_text",
					"text":        finalText,
					"annotations": []interface{}{},
				},
			})
			_ = writeSSEEvent(w, "response.output_item.done", map[string]interface{}{
				"type":         "response.output_item.done",
				"output_index": 0,
				"item": map[string]interface{}{
					"id":     "msg_" + responseID,
					"type":   "message",
					"status": "completed",
					"role":   defaultString(role, "assistant"),
					"content": []map[string]interface{}{
						{
							"type":        "output_text",
							"text":        finalText,
							"annotations": []interface{}{},
						},
					},
				},
			})
		}

		toolCalls := orderedToolCalls(toolCallMap)
		finalChatResponse := &llmChatAPIResponse{
			ID:      responseID,
			Object:  "chat.completion",
			Created: createdAt,
			Model:   model,
			Choices: []llmAPIChoice{
				{
					Index: 0,
					Message: llmChatMessage{
						Role:      defaultString(role, "assistant"),
						Content:   contentBuilder.String(),
						ToolCalls: toolCalls,
					},
					FinishReason: finishReason,
				},
			},
			Usage: usage,
		}
		finalResponse := buildLLMResponsesResponse(req, finalChatResponse)
		_ = writeSSEEvent(w, "response.completed", map[string]interface{}{
			"type":     "response.completed",
			"response": finalResponse,
		})
	})
	return nil
}

func orderedToolCalls(toolCallMap map[int]*core.LLMToolCall) []core.LLMToolCall {
	if len(toolCallMap) == 0 {
		return nil
	}
	toolCalls := make([]core.LLMToolCall, len(toolCallMap))
	for idx, tc := range toolCallMap {
		if idx >= 0 && idx < len(toolCalls) {
			toolCalls[idx] = *tc
		}
	}
	return toolCalls
}

func buildStreamingEnvelope(req *LLMResponsesRequest, responseID, model string, createdAt int64, status string) *LLMResponsesResponse {
	return &LLMResponsesResponse{
		ID:                 responseID,
		Object:             "response",
		CreatedAt:          createdAt,
		Status:             status,
		Model:              model,
		Instructions:       stringOrNil(strings.TrimSpace(req.Instructions)),
		MaxOutputTokens:    intOrNil(req.MaxOutputTokens),
		MaxToolCalls:       intOrNil(req.MaxToolCalls),
		Output:             []map[string]interface{}{},
		ParallelToolCalls:  boolOrDefault(req.ParallelToolCalls, true),
		PreviousResponseID: stringOrNil(strings.TrimSpace(req.PreviousResponseID)),
		Reasoning:          cloneMap(req.Reasoning),
		Store:              boolPtrOrNil(req.Store),
		Temperature:        req.Temperature,
		Text:               cloneMap(req.Text),
		ToolChoice:         req.ToolChoice,
		Tools:              cloneMapSlice(req.Tools),
		TopP:               req.TopP,
		Truncation:         defaultString(req.Truncation, "disabled"),
		User:               req.User,
		Metadata:           cloneMap(req.Metadata),
		Background:         req.Background,
		Conversation:       req.Conversation,
	}
}

func writeSSEEvent(w *bufio.Writer, event string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	return w.Flush()
}

func applyLLMHeaders(req *http.Request, cfg *config.Config, provider *config.LLMProvider) {
	req.Header.Set("Content-Type", "application/json")
	if provider.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+provider.AuthToken)
	}
	if cfg.LLM.CustomHeaders != "" {
		for _, h := range strings.Split(cfg.LLM.CustomHeaders, ",") {
			if parts := strings.SplitN(strings.TrimSpace(h), ":", 2); len(parts) == 2 {
				req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	cloned := make(map[string]interface{}, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneMapSlice(input []map[string]interface{}) []map[string]interface{} {
	if input == nil {
		return nil
	}
	cloned := make([]map[string]interface{}, 0, len(input))
	for _, item := range input {
		cloned = append(cloned, cloneMap(item))
	}
	return cloned
}

func hasResponsesInput(input interface{}) bool {
	if input == nil {
		return false
	}
	switch value := input.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case []interface{}:
		return len(value) > 0
	case map[string]interface{}:
		return len(value) > 0
	default:
		return true
	}
}

func intOrNil(value int) interface{} {
	if value <= 0 {
		return nil
	}
	return value
}

func stringOrNil(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func boolPtrOrNil(value *bool) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func asString(raw interface{}) string {
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

// executeLLMChatRequest executes an HTTP request to the LLM provider
func executeLLMChatRequest(ctx context.Context, cfg *config.Config, provider *config.LLMProvider, apiReq *llmChatAPIRequest) (*llmChatAPIResponse, error) {
	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", provider.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if provider.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+provider.AuthToken)
	}

	// Parse custom headers
	if cfg.LLM.CustomHeaders != "" {
		for _, h := range strings.Split(cfg.LLM.CustomHeaders, ",") {
			if parts := strings.SplitN(strings.TrimSpace(h), ":", 2); len(parts) == 2 {
				req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response llmChatAPIResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if resp.StatusCode >= 400 {
		if response.Error != nil {
			return &response, fmt.Errorf("HTTP %d: %s", resp.StatusCode, response.Error.Message)
		}
		return &response, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return &response, nil
}

// executeLLMEmbeddingRequest executes an embedding request to the LLM provider
func executeLLMEmbeddingRequest(ctx context.Context, cfg *config.Config, provider *config.LLMProvider, apiReq *llmEmbeddingAPIRequest) (*llmEmbeddingAPIResponse, error) {
	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Determine embedding endpoint
	embeddingURL := provider.BaseURL
	if strings.HasSuffix(embeddingURL, "/chat/completions") {
		embeddingURL = strings.Replace(embeddingURL, "/chat/completions", "/embeddings", 1)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", embeddingURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if provider.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+provider.AuthToken)
	}

	// Parse custom headers
	if cfg.LLM.CustomHeaders != "" {
		for _, h := range strings.Split(cfg.LLM.CustomHeaders, ",") {
			if parts := strings.SplitN(strings.TrimSpace(h), ":", 2); len(parts) == 2 {
				req.Header.Set(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response llmEmbeddingAPIResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, string(respBody))
	}

	if resp.StatusCode >= 400 {
		if response.Error != nil {
			return &response, fmt.Errorf("HTTP %d: %s", resp.StatusCode, response.Error.Message)
		}
		return &response, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return &response, nil
}
