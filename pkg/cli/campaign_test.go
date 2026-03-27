package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCampaignCreate_CreatesCampaignAndQueuedRuns(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	writeTestWorkflow(t, cfg.WorkflowsPath, "web-classic", "module")

	resetCampaignCommandStateForTest()
	campaignName = "test-campaign"
	campaignModule = "web-classic"
	campaignTargets = []string{"https://a.example.com", "https://b.example.com"}
	campaignParams = []string{"space_name=campaign-space", "custom=value"}
	campaignRole = "operator"
	campaignSkills = []string{"xss", "sqli"}
	campaignStrategy = "ai-assisted"
	campaignPriority = "critical"
	campaignNotes = "campaign test"

	err := runCampaignCreate(campaignCreateCmd, nil)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = database.Connect(cfg)
	require.NoError(t, err)
	result, err := database.ListCampaigns(ctx, database.CampaignQuery{Limit: 10})
	require.NoError(t, err)
	require.Len(t, result.Data, 1)

	campaign := result.Data[0]
	assert.Equal(t, "test-campaign", campaign.Name)
	assert.Equal(t, "web-classic", campaign.WorkflowName)
	assert.Equal(t, "module", campaign.WorkflowKind)
	assert.Equal(t, 2, campaign.TargetCount)
	assert.Equal(t, "operator", campaign.Role)
	assert.Equal(t, "ai-assisted", campaign.Strategy)
	assert.Equal(t, "campaign test", campaign.Notes)
	assert.Equal(t, "campaign-space", campaign.Params["space_name"])
	assert.Equal(t, "operator", campaign.Params["campaign_role"])
	assert.Equal(t, "xss,sqli", campaign.Params["campaign_skills"])

	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	for _, run := range runs {
		assert.Equal(t, "campaign", run.TriggerType)
		assert.Equal(t, "critical", run.RunPriority)
		assert.Equal(t, "queued", run.Status)
		assert.True(t, run.IsQueued)
		assert.Equal(t, "campaign-space", run.Workspace)
		assert.Equal(t, campaign.ID, run.RunGroupID)
	}
}

