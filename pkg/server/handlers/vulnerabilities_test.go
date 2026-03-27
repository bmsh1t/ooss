package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListVulnerabilities_FiltersByFingerprintAndSourceRun(t *testing.T) {
	cfg, cleanup := setupVulnerabilityHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	first := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "verified",
		SourceRunUUID: "run-a",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	result, err := database.CreateVulnerabilityRecord(ctx, first)
	require.NoError(t, err)
	require.NotNil(t, result.Vulnerability)

	second := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "xss",
		VulnTitle:     "Reflected XSS",
		Severity:      "medium",
		Confidence:    "firm",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/search",
		VulnStatus:    "triaged",
		SourceRunUUID: "run-b",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, second)
	require.NoError(t, err)

	app := fiber.New()
	app.Get("/vulnerabilities", ListVulnerabilities(cfg))

	req := httptest.NewRequest("GET", "/vulnerabilities?fingerprint_key="+result.Vulnerability.FingerprintKey+"&source_run_uuid=run-a", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		Data []database.Vulnerability `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Len(t, payload.Data, 1)
	assert.Equal(t, "run-a", payload.Data[0].SourceRunUUID)
	assert.Equal(t, result.Vulnerability.FingerprintKey, payload.Data[0].FingerprintKey)
}

func TestListVulnerabilities_FiltersOperationalStateAndVerdicts(t *testing.T) {
	cfg, cleanup := setupVulnerabilityHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	for _, vuln := range []*database.Vulnerability{
		{
			Workspace:      "acme",
			VulnInfo:       "sql-injection",
			VulnTitle:      "SQL Injection",
			Severity:       "critical",
			Confidence:     "certain",
			AssetType:      "url",
			AssetValue:     "https://app.acme.test/admin",
			VulnStatus:     "verified",
			AIVerdict:      "confirmed",
			AnalystVerdict: "confirmed",
			AttackChainRef: "report:10:chain-admin",
			SourceRunUUID:  "run-1",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastSeenAt:     now,
		},
		{
			Workspace:      "acme",
			VulnInfo:       "open-redirect",
			VulnTitle:      "Open Redirect",
			Severity:       "high",
			Confidence:     "firm",
			AssetType:      "url",
			AssetValue:     "https://app.acme.test/admin",
			VulnStatus:     "retest",
			RetestStatus:   "running",
			AIVerdict:      "confirmed",
			AttackChainRef: "report:11:chain-admin",
			SourceRunUUID:  "run-2",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastSeenAt:     now,
		},
		{
			Workspace:      "acme",
			VulnInfo:       "xss",
			VulnTitle:      "Reflected XSS",
			Severity:       "medium",
			Confidence:     "firm",
			AssetType:      "url",
			AssetValue:     "https://app.acme.test/search",
			VulnStatus:     "false_positive",
			AIVerdict:      "false_positive",
			AnalystVerdict: "false_positive",
			SourceRunUUID:  "run-3",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastSeenAt:     now,
		},
		{
			Workspace:      "acme",
			VulnInfo:       "ssrf",
			VulnTitle:      "SSRF",
			Severity:       "high",
			Confidence:     "tentative",
			AssetType:      "url",
			AssetValue:     "https://app.acme.test/portal",
			VulnStatus:     "triaged",
			RetestStatus:   "queued",
			AIVerdict:      "needs_verification",
			AnalystVerdict: "false_positive",
			SourceRunUUID:  "run-4",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastSeenAt:     now,
		},
	} {
		_, err := database.CreateVulnerabilityRecord(ctx, vuln)
		require.NoError(t, err)
	}

	app := fiber.New()
	app.Get("/vulnerabilities", ListVulnerabilities(cfg))

	req := httptest.NewRequest("GET", "/vulnerabilities?workspace=acme&active_only=true&retest_status=running&ai_verdict=confirmed&has_attack_chain=true", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var activePayload struct {
		Data []database.Vulnerability `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&activePayload))
	require.Len(t, activePayload.Data, 1)
	assert.Equal(t, "open-redirect", activePayload.Data[0].VulnInfo)
	assert.Equal(t, "running", activePayload.Data[0].RetestStatus)

	req = httptest.NewRequest("GET", "/vulnerabilities?workspace=acme&analyst_verdict=confirmed", nil)
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var reviewPayload struct {
		Data []database.Vulnerability `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&reviewPayload))
	require.Len(t, reviewPayload.Data, 1)
	assert.Equal(t, "sql-injection", reviewPayload.Data[0].VulnInfo)
}

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
			StatusTimeline      []map[string]any                 `json:"status_timeline"`
			RetestTimeline      []map[string]any                 `json:"retest_timeline"`
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
	require.Len(t, payload.Data.StatusTimeline, 2)
	require.Len(t, payload.Data.RetestTimeline, 1)
	assert.Equal(t, "verified", payload.Data.EvidenceTimeline[0].VulnStatus)
	assert.Equal(t, "retest", payload.Data.EvidenceTimeline[1].VulnStatus)
	assert.Equal(t, float64(1), payload.Data.StatusTimeline[0]["evidence_version"])
	assert.Equal(t, "queued", payload.Data.RetestTimeline[0]["status"])
	require.Len(t, payload.Data.RelatedRuns, 2)
	require.Len(t, payload.Data.RelatedAssetRecords, 1)
	require.Len(t, payload.Data.RelatedAttackChains, 1)
	assert.Equal(t, "chain-login", payload.Data.RelatedAttackChains[0]["chain_id"])
	assert.Equal(t, "entry_point_url", payload.Data.RelatedAttackChains[0]["matched_by"])
}

func TestListVulnerabilityGroups_ComputesDerivedCounts(t *testing.T) {
	cfg, cleanup := setupVulnerabilityHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().Add(-2 * time.Minute)

	vuln := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "verified",
		SourceRunUUID: "run-source-1",
		ReportRefs:    []string{"report-a.md"},
		RelatedAssets: []string{"https://app.acme.test/admin"},
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	result, err := database.CreateVulnerabilityRecord(ctx, vuln)
	require.NoError(t, err)
	require.NotNil(t, result.Vulnerability)

	vuln = result.Vulnerability
	vuln.VulnStatus = "retest"
	vuln.RetestStatus = "queued"
	vuln.RetestRunUUID = "retest-run-1"
	vuln.SourceRunUUID = "retest-run-1"
	vuln.AttackChainRef = "report:17:chain-login"
	vuln.ReportRefs = []string{"report-a.md", "report-b.md"}
	vuln.RelatedAssets = []string{"https://app.acme.test/admin", "https://app.acme.test/users"}
	vuln.UpdatedAt = now.Add(time.Minute)
	vuln.LastSeenAt = now.Add(time.Minute)
	require.NoError(t, database.UpdateVulnerabilityRecord(ctx, vuln))

	app := fiber.New()
	app.Get("/vulnerabilities/groups", ListVulnerabilityGroups(cfg))

	req := httptest.NewRequest("GET", "/vulnerabilities/groups?workspace=acme&has_attack_chain=true", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Len(t, payload.Data, 1)

	group := payload.Data[0]
	assert.Equal(t, vuln.FingerprintKey, group["fingerprint_key"])
	assert.Equal(t, float64(2), group["evidence_versions"])
	assert.Equal(t, float64(2), group["distinct_runs"])
	assert.Equal(t, float64(3), group["asset_count"])
	assert.Equal(t, float64(2), group["report_ref_count"])
	assert.Equal(t, true, group["attack_chain_linked"])
	assert.Equal(t, "retest", group["vuln_status"])
}

func TestGetVulnerabilityBoard_EnrichesTopTargetsAndReviewCoverage(t *testing.T) {
	cfg, cleanup := setupVulnerabilityHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	for _, vuln := range []*database.Vulnerability{
		{
			Workspace:      "acme",
			VulnInfo:       "sql-injection",
			VulnTitle:      "SQL Injection",
			Severity:       "critical",
			Confidence:     "certain",
			AssetType:      "url",
			AssetValue:     "https://app.acme.test/admin",
			VulnStatus:     "verified",
			AIVerdict:      "confirmed",
			AnalystVerdict: "confirmed",
			AttackChainRef: "report:10:chain-admin",
			SourceRunUUID:  "run-a",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastSeenAt:     now,
		},
		{
			Workspace:     "acme",
			VulnInfo:      "open-redirect",
			VulnTitle:     "Open Redirect",
			Severity:      "high",
			Confidence:    "firm",
			AssetType:     "url",
			AssetValue:    "https://app.acme.test/admin",
			VulnStatus:    "retest",
			RetestStatus:  "running",
			AIVerdict:     "confirmed",
			SourceRunUUID: "run-b",
			CreatedAt:     now,
			UpdatedAt:     now,
			LastSeenAt:    now,
		},
		{
			Workspace:      "acme",
			VulnInfo:       "xss",
			VulnTitle:      "Reflected XSS",
			Severity:       "medium",
			Confidence:     "firm",
			AssetType:      "url",
			AssetValue:     "https://app.acme.test/search",
			VulnStatus:     "false_positive",
			AIVerdict:      "false_positive",
			AnalystVerdict: "false_positive",
			SourceRunUUID:  "run-c",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastSeenAt:     now,
		},
		{
			Workspace:      "acme",
			VulnInfo:       "ssrf",
			VulnTitle:      "SSRF",
			Severity:       "high",
			Confidence:     "tentative",
			AssetType:      "url",
			AssetValue:     "https://app.acme.test/portal",
			VulnStatus:     "triaged",
			AIVerdict:      "needs_verification",
			AnalystVerdict: "false_positive",
			SourceRunUUID:  "run-d",
			CreatedAt:      now,
			UpdatedAt:      now,
			LastSeenAt:     now,
		},
	} {
		_, err := database.CreateVulnerabilityRecord(ctx, vuln)
		require.NoError(t, err)
	}

	app := fiber.New()
	app.Get("/vulnerabilities/board", GetVulnerabilityBoard(cfg))

	req := httptest.NewRequest("GET", "/vulnerabilities/board?workspace=acme&top_targets_limit=2", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		Data struct {
			Total          int              `json:"total"`
			OpenHighRisk   int              `json:"open_high_risk"`
			Verified       int              `json:"verified"`
			FalsePositive  int              `json:"false_positive"`
			NeedsRetest    int              `json:"needs_retest"`
			TopTargets     []map[string]any `json:"top_targets"`
			ReviewCoverage map[string]any   `json:"review_coverage"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))

	assert.Equal(t, 4, payload.Data.Total)
	assert.Equal(t, 3, payload.Data.OpenHighRisk)
	assert.Equal(t, 1, payload.Data.Verified)
	assert.Equal(t, 1, payload.Data.FalsePositive)
	assert.Equal(t, 1, payload.Data.NeedsRetest)
	require.Len(t, payload.Data.TopTargets, 2)
	assert.Equal(t, "https://app.acme.test/admin", payload.Data.TopTargets[0]["asset_value"])
	assert.Equal(t, "critical", payload.Data.TopTargets[0]["highest_severity"])
	assert.Equal(t, float64(2), payload.Data.TopTargets[0]["findings"])
	assert.Equal(t, float64(2), payload.Data.TopTargets[0]["open_high_risk"])
	assert.Equal(t, float64(1), payload.Data.TopTargets[0]["verified"])
	assert.Equal(t, float64(1), payload.Data.TopTargets[0]["needs_retest"])
	assert.Equal(t, float64(1), payload.Data.TopTargets[0]["attack_chain_linked"])
	assert.Equal(t, "https://app.acme.test/portal", payload.Data.TopTargets[1]["asset_value"])

	assert.Equal(t, float64(3), payload.Data.ReviewCoverage["reviewed"])
	assert.Equal(t, float64(1), payload.Data.ReviewCoverage["unreviewed"])
	assert.Equal(t, float64(2), payload.Data.ReviewCoverage["agreement"])
	assert.Equal(t, float64(1), payload.Data.ReviewCoverage["disagreement"])
	aiVerdicts := payload.Data.ReviewCoverage["ai_verdicts"].(map[string]any)
	analystVerdicts := payload.Data.ReviewCoverage["analyst_verdicts"].(map[string]any)
	assert.Equal(t, float64(2), aiVerdicts["confirmed"])
	assert.Equal(t, float64(1), aiVerdicts["false_positive"])
	assert.Equal(t, float64(1), aiVerdicts["needs_verification"])
	assert.Equal(t, float64(1), analystVerdicts["confirmed"])
	assert.Equal(t, float64(2), analystVerdicts["false_positive"])
}

