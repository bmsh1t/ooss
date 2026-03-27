package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
)

const (
	defaultLearnScope      = "workspace"
	defaultLearnAssetLimit = 20
	defaultLearnVulnLimit  = 20
	defaultLearnRunLimit   = 10
	maxLearnedFileBytes    = 64 * 1024
	maxLearnedFiles        = 8
	maxOperationalItems    = 8
)

// LearnOptions controls automatic knowledge synthesis from existing scan results.
type LearnOptions struct {
	Workspace          string `json:"workspace"`
	Scope              string `json:"scope,omitempty"`
	MaxAssets          int    `json:"max_assets,omitempty"`
	MaxVulnerabilities int    `json:"max_vulnerabilities,omitempty"`
	MaxRuns            int    `json:"max_runs,omitempty"`
	IncludeAIAnalysis  *bool  `json:"include_ai_analysis,omitempty"`
}

// LearnSummary reports the output of a knowledge learning run.
type LearnSummary struct {
	Workspace        string   `json:"workspace"`
	StorageWorkspace string   `json:"storage_workspace,omitempty"`
	Scope            string   `json:"scope"`
	Documents        int      `json:"documents"`
	Chunks           int      `json:"chunks"`
	AssetsIncluded   int      `json:"assets_included"`
	VulnsIncluded    int      `json:"vulnerabilities_included"`
	RunsIncluded     int      `json:"runs_included"`
	AIFilesIncluded  []string `json:"ai_files_included,omitempty"`
	SourcePath       string   `json:"source_path"`
	SourcePaths      []string `json:"source_paths,omitempty"`
}