func TestBuildCampaignStatusResponseCLI_AggregatesLatestRuns(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:                 "camp-status",
		Name:               "status-campaign",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	oldRun := &database.Run{
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
	}
	newRun := &database.Run{
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
	}
	pendingRun := &database.Run{
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
	}
	deepScanRun := &database.Run{
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
	}
	for _, run := range []*database.Run{oldRun, newRun, pendingRun, deepScanRun} {
		_, err := database.GetDB().NewInsert().Model(run).Exec(ctx)
		require.NoError(t, err)
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

	resp, err := buildCampaignStatusResponseCLI(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "partial", resp.Status)
	assert.Equal(t, 2, resp.Progress.Total)
	assert.Equal(t, 1, resp.Progress.Pending)
	assert.Equal(t, 1, resp.Progress.Completed)
	assert.Equal(t, 2, resp.Summary.TargetsTotal)
	assert.Equal(t, 2, resp.Summary.HighRiskTargets)
	assert.Equal(t, 1, resp.Summary.DeepScanQueuedTargets)
	assert.Equal(t, 1, resp.Summary.AttackChainAwareTargets)
	assert.Equal(t, 1, resp.Summary.VerifiedAttackChainTargets)
	assert.Contains(t, resp.HighRiskTargets, "https://a.example.com")
	assert.Contains(t, resp.HighRiskTargets, "https://b.example.com")

	targetMap := make(map[string]campaignTargetStatusCLI)
	for _, target := range resp.Targets {
		targetMap[target.Target] = target
	}
	require.Contains(t, targetMap, "https://a.example.com")
	require.Contains(t, targetMap, "https://b.example.com")
	assert.Equal(t, "completed", targetMap["https://a.example.com"].Status)
	assert.Equal(t, "high", targetMap["https://a.example.com"].RiskLevel)
	assert.True(t, targetMap["https://a.example.com"].DeepScanQueued)
	assert.Equal(t, "queued", targetMap["https://b.example.com"].Status)
	assert.Equal(t, "high", targetMap["https://b.example.com"].RiskLevel)
	require.NotNil(t, targetMap["https://b.example.com"].AttackChainSummary)
	assert.Equal(t, 1, targetMap["https://b.example.com"].AttackChainSummary.VerifiedHits)

	refreshed, err := database.GetCampaignByID(ctx, campaign.ID)
	require.NoError(t, err)
	assert.Equal(t, "partial", refreshed.Status)
}

func TestBuildCampaignStatusResponseCLI_TreatsRetestAttackChainSignalsAsHighRisk(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:                 "camp-status-operational-cli",
		Name:               "status-operational-cli",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	require.NoError(t, database.CreateRun(ctx, &database.Run{
		RunUUID:      "run-operational-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://ops.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-operational-cli",
		CreatedAt:    now,
		UpdatedAt:    now,
	}))

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-operational-cli",
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

	require.NoError(t, database.UpsertAttackChainReport(ctx, &database.AttackChainReport{
		Workspace:        "ws-operational-cli",
		Target:           "https://ops.example.com",
		RunUUID:          "run-chain-operational-cli",
		SourcePath:       "/tmp/operational-cli-chain.json",
		SourceHash:       "operational-cli-chain-hash",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}))

	_, err = database.CreateVulnerabilityRecord(ctx, &database.Vulnerability{
		Workspace:     "ws-operational-cli",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "low",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://ops.example.com/login",
		VulnStatus:    "retest",
		RetestStatus:  "queued",
		SourceRunUUID: "run-vuln-operational-cli",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	})
	require.NoError(t, err)

	resp, err := buildCampaignStatusResponseCLI(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.HighRiskTargets, 1)
	assert.Equal(t, "https://ops.example.com", resp.HighRiskTargets[0])
	assert.Equal(t, 1, resp.Summary.HighRiskTargets)
	assert.Zero(t, resp.Summary.VerifiedAttackChainTargets)
	require.Len(t, resp.Targets, 1)
	assert.Equal(t, "high", resp.Targets[0].RiskLevel)
	require.NotNil(t, resp.Targets[0].AttackChainSummary)
	assert.Zero(t, resp.Targets[0].AttackChainSummary.VerifiedHits)
	assert.Equal(t, 1, resp.Targets[0].AttackChainSummary.OperationalHits)
}

func TestBuildCampaignStatusResponseCLI_IgnoresClosedAndFalsePositiveFindings(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:                 "camp-status-filtered-cli",
		Name:               "status-filtered-cli",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	for _, run := range []*database.Run{
		{
			RunUUID:      "run-filtered-cli-a",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-filtered-cli-a",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-filtered-cli-b",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://b.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-filtered-cli-b",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	for _, vuln := range []*database.Vulnerability{
		{Workspace: "ws-filtered-cli-a", Severity: "critical", AssetValue: "https://a.example.com/login", VulnStatus: "closed", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-filtered-cli-b", Severity: "high", AssetValue: "https://b.example.com/admin", VulnStatus: "false_positive", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	resp, err := buildCampaignStatusResponseCLI(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, resp.Summary.HighRiskTargets)
	assert.Empty(t, resp.HighRiskTargets)
	for _, target := range resp.Targets {
		assert.Equal(t, "none", target.RiskLevel)
	}
}

func TestBuildCampaignStatusResponseCLI_ScopesRiskPerTarget(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:                 "camp-status-scoped-cli",
		Name:               "status-scoped-cli",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	for _, run := range []*database.Run{
		{
			RunUUID:      "run-scoped-cli-a",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "shared-ws-cli",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-scoped-cli-b",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://b.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "shared-ws-cli",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	for _, vuln := range []*database.Vulnerability{
		{
			Workspace:  "shared-ws-cli",
			Severity:   "critical",
			AssetValue: "https://a.example.com/admin",
			VulnStatus: "new",
			CreatedAt:  now,
			UpdatedAt:  now,
			LastSeenAt: now,
		},
		{
			Workspace:  "shared-ws-cli",
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

	resp, err := buildCampaignStatusResponseCLI(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, []string{"https://a.example.com"}, resp.HighRiskTargets)
	assert.Equal(t, 1, resp.Summary.HighRiskTargets)

	targetMap := make(map[string]campaignTargetStatusCLI)
	for _, target := range resp.Targets {
		targetMap[target.Target] = target
	}
	assert.Equal(t, "critical", targetMap["https://a.example.com"].RiskLevel)
	assert.Equal(t, 1, targetMap["https://a.example.com"].VulnSummary["critical"])
	assert.Equal(t, "medium", targetMap["https://b.example.com"].RiskLevel)
	assert.Zero(t, targetMap["https://b.example.com"].VulnSummary["critical"])
}

func TestQueueCampaignDeepScanRunsCLI_QueuesHighRiskTargetsOnly(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:                   "camp-deep",
		Name:                 "deep-campaign",
		WorkflowName:         "web-classic",
		WorkflowKind:         "flow",
		Status:               "queued",
		DeepScanWorkflow:     "deep-flow",
		DeepScanWorkflowKind: "flow",
		HighRiskSeverities:   []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
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
		{Workspace: "ws-a", Severity: "critical", AssetValue: "https://a.example.com/admin", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
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

	queued, targets, err := queueCampaignDeepScanRunsCLI(ctx, cfg, campaign)
	require.NoError(t, err)
	assert.Equal(t, 2, queued)
	assert.ElementsMatch(t, []string{"https://a.example.com", "https://b.example.com"}, targets)

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

	queuedAgain, targetsAgain, err := queueCampaignDeepScanRunsCLI(ctx, cfg, campaign)
	require.NoError(t, err)
	assert.Equal(t, 0, queuedAgain)
	assert.Empty(t, targetsAgain)
}

func TestQueueCampaignDeepScanRunsCLI_ScopesRiskPerTargetInSharedWorkspace(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:                   "camp-deep-scoped-cli",
		Name:                 "deep-scoped-cli",
		WorkflowName:         "web-classic",
		WorkflowKind:         "flow",
		Status:               "queued",
		DeepScanWorkflow:     "deep-flow",
		DeepScanWorkflowKind: "flow",
		HighRiskSeverities:   []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	for _, run := range []*database.Run{
		{
			RunUUID:      "run-deep-scoped-a",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://a.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "shared-deep-cli",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-deep-scoped-b",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://b.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "shared-deep-cli",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	_, err := database.GetDB().NewInsert().Model(&database.Vulnerability{
		Workspace:  "shared-deep-cli",
		Severity:   "critical",
		AssetValue: "https://a.example.com/admin",
		VulnStatus: "new",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}).Exec(ctx)
	require.NoError(t, err)

	queued, targets, err := queueCampaignDeepScanRunsCLI(ctx, cfg, campaign)
	require.NoError(t, err)
	assert.Equal(t, 1, queued)
	assert.Equal(t, []string{"https://a.example.com"}, targets)
}

func TestQueueCampaignRerunFailedRunsCLI_QueuesFailedTargetsOnly(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:           "camp-rerun",
		Name:         "rerun-campaign",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	failedRun := &database.Run{
		RunUUID:      "run-failed",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://failed.example.com",
		Params:       map[string]interface{}{"custom": "value"},
		Status:       "failed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "critical",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-failed",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	supersededFailedRun := &database.Run{
		RunUUID:      "run-failed-old",
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
		CreatedAt:    now.Add(-time.Minute),
		UpdatedAt:    now.Add(-time.Minute),
	}
	recoveredRun := &database.Run{
		RunUUID:      "run-recovered",
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
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	completedRun := &database.Run{
		RunUUID:      "run-completed",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://ok.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-ok",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateRun(ctx, supersededFailedRun))
	require.NoError(t, database.CreateRun(ctx, recoveredRun))
	require.NoError(t, database.CreateRun(ctx, failedRun))
	require.NoError(t, database.CreateRun(ctx, completedRun))

	queued, targets, err := queueCampaignRerunFailedRunsCLI(ctx, cfg, campaign)
	require.NoError(t, err)
	assert.Equal(t, 1, queued)
	assert.Equal(t, []string{"https://failed.example.com"}, targets)

	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	var rerun *database.Run
	for _, run := range runs {
		if run.TriggerType == "campaign-rerun" {
			rerun = run
			break
		}
	}
	require.NotNil(t, rerun)
	assert.Equal(t, "https://failed.example.com", rerun.Target)
	assert.Equal(t, "queued", rerun.Status)
	assert.Equal(t, "critical", rerun.RunPriority)
	assert.Equal(t, "ws-failed", rerun.Workspace)
	assert.Equal(t, "value", rerun.Params["custom"])
}

func TestRunCampaignStatus_JSON(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:                 "camp-status-json",
		Name:               "status-json",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		DeepScanWorkflow:   "deep-flow",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	require.NoError(t, database.CreateRun(ctx, &database.Run{
		RunUUID:      "run-json-a",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://a.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-json-a",
		CreatedAt:    now,
		UpdatedAt:    now,
	}))
	require.NoError(t, database.CreateRun(ctx, &database.Run{
		RunUUID:      "run-json-b",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://b.example.com",
		Status:       "queued",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-json-b",
		CreatedAt:    now,
		UpdatedAt:    now,
	}))
	_, err := database.GetDB().NewInsert().Model(&database.Vulnerability{
		Workspace:  "ws-json-a",
		Severity:   "critical",
		AssetValue: "https://a.example.com/admin",
		VulnStatus: "new",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}).Exec(ctx)
	require.NoError(t, err)

	globalJSON = true
	output := captureStdout(t, func() {
		require.NoError(t, runCampaignStatus(campaignStatusCmd, []string{campaign.ID}))
	})

	var payload campaignStatusResponseCLI
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, campaign.ID, payload.Campaign.ID)
	assert.Equal(t, "partial", payload.Status)
	assert.Equal(t, 2, payload.Progress.Total)
	assert.Len(t, payload.Targets, 2)
	assert.Contains(t, payload.HighRiskTargets, "https://a.example.com")
}

func TestBuildCampaignReportResponseCLI_IncludesAnalyticsAndRerunHistory(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                   "camp-report-cli",
		Name:                 "report-cli",
		WorkflowName:         "web-classic",
		WorkflowKind:         "flow",
		Status:               "queued",
		DeepScanWorkflow:     "deep-flow",
		DeepScanWorkflowKind: "flow",
		HighRiskSeverities:   []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-report-cli-failed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover-cli.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-cli-a",
			CreatedAt:    now.Add(-3 * time.Hour),
			UpdatedAt:    now.Add(-3 * time.Hour),
		},
		{
			RunUUID:      "run-report-cli-rerun",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover-cli.example.com",
			Status:       "completed",
			TriggerType:  "campaign-rerun",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-cli-a",
			CreatedAt:    now.Add(-2 * time.Hour),
			UpdatedAt:    now.Add(-2 * time.Hour),
		},
		{
			RunUUID:      "run-report-cli-deep",
			WorkflowName: "deep-flow",
			WorkflowKind: "flow",
			Target:       "https://recover-cli.example.com",
			Status:       "queued",
			TriggerType:  "campaign-deep-scan",
			RunGroupID:   campaign.ID,
			RunPriority:  "critical",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-cli-a",
			CreatedAt:    now.Add(-90 * time.Minute),
			UpdatedAt:    now.Add(-90 * time.Minute),
		},
		{
			RunUUID:      "run-report-cli-stable",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://stable-cli.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-cli-b",
			CreatedAt:    now.Add(-1 * time.Hour),
			UpdatedAt:    now.Add(-1 * time.Hour),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	_, err := database.CreateVulnerabilityRecord(ctx, &database.Vulnerability{
		Workspace:     "ws-report-cli-a",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://recover-cli.example.com/login",
		VulnStatus:    "verified",
		SourceRunUUID: "run-vuln-report-cli-a",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	})
	require.NoError(t, err)

	chainsJSON, err := json.Marshal([]map[string]any{
		{
			"chain_id": "chain-report-cli-a",
			"entry_point": map[string]any{
				"vulnerability": "SQL Injection",
				"url":           "https://recover-cli.example.com/login",
			},
			"chain_steps": []map[string]any{
				{"step": 1, "action": "exploit login"},
			},
		},
	})
	require.NoError(t, err)

	require.NoError(t, database.UpsertAttackChainReport(ctx, &database.AttackChainReport{
		Workspace:        "ws-report-cli-a",
		Target:           "https://recover-cli.example.com",
		RunUUID:          "run-chain-report-cli-a",
		SourcePath:       "/tmp/report-cli-chain.json",
		SourceHash:       "report-cli-chain-hash",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}))

	report, err := buildCampaignReportResponseCLI(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, report)

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

	assert.Equal(t, "https://recover-cli.example.com", report.Targets[0].Target)
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
	assert.Equal(t, "https://stable-cli.example.com", report.Targets[1].Target)
	assert.Equal(t, "none", report.Targets[1].RiskLevel)
}

func TestRunCampaignExport_CSV(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-export-cli",
		Name:               "export-cli",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	require.NoError(t, database.CreateRun(ctx, &database.Run{
		RunUUID:      "run-export-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://csv-cli.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-export-cli",
		CreatedAt:    now,
		UpdatedAt:    now,
	}))

	_, err := database.CreateVulnerabilityRecord(ctx, &database.Vulnerability{
		Workspace:     "ws-export-cli",
		VulnInfo:      "open-redirect",
		VulnTitle:     "Open Redirect",
		Severity:      "medium",
		Confidence:    "firm",
		AssetType:     "url",
		AssetValue:    "https://csv-cli.example.com/redirect",
		VulnStatus:    "new",
		SourceRunUUID: "run-vuln-export-cli",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	})
	require.NoError(t, err)

	campaignExportFormat = "csv"
	output := captureStdout(t, func() {
		require.NoError(t, runCampaignExport(campaignExportCmd, []string{campaign.ID}))
	})

	reader := csv.NewReader(strings.NewReader(output))
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
	assert.Equal(t, "https://csv-cli.example.com", records[1][0])
	assert.Equal(t, "ws-export-cli", records[1][1])
	assert.Equal(t, "completed", records[1][2])
	assert.Equal(t, "medium", records[1][3])
	assert.Equal(t, "run-export-cli", records[1][4])
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

func TestRunCampaignReport_JSON_AppliesFilters(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-report-filter-cli",
		Name:               "report-filter-cli",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-report-filter-cli-old-failed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover-report-cli.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-filter-cli-a",
			CreatedAt:    now.Add(-2 * time.Hour),
			UpdatedAt:    now.Add(-2 * time.Hour),
		},
		{
			RunUUID:      "run-report-filter-cli-rerun",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://recover-report-cli.example.com",
			Status:       "completed",
			TriggerType:  "campaign-rerun",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-filter-cli-a",
			CreatedAt:    now.Add(-1 * time.Hour),
			UpdatedAt:    now.Add(-1 * time.Hour),
		},
		{
			RunUUID:      "run-report-filter-cli-failed-current",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://failed-report-cli.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-filter-cli-b",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	for _, vuln := range []*database.Vulnerability{
		{
			Workspace:     "ws-report-filter-cli-a",
			VulnInfo:      "sql-injection",
			VulnTitle:     "SQL Injection",
			Severity:      "high",
			Confidence:    "certain",
			AssetType:     "url",
			AssetValue:    "https://recover-report-cli.example.com/login",
			VulnStatus:    "verified",
			SourceRunUUID: "run-vuln-report-filter-cli-a",
			CreatedAt:     now,
			UpdatedAt:     now,
			LastSeenAt:    now,
		},
		{
			Workspace:     "ws-report-filter-cli-b",
			VulnInfo:      "debug-page",
			VulnTitle:     "Debug Page",
			Severity:      "low",
			Confidence:    "firm",
			AssetType:     "url",
			AssetValue:    "https://failed-report-cli.example.com/debug",
			VulnStatus:    "new",
			SourceRunUUID: "run-vuln-report-filter-cli-b",
			CreatedAt:     now,
			UpdatedAt:     now,
			LastSeenAt:    now,
		},
	} {
		_, err := database.CreateVulnerabilityRecord(ctx, vuln)
		require.NoError(t, err)
	}

	campaignReportRiskLevels = []string{"high"}
	campaignReportStatuses = []string{"completed"}
	campaignReportTriggerTypes = []string{"campaign-rerun"}
	campaignReportPreset = "recovered"
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignReport(campaignReportCmd, []string{campaign.ID}))
	})

	var payload campaignReportResponseCLI
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, 2, payload.TotalTargets)
	assert.Equal(t, 1, payload.ResultCount)
	assert.Equal(t, []string{"high"}, payload.FiltersApplied.RiskLevels)
	assert.Equal(t, []string{"completed"}, payload.FiltersApplied.Statuses)
	assert.Equal(t, []string{"campaign-rerun"}, payload.FiltersApplied.TriggerTypes)
	assert.Equal(t, "recovered", payload.FiltersApplied.Preset)
	require.Len(t, payload.Targets, 1)
	assert.Equal(t, "https://recover-report-cli.example.com", payload.Targets[0].Target)
	assert.True(t, payload.Targets[0].Recovered)
}

func TestRunCampaignReport_JSON_AppliesPagination(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-report-page-cli",
		Name:         "report-page-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-page-cli.example.com",
		"https://b-page-cli.example.com",
		"https://c-page-cli.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-report-page-cli-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-page-cli-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	campaignReportStatuses = []string{"completed"}
	campaignReportOffset = 1
	campaignReportLimit = 1
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignReport(campaignReportCmd, []string{campaign.ID}))
	})

	var payload campaignReportResponseCLI
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, 3, payload.TotalTargets)
	assert.Equal(t, 3, payload.ResultCount)
	assert.Equal(t, 1, payload.Pagination.Offset)
	assert.Equal(t, 1, payload.Pagination.Limit)
	assert.Equal(t, 1, payload.Pagination.ReturnedCount)
	assert.True(t, payload.Pagination.HasMore)
	require.Len(t, payload.Targets, 1)
	assert.Equal(t, "https://b-page-cli.example.com", payload.Targets[0].Target)
}

func TestRunCampaignReport_JSON_AppliesSortOverride(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-report-sort-cli",
		Name:         "report-sort-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-sort-cli.example.com",
		"https://b-sort-cli.example.com",
		"https://c-sort-cli.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-report-sort-cli-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-sort-cli-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	campaignReportStatuses = []string{"completed"}
	campaignReportSortBy = "target"
	campaignReportSortOrder = "desc"
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignReport(campaignReportCmd, []string{campaign.ID}))
	})

	var payload campaignReportResponseCLI
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, "target", payload.SortApplied.By)
	assert.Equal(t, "desc", payload.SortApplied.Order)
	require.Len(t, payload.Targets, 3)
	assert.Equal(t, "https://c-sort-cli.example.com", payload.Targets[0].Target)
	assert.Equal(t, "https://b-sort-cli.example.com", payload.Targets[1].Target)
	assert.Equal(t, "https://a-sort-cli.example.com", payload.Targets[2].Target)
}

