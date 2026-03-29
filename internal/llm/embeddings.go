package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/retry"
)

var embeddingRequestGate struct {
	mu   sync.Mutex
	last time.Time
}

type embeddingAPIRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
}

type embeddingAPIResponse struct {
	Object string             `json:"object"`
	Data   []embeddingAPIData `json:"data"`
	Model  string             `json:"model"`
	Usage  map[string]int     `json:"usage"`
	Error  *embeddingAPIError `json:"error,omitempty"`
}

type embeddingAPIData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

type embeddingAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// GenerateEmbeddings generates embeddings using the configured OpenAI-compatible providers.
func GenerateEmbeddings(ctx context.Context, cfg *config.Config, input []string, modelOverride string) ([][]float64, string, error) {
	if err := validateEmbeddingConfig(cfg, input); err != nil {
		return nil, "", err
	}

	timeout := getEmbeddingTimeout(cfg)

	if cfg.IsEmbeddingsConfigEnabled() {
		providerName := strings.TrimSpace(cfg.Embeddings.Provider)
		if providerName == "" {
			return nil, "", fmt.Errorf("embeddings_config.provider is empty")
		}
		provider, _, err := cfg.ResolveEmbeddingProvider(providerName)
		if err != nil {
			return nil, "", err
		}

		model := strings.TrimSpace(modelOverride)
		if model == "" {
			model = strings.TrimSpace(provider.Model)
		}
		if model == "" {
			return nil, "", fmt.Errorf("embedding model is empty for provider %q", providerName)
		}
		return requestEmbeddings(ctx, cfg, provider, input, model, timeout)
	}

	maxRetries := cfg.LLM.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2
	}
	totalAttempts := maxRetries * cfg.LLM.GetProviderCount()
	if totalAttempts < 1 {
		totalAttempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < totalAttempts; attempt++ {
		provider := cfg.LLM.GetCurrentProvider()
		if provider == nil {
			return nil, "", fmt.Errorf("no LLM providers available")
		}

		model := strings.TrimSpace(modelOverride)
		if model == "" {
			model = strings.TrimSpace(provider.Model)
		}

		embeddings, usedModel, err := requestEmbeddings(ctx, cfg, provider, input, model, timeout)
		if err == nil {
			return embeddings, usedModel, nil
		}

		lastErr = err
		if cfg.LLM.GetProviderCount() > 1 {
			cfg.LLM.RotateProvider()
		}
	}

	return nil, "", lastErr
}

// GenerateEmbeddingsWithProvider generates embeddings using the explicitly selected provider.
func GenerateEmbeddingsWithProvider(ctx context.Context, cfg *config.Config, providerName string, input []string, modelOverride string) ([][]float64, string, error) {
	if err := validateEmbeddingConfig(cfg, input); err != nil {
		return nil, "", err
	}

	provider, err := resolveEmbeddingProvider(cfg, providerName)
	if err != nil {
		return nil, "", err
	}

	model := strings.TrimSpace(modelOverride)
	if model == "" {
		model = strings.TrimSpace(provider.Model)
	}
	if model == "" {
		return nil, "", fmt.Errorf("embedding model is empty for provider %q", strings.TrimSpace(providerName))
	}

	return requestEmbeddings(ctx, cfg, provider, input, model, getEmbeddingTimeout(cfg))
}

func validateEmbeddingConfig(cfg *config.Config, input []string) error {
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}
	if len(input) == 0 {
		return fmt.Errorf("input is required")
	}
	if cfg.IsEmbeddingsConfigEnabled() {
		return nil
	}
	if cfg.LLM.GetProviderCount() == 0 {
		return fmt.Errorf("no LLM providers configured")
	}
	return nil
}

func getEmbeddingTimeout(cfg *config.Config) time.Duration {
	timeout := 120 * time.Second
	if parsed, err := time.ParseDuration(strings.TrimSpace(cfg.LLM.Timeout)); err == nil && parsed > 0 {
		timeout = parsed
	}
	return timeout
}

func getEmbeddingRetryConfig(cfg *config.Config) retry.Config {
	retryCfg := retry.DefaultConfig()

	if attempts := cfg.LLM.MaxRetries; attempts > 0 {
		retryCfg.MaxAttempts = attempts
	}
	if parsed, err := time.ParseDuration(strings.TrimSpace(cfg.LLM.RetryDelay)); err == nil && parsed > 0 {
		retryCfg.InitialDelay = parsed
	}
	if cfg.LLM.RetryBackoff {
		retryCfg.Multiplier = 2.0
		maxDelay := retryCfg.InitialDelay * 8
		if maxDelay < retryCfg.InitialDelay {
			maxDelay = retryCfg.InitialDelay
		}
		retryCfg.MaxDelay = maxDelay
	} else {
		retryCfg.Multiplier = 1.0
		retryCfg.MaxDelay = retryCfg.InitialDelay
	}

	return retryCfg
}

