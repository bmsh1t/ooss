package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
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
		{Workspace: "ws-a", Severity: "high", AssetValue: "https://a.example.com/login", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", Severity: "low", AssetValue: "https://b.example.com/home", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", VulnInfo: "sql-injection", VulnTitle: "SQL Injection", Severity: "high", Confidence: "certain", AssetType: "url", AssetValue: "https://b.example.com/login", VulnStatus: "verified", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-b",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://b.example.com/login",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login"},
			},
		},
	})
	require.NoError(t, err)

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
		AttackChainsJSON: string(chainsJSON),
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
		{Workspace: "ws-a", Severity: "critical", AssetValue: "https://a.example.com/login", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", Severity: "low", AssetValue: "https://b.example.com/home", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", VulnInfo: "sql-injection", VulnTitle: "SQL Injection", Severity: "high", Confidence: "certain", AssetType: "url", AssetValue: "https://b.example.com/login", VulnStatus: "verified", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-b",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://b.example.com/login",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login"},
			},
		},
	})
	require.NoError(t, err)

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
		AttackChainsJSON: string(chainsJSON),
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

func TestQueueCampaignDeepScan_ScopesRiskPerTargetInSameWorkspace(t *testing.T) {
	_, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                   "camp-deep-shared",
		Name:                 "deep-campaign-shared",
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

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-shared-a",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-shared",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-shared-b",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://b.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-shared",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	_, err := database.GetDB().NewInsert().Model(&database.Vulnerability{
		Workspace:  "ws-shared",
		Severity:   "critical",
		AssetValue: "https://a.example.com/admin",
		VulnStatus: "new",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}).Exec(ctx)
	require.NoError(t, err)

	queued, targets, err := queueCampaignDeepScanRuns(ctx, campaign)
	require.NoError(t, err)
	assert.Equal(t, 1, queued)
	assert.Equal(t, []string{"https://a.example.com"}, targets)
}

