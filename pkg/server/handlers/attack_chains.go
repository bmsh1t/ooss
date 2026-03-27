package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/j3ssie/osmedeus/v5/internal/attackchain"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/uptrace/bun"
)

type ImportAttackChainRequest struct {
	Workspace   string `json:"workspace"`
	SourcePath  string `json:"source_path"`
	Target      string `json:"target,omitempty"`
	RunUUID     string `json:"run_uuid,omitempty"`
	MermaidPath string `json:"mermaid_path,omitempty"`
	TextPath    string `json:"text_path,omitempty"`
}

type attackChainEntryPoint struct {
	Vulnerability string   `json:"vulnerability"`
	URL           string   `json:"url"`
	Severity      string   `json:"severity"`
	Prerequisites []string `json:"prerequisites,omitempty"`
}

type attackChainStep struct {
	Step          int    `json:"step"`
	Action        string `json:"action"`
	Vulnerability string `json:"vulnerability,omitempty"`
	Result        string `json:"result,omitempty"`
	Command       string `json:"command,omitempty"`
}

type attackChainItem struct {
	ChainID            string                `json:"chain_id"`
	ChainName          string                `json:"chain_name"`
	EntryPoint         attackChainEntryPoint `json:"entry_point"`
	ChainSteps         []attackChainStep     `json:"chain_steps"`
	FinalObjective     string                `json:"final_objective"`
	Difficulty         string                `json:"difficulty"`
	Impact             string                `json:"impact"`
	EstimatedTime      string                `json:"estimated_time,omitempty"`
	SuccessProbability float64               `json:"success_probability"`
	Mitigation         string                `json:"mitigation,omitempty"`
}

type attackChainPath struct {
	Path      []string `json:"path"`
	TotalRisk string   `json:"total_risk"`
	Weakness  string   `json:"weakness,omitempty"`
}

type linkedVulnerability struct {
	ID         int64  `json:"id"`
	VulnTitle  string `json:"vuln_title"`
	Severity   string `json:"severity"`
	VulnStatus string `json:"vuln_status"`
	AssetValue string `json:"asset_value"`
}

type linkedAsset struct {
	ID         int64  `json:"id"`
	AssetValue string `json:"asset_value"`
	URL        string `json:"url,omitempty"`
	AssetType  string `json:"asset_type,omitempty"`
	Source     string `json:"source,omitempty"`
}

type attackChainWorkbenchItem struct {
	attackChainItem
	LinkedVulnerabilities    []linkedVulnerability `json:"linked_vulnerabilities,omitempty"`
	LinkedAssets             []linkedAsset         `json:"linked_assets,omitempty"`
	LinkedVulnerabilityCount int                   `json:"linked_vulnerability_count"`
	VerifiedLinkedCount      int                   `json:"verified_linked_count"`
	OperationalLinkedCount   int                   `json:"operational_linked_count"`
	VerificationRate         float64               `json:"verification_rate"`
	ExecutionReady           bool                  `json:"execution_ready"`
	QueueRecommendation      string                `json:"queue_recommendation,omitempty"`
}

type attackChainWorkbenchSummary struct {
	TotalChains                int      `json:"total_chains"`
	VerifiedChains             int      `json:"verified_chains"`
	ExecutionReadyChains       int      `json:"execution_ready_chains"`
	RecommendedRetestChainIDs  []string `json:"recommended_retest_chain_ids,omitempty"`
	RecommendedDeepScanTargets []string `json:"recommended_deep_scan_targets,omitempty"`
}

