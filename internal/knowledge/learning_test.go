package knowledge

import (
	"context"
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

func TestLearnWorkspace_GeneratesStructuredKnowledgeDocuments(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	workspaceDir := filepath.Join(workspacesDir, "acme")
	aiDir := filepath.Join(workspaceDir, "ai-analysis")
	require.NoError(t, os.MkdirAll(aiDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "unified-analysis-001.md"), []byte("# Insight\n\nUse verified findings first."), 0o644))

	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	ctx := context.Background()
	now := time.Now()

	asset := &database.Asset{
		Workspace:    "acme",
		AssetValue:   "https://app.acme.test/login",
		URL:          "https://app.acme.test/login",
		AssetType:    "url",
		Technologies: []string{"go", "nginx"},
		Source:       "httpx",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastSeenAt:   now,
	}
	_, err = database.GetDB().NewInsert().Model(asset).Exec(ctx)
	require.NoError(t, err)

	verified := &database.Vulnerability{
		Workspace:       "acme",
		VulnInfo:        "sql-injection",
		VulnTitle:       "SQL Injection",
		VulnDesc:        "UNION based SQL injection on login endpoint.",
		Severity:        "high",
		Confidence:      "certain",
		AssetType:       "url",
		AssetValue:      asset.URL,
		VulnStatus:      "verified",
		EvidenceVersion: 2,
		AISummary:       "Reproduced with UNION payload.",
		CreatedAt:       now,
		UpdatedAt:       now,
		LastSeenAt:      now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)

	falsePositive := &database.Vulnerability{
		Workspace:    "acme",
		VulnInfo:     "open-redirect",
		VulnTitle:    "Open Redirect",
		Severity:     "medium",
		Confidence:   "firm",
		AssetType:    "url",
		AssetValue:   "https://app.acme.test/redirect",
		VulnStatus:   "false_positive",
		AnalystNotes: "Validated as intended redirect allowlist behavior.",
		CreatedAt:    now,
		UpdatedAt:    now,
		LastSeenAt:   now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, falsePositive)
	require.NoError(t, err)

	run := &database.Run{
		RunUUID:      "run-1",
		WorkflowName: "superdomain-extensive-ai",
		WorkflowKind: "flow",
		Target:       "app.acme.test",
		Status:       "completed",
		Workspace:    "acme",
		RunPriority:  "high",
		RunMode:      "local",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	require.NoError(t, database.CreateRun(ctx, run))

	summary, err := LearnWorkspace(ctx, cfg, LearnOptions{
		Workspace: "acme",
	})
	require.NoError(t, err)
	assert.Equal(t, 4, summary.Documents)
	assert.Equal(t, "acme", summary.StorageWorkspace)
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/workspace-summary.md")
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/verified-findings.md")
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/false-positive-samples.md")
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/ai-insights.md")

	docs, err := ListDocuments(ctx, "acme", 0, 20)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, docs.TotalCount, 4)

	var summaryDoc database.KnowledgeDocument
	err = database.GetDB().NewSelect().
		Model(&summaryDoc).
		Where("workspace = ?", "acme").
		Where("source_path = ?", "kb://learned/workspace/acme/workspace-summary.md").
		Limit(1).
		Scan(ctx)
	require.NoError(t, err)

	var chunks []database.KnowledgeChunk
	err = database.GetDB().NewSelect().
		Model(&chunks).
		Where("document_id = ?", summaryDoc.ID).
		Order("chunk_index ASC").
		Scan(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	var combined strings.Builder
	for _, chunk := range chunks {
		combined.WriteString(chunk.Content)
		combined.WriteString("\n")
	}
	content := combined.String()
	assert.Contains(t, content, "Lifecycle: verified")
	assert.Contains(t, content, "Lifecycle: false_positive")
}

func TestLearnWorkspace_PublicScopeStoresDocumentsInPublicLayer(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning-public.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	ctx := context.Background()
	now := time.Now()
	verified := &database.Vulnerability{
		Workspace:  "acme",
		VulnInfo:   "sql-injection",
		VulnTitle:  "SQL Injection",
		Severity:   "high",
		Confidence: "certain",
		AssetType:  "url",
		AssetValue: "https://app.acme.test/login",
		VulnStatus: "verified",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)

	summary, err := LearnWorkspace(ctx, cfg, LearnOptions{
		Workspace: "acme",
		Scope:     "public",
	})
	require.NoError(t, err)
	assert.Equal(t, "acme", summary.Workspace)
	assert.Equal(t, "public", summary.StorageWorkspace)

	docs, err := ListDocuments(ctx, "public", 0, 20)
	require.NoError(t, err)
	require.NotEmpty(t, docs.Data)
	assert.Equal(t, "public", docs.Data[0].Workspace)
	assert.Contains(t, docs.Data[0].Metadata, `"source_workspace":"acme"`)
	assert.Contains(t, docs.Data[0].Metadata, `"knowledge_layer":"public"`)
}

func TestLearnWorkspace_AssignsStableRetrievalFingerprintAcrossScopes(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning-fingerprint.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	ctx := context.Background()
	now := time.Now()
	verified := &database.Vulnerability{
		Workspace:  "acme",
		VulnInfo:   "sql-injection",
		VulnTitle:  "SQL Injection",
		Severity:   "high",
		Confidence: "certain",
		AssetType:  "url",
		AssetValue: "https://app.acme.test/login",
		VulnStatus: "verified",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)

	_, err = LearnWorkspace(ctx, cfg, LearnOptions{Workspace: "acme"})
	require.NoError(t, err)
	_, err = LearnWorkspace(ctx, cfg, LearnOptions{Workspace: "acme", Scope: "public"})
	require.NoError(t, err)

	workspaceDocs, err := ListDocuments(ctx, "acme", 0, 20)
	require.NoError(t, err)
	publicDocs, err := ListDocuments(ctx, "public", 0, 20)
	require.NoError(t, err)

	var workspaceFingerprint string
	for _, doc := range workspaceDocs.Data {
		if doc.SourcePath == "kb://learned/workspace/acme/verified-findings.md" {
			metadata := database.ParseKnowledgeMetadata(doc.Metadata)
			require.NotNil(t, metadata)
			workspaceFingerprint = metadata.RetrievalFingerprint
		}
	}
	var publicFingerprint string
	for _, doc := range publicDocs.Data {
		if doc.SourcePath == "kb://learned/public/acme/verified-findings.md" {
			metadata := database.ParseKnowledgeMetadata(doc.Metadata)
			require.NotNil(t, metadata)
			publicFingerprint = metadata.RetrievalFingerprint
		}
	}

	require.NotEmpty(t, workspaceFingerprint)
	require.Equal(t, workspaceFingerprint, publicFingerprint)
}

func TestLearnWorkspace_IncludesOperationalPlaybookFromFollowupArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	workspaceDir := filepath.Join(workspacesDir, "acme")
	aiDir := filepath.Join(workspaceDir, "ai-analysis")
	require.NoError(t, os.MkdirAll(aiDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "applied-ai-decision-acme.json"), []byte(`{
  "scan":{"profile":"balanced","severity":"critical,high"},
  "targets":{"focus_areas":["https://app.acme.test/login"],"priority_targets":["https://app.acme.test/admin"],"rescan_targets":["https://app.acme.test/api"]},
  "reasoning":"Authentication surface is the highest-value path."
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "operator-queue-acme.json"), []byte(`{
  "summary":{"total_tasks":2,"p1_tasks":1,"p2_tasks":1},
  "focus_targets":["https://app.acme.test/admin"],
  "tasks":[
    {"priority":"P1","title":"Validate admin takeover","target":"https://app.acme.test/admin","reason":"verified auth bypass candidate"}
  ]
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "followup-decision-acme.json"), []byte(`{
  "followup_summary":{"rescan_critical":1,"rescan_high":0},
  "seed_targets":{
    "manual_first_targets":["https://app.acme.test/admin"],
    "high_confidence_targets":["https://app.acme.test/api","https://app.acme.test/upload"]
  },
  "seed_focus":{
    "priority_mode":"manual-first",
    "confidence_level":"high",
    "reuse_sources":["operator-queue","targeted-rescan"],
    "signal_scores":{"escalation_score":16}
  },
  "refined_targets":{"focus_areas":["https://app.acme.test/admin"],"priority_targets":["https://app.acme.test/admin","https://app.acme.test/api"]},
  "execution_feedback":{"next_phase":"manual-exploitation","manual_followup_needed":true,"campaign_followup_recommended":false,"queue_followup_effective":false},
  "next_actions":["Capture proof for admin takeover path"]
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "campaign-handoff-acme.json"), []byte(`{
  "handoff_ready": true,
  "campaign_profile": {
    "recommended_flow":"web-classic",
    "retest_priority":"critical",
    "focus_areas":["https://app.acme.test/admin"],
    "previous_priority_mode":"manual-first",
    "previous_confidence_level":"high",
    "previous_next_phase":"manual-exploitation",
    "previous_reuse_sources":["operator-queue","targeted-rescan"],
    "previous_escalation_score":16
  },
  "counts": {
    "campaign_targets":2,
    "operator_tasks":2,
    "previous_followup_targets":3
  },
  "targets": {
    "semantic_priority":["https://app.acme.test/graphql"]
  },
  "next_actions":["Promote admin path into campaign"]
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "campaign-create-acme.json"), []byte(`{
  "status":"created",
  "campaign_id":"camp-42",
  "queued_runs":2,
  "workflow":"web-classic",
  "workflow_kind":"flow",
  "priority":"critical",
  "target_count":2,
  "campaign_priority_mode":"manual-first",
  "campaign_confidence_level":"high",
  "campaign_followup_next_phase":"manual-exploitation",
  "campaign_reuse_sources":"operator-queue,targeted-rescan",
  "previous_followup_targets":3,
  "campaign_escalation_score":16,
  "deep_scan_workflow":"deep-web",
  "deep_scan_workflow_kind":"module",
  "auto_deep_scan":true,
  "high_risk_severities":["critical","high"]
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "retest-queue-summary-acme.json"), []byte(`{
  "status":"queued",
  "workflow":"web-analysis",
  "workflow_kind":"flow",
  "priority":"critical",
  "target_source":"previous_followup_seed",
  "queued_targets":2,
  "previous_followup_targets":3,
  "previous_priority_mode":"manual-first",
  "previous_confidence_level":"high"
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "rescan-summary-acme.md"), []byte("# Rescan\n\nConfirmed secondary auth weakness."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(aiDir, "unified-analysis-knowledge-acme.json"), []byte(`{
  "followup_seed_focus": {
    "priority_mode":"manual-first",
    "confidence_level":"high",
    "next_phase":"manual-exploitation",
    "escalation_score":16,
    "reuse_sources":["operator-queue","targeted-rescan"],
    "next_actions":["Promote admin path into campaign"],
    "manual_followup_needed":true,
    "campaign_followup_recommended":false,
    "queue_followup_effective":false
  },
  "operational_counts": {
    "retest_targets":2,
    "operator_tasks":2,
    "campaign_targets":2,
    "campaign_create_queued_runs":2,
    "retest_queued_targets":2
  },
  "campaign_creation": {
    "status":"created",
    "campaign_id":"camp-42",
    "queued_runs":2
  },
  "artifact_presence": {
    "campaign_ready": true
  }
}`), 0o644))

	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning-operational.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	ctx := context.Background()
	now := time.Now()

	verified := &database.Vulnerability{
		Workspace:  "acme",
		VulnInfo:   "auth-bypass",
		VulnTitle:  "Authentication Bypass",
		Severity:   "critical",
		Confidence: "certain",
		AssetType:  "url",
		AssetValue: "https://app.acme.test/admin",
		VulnStatus: "verified",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)

	summary, err := LearnWorkspace(ctx, cfg, LearnOptions{
		Workspace: "acme",
	})
	require.NoError(t, err)
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/operational-playbook.md")
	assert.Contains(t, summary.AIFilesIncluded, filepath.Join(aiDir, "followup-decision-acme.json"))
	assert.Contains(t, summary.AIFilesIncluded, filepath.Join(aiDir, "operator-queue-acme.json"))
	assert.Contains(t, summary.AIFilesIncluded, filepath.Join(aiDir, "campaign-create-acme.json"))
	assert.Contains(t, summary.AIFilesIncluded, filepath.Join(aiDir, "retest-queue-summary-acme.json"))
	assert.Contains(t, summary.AIFilesIncluded, filepath.Join(aiDir, "unified-analysis-knowledge-acme.json"))

	var doc database.KnowledgeDocument
	err = database.GetDB().NewSelect().
		Model(&doc).
		Where("workspace = ?", "acme").
		Where("source_path = ?", "kb://learned/workspace/acme/operational-playbook.md").
		Limit(1).
		Scan(ctx)
	require.NoError(t, err)
	assert.Equal(t, "learned-operational-playbook", doc.DocType)

	var chunks []database.KnowledgeChunk
	err = database.GetDB().NewSelect().
		Model(&chunks).
		Where("document_id = ?", doc.ID).
		Order("chunk_index ASC").
		Scan(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	var combined strings.Builder
	for _, chunk := range chunks {
		combined.WriteString(chunk.Content)
		combined.WriteString("\n")
	}
	content := combined.String()
	assert.Contains(t, content, "Operational Follow-up Playbook")
	assert.Contains(t, content, "manual-exploitation")
	assert.Contains(t, content, "Validate admin takeover")
	assert.Contains(t, content, "Priority mode: manual-first")
	assert.Contains(t, content, "High-Confidence Targets")
	assert.Contains(t, content, "Campaign Creation Result")
	assert.Contains(t, content, "Campaign confidence level: high")
	assert.Contains(t, content, "Retest Queue Status")
	assert.Contains(t, content, "Target source: previous_followup_seed")
	assert.Contains(t, content, "Knowledge Learning Context")
	assert.Contains(t, content, "Campaign ready artifact present: true")
	assert.Contains(t, content, "Context Reuse Sources")
}

func TestLearnWorkspace_OperationalPlaybookFallsBackToOlderRenderableArtifacts(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	workspaceDir := filepath.Join(workspacesDir, "acme")
	aiDir := filepath.Join(workspaceDir, "ai-analysis")
	require.NoError(t, os.MkdirAll(aiDir, 0o755))

	validFollowup := filepath.Join(aiDir, "followup-decision-acme-valid.json")
	invalidFollowup := filepath.Join(aiDir, "followup-decision-acme-latest.json")
	validRescan := filepath.Join(aiDir, "rescan-summary-acme-valid.md")
	emptyRescan := filepath.Join(aiDir, "rescan-summary-acme-latest.md")

	require.NoError(t, os.WriteFile(validFollowup, []byte(`{
  "followup_summary":{"rescan_critical":1,"rescan_high":0},
  "seed_targets":{
    "manual_first_targets":["https://app.acme.test/admin"],
    "high_confidence_targets":["https://app.acme.test/api"]
  },
  "seed_focus":{
    "priority_mode":"manual-first",
    "confidence_level":"high",
    "reuse_sources":["operator-queue"],
    "signal_scores":{"escalation_score":12}
  },
  "refined_targets":{"focus_areas":["https://app.acme.test/admin"],"priority_targets":["https://app.acme.test/admin"]},
  "execution_feedback":{"next_phase":"manual-exploitation","manual_followup_needed":true,"campaign_followup_recommended":false,"queue_followup_effective":false},
  "next_actions":["Replay admin token path"]
}`), 0o644))
	require.NoError(t, os.WriteFile(invalidFollowup, []byte(`{"seed_focus":`), 0o644))
	require.NoError(t, os.WriteFile(validRescan, []byte("# Rescan\n\nLegacy admin replay note."), 0o644))
	require.NoError(t, os.WriteFile(emptyRescan, []byte("   \n"), 0o644))

	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(validFollowup, oldTime, oldTime))
	require.NoError(t, os.Chtimes(invalidFollowup, newTime, newTime))
	require.NoError(t, os.Chtimes(validRescan, oldTime, oldTime))
	require.NoError(t, os.Chtimes(emptyRescan, newTime, newTime))

	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning-fallback.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	ctx := context.Background()
	now := time.Now()
	verified := &database.Vulnerability{
		Workspace:  "acme",
		VulnInfo:   "auth-bypass",
		VulnTitle:  "Authentication Bypass",
		Severity:   "critical",
		Confidence: "certain",
		AssetType:  "url",
		AssetValue: "https://app.acme.test/admin",
		VulnStatus: "verified",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}
	_, err = database.CreateVulnerabilityRecord(ctx, verified)
	require.NoError(t, err)

	summary, err := LearnWorkspace(ctx, cfg, LearnOptions{Workspace: "acme"})
	require.NoError(t, err)
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/operational-playbook.md")

	var doc database.KnowledgeDocument
	err = database.GetDB().NewSelect().
		Model(&doc).
		Where("workspace = ?", "acme").
		Where("source_path = ?", "kb://learned/workspace/acme/operational-playbook.md").
		Limit(1).
		Scan(ctx)
	require.NoError(t, err)

	var chunks []database.KnowledgeChunk
	err = database.GetDB().NewSelect().
		Model(&chunks).
		Where("document_id = ?", doc.ID).
		Order("chunk_index ASC").
		Scan(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	var combined strings.Builder
	for _, chunk := range chunks {
		combined.WriteString(chunk.Content)
		combined.WriteString("\n")
	}
	content := combined.String()
	assert.Contains(t, content, "manual-exploitation")
	assert.Contains(t, content, "Replay admin token path")
	assert.Contains(t, content, "Legacy admin replay note.")
}

func TestLearnWorkspace_FetchesVerifiedAndFalsePositiveMemoryOutsideRecentMixedWindow(t *testing.T) {
	tmpDir := t.TempDir()
	workspacesDir := filepath.Join(tmpDir, "workspaces")
	cfg := &config.Config{
		BaseFolder:     tmpDir,
		WorkspacesPath: workspacesDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "knowledge-learning-sampling.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	ctx := context.Background()
	now := time.Now()

	oldVerified := &database.Vulnerability{
		Workspace:       "acme",
		VulnInfo:        "legacy-verified",
		VulnTitle:       "Legacy Verified Finding",
		Severity:        "high",
		Confidence:      "certain",
		AssetType:       "url",
		AssetValue:      "https://app.acme.test/legacy",
		VulnStatus:      "verified",
		EvidenceVersion: 3,
		CreatedAt:       now.Add(-48 * time.Hour),
		UpdatedAt:       now.Add(-48 * time.Hour),
		LastSeenAt:      now.Add(-48 * time.Hour),
	}
	_, err = database.CreateVulnerabilityRecord(ctx, oldVerified)
	require.NoError(t, err)

	oldFalsePositive := &database.Vulnerability{
		Workspace:    "acme",
		VulnInfo:     "legacy-fp",
		VulnTitle:    "Legacy False Positive",
		Severity:     "medium",
		Confidence:   "firm",
		AssetType:    "url",
		AssetValue:   "https://app.acme.test/fp",
		VulnStatus:   "false_positive",
		AnalystNotes: "Confirmed benign legacy behavior.",
		CreatedAt:    now.Add(-47 * time.Hour),
		UpdatedAt:    now.Add(-47 * time.Hour),
		LastSeenAt:   now.Add(-47 * time.Hour),
	}
	_, err = database.CreateVulnerabilityRecord(ctx, oldFalsePositive)
	require.NoError(t, err)

	for i := 0; i < 25; i++ {
		vuln := &database.Vulnerability{
			Workspace:  "acme",
			VulnInfo:   "recent-noise",
			VulnTitle:  "Recent Noise",
			Severity:   "low",
			Confidence: "tentative",
			AssetType:  "url",
			AssetValue: "https://app.acme.test/noise-" + strings.TrimSpace(string(rune('a'+(i%26)))),
			VulnStatus: "new",
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:  now.Add(time.Duration(i) * time.Minute),
			LastSeenAt: now.Add(time.Duration(i) * time.Minute),
		}
		_, err = database.CreateVulnerabilityRecord(ctx, vuln)
		require.NoError(t, err)
	}

	summary, err := LearnWorkspace(ctx, cfg, LearnOptions{
		Workspace:          "acme",
		MaxVulnerabilities: 20,
	})
	require.NoError(t, err)
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/verified-findings.md")
	assert.Contains(t, summary.SourcePaths, "kb://learned/workspace/acme/false-positive-samples.md")

	docs, err := ListDocuments(ctx, "acme", 0, 20)
	require.NoError(t, err)

	contentByPath := make(map[string]string)
	for _, doc := range docs.Data {
		var chunks []database.KnowledgeChunk
		err := database.GetDB().NewSelect().
			Model(&chunks).
			Where("document_id = ?", doc.ID).
			Order("chunk_index ASC").
			Scan(ctx)
		require.NoError(t, err)
		var builder strings.Builder
		for _, chunk := range chunks {
			builder.WriteString(chunk.Content)
			builder.WriteString("\n")
		}
		contentByPath[doc.SourcePath] = builder.String()
	}

	assert.Contains(t, contentByPath["kb://learned/workspace/acme/verified-findings.md"], "Legacy Verified Finding")
	assert.Contains(t, contentByPath["kb://learned/workspace/acme/false-positive-samples.md"], "Legacy False Positive")
}

func TestFirstLearnedFile_PrefersNewestArtifact(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "followup-decision-old.json")
	newFile := filepath.Join(dir, "followup-decision-new.json")
	require.NoError(t, os.WriteFile(oldFile, []byte(`{"status":"old"}`), 0o644))
	require.NoError(t, os.WriteFile(newFile, []byte(`{"status":"new"}`), 0o644))

	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(oldFile, oldTime, oldTime))
	require.NoError(t, os.Chtimes(newFile, newTime, newTime))

	assert.Equal(t, newFile, firstLearnedFile(dir, "followup-decision-*.json"))
}

func TestFirstRenderableLearnedSection_PrefersNewestRenderableArtifact(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "followup-decision-old.json")
	newFile := filepath.Join(dir, "followup-decision-new.json")
	require.NoError(t, os.WriteFile(oldFile, []byte(`{
  "execution_feedback":{"next_phase":"manual-exploitation"},
  "seed_focus":{"priority_mode":"manual-first","confidence_level":"high"},
  "followup_summary":{"rescan_critical":1,"rescan_high":0}
}`), 0o644))
	require.NoError(t, os.WriteFile(newFile, []byte(`{"execution_feedback":`), 0o644))

	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(oldFile, oldTime, oldTime))
	require.NoError(t, os.Chtimes(newFile, newTime, newTime))

	file, section := firstRenderableLearnedSection(dir, "followup-decision-*.json", renderFollowupDecisionSection)
	assert.Equal(t, oldFile, file)
	assert.Contains(t, section, "manual-exploitation")
}