func TestGetCampaignStatus_TargetRiskIsScopedPerTarget(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-target-scope-api",
		Name:               "target-scope-api",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-scope-a",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "shared-ws",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-scope-b",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://b.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "shared-ws",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	for _, vuln := range []*database.Vulnerability{
		{
			Workspace:  "shared-ws",
			Severity:   "critical",
			AssetValue: "https://a.example.com/admin",
			VulnStatus: "new",
			CreatedAt:  now,
			UpdatedAt:  now,
			LastSeenAt: now,
		},
		{
			Workspace:  "shared-ws",
			Severity:   "medium",
			AssetValue: "https://b.example.com/robots.txt",
			VulnStatus: "new",
			CreatedAt:  now,
			UpdatedAt:  now,
			LastSeenAt: now,
		},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	app := fiber.New()
	app.Get("/campaigns/:id", GetCampaignStatus(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var result struct {
		HighRiskTargets []string `json:"high_risk_targets"`
		Summary         struct {
			HighRiskTargets int `json:"high_risk_targets"`
		} `json:"summary"`
		Targets []struct {
			Target      string         `json:"target"`
			RiskLevel   string         `json:"risk_level"`
			VulnSummary map[string]int `json:"vuln_summary"`
		} `json:"targets"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	require.Len(t, result.HighRiskTargets, 1)
	assert.Equal(t, "https://a.example.com", result.HighRiskTargets[0])
	assert.Equal(t, 1, result.Summary.HighRiskTargets)

	targetMap := make(map[string]struct {
		RiskLevel   string
		VulnSummary map[string]int
	})
	for _, target := range result.Targets {
		targetMap[target.Target] = struct {
			RiskLevel   string
			VulnSummary map[string]int
		}{
			RiskLevel:   target.RiskLevel,
			VulnSummary: target.VulnSummary,
		}
	}
	assert.Equal(t, "critical", targetMap["https://a.example.com"].RiskLevel)
	assert.Equal(t, 1, targetMap["https://a.example.com"].VulnSummary["critical"])
	assert.Equal(t, "medium", targetMap["https://b.example.com"].RiskLevel)
	assert.Zero(t, targetMap["https://b.example.com"].VulnSummary["critical"])
}

func TestGetCampaignStatus_IgnoresClosedAndFalsePositiveFindings(t *testing.T) {
	_, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-status-filtered",
		Name:               "status-filtered",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-filtered-a",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-filtered-a",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-filtered-b",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://b.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-filtered-b",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	for _, vuln := range []*database.Vulnerability{
		{Workspace: "ws-filtered-a", Severity: "critical", AssetValue: "https://a.example.com/login", VulnStatus: "closed", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-filtered-b", Severity: "high", AssetValue: "https://b.example.com/admin", VulnStatus: "false_positive", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	response, err := buildCampaignStatusResponse(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, 0, response.Summary.HighRiskTargets)
	assert.Empty(t, response.HighRiskTargets)
	for _, target := range response.Targets {
		assert.Equal(t, "none", target.RiskLevel)
	}
}

func TestGetCampaignStatus_UsesLiveAttackChainVerifiedLinks(t *testing.T) {
	_, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-status-live-attack-chain",
		Name:               "status-live-attack-chain",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	run := &database.Run{
		RunUUID:      "run-live-attack-chain",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://live.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-live-attack-chain",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateRun(ctx, run))

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-live",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://live.example.com/login",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login"},
			},
		},
	})
	require.NoError(t, err)

	report := &database.AttackChainReport{
		Workspace:        "ws-live-attack-chain",
		Target:           "https://live.example.com",
		RunUUID:          "run-chain-live",
		SourcePath:       "/tmp/live-attack-chain.json",
		SourceHash:       "live-chain-hash",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	vuln := &database.Vulnerability{
		Workspace:     "ws-live-attack-chain",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://live.example.com/login",
		VulnStatus:    "false_positive",
		SourceRunUUID: "run-vuln-live",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, vuln)
	require.NoError(t, err)

	response, err := buildCampaignStatusResponse(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, 0, response.Summary.HighRiskTargets)
	assert.Zero(t, response.Summary.VerifiedAttackChainTargets)
	assert.Empty(t, response.HighRiskTargets)
	require.Len(t, response.Targets, 1)
	require.NotNil(t, response.Targets[0].AttackChainSummary)
	assert.Zero(t, response.Targets[0].AttackChainSummary.VerifiedHits)
	assert.Equal(t, "none", response.Targets[0].RiskLevel)
}

func TestGetCampaignStatus_TreatsRetestAttackChainSignalsAsHighRisk(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-status-operational-chain",
		Name:               "status-operational-chain",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	run := &database.Run{
		RunUUID:      "run-operational-chain",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://ops.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-operational-chain",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateRun(ctx, run))

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-operational",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://ops.example.com/login",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login"},
			},
		},
	})
	require.NoError(t, err)

	report := &database.AttackChainReport{
		Workspace:        "ws-operational-chain",
		Target:           "https://ops.example.com",
		RunUUID:          "run-chain-operational",
		SourcePath:       "/tmp/operational-attack-chain.json",
		SourceHash:       "operational-chain-hash",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	_, err = database.CreateVulnerabilityRecord(ctx, &database.Vulnerability{
		Workspace:     "ws-operational-chain",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "low",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://ops.example.com/login",
		VulnStatus:    "retest",
		RetestStatus:  "queued",
		SourceRunUUID: "run-vuln-operational",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	})
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/campaigns/:id", GetCampaignStatus(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		HighRiskTargets []string `json:"high_risk_targets"`
		Summary         struct {
			HighRiskTargets            int `json:"high_risk_targets"`
			VerifiedAttackChainTargets int `json:"verified_attack_chain_targets"`
		} `json:"summary"`
		Targets []struct {
			Target             string                                `json:"target"`
			RiskLevel          string                                `json:"risk_level"`
			AttackChainSummary *database.AttackChainWorkspaceSummary `json:"attack_chain_summary"`
		} `json:"targets"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))

	require.Len(t, payload.HighRiskTargets, 1)
	assert.Equal(t, "https://ops.example.com", payload.HighRiskTargets[0])
	assert.Equal(t, 1, payload.Summary.HighRiskTargets)
	assert.Zero(t, payload.Summary.VerifiedAttackChainTargets)
	require.Len(t, payload.Targets, 1)
	assert.Equal(t, "high", payload.Targets[0].RiskLevel)
	require.NotNil(t, payload.Targets[0].AttackChainSummary)
	assert.Zero(t, payload.Targets[0].AttackChainSummary.VerifiedHits)
	assert.Equal(t, 1, payload.Targets[0].AttackChainSummary.OperationalHits)
}

func TestGetCampaignReport_IncludesAnalyticsAndRerunHistory(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                   "camp-report-api",
		Name:                 "report-api",
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

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-report-failed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-a",
			CreatedAt:    now.Add(-3 * time.Hour),
			UpdatedAt:    now.Add(-3 * time.Hour),
		},
		{
			RunUUID:      "run-report-rerun",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover.example.com",
			Status:       "completed",
			TriggerType:  "campaign-rerun",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-a",
			CreatedAt:    now.Add(-2 * time.Hour),
			UpdatedAt:    now.Add(-2 * time.Hour),
		},
		{
			RunUUID:      "run-report-deep",
			WorkflowName: "deep-flow",
			WorkflowKind: "flow",
			Target:       "https://recover.example.com",
			Status:       "queued",
			TriggerType:  "campaign-deep-scan",
			RunGroupID:   campaign.ID,
			RunPriority:  "critical",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-a",
			CreatedAt:    now.Add(-90 * time.Minute),
			UpdatedAt:    now.Add(-90 * time.Minute),
		},
		{
			RunUUID:      "run-report-stable",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://stable.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-b",
			CreatedAt:    now.Add(-1 * time.Hour),
			UpdatedAt:    now.Add(-1 * time.Hour),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	_, err := database.CreateVulnerabilityRecord(ctx, &database.Vulnerability{
		Workspace:     "ws-report-a",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://recover.example.com/login",
		VulnStatus:    "verified",
		SourceRunUUID: "run-vuln-report-a",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	})
	require.NoError(t, err)

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-report-a",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://recover.example.com/login",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login"},
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, database.UpsertAttackChainReport(ctx, &database.AttackChainReport{
		Workspace:        "ws-report-a",
		Target:           "https://recover.example.com",
		RunUUID:          "run-chain-report-a",
		SourcePath:       "/tmp/report-chain.json",
		SourceHash:       "report-chain-hash",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}))

	app := fiber.New()
	app.Get("/campaigns/:id/report", GetCampaignReport(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID+"/report", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var report CampaignReportResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&report))

	assert.Equal(t, "completed", report.Status)
	assert.Equal(t, 2, report.Progress.Total)
	assert.Equal(t, 2, report.Progress.Completed)
	assert.Equal(t, 1, report.Summary.HighRiskTargets)
	assert.Equal(t, 1, report.RiskDistribution["high"])
	assert.Equal(t, 1, report.RiskDistribution["none"])
	assert.Equal(t, 2, report.LatestRunStatusDistribution["completed"])
	assert.Equal(t, 2, report.TriggerDistribution["campaign"])
	assert.Equal(t, 1, report.TriggerDistribution["campaign-rerun"])
	assert.Equal(t, 1, report.TriggerDistribution["campaign-deep-scan"])
	assert.True(t, report.DeepScan.Configured)
	assert.Equal(t, 1, report.DeepScan.EligibleTargets)
	assert.Equal(t, 1, report.DeepScan.QueuedTargets)
	assert.Equal(t, 1, report.DeepScan.TotalRuns)
	assert.Equal(t, 1, report.DeepScan.QueuedRuns)
	assert.InDelta(t, 1.0, report.DeepScan.ConversionRate, 0.001)
	assert.Equal(t, 1, report.RerunHistory.TotalRuns)
	assert.Equal(t, 1, report.RerunHistory.UniqueTargets)
	assert.Equal(t, 1, report.RerunHistory.RecoveredTargets)
	require.NotNil(t, report.RerunHistory.LastRerunAt)
	require.Len(t, report.Targets, 2)

	assert.Equal(t, "https://recover.example.com", report.Targets[0].Target)
	assert.Equal(t, "high", report.Targets[0].RiskLevel)
	assert.Equal(t, 3, report.Targets[0].TotalRuns)
	assert.Equal(t, 1, report.Targets[0].RerunRuns)
	assert.Equal(t, 1, report.Targets[0].DeepScanRuns)
	assert.True(t, report.Targets[0].Recovered)
	assert.True(t, report.Targets[0].DeepScanQueued)
	assert.Equal(t, "campaign-rerun", report.Targets[0].LatestTriggerType)
	assert.Equal(t, 1, report.Targets[0].OpenHighRiskFindings)
	assert.Equal(t, 1, report.Targets[0].AttackChainOperationalHits)
	assert.Equal(t, 1, report.Targets[0].AttackChainVerifiedHits)
	assert.Equal(t, "https://stable.example.com", report.Targets[1].Target)
	assert.Equal(t, "none", report.Targets[1].RiskLevel)
}

