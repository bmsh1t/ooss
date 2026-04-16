package rerank

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
)

type Client struct {
	provider *config.LLMProvider
	http     *http.Client
}

func NewClient(provider *config.LLMProvider, timeout time.Duration) *Client {
	return &Client{
		provider: provider,
		http:     &http.Client{Timeout: timeout},
	}
}

func (c *Client) Rerank(ctx context.Context, req Request) (*Response, error) {
	providerName, err := c.providerName()
	if err != nil {
		return nil, err
	}

	switch providerName {
	case "openai":
		return c.rerankOpenAI(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported rerank provider %q", strings.TrimSpace(c.provider.Provider))
	}
}

func (c *Client) providerName() (string, error) {
	if c == nil || c.provider == nil {
		return "", fmt.Errorf("rerank provider is required")
	}

	providerName := strings.ToLower(strings.TrimSpace(c.provider.Provider))
	if providerName == "" {
		return "", fmt.Errorf("rerank provider is required")
	}

	return providerName, nil
}

func resolveOpenAIRerankURL(baseURL string) (string, error) {
	normalized := strings.TrimSpace(baseURL)
	if normalized == "" {
		return "", fmt.Errorf("rerank base URL is required")
	}

	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", fmt.Errorf("invalid rerank base URL: %w", err)
	}
	if parsed.RawQuery != "" {
		return "", fmt.Errorf("rerank base URL must not include query parameters")
	}
	if parsed.Fragment != "" {
		return "", fmt.Errorf("rerank base URL must not include fragment")
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(parsed.Path, "/rerank") {
		if parsed.Path == "" {
			parsed.Path = "/rerank"
		} else {
			parsed.Path += "/rerank"
		}
	}
	parsed.RawPath = ""

	return parsed.String(), nil
}
