package attackchain

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTargetSummary_UsesLiveVerifiedLinksInsteadOfStoredMetrics(t *testing.T) {
	cfg, cleanup := setupAttackChainSummaryDB(t)
	defer cleanup()
	_ = cfg

	ctx := context.Background()
	now := time.Now()

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-login",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://app.acme.test/login",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login"},
			},
		},
	})
	require.NoError(t, err)

	report := &database.AttackChainReport{
		Workspace:        "acme",
		Target:           "https://app.acme.test",
		RunUUID:          "run-chain-1",
		SourcePath:       "/tmp/attack-chain-summary.json",
		SourceHash:       "chain-hash-1",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	falsePositive := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "false_positive",
		SourceRunUUID: "run-vuln-1",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, falsePositive)
	require.NoError(t, err)

	summary, err := GetTargetSummary(ctx, "acme", "https://app.acme.test")
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 1, summary.ReportCount)
	assert.Equal(t, 1, summary.HighImpactChains)
	assert.Equal(t, 0, summary.VerifiedHits)

	verified := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "verified",
		SourceRunUUID: "run-vuln-2",
		CreatedAt:     now.Add(time.Minute),
		UpdatedAt:     now.Add(time.Minute),
		LastSeenAt:    now.Add(time.Minute),
	}
	result, err := database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Vulnerability)

	summary, err = GetTargetSummary(ctx, "acme", "https://app.acme.test")
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 1, summary.VerifiedHits)
	assert.Equal(t, 1, summary.OperationalHits)

	stored := result.Vulnerability
	stored.VulnStatus = "retest"
	stored.RetestStatus = "queued"
	stored.UpdatedAt = now.Add(2 * time.Minute)
	stored.LastSeenAt = now.Add(2 * time.Minute)
	require.NoError(t, database.UpdateVulnerabilityRecord(ctx, stored))

	summary, err = GetTargetSummary(ctx, "acme", "https://app.acme.test")
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 0, summary.VerifiedHits)
	assert.Equal(t, 1, summary.OperationalHits)
}

func setupAttackChainSummaryDB(t *testing.T) (*config.Config, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "attack-chain-summary.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))

	return cfg, func() {
		_ = database.Close()
		database.SetDB(nil)
	}
}