func TestExportCampaignReport_CSV(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-export-api",
		Name:               "export-api",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	require.NoError(t, database.CreateRun(ctx, &database.Run{
		RunUUID:      "run-export",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://csv.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-export",
		CreatedAt:    now,
		UpdatedAt:    now,
	}))

	_, err := database.CreateVulnerabilityRecord(ctx, &database.Vulnerability{
		Workspace:     "ws-export",
		VulnInfo:      "open-redirect",
		VulnTitle:     "Open Redirect",
		Severity:      "medium",
		Confidence:    "firm",
		AssetType:     "url",
		AssetValue:    "https://csv.example.com/redirect",
		VulnStatus:    "new",
		SourceRunUUID: "run-vuln-export",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	})
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/campaigns/:id/export", ExportCampaignReport(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID+"/export", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/csv")
	assert.Contains(t, resp.Header.Get("Content-Disposition"), "campaign-"+campaign.ID+"-report.csv")

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, []string{
		"target",
		"workspace",
		"status",
		"risk_level",
		"latest_run_uuid",
		"latest_trigger_type",
		"latest_run_at",
		"deep_scan_queued",
		"total_runs",
		"rerun_runs",
		"deep_scan_runs",
		"recovered",
		"critical_findings",
		"high_findings",
		"medium_findings",
		"low_findings",
		"open_high_risk_findings",
		"attack_chain_operational_hits",
		"attack_chain_verified_hits",
	}, records[0])
	assert.Equal(t, "https://csv.example.com", records[1][0])
	assert.Equal(t, "ws-export", records[1][1])
	assert.Equal(t, "completed", records[1][2])
	assert.Equal(t, "medium", records[1][3])
	assert.Equal(t, "run-export", records[1][4])
	assert.Equal(t, "campaign", records[1][5])
	assert.Equal(t, "false", records[1][7])
	assert.Equal(t, "1", records[1][8])
	assert.Equal(t, "0", records[1][9])
	assert.Equal(t, "0", records[1][10])
	assert.Equal(t, "false", records[1][11])
	assert.Equal(t, "0", records[1][12])
	assert.Equal(t, "0", records[1][13])
	assert.Equal(t, "1", records[1][14])
	assert.Equal(t, "0", records[1][15])
	assert.Equal(t, "0", records[1][16])
	assert.Equal(t, "0", records[1][17])
	assert.Equal(t, "0", records[1][18])
}