func getEmbeddingRequestDelay(cfg *config.Config) time.Duration {
	if cfg == nil {
		return 0
	}
	if parsed, err := time.ParseDuration(strings.TrimSpace(cfg.LLM.RequestDelay)); err == nil && parsed > 0 {
		return parsed
	}
	return 0
}

func waitForEmbeddingRequestSlot(ctx context.Context, cfg *config.Config) error {
	delay := getEmbeddingRequestDelay(cfg)
	if delay <= 0 {
		return nil
	}

	embeddingRequestGate.mu.Lock()
	defer embeddingRequestGate.mu.Unlock()

	if !embeddingRequestGate.last.IsZero() {
		wait := time.Until(embeddingRequestGate.last.Add(delay))
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}

	embeddingRequestGate.last = time.Now()
	return nil
}

func resolveEmbeddingProvider(cfg *config.Config, providerName string) (*config.LLMProvider, error) {
	provider, _, err := cfg.ResolveEmbeddingProvider(providerName)
	return provider, err
}

func listEmbeddingProviders(cfg *config.Config) []string {
	names := cfg.ListEmbeddingProviders()
	if len(names) == 0 {
		return []string{"none"}
	}
	return names
}

func requestEmbeddings(ctx context.Context, cfg *config.Config, provider *config.LLMProvider, input []string, model string, timeout time.Duration) ([][]float64, string, error) {
	reqBody := &embeddingAPIRequest{
		Model:          model,
		Input:          input,
		EncodingFormat: "float",
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	embeddingURL := strings.TrimSpace(provider.BaseURL)
	if strings.HasSuffix(embeddingURL, "/chat/completions") {
		embeddingURL = strings.Replace(embeddingURL, "/chat/completions", "/embeddings", 1)
	}
	if embeddingURL == "" {
		return nil, "", fmt.Errorf("embedding endpoint is empty")
	}

	client := &http.Client{Timeout: timeout}
	headers := parseCustomHeaders(cfg.LLM.CustomHeaders)
	if strings.TrimSpace(provider.AuthToken) != "" {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(provider.AuthToken)
	}
	headers["Content-Type"] = "application/json"

	var response embeddingAPIResponse
	retryCfg := getEmbeddingRetryConfig(cfg)
	err = retry.Do(ctx, retryCfg, func() error {
		if err := waitForEmbeddingRequestSlot(ctx, cfg); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, embeddingURL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create embedding request: %w", err)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := client.Do(req)
		if err != nil {
			return retry.Retryable(fmt.Errorf("embedding request failed: %w", err))
		}
		defer func() { _ = resp.Body.Close() }()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return retry.Retryable(fmt.Errorf("failed to read embedding response: %w", err))
		}

		var parsed embeddingAPIResponse
		parseErr := json.Unmarshal(respBody, &parsed)

		if shouldRetryEmbeddingResponse(resp.StatusCode, &parsed) {
			return retry.Retryable(buildEmbeddingResponseError(resp.StatusCode, &parsed))
		}
		if resp.StatusCode >= 400 {
			return buildEmbeddingResponseError(resp.StatusCode, &parsed)
		}
		if parseErr != nil {
			return fmt.Errorf("failed to parse embedding response: %w", parseErr)
		}
		if parsed.Error != nil {
			if isEmbeddingRateLimitError(parsed.Error) {
				return retry.Retryable(buildEmbeddingResponseError(resp.StatusCode, &parsed))
			}
			return buildEmbeddingResponseError(resp.StatusCode, &parsed)
		}

		response = parsed
		return nil
	})
	if err != nil {
		return nil, "", err
	}

	embeddings := make([][]float64, len(response.Data))
	for _, item := range response.Data {
		if item.Index >= 0 && item.Index < len(embeddings) {
			embeddings[item.Index] = item.Embedding
		}
	}
	return embeddings, response.Model, nil
}

func parseCustomHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return headers
	}
	for _, item := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(item), ":", 2)
		if len(parts) != 2 {
			continue
		}
		headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return headers
}

func shouldRetryEmbeddingResponse(statusCode int, response *embeddingAPIResponse) bool {
	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return true
	}
	return isEmbeddingRateLimitError(response.Error)
}

func isEmbeddingRateLimitError(apiErr *embeddingAPIError) bool {
	if apiErr == nil {
		return false
	}
	return apiErr.Type == "rate_limit_error" ||
		strings.Contains(strings.ToLower(apiErr.Code), "rate_limit") ||
		strings.Contains(strings.ToLower(apiErr.Message), "rate limit")
}

func buildEmbeddingResponseError(statusCode int, response *embeddingAPIResponse) error {
	if response != nil && response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return fmt.Errorf("embedding API error: %s", response.Error.Message)
	}
	if statusCode > 0 {
		return fmt.Errorf("embedding HTTP %d", statusCode)
	}
	return fmt.Errorf("embedding request failed")
}