// LearnWorkspace builds a synthetic knowledge document from a workspace's existing findings.
func LearnWorkspace(ctx context.Context, cfg *config.Config, opts LearnOptions) (*LearnSummary, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}

	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	scope := normalizeLearnScope(opts.Scope)
	assetLimit := clampLearnLimit(opts.MaxAssets, defaultLearnAssetLimit, 200)
	vulnLimit := clampLearnLimit(opts.MaxVulnerabilities, defaultLearnVulnLimit, 200)
	runLimit := clampLearnLimit(opts.MaxRuns, defaultLearnRunLimit, 50)
	includeAIAnalysis := true
	if opts.IncludeAIAnalysis != nil {
		includeAIAnalysis = *opts.IncludeAIAnalysis
	}

	assetResult, err := database.ListAssets(ctx, database.AssetQuery{
		Workspace: workspace,
		Offset:    0,
		Limit:     assetLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list assets: %w", err)
	}

	vulnResult, err := database.ListVulnerabilities(ctx, database.VulnerabilityQuery{
		Workspace: workspace,
		Offset:    0,
		Limit:     vulnLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list vulnerabilities: %w", err)
	}
	verifiedResult, err := database.ListVulnerabilities(ctx, database.VulnerabilityQuery{
		Workspace:  workspace,
		VulnStatus: "verified",
		Offset:     0,
		Limit:      vulnLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list verified vulnerabilities: %w", err)
	}
	falsePositiveResult, err := database.ListVulnerabilities(ctx, database.VulnerabilityQuery{
		Workspace:  workspace,
		VulnStatus: "false_positive",
		Offset:     0,
		Limit:      vulnLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list false-positive vulnerabilities: %w", err)
	}

	runResult, err := database.ListRuns(ctx, 0, runLimit, "", "", "", workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to list runs: %w", err)
	}

	vulnSummary, err := database.GetVulnerabilitySummary(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to load vulnerability summary: %w", err)
	}

	assetStats, err := database.GetAssetStats(ctx, workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to load asset stats: %w", err)
	}

	workspaceDir := resolveWorkspacePath(ctx, cfg, workspace)
	aiSections, aiFiles := collectLearnedAISections(workspaceDir, includeAIAnalysis)
	operationalPlaybook, operationalFiles := buildOperationalPlaybookMarkdown(workspace, scope, workspaceDir, includeAIAnalysis)
	aiFiles = uniqueLearnedStrings(append(aiFiles, operationalFiles...))

	targetTypes := collectLearnedTargetTypes(assetResult)
	generatedAt := time.Now().Format(time.RFC3339)
	storageWorkspace := resolveLearnedKnowledgeWorkspace(workspace, scope)
	baseMetadata := map[string]interface{}{
		"scope":                    scope,
		"knowledge_layer":          storageWorkspace,
		"source":                   "auto-learn",
		"source_workspace":         workspace,
		"workspace":                workspace,
		"assets_included":          len(assetResult.Data),
		"vulnerabilities_included": len(vulnResult.Data),
		"runs_included":            len(runResult.Data),
		"ai_files":                 aiFiles,
		"operational_ai_files":     operationalFiles,
		"target_types":             targetTypes,
		"generated_at":             generatedAt,
	}

	type learnedDocument struct {
		sourcePath string
		docType    string
		title      string
		content    string
		metadata   map[string]interface{}
	}

	docs := []learnedDocument{
		{
			sourcePath: fmt.Sprintf("kb://learned/%s/%s/workspace-summary.md", scope, workspace),
			docType:    "learned-summary",
			title:      fmt.Sprintf("Learned Security Notes - %s", workspace),
			content:    buildLearnedKnowledgeMarkdown(workspace, scope, assetResult, vulnResult, runResult, vulnSummary, assetStats, aiSections),
			metadata: mergeLearnMetadata(baseMetadata, map[string]interface{}{
				"kind":              "workspace-summary",
				"source_confidence": 0.72,
				"sample_type":       "workspace-summary",
				"labels":            []string{"auto-learn", "workspace-summary", scope},
			}),
		},
		{
			sourcePath: fmt.Sprintf("kb://learned/%s/%s/verified-findings.md", scope, workspace),
			docType:    "learned-findings",
			title:      fmt.Sprintf("Verified Findings - %s", workspace),
			content:    buildVerifiedFindingsMarkdown(workspace, scope, verifiedResult),
			metadata: mergeLearnMetadata(baseMetadata, map[string]interface{}{
				"kind":              "verified-findings",
				"source_confidence": 0.95,
				"sample_type":       "verified",
				"labels":            []string{"auto-learn", "verified", scope},
			}),
		},
		{
			sourcePath: fmt.Sprintf("kb://learned/%s/%s/false-positive-samples.md", scope, workspace),
			docType:    "learned-false-positives",
			title:      fmt.Sprintf("False Positive Samples - %s", workspace),
			content:    buildFalsePositiveSamplesMarkdown(workspace, scope, falsePositiveResult),
			metadata: mergeLearnMetadata(baseMetadata, map[string]interface{}{
				"kind":              "false-positive-samples",
				"source_confidence": 0.90,
				"sample_type":       "false_positive",
				"labels":            []string{"auto-learn", "false-positive", "negative-sample", scope},
			}),
		},
		{
			sourcePath: fmt.Sprintf("kb://learned/%s/%s/ai-insights.md", scope, workspace),
			docType:    "learned-ai-insights",
			title:      fmt.Sprintf("AI Insights - %s", workspace),
			content:    buildAIInsightsMarkdown(workspace, scope, aiSections),
			metadata: mergeLearnMetadata(baseMetadata, map[string]interface{}{
				"kind":              "ai-insights",
				"source_confidence": 0.64,
				"sample_type":       "ai-analysis",
				"labels":            []string{"auto-learn", "ai-analysis", scope},
			}),
		},
	}
	if normalizeContent(operationalPlaybook) != "" {
		docs = append(docs, learnedDocument{
			sourcePath: fmt.Sprintf("kb://learned/%s/%s/operational-playbook.md", scope, workspace),
			docType:    "learned-operational-playbook",
			title:      fmt.Sprintf("Operational Playbook - %s", workspace),
			content:    operationalPlaybook,
			metadata: mergeLearnMetadata(baseMetadata, map[string]interface{}{
				"kind":              "operational-playbook",
				"source_confidence": 0.88,
				"sample_type":       "operator-followup",
				"labels":            []string{"auto-learn", "operator-followup", "manual-followup", "operator-queue", "retest-plan", "followup-decision", "campaign-handoff", scope},
			}),
		})
	}

	var (
		totalChunks int
		totalDocs   int
		sourcePath  string
		sourcePaths []string
	)
	for _, doc := range docs {
		content := normalizeContent(doc.content)
		if content == "" {
			continue
		}
		metadata := mergeLearnMetadata(doc.metadata, map[string]interface{}{})
		if fingerprint := buildLearnedRetrievalFingerprint(doc.docType, content); fingerprint != "" {
			metadata["retrieval_fingerprint"] = fingerprint
		}
		chunks := chunkContent(content)
		if len(chunks) == 0 {
			continue
		}
		if err := upsertKnowledgeContent(ctx, storageWorkspace, doc.sourcePath, doc.docType, doc.title, content, metadata); err != nil {
			return nil, err
		}
		totalDocs++
		totalChunks += len(chunks)
		if sourcePath == "" {
			sourcePath = doc.sourcePath
		}
		sourcePaths = append(sourcePaths, doc.sourcePath)
	}
	if totalDocs == 0 {
		return nil, fmt.Errorf("generated learned knowledge is empty")
	}

	return &LearnSummary{
		Workspace:        workspace,
		StorageWorkspace: storageWorkspace,
		Scope:            scope,
		Documents:        totalDocs,
		Chunks:           totalChunks,
		AssetsIncluded:   len(assetResult.Data),
		VulnsIncluded:    len(vulnResult.Data),
		RunsIncluded:     len(runResult.Data),
		AIFilesIncluded:  aiFiles,
		SourcePath:       sourcePath,
		SourcePaths:      sourcePaths,
	}, nil
}

func upsertKnowledgeContent(ctx context.Context, workspace, sourcePath, docType, title, content string, metadata map[string]interface{}) error {
	content = normalizeContent(content)
	chunks := chunkContent(content)
	if len(chunks) == 0 {
		return fmt.Errorf("no searchable chunks generated")
	}

	record := &database.KnowledgeDocument{
		Workspace:   normalizeWorkspace(workspace),
		SourcePath:  sourcePath,
		SourceType:  "generated",
		DocType:     docType,
		Title:       title,
		ContentHash: hashString(content),
		Status:      "ready",
		ChunkCount:  len(chunks),
		TotalBytes:  int64(len(content)),
		Metadata:    marshalMetadata(metadata),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	chunkRows := make([]database.KnowledgeChunk, 0, len(chunks))
	for i, chunk := range chunks {
		chunkMeta := map[string]interface{}{
			"source_path": sourcePath,
			"section":     chunk.Section,
			"chunk_index": i,
			"doc_type":    docType,
		}
		for key, value := range metadata {
			chunkMeta[key] = value
		}
		chunkRows = append(chunkRows, database.KnowledgeChunk{
			Workspace:   normalizeWorkspace(workspace),
			ChunkIndex:  i,
			Section:     chunk.Section,
			Content:     chunk.Content,
			ContentHash: hashString(chunk.Content),
			Metadata:    marshalMetadata(chunkMeta),
			CreatedAt:   time.Now(),
		})
	}

	return database.UpsertKnowledgeDocument(ctx, record, chunkRows)
}

func normalizeLearnScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", defaultLearnScope:
		return defaultLearnScope
	case "public", "project":
		return strings.ToLower(strings.TrimSpace(scope))
	default:
		return defaultLearnScope
	}
}

func resolveLearnedKnowledgeWorkspace(workspace, scope string) string {
	workspace = strings.TrimSpace(workspace)
	switch normalizeLearnScope(scope) {
	case "public":
		return "public"
	case "project":
		if workspace == "" {
			return "project"
		}
		return "project:" + workspace
	default:
		return workspace
	}
}

func clampLearnLimit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func resolveWorkspacePath(ctx context.Context, cfg *config.Config, workspace string) string {
	if ws, err := database.GetWorkspaceByName(ctx, workspace); err == nil {
		if strings.TrimSpace(ws.LocalPath) != "" {
			return ws.LocalPath
		}
	}
	if strings.TrimSpace(cfg.WorkspacesPath) == "" {
		return ""
	}
	return filepath.Join(cfg.WorkspacesPath, workspace)
}

func collectLearnedAISections(workspaceDir string, includeAIAnalysis bool) ([]string, []string) {
	if !includeAIAnalysis || strings.TrimSpace(workspaceDir) == "" {
		return nil, nil
	}

	aiDir := filepath.Join(workspaceDir, "ai-analysis")
	patterns := []string{
		"unified-analysis-*.md",
		"unified-analysis-*.json",
		"unified-analysis-knowledge-*.json",
		"vuln-validation-*.json",
		"validated-vulns-*.json",
		"attack-chain-*.json",
		"attack-chains.txt",
		"critical-paths.txt",
	}

	files := collectLearnedFiles(aiDir, patterns, maxLearnedFiles)

	var sections []string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if len(data) > maxLearnedFileBytes {
			data = data[:maxLearnedFileBytes]
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(file), ".json") {
			content = prettyLearnedJSON(data)
		}
		sections = append(sections, fmt.Sprintf("## %s\n\n```text\n%s\n```", filepath.Base(file), strings.TrimSpace(content)))
	}

	return sections, files
}

func buildOperationalPlaybookMarkdown(workspace, scope, workspaceDir string, includeAIAnalysis bool) (string, []string) {
	if !includeAIAnalysis || strings.TrimSpace(workspaceDir) == "" {
		return "", nil
	}

	aiDir := filepath.Join(workspaceDir, "ai-analysis")
	includedFiles := []string{}
	sections := []string{}

	if file := firstLearnedFile(aiDir, "applied-ai-decision-*.json"); file != "" {
		if section := renderAppliedDecisionSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "retest-plan-*.json"); file != "" {
		if section := renderRetestPlanSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "operator-queue-*.json"); file != "" {
		if section := renderOperatorQueueSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "campaign-handoff-*.json"); file != "" {
		if section := renderCampaignHandoffSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "campaign-create-*.json"); file != "" {
		if section := renderCampaignCreateSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "unified-analysis-knowledge-*.json"); file != "" {
		if section := renderKnowledgeLearningContextSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "followup-decision-*.json"); file != "" {
		if section := renderFollowupDecisionSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "retest-queue-summary-*.json"); file != "" {
		if section := renderRetestQueueSection(file); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}
	if file := firstLearnedFile(aiDir, "rescan-summary-*.md"); file != "" {
		if section := renderLearnedTextExcerptSection("Targeted Rescan Notes", file, 1800); section != "" {
			sections = append(sections, section)
			includedFiles = append(includedFiles, file)
		}
	}

	includedFiles = uniqueLearnedStrings(includedFiles)
	if len(sections) == 0 {
		return "", includedFiles
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("# Operational Follow-up Playbook: %s\n\n", workspace))
	builder.WriteString(fmt.Sprintf("- Scope: %s\n", scope))
	builder.WriteString(fmt.Sprintf("- Source artifacts: %d\n\n", len(includedFiles)))
	for _, section := range sections {
		builder.WriteString(section)
		builder.WriteString("\n\n")
	}
	builder.WriteString("## Operational Notes\n\n")
	builder.WriteString("Use this playbook as retrieval memory for retest sequencing, manual exploitation, campaign batching, and evidence-driven follow-up. Prefer current proof over stale AI guidance.\n")
	return builder.String(), includedFiles
}

func renderAppliedDecisionSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	profile := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "scan", "profile")), "focused")
	severity := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "scan", "severity")), "critical,high")
	reasoning := learnedString(lookupLearnedValue(obj, "reasoning"))
	focusAreas := learnedStringSlice(lookupLearnedValue(obj, "targets", "focus_areas"), maxOperationalItems)
	priorityTargets := learnedStringSlice(lookupLearnedValue(obj, "targets", "priority_targets"), maxOperationalItems)
	rescanTargets := learnedStringSlice(lookupLearnedValue(obj, "targets", "rescan_targets"), maxOperationalItems)

	var builder strings.Builder
	builder.WriteString("## Applied Decision Baseline\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Scan profile: %s\n", profile))
	builder.WriteString(fmt.Sprintf("- Severity: %s\n", severity))
	if reasoning != "" {
		builder.WriteString(fmt.Sprintf("- Reasoning: %s\n", squashLearnedText(reasoning, 280)))
	}
	appendLearnedListSection(&builder, "Focus Areas", focusAreas)
	appendLearnedListSection(&builder, "Priority Targets", priorityTargets)
	appendLearnedListSection(&builder, "Rescan Targets", rescanTargets)
	return strings.TrimSpace(builder.String())
}

func renderRetestPlanSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	recommendedFlow := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "summary", "recommended_flow")), "web-analysis")
	priority := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "summary", "priority")), "high")
	totalTargets := learnedInt(lookupLearnedValue(obj, "summary", "total_targets"))
	objective := learnedString(lookupLearnedValue(obj, "summary", "objective"))
	targets := learnedObjectSlice(lookupLearnedValue(obj, "targets"))

	var builder strings.Builder
	builder.WriteString("## Retest Plan\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Recommended flow: %s\n", recommendedFlow))
	builder.WriteString(fmt.Sprintf("- Priority: %s\n", priority))
	builder.WriteString(fmt.Sprintf("- Total targets: %d\n", totalTargets))
	if objective != "" {
		builder.WriteString(fmt.Sprintf("- Objective: %s\n", squashLearnedText(objective, 280)))
	}
	if len(targets) > 0 {
		builder.WriteString("\n### Recommended Retest Targets\n\n")
		for i, target := range targets {
			if i >= maxOperationalItems {
				break
			}
			targetValue := normalizeLearnedValue(learnedString(target["target"]), "n/a")
			targetPriority := normalizeLearnedValue(learnedString(target["priority"]), "P2")
			reason := squashLearnedText(learnedString(target["reason"]), 180)
			builder.WriteString(fmt.Sprintf("- [%s] %s\n", targetPriority, targetValue))
			if reason != "" {
				builder.WriteString(fmt.Sprintf("  reason: %s\n", reason))
			}
		}
	}
	return strings.TrimSpace(builder.String())
}

func renderOperatorQueueSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	totalTasks := learnedInt(lookupLearnedValue(obj, "summary", "total_tasks"))
	p1Tasks := learnedInt(lookupLearnedValue(obj, "summary", "p1_tasks"))
	p2Tasks := learnedInt(lookupLearnedValue(obj, "summary", "p2_tasks"))
	focusTargets := learnedStringSlice(lookupLearnedValue(obj, "focus_targets"), maxOperationalItems)
	tasks := learnedObjectSlice(lookupLearnedValue(obj, "tasks"))

	var builder strings.Builder
	builder.WriteString("## Operator Queue\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Total tasks: %d\n", totalTasks))
	builder.WriteString(fmt.Sprintf("- P1 tasks: %d\n", p1Tasks))
	builder.WriteString(fmt.Sprintf("- P2 tasks: %d\n", p2Tasks))
	appendLearnedListSection(&builder, "Focus Targets", focusTargets)
	if len(tasks) > 0 {
		builder.WriteString("\n### Manual Follow-up Tasks\n\n")
		for i, task := range tasks {
			if i >= maxOperationalItems {
				break
			}
			title := normalizeLearnedValue(learnedString(task["title"]), "Manual follow-up")
			target := normalizeLearnedValue(learnedString(task["target"]), "n/a")
			priority := normalizeLearnedValue(learnedString(task["priority"]), "P2")
			reason := squashLearnedText(learnedString(task["reason"]), 180)
			builder.WriteString(fmt.Sprintf("- [%s] %s -> %s\n", priority, title, target))
			if reason != "" {
				builder.WriteString(fmt.Sprintf("  reason: %s\n", reason))
			}
		}
	}
	return strings.TrimSpace(builder.String())
}

func renderCampaignHandoffSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	handoffReady := learnedBool(lookupLearnedValue(obj, "handoff_ready"))
	recommendedFlow := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "campaign_profile", "recommended_flow")), "web-analysis")
	retestPriority := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "campaign_profile", "retest_priority")), "high")
	focusAreas := learnedStringSlice(lookupLearnedValue(obj, "campaign_profile", "focus_areas"), maxOperationalItems)
	previousPriorityMode := learnedString(lookupLearnedValue(obj, "campaign_profile", "previous_priority_mode"))
	previousConfidenceLevel := learnedString(lookupLearnedValue(obj, "campaign_profile", "previous_confidence_level"))
	previousNextPhase := learnedString(lookupLearnedValue(obj, "campaign_profile", "previous_next_phase"))
	previousEscalationScore := learnedInt(lookupLearnedValue(obj, "campaign_profile", "previous_escalation_score"))
	previousReuseSources := learnedStringSlice(lookupLearnedValue(obj, "campaign_profile", "previous_reuse_sources"), maxOperationalItems)
	semanticTargets := learnedStringSlice(lookupLearnedValue(obj, "targets", "semantic_priority"), maxOperationalItems)
	nextActions := learnedStringSlice(lookupLearnedValue(obj, "next_actions"), maxOperationalItems)
	campaignTargets := learnedInt(lookupLearnedValue(obj, "counts", "campaign_targets"))
	operatorTasks := learnedInt(lookupLearnedValue(obj, "counts", "operator_tasks"))
	previousFollowupTargets := learnedInt(lookupLearnedValue(obj, "counts", "previous_followup_targets"))

	var builder strings.Builder
	builder.WriteString("## Campaign Handoff\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Handoff ready: %t\n", handoffReady))
	builder.WriteString(fmt.Sprintf("- Recommended flow: %s\n", recommendedFlow))
	builder.WriteString(fmt.Sprintf("- Retest priority: %s\n", retestPriority))
	builder.WriteString(fmt.Sprintf("- Campaign targets: %d\n", campaignTargets))
	builder.WriteString(fmt.Sprintf("- Operator tasks feeding campaign: %d\n", operatorTasks))
	builder.WriteString(fmt.Sprintf("- Previous follow-up targets: %d\n", previousFollowupTargets))
	if previousPriorityMode != "" {
		builder.WriteString(fmt.Sprintf("- Previous priority mode: %s\n", previousPriorityMode))
	}
	if previousConfidenceLevel != "" {
		builder.WriteString(fmt.Sprintf("- Previous confidence level: %s\n", previousConfidenceLevel))
	}
	if previousNextPhase != "" {
		builder.WriteString(fmt.Sprintf("- Previous next phase: %s\n", previousNextPhase))
	}
	if previousEscalationScore > 0 {
		builder.WriteString(fmt.Sprintf("- Previous escalation score: %d\n", previousEscalationScore))
	}
	appendLearnedListSection(&builder, "Campaign Focus Areas", focusAreas)
	appendLearnedListSection(&builder, "Semantic Priority Targets", semanticTargets)
	appendLearnedListSection(&builder, "Campaign Reuse Sources", previousReuseSources)
	appendLearnedListSection(&builder, "Campaign Next Actions", nextActions)
	return strings.TrimSpace(builder.String())
}

func renderKnowledgeLearningContextSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	priorityMode := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "followup_seed_focus", "priority_mode")), "not_generated")
	confidenceLevel := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "followup_seed_focus", "confidence_level")), "not_generated")
	nextPhase := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "followup_seed_focus", "next_phase")), "knowledge-consolidation")
	escalationScore := learnedInt(lookupLearnedValue(obj, "followup_seed_focus", "escalation_score"))
	manualFollowup := learnedBool(lookupLearnedValue(obj, "followup_seed_focus", "manual_followup_needed"))
	campaignRecommended := learnedBool(lookupLearnedValue(obj, "followup_seed_focus", "campaign_followup_recommended"))
	queueEffective := learnedBool(lookupLearnedValue(obj, "followup_seed_focus", "queue_followup_effective"))
	reuseSources := learnedStringSlice(lookupLearnedValue(obj, "followup_seed_focus", "reuse_sources"), maxOperationalItems)
	nextActions := learnedStringSlice(lookupLearnedValue(obj, "followup_seed_focus", "next_actions"), maxOperationalItems)
	retestTargets := learnedInt(lookupLearnedValue(obj, "operational_counts", "retest_targets"))
	operatorTasks := learnedInt(lookupLearnedValue(obj, "operational_counts", "operator_tasks"))
	campaignTargets := learnedInt(lookupLearnedValue(obj, "operational_counts", "campaign_targets"))
	retestQueued := learnedInt(lookupLearnedValue(obj, "operational_counts", "retest_queued_targets"))
	campaignStatus := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "campaign_creation", "status")), "not_requested")
	campaignQueuedRuns := learnedInt(lookupLearnedValue(obj, "campaign_creation", "queued_runs"))
	campaignReady := learnedBool(lookupLearnedValue(obj, "artifact_presence", "campaign_ready"))

	var builder strings.Builder
	builder.WriteString("## Knowledge Learning Context\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Priority mode: %s\n", priorityMode))
	builder.WriteString(fmt.Sprintf("- Confidence level: %s\n", confidenceLevel))
	builder.WriteString(fmt.Sprintf("- Next phase: %s\n", nextPhase))
	builder.WriteString(fmt.Sprintf("- Escalation score: %d\n", escalationScore))
	builder.WriteString(fmt.Sprintf("- Manual follow-up needed: %t\n", manualFollowup))
	builder.WriteString(fmt.Sprintf("- Campaign follow-up recommended: %t\n", campaignRecommended))
	builder.WriteString(fmt.Sprintf("- Queue follow-up effective: %t\n", queueEffective))
	builder.WriteString(fmt.Sprintf("- Retest targets: %d\n", retestTargets))
	builder.WriteString(fmt.Sprintf("- Operator tasks: %d\n", operatorTasks))
	builder.WriteString(fmt.Sprintf("- Campaign targets: %d\n", campaignTargets))
	builder.WriteString(fmt.Sprintf("- Retest queued targets: %d\n", retestQueued))
	builder.WriteString(fmt.Sprintf("- Campaign creation status: %s\n", campaignStatus))
	builder.WriteString(fmt.Sprintf("- Campaign queued runs: %d\n", campaignQueuedRuns))
	builder.WriteString(fmt.Sprintf("- Campaign ready artifact present: %t\n", campaignReady))
	appendLearnedListSection(&builder, "Context Reuse Sources", reuseSources)
	appendLearnedListSection(&builder, "Context Next Actions", nextActions)
	return strings.TrimSpace(builder.String())
}

func renderFollowupDecisionSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	nextPhase := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "execution_feedback", "next_phase")), "knowledge-consolidation")
	manualFollowup := learnedBool(lookupLearnedValue(obj, "execution_feedback", "manual_followup_needed"))
	campaignFollowup := learnedBool(lookupLearnedValue(obj, "execution_feedback", "campaign_followup_recommended"))
	queueEffective := learnedBool(lookupLearnedValue(obj, "execution_feedback", "queue_followup_effective"))
	rescanCritical := learnedInt(lookupLearnedValue(obj, "followup_summary", "rescan_critical"))
	rescanHigh := learnedInt(lookupLearnedValue(obj, "followup_summary", "rescan_high"))
	priorityMode := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "seed_focus", "priority_mode")), "knowledge-first")
	confidenceLevel := normalizeLearnedValue(learnedString(lookupLearnedValue(obj, "seed_focus", "confidence_level")), "low")
	escalationScore := learnedInt(lookupLearnedValue(obj, "seed_focus", "signal_scores", "escalation_score"))
	reuseSources := learnedStringSlice(lookupLearnedValue(obj, "seed_focus", "reuse_sources"), maxOperationalItems)
	priorityTargets := learnedStringSlice(lookupLearnedValue(obj, "refined_targets", "priority_targets"), maxOperationalItems)
	focusAreas := learnedStringSlice(lookupLearnedValue(obj, "refined_targets", "focus_areas"), maxOperationalItems)
	manualFirstTargets := learnedStringSlice(lookupLearnedValue(obj, "seed_targets", "manual_first_targets"), maxOperationalItems)
	highConfidenceTargets := learnedStringSlice(lookupLearnedValue(obj, "seed_targets", "high_confidence_targets"), maxOperationalItems)
	nextActions := learnedStringSlice(lookupLearnedValue(obj, "next_actions"), maxOperationalItems)

	var builder strings.Builder
	builder.WriteString("## Follow-up Closure\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Next phase: %s\n", nextPhase))
	builder.WriteString(fmt.Sprintf("- Priority mode: %s\n", priorityMode))
	builder.WriteString(fmt.Sprintf("- Confidence level: %s\n", confidenceLevel))
	builder.WriteString(fmt.Sprintf("- Escalation score: %d\n", escalationScore))
	builder.WriteString(fmt.Sprintf("- Manual follow-up needed: %t\n", manualFollowup))
	builder.WriteString(fmt.Sprintf("- Campaign follow-up recommended: %t\n", campaignFollowup))
	builder.WriteString(fmt.Sprintf("- Queue follow-up effective: %t\n", queueEffective))
	builder.WriteString(fmt.Sprintf("- Rescan severity recap: critical=%d high=%d\n", rescanCritical, rescanHigh))
	appendLearnedListSection(&builder, "Refined Focus Areas", focusAreas)
	appendLearnedListSection(&builder, "Refined Priority Targets", priorityTargets)
	appendLearnedListSection(&builder, "Manual-First Targets", manualFirstTargets)
	appendLearnedListSection(&builder, "High-Confidence Targets", highConfidenceTargets)
	appendLearnedListSection(&builder, "Reuse Sources", reuseSources)
	appendLearnedListSection(&builder, "Follow-up Next Actions", nextActions)
	return strings.TrimSpace(builder.String())
}

func renderCampaignCreateSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	status := normalizeLearnedValue(learnedString(obj["status"]), "unknown")
	campaignID := learnedString(obj["campaign_id"])
	queuedRuns := learnedInt(obj["queued_runs"])
	workflow := normalizeLearnedValue(learnedString(obj["workflow"]), "web-analysis")
	workflowKind := normalizeLearnedValue(learnedString(obj["workflow_kind"]), "flow")
	priority := normalizeLearnedValue(learnedString(obj["priority"]), "high")
	targetCount := learnedInt(obj["target_count"])
	priorityMode := learnedString(obj["campaign_priority_mode"])
	confidenceLevel := learnedString(obj["campaign_confidence_level"])
	nextPhase := learnedString(obj["campaign_followup_next_phase"])
	reuseSources := learnedString(obj["campaign_reuse_sources"])
	previousFollowupTargets := learnedInt(obj["previous_followup_targets"])
	escalationScore := learnedInt(obj["campaign_escalation_score"])
	deepScanWorkflow := learnedString(obj["deep_scan_workflow"])
	deepScanWorkflowKind := learnedString(obj["deep_scan_workflow_kind"])
	autoDeepScan := learnedBool(obj["auto_deep_scan"])
	highRiskSeverities := learnedStringSlice(obj["high_risk_severities"], maxOperationalItems)

	var builder strings.Builder
	builder.WriteString("## Campaign Creation Result\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Status: %s\n", status))
	if campaignID != "" {
		builder.WriteString(fmt.Sprintf("- Campaign ID: %s\n", campaignID))
	}
	if queuedRuns > 0 {
		builder.WriteString(fmt.Sprintf("- Queued runs: %d\n", queuedRuns))
	}
	builder.WriteString(fmt.Sprintf("- Workflow: %s (%s)\n", workflow, workflowKind))
	builder.WriteString(fmt.Sprintf("- Priority: %s\n", priority))
	builder.WriteString(fmt.Sprintf("- Target count: %d\n", targetCount))
	builder.WriteString(fmt.Sprintf("- Previous follow-up targets: %d\n", previousFollowupTargets))
	if priorityMode != "" {
		builder.WriteString(fmt.Sprintf("- Campaign priority mode: %s\n", priorityMode))
	}
	if confidenceLevel != "" {
		builder.WriteString(fmt.Sprintf("- Campaign confidence level: %s\n", confidenceLevel))
	}
	if nextPhase != "" {
		builder.WriteString(fmt.Sprintf("- Campaign follow-up next phase: %s\n", nextPhase))
	}
	if escalationScore > 0 {
		builder.WriteString(fmt.Sprintf("- Campaign escalation score: %d\n", escalationScore))
	}
	if reuseSources != "" {
		builder.WriteString(fmt.Sprintf("- Campaign reuse sources: %s\n", reuseSources))
	}
	if deepScanWorkflow != "" {
		builder.WriteString(fmt.Sprintf("- Deep-scan workflow: %s (%s)\n", deepScanWorkflow, normalizeLearnedValue(deepScanWorkflowKind, "flow")))
	}
	builder.WriteString(fmt.Sprintf("- Auto deep-scan: %t\n", autoDeepScan))
	appendLearnedListSection(&builder, "High-Risk Severities", highRiskSeverities)
	return strings.TrimSpace(builder.String())
}

func renderRetestQueueSection(path string) string {
	obj := readLearnedJSONObject(path)
	if obj == nil {
		return ""
	}

	status := normalizeLearnedValue(learnedString(obj["status"]), "unknown")
	reason := learnedString(obj["reason"])
	workflow := normalizeLearnedValue(learnedString(obj["workflow"]), "web-analysis")
	workflowKind := normalizeLearnedValue(learnedString(obj["workflow_kind"]), "flow")
	priority := normalizeLearnedValue(learnedString(obj["priority"]), "high")
	targetSource := learnedString(obj["target_source"])
	targetCount := learnedInt(obj["queued_targets"])
	previousFollowupTargets := learnedInt(obj["previous_followup_targets"])
	previousPriorityMode := learnedString(obj["previous_priority_mode"])
	previousConfidenceLevel := learnedString(obj["previous_confidence_level"])

	var builder strings.Builder
	builder.WriteString("## Retest Queue Status\n\n")
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n", filepath.Base(path)))
	builder.WriteString(fmt.Sprintf("- Status: %s\n", status))
	if reason != "" {
		builder.WriteString(fmt.Sprintf("- Reason: %s\n", reason))
	}
	builder.WriteString(fmt.Sprintf("- Workflow: %s (%s)\n", workflow, workflowKind))
	builder.WriteString(fmt.Sprintf("- Priority: %s\n", priority))
	builder.WriteString(fmt.Sprintf("- Queued targets: %d\n", targetCount))
	builder.WriteString(fmt.Sprintf("- Previous follow-up targets: %d\n", previousFollowupTargets))
	if targetSource != "" {
		builder.WriteString(fmt.Sprintf("- Target source: %s\n", targetSource))
	}
	if previousPriorityMode != "" {
		builder.WriteString(fmt.Sprintf("- Previous priority mode: %s\n", previousPriorityMode))
	}
	if previousConfidenceLevel != "" {
		builder.WriteString(fmt.Sprintf("- Previous confidence level: %s\n", previousConfidenceLevel))
	}
	return strings.TrimSpace(builder.String())
}

func renderLearnedTextExcerptSection(title, path string, maxBytes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	content = truncateLearnedBlock(content, maxBytes)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## %s\n\n", title))
	builder.WriteString(fmt.Sprintf("- Artifact: %s\n\n", filepath.Base(path)))
	builder.WriteString("```text\n")
	builder.WriteString(content)
	builder.WriteString("\n```")
	return strings.TrimSpace(builder.String())
}

func collectLearnedFiles(baseDir string, patterns []string, limit int) []string {
	if strings.TrimSpace(baseDir) == "" {
		return nil
	}
	type learnedFileInfo struct {
		path    string
		modTime time.Time
	}

	seen := make(map[string]struct{})
	var candidates []learnedFileInfo
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(baseDir, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			info, err := os.Stat(match)
			if err != nil {
				candidates = append(candidates, learnedFileInfo{path: match})
				continue
			}
			candidates = append(candidates, learnedFileInfo{
				path:    match,
				modTime: info.ModTime(),
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].modTime.After(candidates[j].modTime)
		}
		return candidates[i].path > candidates[j].path
	})

	files := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		files = append(files, candidate.path)
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	return files
}

func firstLearnedFile(baseDir, pattern string) string {
	files := collectLearnedFiles(baseDir, []string{pattern}, 1)
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

func uniqueLearnedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func readLearnedJSONObject(path string) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}
	return obj
}

func lookupLearnedValue(obj map[string]interface{}, path ...string) interface{} {
	var current interface{} = obj
	for _, key := range path {
		asMap, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	return current
}

func learnedString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func learnedInt(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func learnedBool(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "y":
			return true
		}
	}
	return false
}