func TestRunCampaignExport_CSV_AppliesPreset(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                 "camp-export-filter-cli",
		Name:               "export-filter-cli",
		WorkflowName:       "web-classic",
		WorkflowKind:       "flow",
		Status:             "queued",
		HighRiskSeverities: []string{"critical", "high"},
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for _, run := range []*database.Run{
		{
			RunUUID:      "run-export-filter-cli-failed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://failed-export-cli.example.com",
			Status:       "failed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-filter-cli-a",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			RunUUID:      "run-export-filter-cli-completed",
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       "https://done-export-cli.example.com",
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-filter-cli-b",
			CreatedAt:    now.Add(time.Minute),
			UpdatedAt:    now.Add(time.Minute),
		},
	} {
		require.NoError(t, database.CreateRun(ctx, run))
	}

	campaignReportPreset = "failed"
	campaignExportFormat = "csv"

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignExport(campaignExportCmd, []string{campaign.ID}))
	})

	reader := csv.NewReader(strings.NewReader(output))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "https://failed-export-cli.example.com", records[1][0])
}

func TestRunCampaignExport_CSV_AppliesPagination(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-export-page-cli",
		Name:         "export-page-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-export-cli.example.com",
		"https://b-export-cli.example.com",
		"https://c-export-cli.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-export-page-cli-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-page-cli-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	campaignReportStatuses = []string{"completed"}
	campaignReportOffset = 1
	campaignReportLimit = 1
	campaignExportFormat = "csv"

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignExport(campaignExportCmd, []string{campaign.ID}))
	})

	reader := csv.NewReader(strings.NewReader(output))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "https://b-export-cli.example.com", records[1][0])
}

