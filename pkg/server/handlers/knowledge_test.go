package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/knowledge"
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
	assert.Contains(t, payload.Data.VectorError, "LLM provider")
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
	assert.Contains(t, payload.Data.VectorError, "LLM provider")
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
