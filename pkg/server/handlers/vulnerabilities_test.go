package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupVulnerabilityHandlerDB(t *testing.T) (*config.Config, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "vulnerability-handler.sqlite"),
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

func TestGetVulnerability_EnrichesTimelineAndRelations(t *testing.T) {
	cfg, cleanup := setupVulnerabilityHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Add(-2 * time.Minute)

	sourceRun := &database.Run{
		RunUUID:      "run-source-1",
		WorkflowName: "general",
		WorkflowKind: "flow",
		Target:       "https://app.acme.test/login",
		Workspace:    "acme",
		Status:       "completed",
		TriggerType:  "manual",
		RunPriority:  "high",
		RunMode:      "local",
	}
	require.NoError(t, database.CreateRun(ctx, sourceRun))

	vuln := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "verified",
		SourceRunUUID: sourceRun.RunUUID,
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	_, err := database.CreateVulnerabilityRecord(ctx, vuln)
	require.NoError(t, err)

	retestRun := &database.Run{
		RunUUID:      "retest-run-1",
		WorkflowName: "web-analysis",
		WorkflowKind: "flow",
		Target:       vuln.AssetValue,
		Workspace:    vuln.Workspace,
		Status:       "queued",
		TriggerType:  "vuln-retest",
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
	}
	vuln.VulnStatus = "retest"
	vuln.RetestStatus = "queued"
	vuln.RetestRunUUID = retestRun.RunUUID
	require.NoError(t, database.QueueVulnerabilityRetest(ctx, vuln, retestRun))

	asset := &database.Asset{
		Workspace:  "acme",
		AssetValue: "https://app.acme.test/login",
		URL:        "https://app.acme.test/login",
		AssetType:  "url",
		Source:     "httpx",
		CreatedAt:  now,
		UpdatedAt:  now.Add(time.Minute),
		LastSeenAt: now.Add(time.Minute),
	}
	_, err = database.GetDB().NewInsert().Model(asset).Exec(ctx)
	require.NoError(t, err)

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id":   "chain-login",
			"chain_name": "Login SQLi Chain",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://app.acme.test/login",
				"severity":      "high",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login form"},
			},
			"final_objective":     "Dump user data",
			"difficulty":          "medium",
			"impact":              "high",
			"success_probability": 0.8,
		},
	})
	require.NoError(t, err)

	report := &database.AttackChainReport{
		Workspace:        "acme",
		Target:           "app.acme.test",
		RunUUID:          sourceRun.RunUUID,
		SourcePath:       "/tmp/attack-chain-login.json",
		SourceHash:       "attack-chain-hash",
		Status:           "ready",
		TotalChains:      1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now.Add(2 * time.Minute),
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	app := fiber.New()
	app.Get("/vulnerabilities/:id", GetVulnerability(cfg))

	req := httptest.NewRequest("GET", "/vulnerabilities/"+strconv.FormatInt(vuln.ID, 10), nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		Data struct {
			ID                  int64                            `json:"id"`
			VulnStatus          string                           `json:"vuln_status"`
			EvidenceVersion     int                              `json:"evidence_version"`
			EvidenceTimeline    []database.VulnerabilityEvidence `json:"evidence_timeline"`
			RelatedRuns         []map[string]any                 `json:"related_runs"`
			RelatedAssetRecords []map[string]any                 `json:"related_asset_records"`
			RelatedAttackChains []map[string]any                 `json:"related_attack_chains"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))

	assert.Equal(t, vuln.ID, payload.Data.ID)
	assert.Equal(t, "retest", payload.Data.VulnStatus)
	assert.Equal(t, 2, payload.Data.EvidenceVersion)
	require.Len(t, payload.Data.EvidenceTimeline, 2)
	assert.Equal(t, "verified", payload.Data.EvidenceTimeline[0].VulnStatus)
	assert.Equal(t, "retest", payload.Data.EvidenceTimeline[1].VulnStatus)
	require.Len(t, payload.Data.RelatedRuns, 2)
	require.Len(t, payload.Data.RelatedAssetRecords, 1)
	require.Len(t, payload.Data.RelatedAttackChains, 1)
	assert.Equal(t, "chain-login", payload.Data.RelatedAttackChains[0]["chain_id"])
	assert.Equal(t, "entry_point_url", payload.Data.RelatedAttackChains[0]["matched_by"])
}
