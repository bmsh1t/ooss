package vectorkb

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/testutil"
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

func TestIndexWorkspaceReindexesWhenMetadataChanges(t *testing.T) {
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
		Metadata:    `{"sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "SQL Injection",
		Content:     "Look for UNION based injection on login endpoints.",
		ContentHash: "chunk-hash-1",
		Metadata:    `{"sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	first, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, first.DocumentsIndexed)

	doc.Metadata = `{"sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"stable-fp"}`
	doc.UpdatedAt = now.Add(time.Minute)
	chunks[0].Metadata = `{"sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"stable-fp"}`
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	second, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, second.DocumentsSeen)
	require.Equal(t, 1, second.DocumentsIndexed)
	require.Equal(t, 0, second.DocumentsSkipped)
	require.Equal(t, 1, second.ChunksEmbedded)
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
	require.False(t, report.SemanticSearchReady)
	require.Equal(t, "consistency_issues", report.SemanticStatus)
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
	require.True(t, reportAfter.SemanticSearchReady)
	require.Equal(t, "ready", reportAfter.SemanticStatus)
}

func TestDoctorReportsProviderNotConfigured(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	cfg.KnowledgeVector.DefaultProvider = ""

	report, err := Doctor(context.Background(), cfg, DoctorOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.NotNil(t, report)
	require.False(t, report.SemanticSearchReady)
	require.Equal(t, "provider_not_configured", report.SemanticStatus)
	require.Contains(t, report.SemanticStatusMessage, "default_provider")
	require.Contains(t, report.AvailableProviders, "openai")
}

func TestDoctorReportsModelNotBound(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	cfg.KnowledgeVector.DefaultModel = ""
	cfg.LLM.LLMProviders[0].Model = ""

	report, err := Doctor(context.Background(), cfg, DoctorOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.NotNil(t, report)
	require.False(t, report.SemanticSearchReady)
	require.Equal(t, "model_not_bound", report.SemanticStatus)
	require.Contains(t, report.SemanticStatusMessage, "default_model")
}

func TestDoctorReportsIndexMissingWhenMainDocsAreNotIndexed(t *testing.T) {
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
		ContentHash: "doc-hash-index-missing",
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
		ContentHash: "chunk-hash-index-missing",
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	report, err := Doctor(ctx, cfg, DoctorOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.NotNil(t, report)
	require.False(t, report.SemanticSearchReady)
	require.Equal(t, "index_missing", report.SemanticStatus)
	require.Equal(t, 1, report.MissingDocuments)
	require.Equal(t, 0, report.SelectedEmbeddings)
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
	wrongServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	vectorServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestVectorKBUsesDedicatedEmbeddingsConfigWithoutLLMProviders(t *testing.T) {
	var embeddingRequests int32
	embeddingServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&embeddingRequests, 1)
		type embeddingRequest struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		var req embeddingRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "jina-embeddings-v5-text-small", req.Model)

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
			Model:  "jina-embeddings-v5-text-small",
		}
		for i, input := range req.Input {
			text := strings.ToLower(input)
			resp.Data = append(resp.Data, embeddingData{
				Object:    "embedding",
				Index:     i,
				Embedding: []float64{scoreTerm(text, "cwe", "session"), scoreTerm(text, "login", "auth"), scoreTerm(text, "xss", "script")},
			})
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer embeddingServer.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge.sqlite"),
		},
		KnowledgeVector: config.KnowledgeVectorConfig{
			DBPath:            filepath.Join(tmpDir, "vector", "vector-kb.sqlite"),
			BatchSize:         8,
			MaxIndexingChunks: 100,
			TopK:              10,
			HybridWeight:      0.7,
			KeywordWeight:     0.3,
		},
		Embeddings: config.EmbeddingsConfig{
			Enabled:  testBoolPtr(true),
			Provider: "jina",
			Jina: config.EmbeddingsProviderConfig{
				APIURL: embeddingServer.URL + "/embeddings",
				Model:  "jina-embeddings-v5-text-small",
				APIKey: "test-jina-key",
			},
		},
		LLM: config.LLMConfig{
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
		SourcePath:  "/tmp/cwe-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "CWE Notes",
		ContentHash: "doc-hash-cwe",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Session",
		Content:     "CWE session fixation and login boundary review guidance.",
		ContentHash: "chunk-hash-cwe",
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	reportBefore, err := Doctor(ctx, cfg, DoctorOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, "jina", reportBefore.Provider)
	require.Equal(t, "jina-embeddings-v5-text-small", reportBefore.Model)
	require.Contains(t, reportBefore.AvailableProviders, "jina")

	summary, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, summary.DocumentsIndexed)
	require.Equal(t, 1, summary.ChunksEmbedded)

	results, err := Search(ctx, cfg, SearchOptions{
		Workspace: "acme",
		Limit:     5,
	}, "session fixation login")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "jina", results[0].Provider)
	require.EqualValues(t, 2, atomic.LoadInt32(&embeddingRequests))
}