func TestRunCampaignExport_CSV_AppliesSortOverride(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-export-sort-cli",
		Name:         "export-sort-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-export-sort-cli.example.com",
		"https://b-export-sort-cli.example.com",
		"https://c-export-sort-cli.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-export-sort-cli-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-sort-cli-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	campaignReportStatuses = []string{"completed"}
	campaignReportSortBy = "target"
	campaignReportSortOrder = "desc"
	campaignExportFormat = "csv"

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignExport(campaignExportCmd, []string{campaign.ID}))
	})

	reader := csv.NewReader(strings.NewReader(output))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4)
	assert.Equal(t, "https://c-export-sort-cli.example.com", records[1][0])
	assert.Equal(t, "https://b-export-sort-cli.example.com", records[2][0])
	assert.Equal(t, "https://a-export-sort-cli.example.com", records[3][0])
}

func TestRunCampaignProfileSaveListDelete_JSON(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:           "camp-profile-cli",
		Name:         "profile-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	campaignReportRiskLevels = []string{"high"}
	campaignReportSortBy = "target"
	campaignReportSortOrder = "desc"
	campaignExportFormat = "json"
	campaignProfileDescription = "operator handoff"
	globalJSON = true

	saveOutput := captureStdout(t, func() {
		require.NoError(t, runCampaignProfileSave(campaignProfileSaveCmd, []string{campaign.ID, "ops-handoff"}))
	})

	var savePayload struct {
		Data database.CampaignReportProfile `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(saveOutput), &savePayload))
	assert.Equal(t, "ops-handoff", savePayload.Data.Name)
	assert.Equal(t, "json", savePayload.Data.Format)
	assert.Equal(t, "operator handoff", savePayload.Data.Description)

	listOutput := captureStdout(t, func() {
		require.NoError(t, runCampaignProfileList(campaignProfileListCmd, []string{campaign.ID}))
	})

	var listPayload struct {
		Data []database.CampaignReportProfile `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(listOutput), &listPayload))
	require.Len(t, listPayload.Data, 1)
	assert.Equal(t, "ops-handoff", listPayload.Data[0].Name)

	deleteOutput := captureStdout(t, func() {
		require.NoError(t, runCampaignProfileDelete(campaignProfileDeleteCmd, []string{campaign.ID, "ops-handoff"}))
	})

	var deletePayload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(deleteOutput), &deletePayload))
	assert.Equal(t, true, deletePayload["deleted"])
}

