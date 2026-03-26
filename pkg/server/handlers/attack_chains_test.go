package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
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
	app.Post("/attack-chains/:id/queue-deep-scan", QueueAttackChainDeepScan(cfg))

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
	assert.Equal(t, "report:"+strconv.FormatInt(report.ID, 10)+":chain-verified", storedVuln.AttackChainRef)
	assert.Contains(t, storedVuln.ReportRefs, report.SourcePath)
	assert.Contains(t, storedVuln.RelatedAssets, "https://app.acme.test/login")

	body, err = json.Marshal(map[string]any{
		"flow":          "web-analysis",
		"verified_only": true,
	})
	require.NoError(t, err)
	req = httptest.NewRequest("POST", reportPath+"/queue-deep-scan", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	queueResult = map[string]any{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&queueResult))
	assert.Equal(t, float64(1), queueResult["queued"])

	storedReport, err := database.GetAttackChainReportByID(ctx, report.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, storedReport.QueueHits)
	assert.Equal(t, 1, storedReport.VerifiedHits)
}

func TestImportAttackChain_BackfillsVulnerabilityLinks(t *testing.T) {
	cfg, cleanup := setupAttackChainHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

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

	payload := map[string]any{
		"attack_chain_summary": map[string]any{
			"total_chains":       1,
			"critical_chains":    1,
			"high_impact_chains": 1,
		},
		"attack_chains": []map[string]any{
			{
				"chain_id":   "chain-login",
				"chain_name": "Login SQLi Chain",
				"entry_point": map[string]any{
					"vulnerability": "SQL Injection",
					"url":           "https://app.acme.test/login",
					"severity":      "high",
				},
				"chain_steps": []map[string]any{
					{"step": 1, "action": "exploit login"},
				},
				"final_objective":     "Dump user data",
				"difficulty":          "medium",
				"impact":              "high",
				"success_probability": 0.8,
			},
		},
		"critical_paths": []map[string]any{},
	}

	sourcePath := filepath.Join(cfg.BaseFolder, "attack-chain-import.json")
	textPath := filepath.Join(cfg.BaseFolder, "attack-chain-import.txt")
	mermaidPath := filepath.Join(cfg.BaseFolder, "attack-chain-import.mmd")
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sourcePath, raw, 0o600))
	require.NoError(t, os.WriteFile(textPath, []byte("attack chain report"), 0o600))
	require.NoError(t, os.WriteFile(mermaidPath, []byte("graph TD"), 0o600))

	app := fiber.New()
	app.Post("/attack-chains/import", ImportAttackChain(cfg))

	body, err := json.Marshal(map[string]any{
		"workspace":    "acme",
		"target":       "app.acme.test",
		"run_uuid":     "run-attack-1",
		"source_path":  sourcePath,
		"text_path":    textPath,
		"mermaid_path": mermaidPath,
	})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/attack-chains/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, float64(1), result["linked_vulnerability_count"])
	data := result["data"].(map[string]any)
	reportID := int64(data["id"].(float64))

	storedVuln, err := database.GetVulnerabilityByID(ctx, created.Vulnerability.ID)
	require.NoError(t, err)
	assert.Equal(t, "report:"+strconv.FormatInt(reportID, 10)+":chain-login", storedVuln.AttackChainRef)
	assert.Contains(t, storedVuln.RelatedAssets, "https://app.acme.test/login")
	assert.Contains(t, storedVuln.ReportRefs, sourcePath)
	assert.Contains(t, storedVuln.ReportRefs, textPath)
	assert.Contains(t, storedVuln.ReportRefs, mermaidPath)
	assert.Equal(t, 2, storedVuln.EvidenceVersion)
}
