package vectorkb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/require"
)

func TestIndexWorkspaceAndSearch(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Playbook",
		ContentHash: "doc-hash-1",
		Status:      "ready",
		ChunkCount:  2,
		TotalBytes:  256,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{
		{
			Workspace:   "acme",
			ChunkIndex:  0,
			Section:     "SQL Injection",
			Content:     "Look for UNION based injection on login endpoints.",
			ContentHash: "chunk-hash-1",
			CreatedAt:   now,
		},
		{
			Workspace:   "acme",
			ChunkIndex:  1,
			Section:     "XSS",
			Content:     "Review reflected payload handling in search results.",
			ContentHash: "chunk-hash-2",
			CreatedAt:   now,
		},
	}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	summary, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, summary.DocumentsSeen)
	require.Equal(t, 1, summary.DocumentsIndexed)
	require.Equal(t, 0, summary.DocumentsSkipped)
	require.Equal(t, 2, summary.ChunksSeen)
	require.Equal(t, 2, summary.ChunksEmbedded)

	store, err := Open(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.Migrate(ctx))

	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Documents)
	require.Equal(t, 2, stats.Chunks)
	require.Equal(t, 2, stats.Embeddings)

	results, err := Search(ctx, cfg, SearchOptions{
		Workspace: "acme",
		Limit:     5,
	}, "union login injection")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "Acme Playbook", results[0].Title)
	require.Equal(t, "vector_kb", results[0].Type)
	require.Contains(t, strings.ToLower(results[0].Snippet), "login")
	require.Greater(t, results[0].RelevanceScore, 0.0)
}

func TestIndexWorkspaceSkipsUnchangedDocuments(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Playbook",
		ContentHash: "doc-hash-1",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{
		{
			Workspace:   "acme",
			ChunkIndex:  0,
			Section:     "SQL Injection",
			Content:     "Look for UNION based injection on login endpoints.",
			ContentHash: "chunk-hash-1",
			CreatedAt:   now,
		},
	}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	first, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, first.DocumentsIndexed)

	second, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, second.DocumentsSeen)
	require.Equal(t, 0, second.DocumentsIndexed)
	require.Equal(t, 1, second.DocumentsSkipped)
	require.Equal(t, 0, second.ChunksEmbedded)
}

