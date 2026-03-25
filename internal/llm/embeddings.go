package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
)

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

func resolveEmbeddingProvider(cfg *config.Config, providerName string) (*config.LLMProvider, error) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		return nil, fmt.Errorf("provider is required")
	}

	var match *config.LLMProvider
	for i := range cfg.LLM.LLMProviders {
		provider := &cfg.LLM.LLMProviders[i]
		if !strings.EqualFold(strings.TrimSpace(provider.Provider), name) {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("multiple LLM providers match %q; provider names must be unique for embeddings", name)
		}
		match = provider
	}
	if match == nil {
		return nil, fmt.Errorf("LLM provider %q is not configured (available: %s)", name, strings.Join(listEmbeddingProviders(cfg), ", "))
	}
	return match, nil
}

func listEmbeddingProviders(cfg *config.Config) []string {
	seen := make(map[string]struct{}, len(cfg.LLM.LLMProviders))
	names := make([]string, 0, len(cfg.LLM.LLMProviders))
	for _, provider := range cfg.LLM.LLMProviders {
		name := strings.TrimSpace(provider.Provider)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
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

	req, err := http.NewRequestWithContext(ctx, "POST", embeddingURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(provider.AuthToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(provider.AuthToken))
	}

	for key, value := range parseCustomHeaders(cfg.LLM.CustomHeaders) {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("embedding request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read embedding response: %w", err)
	}

	var response embeddingAPIResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, "", fmt.Errorf("failed to parse embedding response: %w", err)
	}
	if resp.StatusCode >= 400 {
		if response.Error != nil {
			return nil, "", fmt.Errorf("embedding API error: %s", response.Error.Message)
		}
		return nil, "", fmt.Errorf("embedding HTTP %d", resp.StatusCode)
	}
	if response.Error != nil {
		return nil, "", fmt.Errorf("embedding API error: %s", response.Error.Message)
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
