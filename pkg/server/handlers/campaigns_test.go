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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCampaignHandlerDB(t *testing.T) (*config.Config, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "campaign-handler.sqlite"),
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

func TestGetCampaignStatus_IncludesAttackChainSummary(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-status-api",
		Name:               "status-campaign-api",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	runs := []*database.Run{
		{
			RunUUID:      "run-old",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-a",
			CreatedAt:    now.Add(-2 * time.Hour),
			UpdatedAt:    now.Add(-2 * time.Hour),
		},
		{
			RunUUID:      "run-new",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-a",
			CreatedAt:    now.Add(-1 * time.Hour),
			UpdatedAt:    now.Add(-1 * time.Hour),
		},
		{
			RunUUID:      "run-pending",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://b.example.com",
			Status:       "queued",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-b",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-deep",
			WorkflowName: "deep-flow",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "queued",
			TriggerType:  "campaign-deep-scan",
			RunGroupID:   campaign.ID,
			RunPriority:  "critical",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-a",
			CreatedAt:    now.Add(10 * time.Minute),
			UpdatedAt:    now.Add(10 * time.Minute),
		},
	}
	for _, run := range runs {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	for _, vuln := range []*database.Vulnerability{
		{Workspace: "ws-a", Severity: "high", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", Severity: "low", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	report := &database.AttackChainReport{
		Workspace:        "ws-b",
		Target:           "https://b.example.com",
		RunUUID:          "run-chain-b",
		SourcePath:       "/tmp/ws-b-chain.json",
		SourceHash:       "hash-b",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	app := fiber.New()
	app.Get("/campaigns/:id", GetCampaignStatus(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var result struct {
		Status          string   `json:"status"`
		HighRiskTargets []string `json:"high_risk_targets"`
		Summary         struct {
			TargetsTotal               int `json:"targets_total"`
			HighRiskTargets            int `json:"high_risk_targets"`
			DeepScanQueuedTargets      int `json:"deep_scan_queued_targets"`
			AttackChainAwareTargets    int `json:"attack_chain_aware_targets"`
			VerifiedAttackChainTargets int `json:"verified_attack_chain_targets"`
		} `json:"summary"`
		Targets []struct {
			Target             string                                `json:"target"`
			RiskLevel          string                                `json:"risk_level"`
			DeepScanQueued     bool                                  `json:"deep_scan_queued"`
			AttackChainSummary *database.AttackChainWorkspaceSummary `json:"attack_chain_summary"`
		} `json:"targets"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	assert.Equal(t, "partial", result.Status)
	assert.Equal(t, 2, result.Summary.TargetsTotal)
	assert.Equal(t, 2, result.Summary.HighRiskTargets)
	assert.Equal(t, 1, result.Summary.DeepScanQueuedTargets)
	assert.Equal(t, 1, result.Summary.AttackChainAwareTargets)
	assert.Equal(t, 1, result.Summary.VerifiedAttackChainTargets)
	assert.Contains(t, result.HighRiskTargets, "https://a.example.com")
	assert.Contains(t, result.HighRiskTargets, "https://b.example.com")

	targetMap := make(map[string]struct {
		Target             string
		RiskLevel          string
		DeepScanQueued     bool
		AttackChainSummary *database.AttackChainWorkspaceSummary
	})
	for _, target := range result.Targets {
		targetMap[target.Target] = struct {
			Target             string
			RiskLevel          string
			DeepScanQueued     bool
			AttackChainSummary *database.AttackChainWorkspaceSummary
		}{
			Target:             target.Target,
			RiskLevel:          target.RiskLevel,
			DeepScanQueued:     target.DeepScanQueued,
			AttackChainSummary: target.AttackChainSummary,
		}
	}
	assert.Equal(t, "high", targetMap["https://a.example.com"].RiskLevel)
	assert.True(t, targetMap["https://a.example.com"].DeepScanQueued)
	assert.Equal(t, "high", targetMap["https://b.example.com"].RiskLevel)
	require.NotNil(t, targetMap["https://b.example.com"].AttackChainSummary)
	assert.Equal(t, 1, targetMap["https://b.example.com"].AttackChainSummary.VerifiedHits)
}

func TestQueueCampaignDeepScan_UsesAttackChainSignals(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                   "camp-deep-api",
		Name:                 "deep-campaign-api",
		WorkflowName:         "web-classic",
		WorkflowKind:         "flow",
		Status:               "queued",
		DeepScanWorkflow:     "deep-flow",
		DeepScanWorkflowKind: "flow",
		HighRiskSeverities:   []string{"critical", "high"},
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	runA := &database.Run{
		RunUUID:      "run-a",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://a.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-a",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	runB := &database.Run{
		RunUUID:      "run-b",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://b.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-b",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateRun(ctx, runA))
	require.NoError(t, database.CreateRun(ctx, runB))

	for _, vuln := range []*database.Vulnerability{
		{Workspace: "ws-a", Severity: "critical", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", Severity: "low", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	report := &database.AttackChainReport{
		Workspace:        "ws-b",
		Target:           "https://b.example.com",
		RunUUID:          "run-chain-b",
		SourcePath:       "/tmp/ws-b-chain.json",
		SourceHash:       "hash-b",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	app := fiber.New()
	app.Post("/campaigns/:id/deep-scan", QueueCampaignDeepScan(cfg))

	req := httptest.NewRequest("POST", "/campaigns/"+campaign.ID+"/deep-scan", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var result struct {
		QueuedCount int      `json:"queued_count"`
		Targets     []string `json:"targets"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, 2, result.QueuedCount)
	assert.ElementsMatch(t, []string{"https://a.example.com", "https://b.example.com"}, result.Targets)

	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	var deepScans []*database.Run
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			deepScans = append(deepScans, run)
		}
	}
	require.Len(t, deepScans, 2)
	workspaces := []string{deepScans[0].Workspace, deepScans[1].Workspace}
	assert.ElementsMatch(t, []string{"ws-a", "ws-b"}, workspaces)
	for _, deepScan := range deepScans {
		assert.Equal(t, "deep-flow", deepScan.WorkflowName)
		assert.Equal(t, "critical", deepScan.RunPriority)
		assert.Equal(t, "queue", deepScan.RunMode)
		assert.Equal(t, "deep_scan", deepScan.Params["campaign_stage"])
	}
}

func TestCreateCampaign_UsesHostWorkspaceForURLTarget(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	workflowDir := filepath.Join(cfg.BaseFolder, "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o755))
	cfg.WorkflowsPath = workflowDir
	require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "general.yaml"), []byte("name: general\nkind: module\nsteps:\n  - name: noop\n    type: bash\n    command: echo ok\n"), 0o600))

	app := fiber.New()
	app.Post("/campaigns", CreateCampaign(cfg))

	body, err := json.Marshal(map[string]any{
		"name":     "workspace-normalization",
		"module":   "general",
		"targets":  []string{"https://app.example.com/login"},
		"priority": "high",
	})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/campaigns", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	campaignID := result["campaign_id"].(string)

	runs, err := database.GetRunsByRunGroupID(context.Background(), campaignID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "app.example.com", runs[0].Workspace)
}
