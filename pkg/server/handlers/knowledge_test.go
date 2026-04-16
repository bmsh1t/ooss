package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/knowledge"
	"github.com/j3ssie/osmedeus/v5/internal/testutil"
	"github.com/j3ssie/osmedeus/v5/internal/vectorkb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVectorKnowledgeStats(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	app := fiber.New()
	app.Get("/knowledge/vector/stats", VectorKnowledgeStats(cfg))

	req := httptest.NewRequest("GET", "/knowledge/vector/stats", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestVectorKnowledgeDoctor(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	app := fiber.New()
	app.Get("/knowledge/vector/doctor", VectorKnowledgeDoctor(cfg))

	req := httptest.NewRequest("GET", "/knowledge/vector/doctor?workspace=acme", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	var payload struct {
		Data vectorkb.DoctorReport `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, "acme", payload.Data.Workspace)
	assert.Equal(t, "provider_not_available", payload.Data.SemanticStatus)
	assert.False(t, payload.Data.SemanticSearchReady)
}

func TestVectorKnowledgeDoctor_WithProbeQuery(t *testing.T) {
	tmpDir := t.TempDir()
	embeddingServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"forbidden","type":"insufficient_permissions"}}`, http.StatusForbidden)
	}))
	defer embeddingServer.Close()

	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-handler.sqlite"),
		},
		KnowledgeVector: config.KnowledgeVectorConfig{
			DBPath:          filepath.Join(tmpDir, "vector", "vector-kb.sqlite"),
			DefaultProvider: "openai",
			DefaultModel:    "test-embedding-3-small",
		},
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{{
				Provider:  "openai",
				BaseURL:   embeddingServer.URL + "/embeddings",
				Model:     "test-embedding-3-small",
				AuthToken: "bad-token",
			}},
			MaxRetries: 1,
			Timeout:    "5s",
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	app := fiber.New()
	app.Get("/knowledge/vector/doctor", VectorKnowledgeDoctor(cfg))

	req := httptest.NewRequest("GET", "/knowledge/vector/doctor?workspace=acme&probe=true", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	var payload struct {
		Data vectorkb.DoctorReport `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, "provider_auth_failed", payload.Data.SemanticStatus)
	assert.False(t, payload.Data.SemanticSearchReady)
}

func TestIndexVectorKnowledgeValidation(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	app := fiber.New()
	app.Post("/knowledge/vector/index", IndexVectorKnowledge(cfg))

	req := httptest.NewRequest("POST", "/knowledge/vector/index", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestSearchVectorKnowledgeValidation(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	app := fiber.New()
	app.Post("/knowledge/vector/search", SearchVectorKnowledge(cfg))

	body, err := json.Marshal(map[string]any{
		"workspace": "acme",
	})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/knowledge/vector/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestKnowledgeVectorRankingSourcePreservesEmptyFirstResult(t *testing.T) {
	assert.Equal(t, "", knowledgeVectorRankingSource([]vectorkb.SearchHit{{
		RankingSource: "",
	}}))
	assert.Equal(t, "hybrid", knowledgeVectorRankingSource(nil))
}

func TestSearchVectorKnowledgeSupportsRerank(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	embeddingServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type embeddingRequest struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		type embeddingData struct {
			Object    string    `json:"object"`
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		}

		var req embeddingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

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
				Object: "embedding",
				Index:  i,
				Embedding: []float64{
					knowledgeHandlerScoreTerm(text, "authentication", "bypass", "playbook"),
					knowledgeHandlerScoreTerm(text, "login", "auth", "preview"),
					knowledgeHandlerScoreTerm(text, "token", "session", "route"),
				},
			})
		}

		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	rerankServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/rerank", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 0, "relevance_score": 0.98},
			},
		}))
	}))
	t.Cleanup(func() {
		embeddingServer.Close()
		rerankServer.Close()
	})

	cfg.KnowledgeVector.DefaultProvider = "openai"
	cfg.KnowledgeVector.DefaultModel = "test-embedding-3-small"
	cfg.KnowledgeVector.TopK = 10
	cfg.KnowledgeVector.HybridWeight = 0.7
	cfg.KnowledgeVector.KeywordWeight = 0.3
	cfg.KnowledgeVector.BatchSize = 8
	cfg.LLM.LLMProviders = []config.LLMProvider{{
		Provider: "openai",
		BaseURL:  embeddingServer.URL + "/embeddings",
		Model:    "test-embedding-3-small",
	}}
	cfg.Rerank.Enabled = boolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.TopN = 1
	cfg.Rerank.MaxCandidates = 4
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: rerankServer.URL + "/v1",
		Model:  "test-rerank-model",
	}

	_, err := vectorkb.IndexWorkspace(context.Background(), cfg, vectorkb.IndexOptions{
		Workspace: "acme",
	})
	require.NoError(t, err)

	app := fiber.New()
	app.Post("/knowledge/vector/search", SearchVectorKnowledge(cfg))

	body, err := json.Marshal(map[string]any{
		"query":         "authentication bypass",
		"workspace":     "acme",
		"enable_rerank": true,
		"rerank_top_n":  1,
	})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/knowledge/vector/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	var payload struct {
		RankingSource string               `json:"ranking_source"`
		RerankApplied bool                 `json:"rerank_applied"`
		Data          []vectorkb.SearchHit `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.True(t, payload.RerankApplied)
	assert.Equal(t, "rerank", payload.RankingSource)
	assert.NotEmpty(t, payload.Data)
}

func TestIngestKnowledgeReturnsWarningWhenVectorAutoIndexFails(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	autoIndex := true
	cfg.KnowledgeVector.Enabled = boolPtr(true)
	cfg.KnowledgeVector.AutoIndexOnIngest = &autoIndex

	sourcePath := filepath.Join(t.TempDir(), "notes.md")
	require.NoError(t, os.WriteFile(sourcePath, []byte("# Auth Notes\n\nToken confusion checks are required.\n"), 0o600))

	body, err := json.Marshal(KnowledgeIngestRequest{
		Path:      sourcePath,
		Workspace: "acme",
		Recursive: boolPtr(false),
	})
	require.NoError(t, err)

	app := fiber.New()
	app.Post("/knowledge/ingest", IngestKnowledge(cfg))

	req := httptest.NewRequest("POST", "/knowledge/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	var payload struct {
		Data    knowledge.IngestSummary `json:"data"`
		Message string                  `json:"message"`
		Warning string                  `json:"warning"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.NotNil(t, payload.Data.VectorIndexed)
	assert.False(t, *payload.Data.VectorIndexed)
	assert.Contains(t, payload.Data.VectorError, "embedding provider")
	assert.Contains(t, payload.Data.VectorError, "not configured")
	assert.Contains(t, payload.Message, "vector auto-index warning")
	assert.Contains(t, payload.Warning, "Vector auto-index failed")
}

func TestLearnKnowledgeReturnsWarningWhenVectorAutoIndexFails(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	autoIndex := true
	cfg.KnowledgeVector.Enabled = boolPtr(true)
	cfg.KnowledgeVector.AutoIndexOnLearn = &autoIndex

	app := fiber.New()
	app.Post("/knowledge/learn", LearnKnowledge(cfg))

	body, err := json.Marshal(KnowledgeLearnRequest{
		Workspace: "acme",
		Scope:     "public",
	})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/knowledge/learn", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	var payload struct {
		Data    knowledge.LearnSummary `json:"data"`
		Message string                 `json:"message"`
		Warning string                 `json:"warning"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.NotNil(t, payload.Data.VectorIndexed)
	assert.False(t, *payload.Data.VectorIndexed)
	assert.Equal(t, "public", payload.Data.StorageWorkspace)
	assert.Contains(t, payload.Data.VectorError, "embedding provider")
	assert.Contains(t, payload.Data.VectorError, "not configured")
	assert.Contains(t, payload.Message, "vector auto-index warning")
	assert.Contains(t, payload.Warning, "Vector auto-index failed")
}

func setupKnowledgeHandlerDB(t *testing.T) (*config.Config, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-handler.sqlite"),
		},
		KnowledgeVector: config.KnowledgeVectorConfig{
			DBPath:          filepath.Join(tmpDir, "vector", "vector-kb.sqlite"),
			DefaultProvider: "test-openai",
			DefaultModel:    "test-embedding-3-small",
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))

	now := time.Now()
	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/doc.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Doc",
		ContentHash: "hash-1",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  64,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{
		{
			Workspace:   "acme",
			ChunkIndex:  0,
			Section:     "Section",
			Content:     "Authentication bypass playbook",
			ContentHash: "chunk-1",
			CreatedAt:   now,
		},
	}
	require.NoError(t, database.UpsertKnowledgeDocument(context.Background(), doc, chunks))

	return cfg, func() {
		_ = database.Close()
		database.SetDB(nil)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func knowledgeHandlerScoreTerm(text string, terms ...string) float64 {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return 1
		}
	}
	return 0
}