func TestBulkVulnerabilityAction_UpdatesByIDsAndFingerprints(t *testing.T) {
	cfg, cleanup := setupVulnerabilityHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	first := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "new",
		SourceRunUUID: "run-a",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	second := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "ssrf",
		VulnTitle:     "SSRF",
		Severity:      "medium",
		Confidence:    "firm",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/proxy",
		VulnStatus:    "new",
		SourceRunUUID: "run-b",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	third := &database.Vulnerability{
		Workspace:     "beta",
		VulnInfo:      "xss",
		VulnTitle:     "Reflected XSS",
		Severity:      "low",
		Confidence:    "tentative",
		AssetType:     "url",
		AssetValue:    "https://beta.test/search",
		VulnStatus:    "new",
		SourceRunUUID: "run-c",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}

	resultA, err := database.CreateVulnerabilityRecord(ctx, first)
	require.NoError(t, err)
	resultB, err := database.CreateVulnerabilityRecord(ctx, second)
	require.NoError(t, err)
	_, err = database.CreateVulnerabilityRecord(ctx, third)
	require.NoError(t, err)

	app := fiber.New()
	app.Post("/vulnerabilities/bulk", BulkVulnerabilityAction(cfg))

	body := `{
		"action": "triage",
		"workspace": "acme",
		"ids": [` + strconv.FormatInt(resultA.Vulnerability.ID, 10) + `],
		"fingerprint_keys": ["` + resultB.Vulnerability.FingerprintKey + `"],
		"analyst_verdict": "confirmed",
		"analyst_notes": "batch reviewed"
	}`
	req := httptest.NewRequest("POST", "/vulnerabilities/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		Summary struct {
			Selected int    `json:"selected"`
			Updated  int    `json:"updated"`
			Action   string `json:"action"`
		} `json:"summary"`
		Data []struct {
			ID         int64  `json:"id"`
			Outcome    string `json:"outcome"`
			VulnStatus string `json:"vuln_status"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, 2, payload.Summary.Selected)
	assert.Equal(t, 2, payload.Summary.Updated)
	assert.Equal(t, "triage", payload.Summary.Action)
	require.Len(t, payload.Data, 2)
	for _, item := range payload.Data {
		assert.Equal(t, "updated", item.Outcome)
		assert.Equal(t, "triaged", item.VulnStatus)
	}

	storedA, err := database.GetVulnerabilityByID(ctx, resultA.Vulnerability.ID)
	require.NoError(t, err)
	storedB, err := database.GetVulnerabilityByID(ctx, resultB.Vulnerability.ID)
	require.NoError(t, err)
	assert.Equal(t, "triaged", storedA.VulnStatus)
	assert.Equal(t, "confirmed", storedA.AnalystVerdict)
	assert.Equal(t, "batch reviewed", storedA.AnalystNotes)
	assert.Equal(t, "triaged", storedB.VulnStatus)
	assert.Equal(t, "confirmed", storedB.AnalystVerdict)
	assert.Equal(t, "batch reviewed", storedB.AnalystNotes)
}

func TestBulkVulnerabilityAction_QueuesRetestAndSkipsInProgress(t *testing.T) {
	cfg, cleanup := setupVulnerabilityHandlerDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	first := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "sql-injection",
		VulnTitle:     "SQL Injection",
		Severity:      "high",
		Confidence:    "certain",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/login",
		VulnStatus:    "verified",
		SourceRunUUID: "run-a",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	second := &database.Vulnerability{
		Workspace:     "acme",
		VulnInfo:      "ssrf",
		VulnTitle:     "SSRF",
		Severity:      "high",
		Confidence:    "firm",
		AssetType:     "url",
		AssetValue:    "https://app.acme.test/proxy",
		VulnStatus:    "retest",
		RetestStatus:  "running",
		RetestRunUUID: "existing-retest-run",
		SourceRunUUID: "run-b",
		CreatedAt:     now,
		UpdatedAt:     now,
		LastSeenAt:    now,
	}
	resultA, err := database.CreateVulnerabilityRecord(ctx, first)
	require.NoError(t, err)
	resultB, err := database.CreateVulnerabilityRecord(ctx, second)
	require.NoError(t, err)

	app := fiber.New()
	app.Post("/vulnerabilities/bulk", BulkVulnerabilityAction(cfg))

	body := `{
		"action": "retest",
		"ids": [` + strconv.FormatInt(resultA.Vulnerability.ID, 10) + `, ` + strconv.FormatInt(resultB.Vulnerability.ID, 10) + `],
		"module": "web-classic",
		"priority": "critical",
		"params": {
			"recheck_mode": "focused"
		}
	}`
	req := httptest.NewRequest("POST", "/vulnerabilities/bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var payload struct {
		Summary struct {
			Selected int `json:"selected"`
			Queued   int `json:"queued"`
			Skipped  int `json:"skipped"`
		} `json:"summary"`
		Data []struct {
			ID           int64  `json:"id"`
			Outcome      string `json:"outcome"`
			RunUUID      string `json:"run_uuid"`
			VulnStatus   string `json:"vuln_status"`
			RetestStatus string `json:"retest_status"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, 2, payload.Summary.Selected)
	assert.Equal(t, 1, payload.Summary.Queued)
	assert.Equal(t, 1, payload.Summary.Skipped)
	require.Len(t, payload.Data, 2)

	var queuedRunUUID string
	for _, item := range payload.Data {
		switch item.ID {
		case resultA.Vulnerability.ID:
			assert.Equal(t, "queued", item.Outcome)
			assert.Equal(t, "retest", item.VulnStatus)
			assert.Equal(t, "queued", item.RetestStatus)
			queuedRunUUID = item.RunUUID
		case resultB.Vulnerability.ID:
			assert.Equal(t, "skipped", item.Outcome)
		}
	}
	require.NotEmpty(t, queuedRunUUID)

	storedA, err := database.GetVulnerabilityByID(ctx, resultA.Vulnerability.ID)
	require.NoError(t, err)
	assert.Equal(t, "retest", storedA.VulnStatus)
	assert.Equal(t, "queued", storedA.RetestStatus)
	assert.Equal(t, queuedRunUUID, storedA.RetestRunUUID)

	var runs []database.Run
	err = database.GetDB().NewSelect().
		Model(&runs).
		Where("trigger_type = ?", "vuln-retest").
		Scan(ctx)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "web-classic", runs[0].WorkflowName)
	assert.Equal(t, "module", runs[0].WorkflowKind)
	assert.Equal(t, "critical", runs[0].RunPriority)
	assert.Equal(t, "focused", runs[0].Params["recheck_mode"])
	assert.Equal(t, strconv.FormatInt(resultA.Vulnerability.ID, 10), runs[0].Params["retest_vulnerability_id"])
}