func TestRunCampaignReport_JSON_UsesSavedProfile(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-report-profile-cli",
		Name:         "report-profile-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-profile-cli.example.com",
		"https://b-profile-cli.example.com",
		"https://c-profile-cli.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-report-profile-cli-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-report-profile-cli-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	_, err := database.UpsertCampaignReportProfile(ctx, campaign.ID, database.CampaignReportProfile{
		Name: "ops-desc",
		Filters: database.CampaignReportProfileFilters{
			Statuses: []string{"completed"},
		},
		Sort: database.CampaignReportProfileSort{
			By:    "target",
			Order: "desc",
		},
	})
	require.NoError(t, err)

	campaignReportProfileName = "ops-desc"
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignReport(campaignReportCmd, []string{campaign.ID}))
	})

	var payload campaignReportResponseCLI
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, "ops-desc", payload.ProfileApplied)
	assert.Equal(t, "target", payload.SortApplied.By)
	assert.Equal(t, "desc", payload.SortApplied.Order)
	require.Len(t, payload.Targets, 3)
	assert.Equal(t, "https://c-profile-cli.example.com", payload.Targets[0].Target)
}

func TestRunCampaignExport_UsesSavedProfileFormat(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:           "camp-export-profile-cli",
		Name:         "export-profile-cli",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	for index, target := range []string{
		"https://a-export-profile-cli.example.com",
		"https://b-export-profile-cli.example.com",
		"https://c-export-profile-cli.example.com",
	} {
		require.NoError(t, database.CreateRun(ctx, &database.Run{
			RunUUID:      "run-export-profile-cli-" + string(rune('a'+index)),
			WorkflowName: "web-classic",
			WorkflowKind: "flow",
			Target:       target,
			Status:       "completed",
			TriggerType:  "campaign",
			RunGroupID:   campaign.ID,
			RunPriority:  "high",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    "ws-export-profile-cli-" + string(rune('a'+index)),
			CreatedAt:    now.Add(time.Duration(index) * time.Minute),
			UpdatedAt:    now.Add(time.Duration(index) * time.Minute),
		}))
	}

	_, err := database.UpsertCampaignReportProfile(ctx, campaign.ID, database.CampaignReportProfile{
		Name:   "ops-json",
		Format: "json",
		Filters: database.CampaignReportProfileFilters{
			Statuses: []string{"completed"},
		},
		Sort: database.CampaignReportProfileSort{
			By:    "target",
			Order: "desc",
		},
	})
	require.NoError(t, err)

	campaignReportProfileName = "ops-json"

	output := captureStdout(t, func() {
		require.NoError(t, runCampaignExport(campaignExportCmd, []string{campaign.ID}))
	})

	var payload campaignReportResponseCLI
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, "ops-json", payload.ProfileApplied)
	assert.Equal(t, "target", payload.SortApplied.By)
	assert.Equal(t, "https://c-export-profile-cli.example.com", payload.Targets[0].Target)
}

