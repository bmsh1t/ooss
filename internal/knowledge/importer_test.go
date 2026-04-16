package knowledge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/require"
)

func setupKnowledgeImportDB(t *testing.T) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-import.sqlite"),
		},
	}

	oldCfg := config.Get()
	config.Set(cfg)

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))

	t.Cleanup(func() {
		config.Set(oldCfg)
		_ = database.Close()
		database.SetDB(nil)
	})

	return cfg
}

func TestImport_ValidatesOptions(t *testing.T) {
	cfg := setupKnowledgeImportDB(t)
	ctx := context.Background()

	_, err := Import(ctx, cfg, ImportOptions{})
	require.ErrorContains(t, err, "type is required")

	_, err = Import(ctx, cfg, ImportOptions{Type: "security-sqlite"})
	require.ErrorContains(t, err, "path is required")

	_, err = Import(ctx, cfg, ImportOptions{Type: "security-sqlite", Path: "/tmp/security_kb.sqlite"})
	require.ErrorContains(t, err, "workspace is required")
}

func TestImport_RejectsUnsupportedType(t *testing.T) {
	cfg := setupKnowledgeImportDB(t)
	ctx := context.Background()

	_, err := Import(ctx, cfg, ImportOptions{
		Type:      "unknown-importer",
		Path:      "/tmp/source.sqlite",
		Workspace: "security-kb",
	})
	require.ErrorContains(t, err, "unsupported importer type")
}