func TestDoctorAndPurgeDetectAndCleanStaleData(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Playbook",
		ContentHash: "doc-hash-1",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "SQL Injection",
		Content:     "Look for UNION based injection on login endpoints.",
		ContentHash: "chunk-hash-1",
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))
	_, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)

	store, err := Open(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.Migrate(ctx))

	staleDoc := &VectorDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/stale-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Stale Playbook",
		ContentHash: "stale-hash",
		Status:      "ready",
		ChunkCount:  1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = store.db.NewInsert().Model(staleDoc).Exec(ctx)
	require.NoError(t, err)
	staleChunk := &VectorChunk{
		DocumentID:  staleDoc.ID,
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Old",
		Content:     "Outdated stale knowledge",
		ContentHash: "stale-chunk-hash",
		CreatedAt:   now,
	}
	_, err = store.db.NewInsert().Model(staleChunk).Exec(ctx)
	require.NoError(t, err)
	staleEmbedding := &VectorEmbedding{
		ChunkID:       staleChunk.ID,
		Provider:      cfg.KnowledgeVector.DefaultProvider,
		Model:         cfg.KnowledgeVector.DefaultModel,
		Dimension:     3,
		EmbeddingJSON: "[1,0,0]",
		EmbeddingHash: "stale-embedding-hash",
		CreatedAt:     now,
	}
	_, err = store.db.NewInsert().Model(staleEmbedding).Exec(ctx)
	require.NoError(t, err)

	orphanChunk := &VectorChunk{
		DocumentID:  999999,
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Orphan",
		Content:     "Chunk without parent doc",
		ContentHash: "orphan-chunk-hash",
		CreatedAt:   now,
	}
	_, err = store.db.NewInsert().Model(orphanChunk).Exec(ctx)
	require.NoError(t, err)
	orphanEmbedding := &VectorEmbedding{
		ChunkID:       888888,
		Provider:      cfg.KnowledgeVector.DefaultProvider,
		Model:         cfg.KnowledgeVector.DefaultModel,
		Dimension:     3,
		EmbeddingJSON: "[0,1,0]",
		EmbeddingHash: "orphan-embedding-hash",
		CreatedAt:     now,
	}
	_, err = store.db.NewInsert().Model(orphanEmbedding).Exec(ctx)
	require.NoError(t, err)

	report, err := Doctor(ctx, cfg, DoctorOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.False(t, report.Healthy)
	require.GreaterOrEqual(t, report.StaleDocuments, 1)
	require.Equal(t, 1, report.OrphanChunks)
	require.Equal(t, 1, report.OrphanEmbeddings)

	purge, err := Purge(ctx, cfg, "acme")
	require.NoError(t, err)
	require.GreaterOrEqual(t, purge.RemovedDocuments, 1)
	require.GreaterOrEqual(t, purge.RemovedChunks, 2)
	require.GreaterOrEqual(t, purge.RemovedEmbeddings, 2)

	reportAfter, err := Doctor(ctx, cfg, DoctorOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.True(t, reportAfter.Healthy)
}

func TestRebuildWorkspaceRemovesOldDataAndReindexes(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Playbook",
		ContentHash: "doc-hash-1",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Auth",
		Content:     "Authentication bypass review on admin entry points.",
		ContentHash: "chunk-hash-1",
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	store, err := Open(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.Migrate(ctx))

	staleDoc := &VectorDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/old.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Old",
		ContentHash: "old-hash",
		Status:      "ready",
		ChunkCount:  1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = store.db.NewInsert().Model(staleDoc).Exec(ctx)
	require.NoError(t, err)

	summary, err := Rebuild(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.NotNil(t, summary.Purge)
	require.NotNil(t, summary.Index)
	require.GreaterOrEqual(t, summary.Purge.RemovedDocuments, 1)
	require.Equal(t, 1, summary.Index.DocumentsIndexed)

	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stats.Documents)
}

func TestSyncPurgesStaleDataAndIndexesCurrentDocuments(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()
	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/current.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Current",
		ContentHash: "current-hash",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  64,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "API",
		Content:     "Review admin api and auth boundaries.",
		ContentHash: "current-chunk",
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	store, err := Open(cfg)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	require.NoError(t, store.Migrate(ctx))

	staleDoc := &VectorDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/stale.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Stale",
		ContentHash: "stale-hash",
		Status:      "ready",
		ChunkCount:  1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = store.db.NewInsert().Model(staleDoc).Exec(ctx)
	require.NoError(t, err)

	summary, err := Sync(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.NotNil(t, summary.Purge)
	require.NotNil(t, summary.Index)
	require.GreaterOrEqual(t, summary.Purge.RemovedDocuments, 1)
	require.Equal(t, 1, summary.Index.DocumentsIndexed)

	report, err := Doctor(ctx, cfg, DoctorOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.True(t, report.Healthy)
}

func TestVectorKBUsesExplicitConfiguredProvider(t *testing.T) {
	var wrongRequests int32
	wrongServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&wrongRequests, 1)
		type embeddingRequest struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		var req embeddingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))

		resp := struct {
			Object string `json:"object"`
			Data   []struct {
				Object    string    `json:"object"`
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			} `json:"data"`
			Model string `json:"model"`
		}{
			Object: "list",
			Data: make([]struct {
				Object    string    `json:"object"`
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}, 0, len(req.Input)),
			Model: "wrong-embedding-model",
		}
		for i := range req.Input {
			resp.Data = append(resp.Data, struct {
				Object    string    `json:"object"`
				Embedding []float64 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				Object:    "embedding",
				Embedding: []float64{9, 9, 9},
				Index:     i,
			})
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer wrongServer.Close()

	var vectorRequests int32
	vectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&vectorRequests, 1)
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
				Embedding: []float64{scoreTerm(text, "sql", "union", "injection"), scoreTerm(text, "login", "auth"), scoreTerm(text, "xss", "payload", "script")},
			})
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer vectorServer.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge.sqlite"),
		},
		KnowledgeVector: config.KnowledgeVectorConfig{
			DBPath:            filepath.Join(tmpDir, "vector", "vector-kb.sqlite"),
			DefaultProvider:   "vector-provider",
			DefaultModel:      "test-embedding-3-small",
			BatchSize:         8,
			MaxIndexingChunks: 100,
			TopK:              10,
			HybridWeight:      0.7,
			KeywordWeight:     0.3,
		},
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					Provider: "wrong-provider",
					BaseURL:  wrongServer.URL + "/embeddings",
					Model:    "wrong-embedding-model",
				},
				{
					Provider: "vector-provider",
					BaseURL:  vectorServer.URL + "/embeddings",
					Model:    "test-embedding-3-small",
				},
			},
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

	ctx := context.Background()
	now := time.Now()
	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Playbook",
		ContentHash: "doc-hash-1",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "SQL Injection",
		Content:     "Look for UNION based injection on login endpoints.",
		ContentHash: "chunk-hash-1",
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	_, err = IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)

	results, err := Search(ctx, cfg, SearchOptions{
		Workspace: "acme",
		Limit:     5,
	}, "union login injection")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "vector-provider", results[0].Provider)
	require.EqualValues(t, 0, atomic.LoadInt32(&wrongRequests))
	require.EqualValues(t, 2, atomic.LoadInt32(&vectorRequests))
}

func setupVectorKBTestEnv(t *testing.T) (*config.Config, func()) {
	t.Helper()

	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				Embedding: []float64{scoreTerm(text, "sql", "union", "injection"), scoreTerm(text, "login", "auth"), scoreTerm(text, "xss", "payload", "script")},
			})
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge.sqlite"),
		},
		KnowledgeVector: config.KnowledgeVectorConfig{
			DBPath:            filepath.Join(tmpDir, "vector", "vector-kb.sqlite"),
			DefaultProvider:   "openai",
			DefaultModel:      "test-embedding-3-small",
			BatchSize:         8,
			MaxIndexingChunks: 100,
			TopK:              10,
			HybridWeight:      0.7,
			KeywordWeight:     0.3,
		},
		LLM: config.LLMConfig{
			LLMProviders: []config.LLMProvider{
				{
					Provider: "openai",
					BaseURL:  embeddingServer.URL + "/embeddings",
					Model:    "test-embedding-3-small",
				},
			},
			MaxRetries: 1,
			Timeout:    "5s",
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))

	return cfg, func() {
		embeddingServer.Close()
		_ = database.Close()
		database.SetDB(nil)
	}
}

func scoreTerm(text string, terms ...string) float64 {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return 1
		}
	}
	return 0
}
