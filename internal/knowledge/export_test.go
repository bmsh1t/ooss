package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/require"
)

func setupKnowledgeExportDB(t *testing.T) func() {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-export.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))

	return func() {
		_ = database.Close()
		database.SetDB(nil)
	}
}

func TestExportChunks_WritesLineCorpus(t *testing.T) {
	cleanup := setupKnowledgeExportDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	doc := &database.KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "/tmp/acme-playbook.md",
		SourceType:  "file",
		DocType:     "md",
		Title:       "Acme Playbook",
		ContentHash: "doc-hash",
		Status:      "ready",
		ChunkCount:  2,
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

	outputPath := filepath.Join(t.TempDir(), "knowledge-index.txt")
	summary, err := ExportChunks(ctx, "acme", outputPath, 100)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Documents)
	require.Equal(t, 2, summary.Chunks)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "[kb][workspace=acme][type=md][title=Acme Playbook][section=SQL Injection][path=/tmp/acme-playbook.md]")
	require.Contains(t, content, "Look for UNION based injection on login endpoints.")
	require.Contains(t, content, "Review reflected payload handling in search results.")
	require.Equal(t, 2, strings.Count(strings.TrimSpace(content), "\n")+1)
}