func TestRunCampaignRerunFailed_JSON(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	ctx := context.Background()

	campaign := &database.Campaign{
		ID:           "camp-rerun-json",
		Name:         "rerun-json",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Status:       "queued",
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	now := time.Now()
	require.NoError(t, database.CreateRun(ctx, &database.Run{
		RunUUID:      "run-failed-json",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://failed-json.example.com",
		Status:       "failed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-rerun-json",
		CreatedAt:    now,
		UpdatedAt:    now,
	}))

	config.Set(cfg)
	globalJSON = true
	output := captureStdout(t, func() {
		require.NoError(t, runCampaignRerunFailed(campaignRerunFailedCmd, []string{campaign.ID}))
	})

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	assert.Equal(t, campaign.ID, payload["campaign_id"])
	assert.Equal(t, float64(1), payload["queued_count"])

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	var rerunCount int
	for _, run := range runs {
		if run.TriggerType == "campaign-rerun" {
			rerunCount++
		}
	}
	assert.Equal(t, 1, rerunCount)
}

func setupCampaignTestEnv(t *testing.T) *config.Config {
	t.Helper()

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkflowsPath:  workflowsDir,
		WorkspacesPath: filepath.Join(tmpDir, "workspaces"),
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "test.sqlite"),
		},
	}

	oldCfg := config.Get()
	config.Set(cfg)

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))

	t.Cleanup(func() {
		resetCampaignCommandStateForTest()
		config.Set(oldCfg)
		_ = database.Close()
		database.SetDB(nil)
	})

	return cfg
}