func TestGetCampaignReport_AppliesFiltersAndPresets(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-report-filter-api",
		Name:               "report-filter-api",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-filter-api-failed-old",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover-filter.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-filter-api-a",
			CreatedAt:    now.Add(-2 * time.Hour),
			UpdatedAt:    now.Add(-2 * time.Hour),
		},
		{
			RunUUID:      "run-filter-api-rerun",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover-filter.example.com",
			Status:       "completed",
			TriggerType:  "campaign-rerun",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-filter-api-a",
			CreatedAt:    now.Add(-1 * time.Hour),
			UpdatedAt:    now.Add(-1 * time.Hour),
		},
		{
			RunUUID:      "run-filter-api-failed-current",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://failed-filter.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-filter-api-b",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	for _, vuln := range []*database.Vulnerability{
		{
			Workspace:     "ws-filter-api-a",
			VulnInfo:      "sql-injection",
			VulnTitle:     "SQL Injection",
			Severity:      "high",
			Confidence:    "certain",
			AssetType:     "url",
			AssetValue:    "https://recover-filter.example.com/login",
			VulnStatus:    "verified",
			SourceRunUUID: "run-vuln-filter-api-a",
			CreatedAt:     now,
			UpdatedAt:     now,
			LastSeenAt:    now,
		},
		{
			Workspace:     "ws-filter-api-b",
			VulnInfo:      "debug-page",
			VulnTitle:     "Debug Page",
			Severity:      "low",
			Confidence:    "firm",
			AssetType:     "url",
			AssetValue:    "https://failed-filter.example.com/debug",
			VulnStatus:    "new",
			SourceRunUUID: "run-vuln-filter-api-b",
			CreatedAt:     now,
			UpdatedAt:     now,
			LastSeenAt:    now,
		},
	} {
		_, err := database.CreateVulnerabilityRecord(ctx, vuln)
		require.NoError(t, err)
	}

	app := fiber.New()
	app.Get("/campaigns/:id/report", GetCampaignReport(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID+"/report?risk=high&status=completed&trigger=campaign-rerun&preset=recovered", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var report CampaignReportResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&report))
	assert.Equal(t, 2, report.TotalTargets)
	assert.Equal(t, 1, report.ResultCount)
	assert.Equal(t, []string{"high"}, report.FiltersApplied.RiskLevels)
	assert.Equal(t, []string{"completed"}, report.FiltersApplied.Statuses)
	assert.Equal(t, []string{"campaign-rerun"}, report.FiltersApplied.TriggerTypes)
	assert.Equal(t, "recovered", report.FiltersApplied.Preset)
	require.Len(t, report.Targets, 1)
	assert.Equal(t, "https://recover-filter.example.com", report.Targets[0].Target)
	assert.True(t, report.Targets[0].Recovered)
	assert.Equal(t, "campaign-rerun", report.Targets[0].LatestTriggerType)
}

