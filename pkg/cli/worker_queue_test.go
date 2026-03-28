package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueueRuns_AssignsWorkspaceMetadata(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	writeTestWorkflow(t, cfg.WorkflowsPath, "queue-smoke", "module")

	ctx := context.Background()
	printer := terminal.NewPrinter()

	require.NoError(t, queueRuns(ctx, cfg, "queue-smoke", "module", []string{"https://cli.example.com/login"}, "", map[string]interface{}{
		"space_name": "queue-cli-space",
	}, printer))
	require.NoError(t, queueRuns(ctx, cfg, "queue-smoke", "module", []string{"https://derived.example.com/path"}, "", map[string]interface{}{}, printer))

	result, err := database.ListRuns(ctx, 0, 10, "", "queue-smoke", "", "")
	require.NoError(t, err)
	require.Len(t, result.Data, 2)

	workspaceByTarget := make(map[string]string, len(result.Data))
	for _, run := range result.Data {
		workspaceByTarget[run.Target] = run.Workspace
	}

	assert.Equal(t, "queue-cli-space", workspaceByTarget["https://cli.example.com/login"])
	assert.Equal(t, computeWorkspace("https://derived.example.com/path", map[string]string{}), workspaceByTarget["https://derived.example.com/path"])
}

func TestMaybeQueueCampaignDeepScan_ScopesRiskPerTarget(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                   "camp-worker-scope",
		Name:                 "worker-scope",
		WorkflowName:         "web-classic",
		WorkflowKind:         "flow",
		Status:               "queued",
		DeepScanWorkflow:     "deep-flow",
		DeepScanWorkflowKind: "flow",
		AutoDeepScan:         true,
		HighRiskSeverities:   []string{"critical", "high"},
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	runA := &database.Run{
		RunUUID:      "run-worker-a",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://a.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "shared-worker",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	runB := &database.Run{
		RunUUID:      "run-worker-b",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://b.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "shared-worker",
		CreatedAt:    now.Add(time.Minute),
		UpdatedAt:    now.Add(time.Minute),
	}
	require.NoError(t, database.CreateRun(ctx, runA))
	require.NoError(t, database.CreateRun(ctx, runB))

	_, err := database.GetDB().NewInsert().Model(&database.Vulnerability{
		Workspace:  "shared-worker",
		Severity:   "critical",
		AssetValue: "https://a.example.com/admin",
		VulnStatus: "new",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}).Exec(ctx)
	require.NoError(t, err)

	maybeQueueCampaignDeepScan(ctx, runA.RunUUID)
	maybeQueueCampaignDeepScan(ctx, runB.RunUUID)

	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)

	var deepScans []*database.Run
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			deepScans = append(deepScans, run)
		}
	}
	require.Len(t, deepScans, 1)
	assert.Equal(t, "https://a.example.com", deepScans[0].Target)
	assert.Equal(t, "deep-flow", deepScans[0].WorkflowName)
}

func TestMaybeQueueCampaignDeepScan_UsesOperationalAttackChainSignals(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	ctx := context.Background()
	now := time.Now()

	campaign := &database.Campaign{
		ID:                   "camp-worker-live-chain",
		Name:                 "worker-live-chain",
		WorkflowName:         "web-classic",
		WorkflowKind:         "flow",
		Status:               "queued",
		DeepScanWorkflow:     "deep-flow",
		DeepScanWorkflowKind: "flow",
		AutoDeepScan:         true,
		HighRiskSeverities:   []string{"critical", "high"},
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	require.NoError(t, database.CreateCampaign(ctx, campaign))

	run := &database.Run{
		RunUUID:      "run-worker-live-chain",
		WorkflowName: "web-classic",
		WorkflowKind: "flow",
		Target:       "https://live.example.com",
		Status:       "completed",
		TriggerType:  "campaign",
		RunGroupID:   campaign.ID,
		RunPriority:  "high",
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    "ws-worker-live-chain",
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
		Workspace:        "ws-worker-live-chain",
		Target:           "https://live.example.com",
		RunUUID:          "run-chain-live-worker",
		SourcePath:       "/tmp/live-worker-chain.json",
		SourceHash:       "live-worker-chain-hash",
		Status:           "ready",
		TotalChains:      1,
		HighImpactChains: 1,
		VerifiedHits:     1,
		AttackChainsJSON: string(chainsJSON),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	require.NoError(t, database.UpsertAttackChainReport(ctx, report))

	result, err := database.CreateVulnerabilityRecord(ctx, &database.Vulnerability{
		Workspace:     "ws-worker-live-chain",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://live.example.com/login",
		VulnStatus:    "false_positive",
		SourceRunUUID: "run-vuln-worker-live-fp",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Vulnerability)

	maybeQueueCampaignDeepScan(ctx, run.RunUUID)

	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	var deepScans []*database.Run
	for _, item := range runs {
		if item.TriggerType == "campaign-deep-scan" {
			deepScans = append(deepScans, item)
		}
	}
	assert.Len(t, deepScans, 0)

	stored := result.Vulnerability
	stored.VulnStatus = "retest"
	stored.RetestStatus = "queued"
	stored.SourceRunUUID = "run-vuln-worker-live-ok"
	stored.UpdatedAt = now.Add(time.Minute)
	stored.LastSeenAt = now.Add(time.Minute)
	require.NoError(t, database.UpdateVulnerabilityRecord(ctx, stored))

	maybeQueueCampaignDeepScan(ctx, run.RunUUID)

	runs, err = database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	deepScans = deepScans[:0]
	for _, item := range runs {
		if item.TriggerType == "campaign-deep-scan" {
			deepScans = append(deepScans, item)
		}
	}
	require.Len(t, deepScans, 1)
	assert.Equal(t, "https://live.example.com", deepScans[0].Target)
	assert.Equal(t, "deep-flow", deepScans[0].WorkflowName)
}