type QueueAttackChainRequest struct {
	Flow         string            `json:"flow,omitempty"`
	Module       string            `json:"module,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
	Priority     string            `json:"priority,omitempty"`
	ChainIDs     []string          `json:"chain_ids,omitempty"`
	VerifiedOnly bool              `json:"verified_only,omitempty"`
}

// ListAttackChains returns normalized attack-chain reports.
func ListAttackChains(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		limit, _ := strconv.Atoi(c.Query("limit", "20"))
		if offset < 0 {
			offset = 0
		}
		if limit <= 0 {
			limit = 20
		}
		if limit > 1000 {
			limit = 1000
		}

		result, err := database.ListAttackChainReports(context.Background(), database.AttackChainQuery{
			Workspace: c.Query("workspace"),
			Target:    c.Query("target"),
			RunUUID:   c.Query("run_uuid"),
			Offset:    offset,
			Limit:     limit,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"data": result.Data,
			"pagination": fiber.Map{
				"total":  result.TotalCount,
				"offset": result.Offset,
				"limit":  result.Limit,
			},
		})
	}
}

// GetAttackChain returns a detailed workbench view for a single attack-chain report.
func GetAttackChain(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid attack chain ID",
			})
		}

		report, err := database.GetAttackChainReportByID(context.Background(), id)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Attack chain report not found",
			})
		}

		chains := decodeAttackChains(report.AttackChainsJSON)
		if c.QueryBool("verifiable_only", false) {
			filtered := make([]attackChainItem, 0, len(chains))
			for _, chain := range chains {
				if isVerifiableAttackChain(chain) {
					filtered = append(filtered, chain)
				}
			}
			chains = filtered
		}

		enrichedChains := enrichAttackChains(context.Background(), report.Workspace, chains)
		if c.QueryBool("verified_only", false) {
			filtered := make([]attackChainWorkbenchItem, 0, len(enrichedChains))
			for _, chain := range enrichedChains {
				if chain.VerifiedLinkedCount > 0 {
					filtered = append(filtered, chain)
				}
			}
			enrichedChains = filtered
		}

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"report":              report,
				"summary":             buildAttackChainWorkbenchSummary(report, enrichedChains),
				"chains":              enrichedChains,
				"critical_paths":      decodeAttackPaths(report.CriticalPathsJSON),
				"execution_checklist": buildExecutionChecklist(extractAttackChainItems(enrichedChains)),
				"source_files": fiber.Map{
					"json":    report.SourcePath,
					"mermaid": report.MermaidPath,
					"text":    report.TextPath,
				},
			},
		})
	}
}

// QueueAttackChainRetest turns selected linked vulnerabilities into queued retest runs.
func QueueAttackChainRetest(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		report, req, workflowName, workflowKind, err := parseAttackChainQueueRequest(c)
		if err != nil {
			return err
		}

		ctx := context.Background()
		chains := selectAttackChainQueueItems(ctx, report, req, false)
		if len(chains) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "No eligible attack chains selected",
			})
		}

		runGroupID := fmt.Sprintf("attack-chain-report-%d", report.ID)
		seen := make(map[int64]struct{})
		var (
			queued      int
			skipped     int
			runUUIDs    []string
			verifiedHit int
		)

		for _, chain := range chains {
			for _, linked := range chain.LinkedVulnerabilities {
				if _, ok := seen[linked.ID]; ok {
					continue
				}
				seen[linked.ID] = struct{}{}
				if req.VerifiedOnly && !isAttackChainOperationallyVerifiedStatus(linked.VulnStatus, false) {
					continue
				}
				if linked.VulnStatus == "false_positive" || linked.VulnStatus == "closed" {
					skipped++
					continue
				}
				vuln, err := database.GetVulnerabilityByID(ctx, linked.ID)
				if err != nil {
					skipped++
					continue
				}
				target := strings.TrimSpace(vuln.AssetValue)
				if target == "" {
					target = chooseAttackChainTarget(report.Target, chain.attackChainItem)
				}
				if target == "" {
					target = report.Workspace
				}
				params := make(map[string]interface{})
				for key, value := range req.Params {
					params[key] = value
				}
				params["target"] = target
				params["workspace"] = report.Workspace
				params["space_name"] = report.Workspace
				params["retest_vulnerability_id"] = strconv.FormatInt(vuln.ID, 10)
				params["attack_chain_report_id"] = strconv.FormatInt(report.ID, 10)
				params["attack_chain_chain_id"] = chain.ChainID

				retestRunUUID := uuid.New().String()
				run := &database.Run{
					RunUUID:      retestRunUUID,
					WorkflowName: workflowName,
					WorkflowKind: workflowKind,
					Target:       target,
					Params:       params,
					Status:       "queued",
					TriggerType:  "attack-chain-retest",
					RunGroupID:   runGroupID,
					RunPriority:  normalizeRetestPriority(req.Priority),
					RunMode:      "queue",
					IsQueued:     true,
					Workspace:    report.Workspace,
				}

				applyAttackChainLinkageToVulnerability(vuln, formatAttackChainRef(report.ID, chain.ChainID), collectAttackChainLinkAssets(report, chain.attackChainItem), buildAttackChainReportRefs(report), true)
				vuln.VulnStatus = "retest"
				vuln.RetestStatus = "queued"
				vuln.RetestRunUUID = retestRunUUID
				applyVulnerabilityStatusTimestamps(vuln)
				if err := database.QueueVulnerabilityRetest(ctx, vuln, run); err != nil {
					skipped++
					continue
				}
				queued++
				runUUIDs = append(runUUIDs, retestRunUUID)
				if linked.VulnStatus == "verified" {
					verifiedHit++
				}
			}
		}

		if queued > 0 {
			_ = database.RecordAttackChainQueueActivity(ctx, report.ID, queued, verifiedHit)
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message":       "Attack chain retest queue generated",
			"queued":        queued,
			"skipped":       skipped,
			"run_uuids":     runUUIDs,
			"verified_hits": verifiedHit,
		})
	}
}

// QueueAttackChainDeepScan turns selected chains into queued deep-scan runs.
func QueueAttackChainDeepScan(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		report, req, workflowName, workflowKind, err := parseAttackChainQueueRequest(c)
		if err != nil {
			return err
		}

		ctx := context.Background()
		chains := selectAttackChainQueueItems(ctx, report, req, true)
		if len(chains) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "No eligible attack chains selected",
			})
		}

		runGroupID := fmt.Sprintf("attack-chain-report-%d", report.ID)
		targets := collectAttackChainTargets(report, chains)
		var (
			queued      int
			skipped     int
			runUUIDs    []string
			verifiedHit int
		)
		for _, chain := range chains {
			verifiedHit += countOperationallyVerifiedLinks(chain.LinkedVulnerabilities, true)
		}

		for _, target := range targets {
			if attackChainDeepScanRunExists(ctx, runGroupID, workflowName, report.Workspace, target) {
				skipped++
				continue
			}
			params := make(map[string]interface{})
			for key, value := range req.Params {
				params[key] = value
			}
			params["target"] = target
			params["workspace"] = report.Workspace
			params["space_name"] = report.Workspace
			params["attack_chain_report_id"] = strconv.FormatInt(report.ID, 10)
			params["attack_chain_chain_ids"] = strings.Join(extractChainIDs(chains), ",")
			params["attack_chain_mode"] = "deep_scan"

			runUUID := uuid.New().String()
			run := &database.Run{
				RunUUID:      runUUID,
				WorkflowName: workflowName,
				WorkflowKind: workflowKind,
				Target:       target,
				Params:       params,
				Status:       "queued",
				TriggerType:  "attack-chain-deep-scan",
				RunGroupID:   runGroupID,
				RunPriority:  normalizeRetestPriority(req.Priority),
				RunMode:      "queue",
				IsQueued:     true,
				Workspace:    report.Workspace,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}
			if err := database.CreateRun(ctx, run); err != nil {
				skipped++
				continue
			}
			queued++
			runUUIDs = append(runUUIDs, runUUID)
		}

		if queued > 0 {
			_ = database.RecordAttackChainQueueActivity(ctx, report.ID, queued, verifiedHit)
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message":       "Attack chain deep-scan queue generated",
			"queued":        queued,
			"skipped":       skipped,
			"run_uuids":     runUUIDs,
			"verified_hits": verifiedHit,
			"targets":       targets,
		})
	}
}

// ImportAttackChain imports an attack-chain JSON report into the local workbench store.
func ImportAttackChain(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req ImportAttackChainRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}

		ctx := context.Background()
		summary, err := attackchain.ImportFile(
			ctx,
			req.Workspace,
			req.SourcePath,
			req.Target,
			req.RunUUID,
			req.MermaidPath,
			req.TextPath,
		)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		linkedWritebacks := 0
		if summary != nil && summary.ID > 0 {
			if report, err := database.GetAttackChainReportByID(ctx, summary.ID); err == nil {
				linkedWritebacks, _ = backfillAttackChainVulnerabilityLinks(ctx, report)
			}
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data":                       summary,
			"message":                    "Attack chain report imported successfully",
			"linked_vulnerability_count": linkedWritebacks,
		})
	}
}

func decodeAttackChains(raw string) []attackChainItem {
	if strings.TrimSpace(raw) == "" {
		return []attackChainItem{}
	}

	var chains []attackChainItem
	if err := json.Unmarshal([]byte(raw), &chains); err != nil {
		return []attackChainItem{}
	}
	return chains
}

func decodeAttackPaths(raw string) []attackChainPath {
	if strings.TrimSpace(raw) == "" {
		return []attackChainPath{}
	}

	var paths []attackChainPath
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return []attackChainPath{}
	}
	return paths
}

func isVerifiableAttackChain(chain attackChainItem) bool {
	if strings.TrimSpace(chain.EntryPoint.URL) == "" && strings.TrimSpace(chain.EntryPoint.Vulnerability) == "" {
		return false
	}
	if len(chain.ChainSteps) == 0 {
		return false
	}
	for _, step := range chain.ChainSteps {
		if strings.TrimSpace(step.Action) == "" {
			return false
		}
	}
	return true
}

func buildExecutionChecklist(chains []attackChainItem) []string {
	checklist := make([]string, 0)
	for _, chain := range chains {
		name := strings.TrimSpace(chain.ChainName)
		if name == "" {
			name = strings.TrimSpace(chain.ChainID)
		}
		if name == "" {
			name = "unnamed-chain"
		}

		for _, step := range chain.ChainSteps {
			entry := name + ": " + strings.TrimSpace(step.Action)
			if strings.TrimSpace(step.Command) != "" {
				entry += " [" + strings.TrimSpace(step.Command) + "]"
			}
			checklist = append(checklist, entry)
		}
	}
	return checklist
}

func enrichAttackChains(ctx context.Context, workspace string, chains []attackChainItem) []attackChainWorkbenchItem {
	if strings.TrimSpace(workspace) == "" || len(chains) == 0 {
		result := make([]attackChainWorkbenchItem, 0, len(chains))
		for _, chain := range chains {
			result = append(result, attackChainWorkbenchItem{attackChainItem: chain})
		}
		return result
	}

	result := make([]attackChainWorkbenchItem, 0, len(chains))
	for _, chain := range chains {
		linkedVulns := findLinkedVulnerabilities(ctx, workspace, chain)
		linkedAssets := findLinkedAssets(ctx, workspace, chain)
		verifiedCount := 0
		for _, vuln := range linkedVulns {
			if isAttackChainVerifiedStatus(vuln.VulnStatus) {
				verifiedCount++
			}
		}
		operationalCount := countOperationallyVerifiedLinks(linkedVulns, true)
		verificationRate := 0.0
		if len(linkedVulns) > 0 {
			verificationRate = float64(verifiedCount) / float64(len(linkedVulns))
		}
		recommendation := "manual-review"
		switch {
		case operationalCount > 0:
			recommendation = "queue-retest"
		case len(linkedVulns) > 0 || len(linkedAssets) > 0:
			recommendation = "queue-deep-scan"
		}
		result = append(result, attackChainWorkbenchItem{
			attackChainItem:          chain,
			LinkedVulnerabilities:    linkedVulns,
			LinkedAssets:             linkedAssets,
			LinkedVulnerabilityCount: len(linkedVulns),
			VerifiedLinkedCount:      verifiedCount,
			OperationalLinkedCount:   operationalCount,
			VerificationRate:         verificationRate,
			ExecutionReady:           operationalCount > 0,
			QueueRecommendation:      recommendation,
		})
	}
	return result
}

func buildAttackChainWorkbenchSummary(report *database.AttackChainReport, items []attackChainWorkbenchItem) attackChainWorkbenchSummary {
	summary := attackChainWorkbenchSummary{
		TotalChains: len(items),
	}
	for _, item := range items {
		if item.VerifiedLinkedCount > 0 {
			summary.VerifiedChains++
		}
		if item.ExecutionReady {
			summary.ExecutionReadyChains++
			if trimmed := strings.TrimSpace(item.ChainID); trimmed != "" {
				summary.RecommendedRetestChainIDs = append(summary.RecommendedRetestChainIDs, trimmed)
			}
		}
	}
	summary.RecommendedRetestChainIDs = dedupeNonEmptyStrings(summary.RecommendedRetestChainIDs)
	summary.RecommendedDeepScanTargets = collectAttackChainTargets(report, items)
	return summary
}

func findLinkedVulnerabilities(ctx context.Context, workspace string, chain attackChainItem) []linkedVulnerability {
	vulns, err := attackchain.FindLinkedVulnerabilities(ctx, workspace, chain.EntryPoint.Vulnerability, chain.EntryPoint.URL, 10)
	if err != nil {
		return nil
	}

	result := make([]linkedVulnerability, 0, len(vulns))
	for _, vuln := range vulns {
		result = append(result, linkedVulnerability{
			ID:         vuln.ID,
			VulnTitle:  vuln.VulnTitle,
			Severity:   vuln.Severity,
			VulnStatus: vuln.VulnStatus,
			AssetValue: vuln.AssetValue,
		})
	}
	return result
}

func findLinkedAssets(ctx context.Context, workspace string, chain attackChainItem) []linkedAsset {
	db := database.GetDB()
	if db == nil {
		return nil
	}

	entryURL := strings.TrimSpace(chain.EntryPoint.URL)
	if entryURL == "" {
		return nil
	}

	var assets []database.Asset
	if err := db.NewSelect().
		Model(&assets).
		Column("id", "asset_value", "url", "asset_type", "source").
		Where("workspace = ?", workspace).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.
				WhereOr("asset_value LIKE ?", "%"+entryURL+"%").
				WhereOr("url LIKE ?", "%"+entryURL+"%").
				WhereOr("external_url LIKE ?", "%"+entryURL+"%")
		}).
		Order("updated_at DESC").
		Limit(10).
		Scan(ctx); err != nil {
		return nil
	}

	result := make([]linkedAsset, 0, len(assets))
	for _, asset := range assets {
		result = append(result, linkedAsset{
			ID:         asset.ID,
			AssetValue: asset.AssetValue,
			URL:        asset.URL,
			AssetType:  asset.AssetType,
			Source:     asset.Source,
		})
	}
	return result
}

func parseAttackChainQueueRequest(c *fiber.Ctx) (*database.AttackChainReport, QueueAttackChainRequest, string, string, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return nil, QueueAttackChainRequest{}, "", "", c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "Invalid attack chain ID",
		})
	}

	var req QueueAttackChainRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, QueueAttackChainRequest{}, "", "", c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "Invalid request body",
		})
	}

	workflowName, workflowKind := resolveRetestWorkflow(req.Flow, req.Module)
	if workflowName == "" {
		return nil, QueueAttackChainRequest{}, "", "", c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   true,
			"message": "Either flow or module is required",
		})
	}

	report, err := database.GetAttackChainReportByID(context.Background(), id)
	if err != nil {
		return nil, QueueAttackChainRequest{}, "", "", c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error":   true,
			"message": "Attack chain report not found",
		})
	}

	return report, req, workflowName, workflowKind, nil
}

func selectAttackChainQueueItems(ctx context.Context, report *database.AttackChainReport, req QueueAttackChainRequest, includeRetestAsVerified bool) []attackChainWorkbenchItem {
	chains := decodeAttackChains(report.AttackChainsJSON)
	if len(req.ChainIDs) > 0 {
		selected := make([]attackChainItem, 0, len(chains))
		allow := make(map[string]struct{}, len(req.ChainIDs))
		for _, chainID := range req.ChainIDs {
			allow[strings.TrimSpace(chainID)] = struct{}{}
		}
		for _, chain := range chains {
			if _, ok := allow[strings.TrimSpace(chain.ChainID)]; ok {
				selected = append(selected, chain)
			}
		}
		chains = selected
	}
	items := enrichAttackChains(ctx, report.Workspace, chains)
	if !req.VerifiedOnly {
		return items
	}
	filtered := make([]attackChainWorkbenchItem, 0, len(items))
	for _, item := range items {
		if countOperationallyVerifiedLinks(item.LinkedVulnerabilities, includeRetestAsVerified) > 0 {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func countOperationallyVerifiedLinks(vulns []linkedVulnerability, includeRetest bool) int {
	count := 0
	for _, vuln := range vulns {
		if isAttackChainOperationallyVerifiedStatus(vuln.VulnStatus, includeRetest) {
			count++
		}
	}
	return count
}

func isAttackChainOperationallyVerifiedStatus(status string, includeRetest bool) bool {
	if isAttackChainVerifiedStatus(status) {
		return true
	}
	return includeRetest && strings.EqualFold(strings.TrimSpace(status), "retest")
}

func isAttackChainVerifiedStatus(status string) bool {
	return strings.EqualFold(strings.TrimSpace(status), "verified")
}

func extractAttackChainItems(items []attackChainWorkbenchItem) []attackChainItem {
	result := make([]attackChainItem, 0, len(items))
	for _, item := range items {
		result = append(result, item.attackChainItem)
	}
	return result
}

func extractChainIDs(items []attackChainWorkbenchItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item.ChainID); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func collectAttackChainTargets(report *database.AttackChainReport, items []attackChainWorkbenchItem) []string {
	seen := make(map[string]struct{})
	var targets []string
	for _, item := range items {
		for _, vuln := range item.LinkedVulnerabilities {
			if value := strings.TrimSpace(vuln.AssetValue); value != "" {
				if _, ok := seen[value]; !ok {
					seen[value] = struct{}{}
					targets = append(targets, value)
				}
			}
		}
		for _, asset := range item.LinkedAssets {
			for _, value := range []string{asset.URL, asset.AssetValue} {
				value = strings.TrimSpace(value)
				if value == "" {
					continue
				}
				if _, ok := seen[value]; ok {
					continue
				}
				seen[value] = struct{}{}
				targets = append(targets, value)
			}
		}
		if value := chooseAttackChainTarget(report.Target, item.attackChainItem); value != "" {
			if _, ok := seen[value]; !ok {
				seen[value] = struct{}{}
				targets = append(targets, value)
			}
		}
	}
	return targets
}

func chooseAttackChainTarget(fallback string, chain attackChainItem) string {
	for _, candidate := range []string{chain.EntryPoint.URL, fallback} {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func attackChainDeepScanRunExists(ctx context.Context, runGroupID, workflowName, workspace, target string) bool {
	db := database.GetDB()
	if db == nil {
		return false
	}
	count, err := db.NewSelect().
		Model((*database.Run)(nil)).
		Where("run_group_id = ?", runGroupID).
		Where("trigger_type = ?", "attack-chain-deep-scan").
		Where("workflow_name = ?", workflowName).
		Where("workspace = ?", workspace).
		Where("target = ?", target).
		Count(ctx)
	return err == nil && count > 0
}

func backfillAttackChainVulnerabilityLinks(ctx context.Context, report *database.AttackChainReport) (int, error) {
	if report == nil || strings.TrimSpace(report.Workspace) == "" {
		return 0, nil
	}

	assignments := make(map[int64]attackChainVulnerabilityWriteback)
	for _, item := range enrichAttackChains(ctx, report.Workspace, decodeAttackChains(report.AttackChainsJSON)) {
		assets := collectAttackChainLinkAssets(report, item.attackChainItem)
		reportRefs := buildAttackChainReportRefs(report)
		chainRef := formatAttackChainRef(report.ID, item.ChainID)
		for _, linked := range item.LinkedVulnerabilities {
			current := assignments[linked.ID]
			if current.ChainRef == "" {
				current.ChainRef = chainRef
			}
			current.RelatedAssets = append(current.RelatedAssets, assets...)
			current.ReportRefs = append(current.ReportRefs, reportRefs...)
			assignments[linked.ID] = current
		}
	}

	updated := 0
	for vulnID, assignment := range assignments {
		vuln, err := database.GetVulnerabilityByID(ctx, vulnID)
		if err != nil || vuln == nil {
			continue
		}
		if !applyAttackChainLinkageToVulnerability(vuln, assignment.ChainRef, assignment.RelatedAssets, assignment.ReportRefs, false) {
			continue
		}
		if err := database.UpdateVulnerabilityRecord(ctx, vuln); err != nil {
			continue
		}
		updated++
	}
	return updated, nil
}

type attackChainVulnerabilityWriteback struct {
	ChainRef      string
	RelatedAssets []string
	ReportRefs    []string
}

func applyAttackChainLinkageToVulnerability(vuln *database.Vulnerability, chainRef string, relatedAssets, reportRefs []string, preferChainRef bool) bool {
	if vuln == nil {
		return false
	}

	changed := false
	chainRef = strings.TrimSpace(chainRef)
	if chainRef != "" {
		switch {
		case preferChainRef && vuln.AttackChainRef != chainRef:
			vuln.AttackChainRef = chainRef
			changed = true
		case strings.TrimSpace(vuln.AttackChainRef) == "":
			vuln.AttackChainRef = chainRef
			changed = true
		}
	}

	mergedAssets := dedupeNonEmptyStrings(append(append([]string{}, vuln.RelatedAssets...), relatedAssets...))
	if !equalStringSlices(vuln.RelatedAssets, mergedAssets) {
		vuln.RelatedAssets = mergedAssets
		changed = true
	}

	mergedReportRefs := dedupeNonEmptyStrings(append(append([]string{}, vuln.ReportRefs...), reportRefs...))
	if !equalStringSlices(vuln.ReportRefs, mergedReportRefs) {
		vuln.ReportRefs = mergedReportRefs
		changed = true
	}

	return changed
}

func collectAttackChainLinkAssets(report *database.AttackChainReport, chain attackChainItem) []string {
	return dedupeNonEmptyStrings([]string{report.Target, chain.EntryPoint.URL})
}

func buildAttackChainReportRefs(report *database.AttackChainReport) []string {
	if report == nil {
		return nil
	}
	return dedupeNonEmptyStrings([]string{report.SourcePath, report.TextPath, report.MermaidPath})
}

func formatAttackChainRef(reportID int64, chainID string) string {
	chainID = strings.TrimSpace(chainID)
	if reportID <= 0 || chainID == "" {
		return ""
	}
	return fmt.Sprintf("report:%d:%s", reportID, chainID)
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}
