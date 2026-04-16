package vectorkb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/require"
)

func TestSearchAppliesRerankWhenEnabled(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	rerankServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/rerank", r.URL.Path)

		var req struct {
			Query     string   `json:"query"`
			Model     string   `json:"model"`
			Documents []string `json:"documents"`
			TopN      int      `json:"top_n"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "union login injection", req.Query)
		require.Equal(t, "test-rerank-model", req.Model)
		require.Len(t, req.Documents, 2)

		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.97},
				{"index": 0, "relevance_score": 0.42},
			},
		}))
	}))
	defer rerankServer.Close()

	cfg.Rerank.Enabled = testBoolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.TopN = 2
	cfg.Rerank.MaxCandidates = 4
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: rerankServer.URL + "/v1",
		Model:  "test-rerank-model",
	}

	seedVectorKBRerankFixture(t, cfg)

	results, err := Search(context.Background(), cfg, SearchOptions{
		Workspace:           "acme",
		Limit:               2,
		EnableRerank:        true,
		RerankTopN:          2,
		RerankMaxCandidates: 2,
	}, "union login injection")
	require.NoError(t, err)
	require.Len(t, results, 2)

	require.Equal(t, "Acme Fallback Note", results[0].Title)
	require.Equal(t, "rerank", results[0].RankingSource)
	require.Equal(t, 0.97, results[0].RelevanceScore)
	require.Equal(t, 0.97, results[0].RerankScore)
	require.Greater(t, results[0].BaseRelevanceScore, 0.0)

	require.Equal(t, "Acme Primary Playbook", results[1].Title)
	require.Equal(t, "rerank", results[1].RankingSource)
	require.Equal(t, 0.42, results[1].RerankScore)
	require.Greater(t, results[1].BaseRelevanceScore, results[1].RerankScore)
}

func TestSearchFallsBackWhenRerankFails(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	rerankServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/rerank", r.URL.Path)
		http.Error(w, "rerank backend exploded", http.StatusBadGateway)
	}))
	defer rerankServer.Close()

	cfg.Rerank.Enabled = testBoolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.TopN = 2
	cfg.Rerank.MaxCandidates = 4
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: rerankServer.URL + "/v1",
		Model:  "test-rerank-model",
	}

	seedVectorKBRerankFixture(t, cfg)

	results, err := Search(context.Background(), cfg, SearchOptions{
		Workspace:    "acme",
		Limit:        2,
		EnableRerank: true,
	}, "union login injection")
	require.NoError(t, err)
	require.Len(t, results, 2)

	require.Equal(t, "Acme Primary Playbook", results[0].Title)
	require.Equal(t, "fallback_hybrid", results[0].RankingSource)
	require.Zero(t, results[0].RerankScore)
	require.Equal(t, results[0].BaseRelevanceScore, results[0].RelevanceScore)

	require.Equal(t, "fallback_hybrid", results[1].RankingSource)
	require.Zero(t, results[1].RerankScore)
	require.Equal(t, results[1].BaseRelevanceScore, results[1].RelevanceScore)
}

func TestSearchRerankKeepsNonCandidateTail(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	rerankServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/rerank", r.URL.Path)

		var req struct {
			Documents []string `json:"documents"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Len(t, req.Documents, 2)

		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.93},
				{"index": 0, "relevance_score": 0.41},
			},
		}))
	}))
	defer rerankServer.Close()

	cfg.Rerank.Enabled = testBoolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.TopN = 2
	cfg.Rerank.MaxCandidates = 2
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: rerankServer.URL + "/v1",
		Model:  "test-rerank-model",
	}

	seedVectorKBRerankFixture(t, cfg)

	results, err := Search(context.Background(), cfg, SearchOptions{
		Workspace:           "acme",
		Limit:               3,
		EnableRerank:        true,
		RerankTopN:          2,
		RerankMaxCandidates: 2,
	}, "union login injection")
	require.NoError(t, err)
	require.Len(t, results, 3)

	require.Equal(t, "Acme Fallback Note", results[0].Title)
	require.Equal(t, "rerank", results[0].RankingSource)

	require.Equal(t, "Acme Primary Playbook", results[1].Title)
	require.Equal(t, "rerank", results[1].RankingSource)

	require.Equal(t, "Acme Session Notes", results[2].Title)
	require.Equal(t, "hybrid", results[2].RankingSource)
	require.Zero(t, results[2].RerankScore)
	require.Equal(t, results[2].BaseRelevanceScore, results[2].RelevanceScore)
}

func seedVectorKBRerankFixture(t *testing.T, cfg *config.Config) {
	t.Helper()

	ctx := context.Background()
	now := time.Now()

	doc1 := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-primary-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Primary Playbook",
		ContentHash: "doc-rerank-1",
		Status:      "ready",
		ChunkCount:  1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks1 := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "SQL Injection",
		Content:     "Look for UNION based injection on login endpoints and auth previews.",
		ContentHash: "chunk-rerank-1",
		Metadata:    `{"scope":"target","workspace":"acme"}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc1, chunks1))

	doc2 := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-fallback-note.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Fallback Note",
		ContentHash: "doc-rerank-2",
		Status:      "ready",
		ChunkCount:  1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks2 := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Auth",
		Content:     "Document preview route behavior for login workflows and auth confusion edges.",
		ContentHash: "chunk-rerank-2",
		Metadata:    `{"scope":"target","workspace":"acme"}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc2, chunks2))

	doc3 := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-session-notes.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Session Notes",
		ContentHash: "doc-rerank-3",
		Status:      "ready",
		ChunkCount:  1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks3 := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Session",
		Content:     "General auth session checklist for operator review.",
		ContentHash: "chunk-rerank-3",
		Metadata:    `{"scope":"target","workspace":"acme"}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc3, chunks3))

	_, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
}
