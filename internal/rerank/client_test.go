package rerank

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClientRerankOpenAICompatible(t *testing.T) {
	t.Parallel()

	type rerankRequest struct {
		Model           string   `json:"model"`
		Query           string   `json:"query"`
		Documents       []string `json:"documents"`
		TopN            int      `json:"top_n"`
		ReturnDocuments bool     `json:"return_documents"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/rerank", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var payload rerankRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "Pro/BAAI/bge-reranker-v2-m3", payload.Model)
		require.Equal(t, "what is rerank", payload.Query)
		require.Equal(t, []string{"doc one", "doc two"}, payload.Documents)
		require.Equal(t, 2, payload.TopN)
		require.True(t, payload.ReturnDocuments)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"id": "rerank-123",
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.98},
				{"index": 0, "relevance_score": 0.72},
			},
		}))
	}))
	defer server.Close()

	client := NewClient(&config.LLMProvider{
		Provider:  "openai",
		BaseURL:   server.URL + "/v1",
		AuthToken: "test-key",
		Model:     "Pro/BAAI/bge-reranker-v2-m3",
	}, 5*time.Second)

	resp, err := client.Rerank(context.Background(), Request{
		Query: "what is rerank",
		Documents: []Document{
			{ID: "doc-1", Text: "doc one"},
			{ID: "doc-2", Text: "doc two", Metadata: map[string]string{"source": "kb"}},
			{ID: "doc-3", Text: "doc three"},
		},
		TopN:          2,
		MaxCandidates: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "openai", resp.Provider)
	require.Equal(t, "Pro/BAAI/bge-reranker-v2-m3", resp.Model)
	require.Len(t, resp.Results, 2)

	require.Equal(t, "doc-2", resp.Results[0].ID)
	require.Equal(t, 1, resp.Results[0].Index)
	require.Equal(t, 0.98, resp.Results[0].Score)
	require.Equal(t, "doc two", resp.Results[0].Text)
	require.Equal(t, map[string]string{"source": "kb"}, resp.Results[0].Metadata)

	require.Equal(t, "doc-1", resp.Results[1].ID)
	require.Equal(t, 0, resp.Results[1].Index)
	require.Equal(t, 0.72, resp.Results[1].Score)
	require.Equal(t, "doc one", resp.Results[1].Text)
}

func TestClientRerankOpenAICompatibleKeepsExistingRerankPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/rerank", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"id": "rerank-keep",
			"results": []map[string]any{
				{"index": 0, "relevance_score": 0.99},
			},
		}))
	}))
	defer server.Close()

	client := NewClient(&config.LLMProvider{
		Provider: "openai",
		BaseURL:  server.URL + "/v1/rerank",
		Model:    "model",
	}, 5*time.Second)

	resp, err := client.Rerank(context.Background(), Request{
		Query:     "query",
		Documents: []Document{{ID: "doc-1", Text: "hello"}},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "doc-1", resp.Results[0].ID)
}

func TestResolveOpenAIRerankURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		wantURL string
	}{
		{
			name:    "append rerank",
			baseURL: "http://host/v1",
			wantURL: "http://host/v1/rerank",
		},
		{
			name:    "trim trailing slash before append",
			baseURL: "http://host/v1/",
			wantURL: "http://host/v1/rerank",
		},
		{
			name:    "keep existing rerank path",
			baseURL: "http://host/v1/rerank",
			wantURL: "http://host/v1/rerank",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotURL, err := resolveOpenAIRerankURL(tt.baseURL)
			require.NoError(t, err)
			require.Equal(t, tt.wantURL, gotURL)
		})
	}
}

func TestResolveOpenAIRerankURLRejectsQueryOrFragment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		wantText string
	}{
		{
			name:     "reject query",
			baseURL:  "http://host/v1?foo=bar",
			wantText: "must not include query",
		},
		{
			name:     "reject fragment",
			baseURL:  "http://host/v1#frag",
			wantText: "must not include fragment",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveOpenAIRerankURL(tt.baseURL)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantText)
		})
	}
}

func TestClientRerankRejectsUnsupportedProvider(t *testing.T) {
	t.Parallel()

	client := NewClient(&config.LLMProvider{
		Provider: "cohere",
		BaseURL:  "http://example.test/rerank",
		Model:    "rerank-v1",
	}, time.Second)

	_, err := client.Rerank(context.Background(), Request{
		Query:     "query",
		Documents: []Document{{ID: "doc-1", Text: "hello"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported rerank provider")
}

func TestClientRerankRequiresQueryOrModelOrBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		client   *Client
		req      Request
		wantText string
	}{
		{
			name:   "provider is required",
			client: NewClient(nil, time.Second),
			req: Request{
				Query:     "query",
				Documents: []Document{{ID: "doc-1", Text: "hello"}},
			},
			wantText: "rerank provider is required",
		},
		{
			name: "base url is required",
			client: NewClient(&config.LLMProvider{
				Provider: "openai",
				Model:    "model",
			}, time.Second),
			req: Request{
				Query:     "query",
				Documents: []Document{{ID: "doc-1", Text: "hello"}},
			},
			wantText: "rerank base URL is required",
		},
		{
			name: "query is required",
			client: NewClient(&config.LLMProvider{
				Provider: "openai",
				BaseURL:  "http://example.test/rerank",
				Model:    "model",
			}, time.Second),
			req: Request{
				Documents: []Document{{ID: "doc-1", Text: "hello"}},
			},
			wantText: "rerank query is required",
		},
		{
			name: "model is required",
			client: NewClient(&config.LLMProvider{
				Provider: "openai",
				BaseURL:  "http://example.test/rerank",
			}, time.Second),
			req: Request{
				Query:     "query",
				Documents: []Document{{ID: "doc-1", Text: "hello"}},
			},
			wantText: "rerank model is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.client.Rerank(context.Background(), tt.req)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantText)
		})
	}
}

func TestClientRerankReturnsEmptyOnNoDocuments(t *testing.T) {
	t.Parallel()

	client := NewClient(&config.LLMProvider{
		Provider: "openai",
		BaseURL:  "http://example.test/rerank",
		Model:    "model",
	}, time.Second)

	resp, err := client.Rerank(context.Background(), Request{
		Query: "query",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "openai", resp.Provider)
	require.Equal(t, "model", resp.Model)
	require.Empty(t, resp.Results)
}

func TestClientRerankAppliesMinScoreAndTopN(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"id": "rerank-456",
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.44},
				{"index": 3, "relevance_score": 0.87},
				{"index": 0, "relevance_score": 0.62},
				{"index": 2, "relevance_score": 0.91},
			},
		}))
	}))
	defer server.Close()

	client := NewClient(&config.LLMProvider{
		Provider: "openai",
		BaseURL:  server.URL + "/v1/rerank",
		Model:    "model",
	}, time.Second)

	resp, err := client.Rerank(context.Background(), Request{
		Query: "query",
		Documents: []Document{
			{ID: "doc-0", Text: "zero"},
			{ID: "doc-1", Text: "one"},
			{ID: "doc-2", Text: "two"},
			{ID: "doc-3", Text: "three"},
		},
		TopN:     2,
		MinScore: 0.6,
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	require.Equal(t, "doc-2", resp.Results[0].ID)
	require.Equal(t, 0.91, resp.Results[0].Score)
	require.Equal(t, "doc-3", resp.Results[1].ID)
	require.Equal(t, 0.87, resp.Results[1].Score)
}

func TestClientRerankHandlesNon2xx(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(&config.LLMProvider{
		Provider: "openai",
		BaseURL:  server.URL + "/v1/rerank",
		Model:    "model",
	}, time.Second)

	_, err := client.Rerank(context.Background(), Request{
		Query:     "query",
		Documents: []Document{{ID: "doc-1", Text: "hello"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("HTTP %d", http.StatusBadGateway))
	require.Contains(t, err.Error(), "upstream bad gateway")
}

func TestClientRerankRejectsInvalidResultIndex(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"id": "rerank-invalid-index",
			"results": []map[string]any{
				{"index": 2, "relevance_score": 0.99},
			},
		}))
	}))
	defer server.Close()

	client := NewClient(&config.LLMProvider{
		Provider: "openai",
		BaseURL:  server.URL + "/v1",
		Model:    "model",
	}, time.Second)

	_, err := client.Rerank(context.Background(), Request{
		Query:     "query",
		Documents: []Document{{ID: "doc-1", Text: "hello"}},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid rerank result index")
}