func TestGetCampaignReport_AppliesPagination(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-report-page-api",
		Name:         "report-page-api",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-page-api.example.com",
		"https://b-page-api.example.com",
		"https://c-page-api.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-report-page-api-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-page-api-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	app := fiber.New()
	app.Get("/campaigns/:id/report", GetCampaignReport(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID+"/report?status=completed&offset=1&limit=1", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var report CampaignReportResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&report))
	assert.Equal(t, 3, report.TotalTargets)
	assert.Equal(t, 3, report.ResultCount)
	assert.Equal(t, 1, report.Pagination.Offset)
	assert.Equal(t, 1, report.Pagination.Limit)
	assert.Equal(t, 1, report.Pagination.ReturnedCount)
	assert.True(t, report.Pagination.HasMore)
	require.Len(t, report.Targets, 1)
	assert.Equal(t, "https://b-page-api.example.com", report.Targets[0].Target)
}

func TestExportCampaignReport_CSV_AppliesPreset(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-export-filter-api",
		Name:               "export-filter-api",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		HighRiskSeverities: []string{"critical", "high"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-export-filter-failed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://failed-export.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-filter-a",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-export-filter-completed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://done-export.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-filter-b",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	app := fiber.New()
	app.Get("/campaigns/:id/export", ExportCampaignReport(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID+"/export?preset=failed", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "https://failed-export.example.com", records[1][0])
}

func TestExportCampaignReport_CSV_AppliesPagination(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-export-page-api",
		Name:         "export-page-api",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-export-api.example.com",
		"https://b-export-api.example.com",
		"https://c-export-api.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-export-page-api-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-page-api-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	app := fiber.New()
	app.Get("/campaigns/:id/export", ExportCampaignReport(cfg))

	req := httptest.NewRequest("GET", "/campaigns/"+campaign.ID+"/export?status=completed&offset=1&limit=1", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "https://b-export-api.example.com", records[1][0])
}

func TestRerunFailedCampaignTargets_OnlyRerunsLatestFailedTargets(t *testing.T) {
	cfg, cleanup := setupCampaignHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-rerun-latest",
		Name:         "rerun-latest",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-superseded-failed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://stable.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-stable",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-superseded-completed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://stable.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-stable",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
		{
			RunUUID:      "run-current-failed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://failed.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "critical",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-failed",
			CreatedAt:    now.Add(2 * time.Minute),
			UpdatedAt:    now.Add(2 * time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	app := fiber.New()
	app.Post("/campaigns/:id/rerun-failed", RerunFailedCampaignTargets(cfg))

	req := httptest.NewRequest("POST", "/campaigns/"+campaign.ID+"/rerun-failed", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		QueuedCount int `json:"queued_count"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, 1, payload.QueuedCount)

	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	var rerunTargets []string
	for _, run := range runs {
		if run.TriggerType == "campaign-rerun" {
			rerunTargets = append(rerunTargets, run.Target)
		}
	}
	assert.Equal(t, []string{"https://failed.example.com"}, rerunTargets)
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
