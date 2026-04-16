package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type openAIRerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n"`
	ReturnDocuments bool     `json:"return_documents"`
}

type openAIRerankResponse struct {
	ID      string                `json:"id"`
	Error   *openAIRerankAPIError `json:"error,omitempty"`
	Results []openAIRerankResult  `json:"results"`
}

type openAIRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type openAIRerankAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

func (c *Client) rerankOpenAI(ctx context.Context, req Request) (*Response, error) {
	providerName, err := c.providerName()
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(c.provider.BaseURL) == "" {
		return nil, fmt.Errorf("rerank base URL is required")
	}

	rerankURL, err := resolveOpenAIRerankURL(c.provider.BaseURL)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(req.ModelOverride)
	if model == "" {
		model = strings.TrimSpace(c.provider.Model)
	}

	if len(req.Documents) == 0 {
		return &Response{
			Provider: providerName,
			Model:    model,
			Results:  []Result{},
		}, nil
	}

	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("rerank query is required")
	}
	if model == "" {
		return nil, fmt.Errorf("rerank model is required")
	}

	documents := req.Documents
	if req.MaxCandidates > 0 && len(documents) > req.MaxCandidates {
		documents = documents[:req.MaxCandidates]
	}

	requestBody := openAIRerankRequest{
		Model:           model,
		Query:           strings.TrimSpace(req.Query),
		Documents:       make([]string, 0, len(documents)),
		TopN:            len(documents),
		ReturnDocuments: true,
	}
	for _, document := range documents {
		requestBody.Documents = append(requestBody.Documents, document.Text)
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rerank request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, rerankURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create rerank request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(c.provider.AuthToken); token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	httpClient := c.http
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("rerank request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read rerank response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, buildOpenAIRerankHTTPError(resp.StatusCode, respBody)
	}

	var apiResp openAIRerankResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse rerank response: %w (body: %s)", err, summarizeBody(respBody))
	}
	if apiResp.Error != nil && strings.TrimSpace(apiResp.Error.Message) != "" {
		return nil, fmt.Errorf("rerank API error: %s", strings.TrimSpace(apiResp.Error.Message))
	}

	results := make([]Result, 0, len(apiResp.Results))
	for _, item := range apiResp.Results {
		if item.Index < 0 || item.Index >= len(documents) {
			return nil, fmt.Errorf("invalid rerank result index %d for %d documents", item.Index, len(documents))
		}

		document := documents[item.Index]
		results = append(results, Result{
			ID:       document.ID,
			Index:    item.Index,
			Score:    item.RelevanceScore,
			Text:     document.Text,
			Metadata: cloneMetadata(document.Metadata),
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Index < results[j].Index
		}
		return results[i].Score > results[j].Score
	})

	if req.MinScore > 0 {
		filtered := results[:0]
		for _, item := range results {
			if item.Score >= req.MinScore {
				filtered = append(filtered, item)
			}
		}
		results = filtered
	}

	if req.TopN > 0 && len(results) > req.TopN {
		results = results[:req.TopN]
	}

	return &Response{
		Provider: providerName,
		Model:    model,
		Results:  results,
	}, nil
}

func buildOpenAIRerankHTTPError(statusCode int, body []byte) error {
	var apiResp openAIRerankResponse
	if err := json.Unmarshal(body, &apiResp); err == nil {
		if apiResp.Error != nil && strings.TrimSpace(apiResp.Error.Message) != "" {
			return fmt.Errorf("HTTP %d: %s", statusCode, strings.TrimSpace(apiResp.Error.Message))
		}
	}
	return fmt.Errorf("HTTP %d: %s", statusCode, summarizeBody(body))
}

func summarizeBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "<empty response>"
	}

	text = strings.Join(strings.Fields(text), " ")
	const maxLen = 240
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}

func cloneMetadata(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}