func writeTestWorkflow(t *testing.T, workflowsDir, name, kind string) {
	t.Helper()

	content := "kind: " + kind + "\nname: " + name + "\nsteps:\n  - name: noop\n    type: bash\n    command: echo ok\n"
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, name+".yaml"), []byte(content), 0o644))
}

func resetCampaignCommandStateForTest() {
	campaignName = ""
	campaignFlow = ""
	campaignModule = ""
	campaignTargets = nil
	campaignTargetFile = ""
	campaignParams = nil
	campaignRole = ""
	campaignSkills = nil
	campaignStrategy = ""
	campaignPriority = ""
	campaignDeepScanWorkflow = ""
	campaignDeepScanWorkflowKind = ""
	campaignAutoDeepScan = false
	campaignHighRiskSeverities = nil
	campaignNotes = ""
	campaignStatusFilter = ""
	campaignOffset = 0
	campaignLimit = 0
	campaignReportRiskLevels = nil
	campaignReportStatuses = nil
	campaignReportTriggerTypes = nil
	campaignReportPreset = ""
	campaignReportOffset = 0
	campaignReportLimit = 0
	campaignReportSortBy = ""
	campaignReportSortOrder = ""
	campaignReportProfileName = ""
	campaignProfileDescription = ""
	campaignExportFormat = ""
	campaignExportOutput = ""
	globalJSON = false
	disableDB = false
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()

	require.NoError(t, w.Close())
	os.Stdout = originalStdout
	output := <-done
	require.NoError(t, r.Close())
	return strings.TrimSpace(output)
}
