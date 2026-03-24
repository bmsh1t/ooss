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
	Workspace       string   `json:"workspace"`
	Scope           string   `json:"scope"`
	Documents       int      `json:"documents"`
	Chunks          int      `json:"chunks"`
	AssetsIncluded  int      `json:"assets_included"`
	VulnsIncluded   int      `json:"vulnerabilities_included"`
	RunsIncluded    int      `json:"runs_included"`
	AIFilesIncluded []string `json:"ai_files_included,omitempty"`
	SourcePath      string   `json:"source_path"`
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

	content := buildLearnedKnowledgeMarkdown(workspace, scope, assetResult, vulnResult, runResult, vulnSummary, assetStats, aiSections)
	content = normalizeContent(content)
	chunks := chunkContent(content)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("generated learned knowledge is empty")
	}

	sourcePath := fmt.Sprintf("kb://learned/%s/%s/workspace-summary.md", scope, workspace)
	metadata := map[string]interface{}{
		"scope":                    scope,
		"source":                   "auto-learn",
		"workspace":                workspace,
		"kind":                     "workspace-summary",
		"assets_included":          len(assetResult.Data),
		"vulnerabilities_included": len(vulnResult.Data),
		"runs_included":            len(runResult.Data),
		"ai_files":                 aiFiles,
		"generated_at":             time.Now().Format(time.RFC3339),
	}

	if err := upsertKnowledgeContent(ctx, workspace, sourcePath, "learned-summary", fmt.Sprintf("Learned Security Notes - %s", workspace), content, metadata); err != nil {
		return nil, err
	}

	return &LearnSummary{
		Workspace:       workspace,
		Scope:           scope,
		Documents:       1,
		Chunks:          len(chunks),
		AssetsIncluded:  len(assetResult.Data),
		VulnsIncluded:   len(vulnResult.Data),
		RunsIncluded:    len(runResult.Data),
		AIFilesIncluded: aiFiles,
		SourcePath:      sourcePath,
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
		"vuln-validation-*.json",
		"validated-vulns-*.json",
		"attack-chain-*.json",
		"attack-chains.txt",
		"critical-paths.txt",
	}

	seen := make(map[string]struct{})
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(aiDir, pattern))
		if err != nil {
			continue
		}
		sort.Strings(matches)
		for _, match := range matches {
			if _, ok := seen[match]; ok {
				continue
			}
			seen[match] = struct{}{}
			files = append(files, match)
			if len(files) >= maxLearnedFiles {
				break
			}
		}
		if len(files) >= maxLearnedFiles {
			break
		}
	}

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
	builder.WriteString(fmt.Sprintf("- Generated: %s\n", time.Now().Format(time.RFC3339)))
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
			builder.WriteString(fmt.Sprintf("- Confidence: %s\n", normalizeLearnedValue(vuln.Confidence, "unknown")))
			if strings.TrimSpace(vuln.VulnDesc) != "" {
				builder.WriteString(fmt.Sprintf("- Description: %s\n", squashLearnedText(vuln.VulnDesc, 320)))
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
