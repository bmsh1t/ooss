package cli

import (
	"context"
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
		{Workspace: "ws-a", Severity: "high", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", Severity: "low", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	resp, err := buildCampaignStatusResponseCLI(ctx, campaign)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "partial", resp.Status)
	assert.Equal(t, 2, resp.Progress.Total)
	assert.Equal(t, 1, resp.Progress.Pending)
	assert.Equal(t, 1, resp.Progress.Completed)
	assert.Contains(t, resp.HighRiskTargets, "https://a.example.com")

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

	refreshed, err := database.GetCampaignByID(ctx, campaign.ID)
	require.NoError(t, err)
	assert.Equal(t, "partial", refreshed.Status)
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
		{Workspace: "ws-a", Severity: "critical", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
		{Workspace: "ws-b", Severity: "low", VulnStatus: "new", CreatedAt: now, UpdatedAt: now, LastSeenAt: now},
	} {
		_, err := database.GetDB().NewInsert().Model(vuln).Exec(ctx)
		require.NoError(t, err)
	}

	queued, targets, err := queueCampaignDeepScanRunsCLI(ctx, cfg, campaign)
	require.NoError(t, err)
	assert.Equal(t, 1, queued)
	assert.Equal(t, []string{"https://a.example.com"}, targets)

	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	require.NoError(t, err)
	var deepScan *database.Run
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			deepScan = run
			break
		}
	}
	require.NotNil(t, deepScan)
	assert.Equal(t, "deep-flow", deepScan.WorkflowName)
	assert.Equal(t, "critical", deepScan.RunPriority)
	assert.Equal(t, "queue", deepScan.RunMode)
	assert.Equal(t, "ws-a", deepScan.Workspace)
	assert.Equal(t, "deep_scan", deepScan.Params["campaign_stage"])
	assert.Equal(t, "run-a", deepScan.Params["campaign_source_run_uuid"])

	queuedAgain, targetsAgain, err := queueCampaignDeepScanRunsCLI(ctx, cfg, campaign)
	require.NoError(t, err)
	assert.Equal(t, 0, queuedAgain)
	assert.Empty(t, targetsAgain)
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