func learnedStringSlice(value interface{}, limit int) []string {
	rawValues, ok := value.([]interface{})
	if !ok {
		return nil
	}
	items := make([]string, 0, len(rawValues))
	for _, rawValue := range rawValues {
		text := learnedString(rawValue)
		if text == "" {
			continue
		}
		items = append(items, text)
	}
	items = uniqueLearnedStrings(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func learnedObjectSlice(value interface{}) []map[string]interface{} {
	rawValues, ok := value.([]interface{})
	if !ok {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(rawValues))
	for _, rawValue := range rawValues {
		asMap, ok := rawValue.(map[string]interface{})
		if !ok {
			continue
		}
		items = append(items, asMap)
	}
	return items
}

func appendLearnedListSection(builder *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	builder.WriteString(fmt.Sprintf("\n### %s\n\n", title))
	for _, item := range items {
		builder.WriteString(fmt.Sprintf("- %s\n", squashLearnedText(item, 200)))
	}
}

func truncateLearnedBlock(input string, limit int) string {
	input = strings.TrimSpace(input)
	if len(input) <= limit || limit <= 0 {
		return input
	}
	if limit <= 3 {
		return input[:limit]
	}
	return strings.TrimSpace(input[:limit-3]) + "..."
}

func prettyLearnedJSON(data []byte) string {
	var obj interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return string(data)
	}
	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return string(data)
	}
	return string(pretty)
}

func buildLearnedKnowledgeMarkdown(
	workspace, scope string,
	assetResult *database.AssetResult,
	vulnResult *database.VulnerabilityResult,
	runResult *database.RunResult,
	vulnSummary map[string]int,
	assetStats *database.AssetStatsData,
	aiSections []string,
) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("# Learned Security Notes: %s\n\n", workspace))
	builder.WriteString(fmt.Sprintf("- Scope: %s\n", scope))
	builder.WriteString(fmt.Sprintf("- Assets sampled: %d\n", len(assetResult.Data)))
	builder.WriteString(fmt.Sprintf("- Vulnerabilities sampled: %d\n", len(vulnResult.Data)))
	builder.WriteString(fmt.Sprintf("- Runs sampled: %d\n\n", len(runResult.Data)))

	builder.WriteString("## Vulnerability Summary\n\n")
	if len(vulnSummary) == 0 {
		builder.WriteString("No vulnerability summary available.\n\n")
	} else {
		keys := make([]string, 0, len(vulnSummary))
		for severity := range vulnSummary {
			keys = append(keys, severity)
		}
		sort.Strings(keys)
		for _, severity := range keys {
			builder.WriteString(fmt.Sprintf("- %s: %d\n", severity, vulnSummary[severity]))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Technology and Asset Profile\n\n")
	if assetStats != nil {
		builder.WriteString(fmt.Sprintf("- Asset types: %s\n", joinLearnedValues(assetStats.AssetTypes, 12)))
		builder.WriteString(fmt.Sprintf("- Technologies: %s\n", joinLearnedValues(assetStats.Technologies, 20)))
		builder.WriteString(fmt.Sprintf("- Sources: %s\n", joinLearnedValues(assetStats.Sources, 10)))
		builder.WriteString(fmt.Sprintf("- Remarks: %s\n\n", joinLearnedValues(assetStats.Remarks, 12)))
	}

	builder.WriteString("## Recent Vulnerability Samples\n\n")
	if len(vulnResult.Data) == 0 {
		builder.WriteString("No vulnerabilities recorded.\n\n")
	} else {
		for _, vuln := range vulnResult.Data {
			builder.WriteString(fmt.Sprintf("### [%s] %s\n", normalizeLearnedValue(vuln.Severity, "unknown"), normalizeLearnedValue(vuln.VulnTitle, normalizeLearnedValue(vuln.VulnInfo, "Untitled vulnerability"))))
			builder.WriteString(fmt.Sprintf("- Asset: %s (%s)\n", normalizeLearnedValue(vuln.AssetValue, "n/a"), normalizeLearnedValue(vuln.AssetType, "unknown")))
			builder.WriteString(fmt.Sprintf("- Lifecycle: %s\n", normalizeLearnedValue(vuln.VulnStatus, "new")))
			builder.WriteString(fmt.Sprintf("- Confidence: %s\n", normalizeLearnedValue(vuln.Confidence, "unknown")))
			if vuln.EvidenceVersion > 0 {
				builder.WriteString(fmt.Sprintf("- Evidence version: %d\n", vuln.EvidenceVersion))
			}
			if strings.TrimSpace(vuln.VulnDesc) != "" {
				builder.WriteString(fmt.Sprintf("- Description: %s\n", squashLearnedText(vuln.VulnDesc, 320)))
			}
			if strings.TrimSpace(vuln.AISummary) != "" {
				builder.WriteString(fmt.Sprintf("- AI summary: %s\n", squashLearnedText(vuln.AISummary, 220)))
			}
			if len(vuln.Tags) > 0 {
				builder.WriteString(fmt.Sprintf("- Tags: %s\n", strings.Join(vuln.Tags, ", ")))
			}
			builder.WriteString("\n")
		}
	}

	builder.WriteString("## Recent Asset Samples\n\n")
	if len(assetResult.Data) == 0 {
		builder.WriteString("No assets recorded.\n\n")
	} else {
		for _, asset := range assetResult.Data {
			builder.WriteString(fmt.Sprintf("### %s\n", normalizeLearnedValue(asset.AssetValue, normalizeLearnedValue(asset.URL, "unknown asset"))))
			builder.WriteString(fmt.Sprintf("- Type: %s\n", normalizeLearnedValue(asset.AssetType, "unknown")))
			builder.WriteString(fmt.Sprintf("- URL: %s\n", normalizeLearnedValue(asset.URL, "n/a")))
			builder.WriteString(fmt.Sprintf("- Title: %s\n", normalizeLearnedValue(asset.Title, "n/a")))
			builder.WriteString(fmt.Sprintf("- Status: %d\n", asset.StatusCode))
			if len(asset.Technologies) > 0 {
				builder.WriteString(fmt.Sprintf("- Technologies: %s\n", strings.Join(asset.Technologies, ", ")))
			}
			if asset.IsWAF || asset.IsCDN || asset.IsCloud {
				builder.WriteString(fmt.Sprintf("- Edge: waf=%t cdn=%t cloud=%t\n", asset.IsWAF, asset.IsCDN, asset.IsCloud))
			}
			builder.WriteString("\n")
		}
	}

	builder.WriteString("## Recent Runs\n\n")
	if len(runResult.Data) == 0 {
		builder.WriteString("No runs recorded.\n\n")
	} else {
		for _, run := range runResult.Data {
			builder.WriteString(fmt.Sprintf("- [%s] workflow=%s target=%s status=%s created=%s\n",
				normalizeLearnedValue(run.RunUUID, "n/a"),
				normalizeLearnedValue(run.WorkflowName, "unknown"),
				normalizeLearnedValue(run.Target, "unknown"),
				normalizeLearnedValue(run.Status, "unknown"),
				run.CreatedAt.Format(time.RFC3339),
			))
		}
		builder.WriteString("\n")
	}

	if len(aiSections) > 0 {
		builder.WriteString("## AI Analysis Memory\n\n")
		for _, section := range aiSections {
			builder.WriteString(section)
			builder.WriteString("\n\n")
		}
	}

	builder.WriteString("## Operator Notes\n\n")
	builder.WriteString("This document is auto-generated from existing scan records and AI analysis artifacts. Treat it as retrievable context, not as source-of-truth evidence.\n")

	return builder.String()
}

func buildVerifiedFindingsMarkdown(workspace, scope string, vulnResult *database.VulnerabilityResult) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("# Verified Findings Memory: %s\n\n", workspace))
	builder.WriteString(fmt.Sprintf("- Scope: %s\n", scope))
	builder.WriteString("\n")

	count := 0
	for _, vuln := range vulnResult.Data {
		if strings.ToLower(strings.TrimSpace(vuln.VulnStatus)) != "verified" {
			continue
		}
		count++
		builder.WriteString(fmt.Sprintf("## %s\n\n", normalizeLearnedValue(vuln.VulnTitle, normalizeLearnedValue(vuln.VulnInfo, "Untitled vulnerability"))))
		builder.WriteString(fmt.Sprintf("- Severity: %s\n", normalizeLearnedValue(vuln.Severity, "unknown")))
		builder.WriteString(fmt.Sprintf("- Asset: %s (%s)\n", normalizeLearnedValue(vuln.AssetValue, "n/a"), normalizeLearnedValue(vuln.AssetType, "unknown")))
		builder.WriteString(fmt.Sprintf("- Confidence: %s\n", normalizeLearnedValue(vuln.Confidence, "unknown")))
		builder.WriteString(fmt.Sprintf("- Evidence version: %d\n", vuln.EvidenceVersion))
		if strings.TrimSpace(vuln.VulnDesc) != "" {
			builder.WriteString(fmt.Sprintf("- Description: %s\n", squashLearnedText(vuln.VulnDesc, 320)))
		}
		if strings.TrimSpace(vuln.AISummary) != "" {
			builder.WriteString(fmt.Sprintf("- AI summary: %s\n", squashLearnedText(vuln.AISummary, 320)))
		}
		if len(vuln.Tags) > 0 {
			builder.WriteString(fmt.Sprintf("- Tags: %s\n", strings.Join(vuln.Tags, ", ")))
		}
		builder.WriteString("\n")
	}
	if count == 0 {
		return ""
	}
	return builder.String()
}