func TestIndexWorkspaceRetriesEmbeddingRateLimit(t *testing.T) {
	var embeddingRequests int32
	embeddingServer := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(&embeddingRequests, 1)
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"message": "rate limit exceeded",
					"type":    "rate_limit_error",
					"code":    "rate_limit",
				},
			}))
			return
		}

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
				Embedding: []float64{scoreTerm(text, "auth", "login"), scoreTerm(text, "sql", "union"), scoreTerm(text, "xss", "script")},
			})
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	defer embeddingServer.Close()

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
			MaxRetries:   2,
			RetryDelay:   "1ms",
			RetryBackoff: false,
			Timeout:      "5s",
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
		SourcePath:  "/tmp/rate-limit.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Rate Limit Retry",
		ContentHash: "doc-hash-rate-limit",
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
		Content:     "Review login and auth choke points after rate limit recovery.",
		ContentHash: "chunk-hash-rate-limit",
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	summary, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, summary.DocumentsIndexed)
	require.EqualValues(t, 2, atomic.LoadInt32(&embeddingRequests))
}

func TestSearchIncludesPublicLayerFallbackAndMetadata(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	doc := &database.KnowledgeDocument{
		Workspace:   "public",
		SourcePath:  "kb://learned/public/shared/verified-findings.md",
		SourceType:  "generated",
		DocType:     "learned-findings",
		Title:       "Shared Verified Findings",
		ContentHash: "shared-doc-hash",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		Metadata:    `{"scope":"public","knowledge_layer":"public","source_workspace":"acme","sample_type":"verified","source_confidence":0.95,"target_types":["url"],"labels":["auto-learn","verified"]}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	chunks := []database.KnowledgeChunk{{
		Workspace:   "public",
		ChunkIndex:  0,
		Section:     "SQL Injection",
		Content:     "Verified SQL injection on login URL with UNION payload.",
		ContentHash: "shared-chunk-hash",
		Metadata:    `{"scope":"public","knowledge_layer":"public","source_workspace":"acme","sample_type":"verified","source_confidence":0.95,"target_types":["url"],"labels":["auto-learn","verified"]}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, doc, chunks))

	_, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "public"})
	require.NoError(t, err)

	results, err := Search(ctx, cfg, SearchOptions{
		Workspace: "acme",
		Limit:     5,
	}, "verified sql injection login url")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "public", results[0].Workspace)
	require.NotNil(t, results[0].Metadata)
	require.Equal(t, "public", results[0].Metadata.Scope)
	require.Equal(t, "verified", results[0].Metadata.SampleType)
	require.Contains(t, results[0].Metadata.TargetTypes, "url")
}

