package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
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
