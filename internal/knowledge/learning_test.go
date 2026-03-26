package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLearnWorkspace_GeneratesStructuredKnowledgeDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	workspaceDir := filepath.Join(workspacesDir, "acme")
	aiDir := filepath.Join(workspaceDir, "ai-analysis")
	require.NoError(t, os.MkdirAll(aiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "unified-analysis-001.md"), []byte("# Insight\n\nUse verified findings first."), 0o644))

	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning.sqlite"),
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

	asset := &database.Asset{
		Workspace:    "acme",
		AssetValue:   "https://app.acme.test/login",
		URL:          "https://app.acme.test/login",
		AssetType:    "url",
		Technologies: []string{"go", "nginx"},
		Source:       "httpx",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastSeenAt:   now,
	}
	_, err = database.GetDB().NewInsert().Model(asset).Exec(ctx)
	require.NoError(t, err)

	verified := &database.Vulnerability{
		Workspace:       "acme",
		VulnInfo:        "sql-injection",
		VulnTitle:       "SQL Injection",
		VulnDesc:        "UNION based SQL injection on login endpoint.",
		Severity:        "high",
		Confidence:      "certain",
		AssetType:       "url",
		AssetValue:      asset.URL,
		VulnStatus:      "verified",
		EvidenceVersion: 2,
		AISummary:       "Reproduced with UNION payload.",
		CreatedAt:       now,
		UpdatedAt:       now,
		LastSeenAt:      now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)

	falsePositive := &database.Vulnerability{
		Workspace:    "acme",
		VulnInfo:     "open-redirect",
		VulnTitle:    "Open Redirect",
		Severity:     "medium",
		Confidence:   "firm",
		AssetType:    "url",
		AssetValue:   "https://app.acme.test/redirect",
		VulnStatus:   "false_positive",
		AnalystNotes: "Validated as intended redirect allowlist behavior.",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastSeenAt:   now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, falsePositive)
	require.NoError(t, err)

	run := &database.Run{
		RunUUID:      "run-1",
		WorkflowName: "superdomain-extensive-ai",
		WorkflowKind: "flow",
		Target:       "app.acme.test",
		Status:       "completed",
		Workspace:    "acme",
		RunPriority:  "high",
		RunMode:      "local",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateRun(ctx, run))

	summary, err := LearnWorkspace(ctx, cfg, LearnOptions{
		Workspace: "acme",
	})
	require.NoError(t, err)
	assert.Equal(t, 4, summary.Documents)
	assert.Equal(t, "acme", summary.StorageWorkspace)
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/workspace-summary.md")
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/verified-findings.md")
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/false-positive-samples.md")
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/ai-insights.md")

	docs, err := ListDocuments(ctx, "acme", 0, 20)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, docs.TotalCount, 4)
}

func TestLearnWorkspace_PublicScopeStoresDocumentsInPublicLayer(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning-public.sqlite"),
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
	verified := &database.Vulnerability{
		Workspace:  "acme",
		VulnInfo:   "sql-injection",
		VulnTitle:  "SQL Injection",
		Severity:   "high",
		Confidence: "certain",
		AssetType:  "url",
		AssetValue: "https://app.acme.test/login",
		VulnStatus: "verified",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)

	summary, err := LearnWorkspace(ctx, cfg, LearnOptions{
		Workspace: "acme",
		Scope:     "public",
	})
	require.NoError(t, err)
	assert.Equal(t, "acme", summary.Workspace)
	assert.Equal(t, "public", summary.StorageWorkspace)

	docs, err := ListDocuments(ctx, "public", 0, 20)
	require.NoError(t, err)
	require.NotEmpty(t, docs.Data)
	assert.Equal(t, "public", docs.Data[0].Workspace)
	assert.Contains(t, docs.Data[0].Metadata, `"source_workspace":"acme"`)
	assert.Contains(t, docs.Data[0].Metadata, `"knowledge_layer":"public"`)
}