func TestSearchDedupesByMetadataFingerprintAcrossLayers(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, database.UpsertKnowledgeDocument(ctx, &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/verified-findings.md",
		SourceType:  "generated",
		DocType:     "learned-findings",
		Title:       "Verified Findings",
		ContentHash: "doc-hash-acme",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-vector-fp"}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Primary",
		Content:     "Verified SQL injection on login with UNION proof",
		ContentHash: "chunk-hash-acme",
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-vector-fp"}`,
		CreatedAt:   now,
	}}))

	require.NoError(t, database.UpsertKnowledgeDocument(ctx, &database.KnowledgeDocument{
		Workspace:   "public",
		SourcePath:  "kb://learned/public/acme/verified-findings.md",
		SourceType:  "generated",
		DocType:     "learned-findings",
		Title:       "Verified Findings",
		ContentHash: "doc-hash-public",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		Metadata:    `{"scope":"public","knowledge_layer":"public","source_workspace":"acme","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-vector-fp"}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []database.KnowledgeChunk{{
		Workspace:   "public",
		ChunkIndex:  0,
		Section:     "Shared",
		Content:     "Verified SQL injection on login with UNION payload",
		ContentHash: "chunk-hash-public",
		Metadata:    `{"scope":"public","knowledge_layer":"public","source_workspace":"acme","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-vector-fp"}`,
		CreatedAt:   now,
	}}))

	_, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)
	_, err = IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "public"})
	require.NoError(t, err)

	results, err := Search(ctx, cfg, SearchOptions{
		Workspace: "acme",
		Limit:     10,
	}, "verified sql injection login union")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "acme", results[0].Workspace)
}

func TestSearchFiltersLowConfidenceAndFalsePositiveSamples(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, database.UpsertKnowledgeDocument(ctx, &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/ai-insights.md",
		SourceType:  "generated",
		DocType:     "learned-ai-insights",
		Title:       "AI Insights",
		ContentHash: "doc-hash-ai",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"ai-analysis","source_confidence":0.52}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "AI",
		Content:     "Auth bypass hypothesis and exploit path",
		ContentHash: "chunk-hash-ai",
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"ai-analysis","source_confidence":0.52}`,
		CreatedAt:   now,
	}}))

	require.NoError(t, database.UpsertKnowledgeDocument(ctx, &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/false-positive-samples.md",
		SourceType:  "generated",
		DocType:     "learned-false-positives",
		Title:       "False Positive Samples",
		ContentHash: "doc-hash-fp",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"false_positive","source_confidence":0.90}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Noise",
		Content:     "False positive auth bypass caused by benign redirect",
		ContentHash: "chunk-hash-fp",
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"false_positive","source_confidence":0.90}`,
		CreatedAt:   now,
	}}))

	require.NoError(t, database.UpsertKnowledgeDocument(ctx, &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/verified-findings.md",
		SourceType:  "generated",
		DocType:     "learned-findings",
		Title:       "Verified Findings",
		ContentHash: "doc-hash-verified",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  128,
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Proof",
		Content:     "Verified auth bypass proof with retest notes",
		ContentHash: "chunk-hash-verified",
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
	}}))

	_, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)

	results, err := Search(ctx, cfg, SearchOptions{
		Workspace:           "acme",
		Limit:               10,
		MinSourceConfidence: 0.60,
		ExcludeSampleTypes:  []string{"false_positive"},
	}, "auth bypass proof")
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].Metadata)
	require.Equal(t, "verified", results[0].Metadata.SampleType)
}

func TestSearchPrefersOperationalPlaybookForOperationalQuery(t *testing.T) {
	cfg, cleanup := setupVectorKBTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	operationalDoc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/operational-playbook.md",
		SourceType:  "generated",
		DocType:     "learned-operational-playbook",
		Title:       "Operational Playbook",
		ContentHash: "operational-doc-hash",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  180,
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"operator-followup","source_confidence":0.88,"target_types":["url"],"labels":["operator-followup","manual-followup","operator-queue","retest-plan","followup-decision","campaign-handoff"]}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	operationalChunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Manual Follow-up",
		Content:     "Manual exploitation retest plan for admin auth bypass with proof capture and operator followup.",
		ContentHash: "operational-chunk-hash",
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"operator-followup","source_confidence":0.88,"target_types":["url"],"labels":["operator-followup","manual-followup","operator-queue","retest-plan","followup-decision","campaign-handoff"]}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, operationalDoc, operationalChunks))

	aiDoc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/ai-insights.md",
		SourceType:  "generated",
		DocType:     "learned-ai-insights",
		Title:       "AI Insights",
		ContentHash: "ai-doc-hash",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  170,
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"ai-analysis","source_confidence":0.64,"target_types":["url"],"labels":["auto-learn","ai-analysis"]}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	aiChunks := []database.KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Summary",
		Content:     "AI summary about admin auth bypass and attack path observations.",
		ContentHash: "ai-chunk-hash",
		Metadata:    `{"scope":"workspace","knowledge_layer":"acme","source_workspace":"acme","sample_type":"ai-analysis","source_confidence":0.64,"target_types":["url"],"labels":["auto-learn","ai-analysis"]}`,
		CreatedAt:   now,
	}}
	require.NoError(t, database.UpsertKnowledgeDocument(ctx, aiDoc, aiChunks))

	_, err := IndexWorkspace(ctx, cfg, IndexOptions{Workspace: "acme"})
	require.NoError(t, err)

	results, err := Search(ctx, cfg, SearchOptions{
		Workspace: "acme",
		Limit:     5,
	}, "manual exploitation retest auth bypass admin proof")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NotNil(t, results[0].Metadata)
	require.Equal(t, "operator-followup", results[0].Metadata.SampleType)
	require.Equal(t, "Operational Playbook", results[0].Title)
}

func setupVectorKBTestEnv(t *testing.T) (*config.Config, func()) {
	t.Helper()

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

func testBoolPtr(value bool) *bool {
	return &value
}

func scoreTerm(text string, terms ...string) float64 {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return 1
		}
	}
	return 0
}
