package handlers

import (
	"bytes"
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

func setupAttackChainHandlerDB(t *testing.T) (*config.Config, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "attack-chain-handler.sqlite"),
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

func TestGetAttackChainVerifiedOnlyAndQueueRetest(t *testing.T) {
	cfg, cleanup := setupAttackChainHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id":   "chain-verified",
			"chain_name": "Verified SQLi Chain",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://app.acme.test/login",
				"severity":      "high",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login", "vulnerability": "SQL Injection"},
			},
			"final_objective":     "Dump session data",
			"difficulty":          "medium",
			"impact":              "high",
			"success_probability": 0.8,
		},
		{
			"chain_id":   "chain-unverified",
			"chain_name": "Unverified Chain",
			"entry_point": map[string]any{
				"vulnerability": "Open Redirect",
				"url":           "https://app.acme.test/redirect",
				"severity":      "low",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "bounce victim"},
			},
			"final_objective":     "Phish user",
			"difficulty":          "low",
			"impact":              "medium",
			"success_probability": 0.3,
		},
	})
	require.NoError(t, err)

	report := &database.AttackChainReport{
		Workspace:        "acme",
		Target:           "app.acme.test",
		RunUUID:          "run-attack-1",
		SourcePath:       "/tmp/attack-chain.json",
		SourceHash:       "hash-1",
		Status:           "ready",
		TotalChains:      2,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	vuln := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "verified",
		SourceRunUUID: "run-1",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	created, err := database.CreateVulnerabilityRecord(ctx, vuln)
	require.NoError(t, err)
	require.NotNil(t, created.Vulnerability)

	app := fiber.New()
	app.Get("/attack-chains/:id", GetAttackChain(cfg))
	app.Post("/attack-chains/:id/queue-retest", QueueAttackChainRetest(cfg))

	reportPath := "/attack-chains/" + strconv.FormatInt(report.ID, 10)
	req := httptest.NewRequest("GET", reportPath+"?verified_only=true", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var getResult map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&getResult))
	data := getResult["data"].(map[string]any)
	chains := data["chains"].([]any)
	require.Len(t, chains, 1)
	chain := chains[0].(map[string]any)
	assert.Equal(t, "chain-verified", chain["chain_id"])

	body, err := json.Marshal(map[string]any{
		"module":        "retest-module",
		"verified_only": true,
	})
	require.NoError(t, err)
	req = httptest.NewRequest("POST", reportPath+"/queue-retest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var queueResult map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&queueResult))
	assert.Equal(t, float64(1), queueResult["queued"])
	assert.Equal(t, float64(1), queueResult["verified_hits"])

	storedVuln, err := database.GetVulnerabilityByID(ctx, created.Vulnerability.ID)
	require.NoError(t, err)
	assert.Equal(t, "retest", storedVuln.VulnStatus)
	assert.Equal(t, "queued", storedVuln.RetestStatus)
	assert.NotEmpty(t, storedVuln.RetestRunUUID)

	storedReport, err := database.GetAttackChainReportByID(ctx, report.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, storedReport.QueueHits)
	assert.Equal(t, 1, storedReport.VerifiedHits)
}