func buildFalsePositiveSamplesMarkdown(workspace, scope string, vulnResult *database.VulnerabilityResult) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("# False Positive Samples: %s\n\n", workspace))
	builder.WriteString(fmt.Sprintf("- Scope: %s\n", scope))
	builder.WriteString("\n")

	count := 0
	for _, vuln := range vulnResult.Data {
		if strings.ToLower(strings.TrimSpace(vuln.VulnStatus)) != "false_positive" {
			continue
		}
		count++
		builder.WriteString(fmt.Sprintf("## %s\n\n", normalizeLearnedValue(vuln.VulnTitle, normalizeLearnedValue(vuln.VulnInfo, "Untitled vulnerability"))))
		builder.WriteString(fmt.Sprintf("- Asset: %s (%s)\n", normalizeLearnedValue(vuln.AssetValue, "n/a"), normalizeLearnedValue(vuln.AssetType, "unknown")))
		builder.WriteString(fmt.Sprintf("- Confidence: %s\n", normalizeLearnedValue(vuln.Confidence, "unknown")))
		if strings.TrimSpace(vuln.AnalystNotes) != "" {
			builder.WriteString(fmt.Sprintf("- Analyst notes: %s\n", squashLearnedText(vuln.AnalystNotes, 320)))
		}
		if strings.TrimSpace(vuln.AISummary) != "" {
			builder.WriteString(fmt.Sprintf("- AI summary: %s\n", squashLearnedText(vuln.AISummary, 320)))
		}
		if strings.TrimSpace(vuln.RawVulnJSON) != "" {
			builder.WriteString(fmt.Sprintf("- Raw sample hash hint: %s\n", hashString(vuln.RawVulnJSON)[:16]))
		}
		builder.WriteString("\n")
	}
	if count == 0 {
		return ""
	}
	return builder.String()
}

func buildAIInsightsMarkdown(workspace, scope string, aiSections []string) string {
	if len(aiSections) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("# AI Insights Memory: %s\n\n", workspace))
	builder.WriteString(fmt.Sprintf("- Scope: %s\n", scope))
	builder.WriteString("\n")
	for _, section := range aiSections {
		builder.WriteString(section)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

func buildLearnedRetrievalFingerprint(docType, content string) string {
	normalized := normalizeLearnedFingerprintContent(content)
	if normalized == "" {
		return ""
	}
	return hashString(strings.TrimSpace(docType) + "::" + normalized)
}

func normalizeLearnedFingerprintContent(content string) string {
	content = normalizeContent(content)
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "- generated:") {
			continue
		}
		if strings.HasPrefix(lower, "- scope:") {
			continue
		}
		clean = append(clean, line)
	}
	return strings.Join(clean, "\n")
}

func mergeLearnMetadata(base map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func collectLearnedTargetTypes(assetResult *database.AssetResult) []string {
	if assetResult == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var result []string
	for _, asset := range assetResult.Data {
		assetType := strings.TrimSpace(asset.AssetType)
		if assetType == "" {
			continue
		}
		if _, ok := seen[assetType]; ok {
			continue
		}
		seen[assetType] = struct{}{}
		result = append(result, assetType)
	}
	sort.Strings(result)
	return result
}

func normalizeLearnedValue(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func joinLearnedValues(values []string, limit int) string {
	if len(values) == 0 {
		return "n/a"
	}
	if len(values) > limit {
		values = values[:limit]
	}
	return strings.Join(values, ", ")
}

func squashLearnedText(input string, limit int) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(input)), " ")
	if len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}
