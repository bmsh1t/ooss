package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/testutil"
	"github.com/j3ssie/osmedeus/v5/internal/vectorkb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunKBVectorDoctor_JSONReportIncludesSemanticStatus(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	cfg.LLM.LLMProviders = []config.LLMProvider{{
		Provider: "openai",
		BaseURL:  "http://127.0.0.1:1/embeddings",
		Model:    "test-embedding-3-small",
	}}

	oldWorkspace := kbWorkspace
	oldProvider := kbVectorProvider
	oldModel := kbVectorModel
	oldJSON := globalJSON
	oldSilent := os.Getenv("OSMEDEUS_SILENT")
	t.Cleanup(func() {
		kbWorkspace = oldWorkspace
		kbVectorProvider = oldProvider
		kbVectorModel = oldModel
		globalJSON = oldJSON
		if oldSilent == "" {
			_ = os.Unsetenv("OSMEDEUS_SILENT")
		} else {
			_ = os.Setenv("OSMEDEUS_SILENT", oldSilent)
		}
	})

	kbWorkspace = "acme"
	kbVectorProvider = ""
	kbVectorModel = ""
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runKBVectorDoctor(kbVectorDoctorCmd, nil))
	})

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.Equal(t, "provider_not_configured", report["semantic_status"])
	assert.Equal(t, false, report["semantic_search_ready"])
	assert.Contains(t, report["semantic_status_message"], "default_provider")
	assert.NotNil(t, report["available_providers"])
}

func TestRunKBVectorSearch_JSONIncludesRankingSource(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	embeddingServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type embeddingRequest struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		var req embeddingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		type embeddingData struct {
			Object    string    `json:"object"`
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		}
		resp := struct {
			Object string          `json:"object"`
			Data   []embeddingData `json:"data"`
			Model  string          `json:"model"`
		}{
			Object: "list",
			Data:   make([]embeddingData, 0, len(req.Input)),
			Model:  "test-embedding-3-small",
		}
		for i, input := range req.Input {
			text := strings.ToLower(input)
			resp.Data = append(resp.Data, embeddingData{
				Object:    "embedding",
				Index:     i,
				Embedding: []float64{cliScoreTerm(text, "sql", "union", "injection"), cliScoreTerm(text, "login", "auth"), cliScoreTerm(text, "xss", "payload", "script")},
			})
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	rerankServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/rerank", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.99},
				{"index": 0, "relevance_score": 0.37},
			},
		}))
	}))

	oldWorkspace := kbWorkspace
	oldProvider := kbVectorProvider
	oldModel := kbVectorModel
	oldQuery := kbQuery
	oldLimit := kbLimit
	oldJSON := globalJSON
	oldEnableRerank := kbEnableRerank
	oldRerankProvider := kbRerankProvider
	oldRerankModel := kbRerankModel
	oldRerankTopN := kbRerankTopN
	oldRerankMaxCandidates := kbRerankMaxCandidates
	t.Cleanup(func() {
		embeddingServer.Close()
		rerankServer.Close()
		kbWorkspace = oldWorkspace
		kbVectorProvider = oldProvider
		kbVectorModel = oldModel
		kbQuery = oldQuery
		kbLimit = oldLimit
		globalJSON = oldJSON
		kbEnableRerank = oldEnableRerank
		kbRerankProvider = oldRerankProvider
		kbRerankModel = oldRerankModel
		kbRerankTopN = oldRerankTopN
		kbRerankMaxCandidates = oldRerankMaxCandidates
	})

	cfg.KnowledgeVector = config.KnowledgeVectorConfig{
		DBPath:          filepath.Join(cfg.BaseFolder, "vector", "vector-kb.sqlite"),
		DefaultProvider: "openai",
		DefaultModel:    "test-embedding-3-small",
		TopK:            10,
		HybridWeight:    0.7,
		KeywordWeight:   0.3,
		BatchSize:       8,
	}
	cfg.LLM.LLMProviders = []config.LLMProvider{{
		Provider: "openai",
		BaseURL:  embeddingServer.URL + "/embeddings",
		Model:    "test-embedding-3-small",
	}}
	cfg.Rerank.Enabled = boolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.TopN = 2
	cfg.Rerank.MaxCandidates = 4
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: rerankServer.URL + "/v1",
		Model:  "test-rerank-model",
	}

	seedCLIKBVectorSearchFixture(t, cfg)

	kbWorkspace = "acme"
	kbVectorProvider = ""
	kbVectorModel = ""
	kbQuery = "union login injection"
	kbLimit = 2
	globalJSON = true
	kbEnableRerank = true
	kbRerankProvider = ""
	kbRerankModel = ""
	kbRerankTopN = 2
	kbRerankMaxCandidates = 2

	output := captureStdout(t, func() {
		require.NoError(t, runKBVectorSearch(kbVectorSearchCmd, nil))
	})

	var results []map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &results))
	require.Len(t, results, 2)
	assert.Equal(t, "rerank", results[0]["ranking_source"])
	assert.Contains(t, results[0], "rerank_score")
	assert.Contains(t, results[0], "base_relevance_score")
}

func seedCLIKBVectorSearchFixture(t *testing.T, cfg *config.Config) {
	t.Helper()

	ctx := context.Background()
	now := time.Now()

	doc1 := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-primary-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Primary Playbook",
		ContentHash: "cli-doc-rerank-1",
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
		ContentHash: "cli-chunk-rerank-1",
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
		ContentHash: "cli-doc-rerank-2",
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
		ContentHash: "cli-chunk-rerank-2",
		Metadata:    `{"scope":"target","workspace":"acme"}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc2, chunks2))

	_, err := vectorkb.IndexWorkspace(ctx, cfg, vectorkb.IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
}

func cliScoreTerm(text string, terms ...string) float64 {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return 1
		}
	}
	return 0
}

func boolPtr(value bool) *bool {
	return &value
}
