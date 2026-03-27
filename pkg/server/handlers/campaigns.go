package handlers

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/j3ssie/osmedeus/v5/internal/attackchain"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/parser"
)

// CreateCampaignRequest creates a queued campaign batch.
type CreateCampaignRequest struct {
	Name                 string            `json:"name"`
	Flow                 string            `json:"flow,omitempty"`
	Module               string            `json:"module,omitempty"`
	Targets              []string          `json:"targets,omitempty"`
	TargetFile           string            `json:"target_file,omitempty"`
	Params               map[string]string `json:"params,omitempty"`
	Role                 string            `json:"role,omitempty"`
	Skills               []string          `json:"skills,omitempty"`
	Strategy             string            `json:"strategy,omitempty"`
	Priority             string            `json:"priority,omitempty"`
	DeepScanWorkflow     string            `json:"deep_scan_workflow,omitempty"`
	DeepScanWorkflowKind string            `json:"deep_scan_workflow_kind,omitempty"`
	AutoDeepScan         bool              `json:"auto_deep_scan,omitempty"`
	HighRiskSeverities   []string          `json:"high_risk_severities,omitempty"`
	Notes                string            `json:"notes,omitempty"`
}

// CampaignTargetStatus is an aggregated target-level view for a campaign.
type CampaignTargetStatus struct {
	Target             string                                `json:"target"`
	Workspace          string                                `json:"workspace"`
	Status             string                                `json:"status"`
	RiskLevel          string                                `json:"risk_level"`
	VulnSummary        map[string]int                        `json:"vuln_summary"`
	AttackChainSummary *database.AttackChainWorkspaceSummary `json:"attack_chain_summary,omitempty"`
	DeepScanQueued     bool                                  `json:"deep_scan_queued"`
	RunUUID            string                                `json:"run_uuid"`
}

// CampaignStatusResponse returns an aggregated campaign view.
type CampaignStatusResponse struct {
	Campaign        *database.Campaign     `json:"campaign"`
	Status          string                 `json:"status"`
	Progress        JobProgress            `json:"progress"`
	Summary         CampaignSignalSummary  `json:"summary"`
	Targets         []CampaignTargetStatus `json:"targets"`
	HighRiskTargets []string               `json:"high_risk_targets"`
	Runs            []*database.Run        `json:"runs"`
}

// CampaignSignalSummary exposes aggregate risk/queue signals for a campaign.
type CampaignSignalSummary struct {
	TargetsTotal               int `json:"targets_total"`
	HighRiskTargets            int `json:"high_risk_targets"`
	DeepScanQueuedTargets      int `json:"deep_scan_queued_targets"`
	AttackChainAwareTargets    int `json:"attack_chain_aware_targets"`
	VerifiedAttackChainTargets int `json:"verified_attack_chain_targets"`
}

// CampaignReportResponse exposes campaign-level analytics for export and review.
type CampaignReportResponse struct {
	Campaign                    *database.Campaign        `json:"campaign"`
	GeneratedAt                 time.Time                 `json:"generated_at"`
	Status                      string                    `json:"status"`
	Progress                    JobProgress               `json:"progress"`
	Summary                     CampaignSignalSummary     `json:"summary"`
	RiskDistribution            map[string]int            `json:"risk_distribution"`
	LatestRunStatusDistribution map[string]int            `json:"latest_run_status_distribution"`
	TriggerDistribution         map[string]int            `json:"trigger_distribution"`
	DeepScan                    CampaignDeepScanReport    `json:"deep_scan"`
	RerunHistory                CampaignRerunHistory      `json:"rerun_history"`
	FiltersApplied              CampaignReportFilters     `json:"filters_applied"`
	SortApplied                 CampaignReportSort        `json:"sort_applied"`
	ProfileApplied              string                    `json:"profile_applied,omitempty"`
	TotalTargets                int                       `json:"total_targets"`
	ResultCount                 int                       `json:"result_count"`
	Pagination                  CampaignReportPagination  `json:"pagination"`
	Targets                     []CampaignReportTargetRow `json:"targets"`
}

// CampaignReportFilters narrows campaign report/export target rows.
type CampaignReportFilters struct {
	RiskLevels   []string `json:"risk_levels,omitempty"`
	Statuses     []string `json:"statuses,omitempty"`
	TriggerTypes []string `json:"trigger_types,omitempty"`
	Preset       string   `json:"preset,omitempty"`
}

// CampaignReportSort describes the active target-row ordering.
type CampaignReportSort struct {
	By    string `json:"by"`
	Order string `json:"order"`
}

// CampaignProfileRequest stores a reusable report/export profile.
type CampaignProfileRequest struct {
	Description string                                `json:"description,omitempty"`
	Filters     database.CampaignReportProfileFilters `json:"filters,omitempty"`
	Sort        database.CampaignReportProfileSort    `json:"sort,omitempty"`
	Format      string                                `json:"format,omitempty"`
}

// CampaignReportPagination describes the current report page after filters are applied.
type CampaignReportPagination struct {
	Offset        int  `json:"offset"`
	Limit         int  `json:"limit"`
	ReturnedCount int  `json:"returned_count"`
	HasMore       bool `json:"has_more"`
}

// CampaignDeepScanReport summarizes follow-up deep-scan activity for a campaign.
type CampaignDeepScanReport struct {
	Configured      bool    `json:"configured"`
	EligibleTargets int     `json:"eligible_targets"`
	QueuedTargets   int     `json:"queued_targets"`
	TotalRuns       int     `json:"total_runs"`
	QueuedRuns      int     `json:"queued_runs"`
	RunningRuns     int     `json:"running_runs"`
	CompletedRuns   int     `json:"completed_runs"`
	FailedRuns      int     `json:"failed_runs"`
	ConversionRate  float64 `json:"conversion_rate"`
}

// CampaignRerunHistory summarizes failure recovery activity for a campaign.
type CampaignRerunHistory struct {
	TotalRuns        int        `json:"total_runs"`
	UniqueTargets    int        `json:"unique_targets"`
	RecoveredTargets int        `json:"recovered_targets"`
	LastRerunAt      *time.Time `json:"last_rerun_at,omitempty"`
}

// CampaignReportTargetRow is the flattened per-target audit row used by report/export endpoints.
type CampaignReportTargetRow struct {
	Target                     string     `json:"target"`
	Workspace                  string     `json:"workspace"`
	Status                     string     `json:"status"`
	RiskLevel                  string     `json:"risk_level"`
	LatestRunUUID              string     `json:"latest_run_uuid,omitempty"`
	LatestTriggerType          string     `json:"latest_trigger_type,omitempty"`
	LatestRunAt                time.Time  `json:"latest_run_at,omitempty"`
	DeepScanQueued             bool       `json:"deep_scan_queued"`
	TotalRuns                  int        `json:"total_runs"`
	RerunRuns                  int        `json:"rerun_runs"`
	DeepScanRuns               int        `json:"deep_scan_runs"`
	Recovered                  bool       `json:"recovered"`
	LastRerunAt                *time.Time `json:"last_rerun_at,omitempty"`
	LastDeepScanAt             *time.Time `json:"last_deep_scan_at,omitempty"`
	CriticalFindings           int        `json:"critical_findings"`
	HighFindings               int        `json:"high_findings"`
	MediumFindings             int        `json:"medium_findings"`
	LowFindings                int        `json:"low_findings"`
	OpenHighRiskFindings       int        `json:"open_high_risk_findings"`
	AttackChainOperationalHits int        `json:"attack_chain_operational_hits"`
	AttackChainVerifiedHits    int        `json:"attack_chain_verified_hits"`
}

// ListCampaigns handles listing campaigns.
func ListCampaigns(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		limit, _ := strconv.Atoi(c.Query("limit", "20"))
		ctx := context.Background()
		result, err := database.ListCampaigns(ctx, database.CampaignQuery{
			Status: c.Query("status"),
			Offset: offset,
			Limit:  limit,
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

// CreateCampaign handles campaign batch creation.
func CreateCampaign(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req CreateCampaignRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}

		workflowName, workflowKind := resolveCampaignWorkflow(req.Flow, req.Module)
		if workflowName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Either flow or module is required",
			})
		}

		loader := parser.NewLoader(cfg.WorkflowsPath)
		if _, err := loader.LoadWorkflow(workflowName); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Workflow not found",
			})
		}
		if strings.TrimSpace(req.DeepScanWorkflow) != "" {
			if _, err := loader.LoadWorkflow(req.DeepScanWorkflow); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error":   true,
					"message": "Deep scan workflow not found",
				})
			}
		}

		targets, err := collectCampaignTargets(req.Targets, req.TargetFile)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		if len(targets) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "At least one target is required",
			})
		}

		priority := normalizeCampaignPriority(req.Priority)
		params := make(map[string]interface{})
		for key, value := range req.Params {
			params[key] = value
		}
		if strings.TrimSpace(req.Role) != "" {
			params["campaign_role"] = req.Role
		}
		if len(req.Skills) > 0 {
			params["campaign_skills"] = strings.Join(req.Skills, ",")
		}
		if strings.TrimSpace(req.Strategy) != "" {
			params["campaign_strategy"] = req.Strategy
		}

		campaignID := uuid.New().String()
		campaignName := strings.TrimSpace(req.Name)
		if campaignName == "" {
			campaignName = fmt.Sprintf("%s-%s", workflowName, time.Now().Format("20060102-150405"))
		}

		ctx := context.Background()
		campaign := &database.Campaign{
			ID:                   campaignID,
			Name:                 campaignName,
			WorkflowName:         workflowName,
			WorkflowKind:         workflowKind,
			Status:               "queued",
			Role:                 req.Role,
			Strategy:             req.Strategy,
			Skills:               req.Skills,
			Params:               params,
			DeepScanWorkflow:     req.DeepScanWorkflow,
			DeepScanWorkflowKind: normalizeCampaignWorkflowKind(req.DeepScanWorkflowKind),
			AutoDeepScan:         req.AutoDeepScan,
			HighRiskSeverities:   normalizeHighRiskSeverities(req.HighRiskSeverities),
			TargetCount:          len(targets),
			Notes:                req.Notes,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		}
		if err := database.CreateCampaign(ctx, campaign); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		var queued []string
		for _, target := range targets {
			run := &database.Run{
				RunUUID:       uuid.New().String(),
				WorkflowName:  workflowName,
				WorkflowKind:  workflowKind,
				Target:        target,
				Params:        params,
				Status:        "queued",
				TriggerType:   "campaign",
				RunGroupID:    campaignID,
				RunPriority:   priority,
				RunMode:       "queue",
				IsQueued:      true,
				Workspace:     computeWorkspace(target, req.Params),
				InputIsFile:   req.TargetFile != "",
				InputFilePath: normalizeCampaignInputFile(req.TargetFile),
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}
			if err := database.CreateRun(ctx, run); err != nil {
				continue
			}
			queued = append(queued, run.RunUUID)
		}
		if len(queued) == 0 {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": "Failed to queue campaign runs",
			})
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message":      "Campaign created",
			"campaign_id":  campaignID,
			"queued_runs":  len(queued),
			"target_count": len(targets),
			"data":         campaign,
		})
	}
}

// GetCampaignStatus handles campaign status retrieval.
func GetCampaignStatus(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		campaign, err := database.GetCampaignByID(ctx, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Campaign not found",
			})
		}

		response, err := buildCampaignStatusResponse(ctx, campaign)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		return c.JSON(response)
	}
}

// GetCampaignReport returns campaign analytics and target-level audit rows.
func GetCampaignReport(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		campaign, err := database.GetCampaignByID(ctx, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Campaign not found",
			})
		}

		filters, sortSpec, _, profileApplied, statusCode, err := resolveCampaignReportQueryOptions(c, campaign)
		if err != nil {
			return c.Status(statusCode).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		offset, limit := campaignReportPageFromQuery(c)

		report, err := buildCampaignReportResponse(ctx, campaign)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		report = applyCampaignReportFilters(report, campaign, filters)
		report = sortCampaignReport(report, sortSpec)
		report.ProfileApplied = profileApplied
		report = paginateCampaignReport(report, offset, limit)
		return c.JSON(report)
	}
}

// ExportCampaignReport exports campaign target analytics as CSV or JSON.
func ExportCampaignReport(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		campaign, err := database.GetCampaignByID(ctx, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Campaign not found",
			})
		}

		filters, sortSpec, format, profileApplied, statusCode, err := resolveCampaignReportQueryOptions(c, campaign)
		if err != nil {
			return c.Status(statusCode).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		offset, limit := campaignReportPageFromQuery(c)

		report, err := buildCampaignReportResponse(ctx, campaign)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		report = applyCampaignReportFilters(report, campaign, filters)
		report = sortCampaignReport(report, sortSpec)
		report.ProfileApplied = profileApplied
		report = paginateCampaignReport(report, offset, limit)

		if strings.EqualFold(strings.TrimSpace(format), "json") {
			return c.JSON(report)
		}

		body, err := renderCampaignReportCSV(report)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		filename := fmt.Sprintf("campaign-%s-report.csv", campaign.ID)
		c.Set(fiber.HeaderContentType, "text/csv; charset=utf-8")
		c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("attachment; filename=%q", filename))
		return c.Send(body)
	}
}

// ListCampaignProfiles returns saved report/export profiles for a campaign.
func ListCampaignProfiles(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		campaign, err := database.GetCampaignByID(ctx, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Campaign not found",
			})
		}
		profiles, err := database.ListCampaignReportProfiles(campaign)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		return c.JSON(fiber.Map{
			"data": profiles,
		})
	}
}

// SaveCampaignProfile stores or updates a report/export profile.
func SaveCampaignProfile(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req CampaignProfileRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}
		profile, err := database.UpsertCampaignReportProfile(context.Background(), c.Params("id"), database.CampaignReportProfile{
			Name:        c.Params("name"),
			Description: req.Description,
			Filters:     req.Filters,
			Sort:        req.Sort,
			Format:      req.Format,
		})
		if err != nil {
			statusCode := fiber.StatusInternalServerError
			if strings.Contains(err.Error(), "campaign not found") {
				statusCode = fiber.StatusNotFound
			} else if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "unsupported") {
				statusCode = fiber.StatusBadRequest
			}
			return c.Status(statusCode).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		return c.JSON(fiber.Map{
			"message":     "Campaign profile saved",
			"campaign_id": c.Params("id"),
			"data":        profile,
		})
	}
}

// DeleteCampaignProfile removes a saved report/export profile.
func DeleteCampaignProfile(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		deleted, err := database.DeleteCampaignReportProfile(context.Background(), c.Params("id"), c.Params("name"))
		if err != nil {
			statusCode := fiber.StatusInternalServerError
			switch {
			case strings.Contains(err.Error(), "campaign not found"):
				statusCode = fiber.StatusNotFound
			case errors.Is(err, database.ErrCampaignReportProfileNotFound):
				statusCode = fiber.StatusNotFound
			case strings.Contains(err.Error(), "invalid"):
				statusCode = fiber.StatusBadRequest
			}
			return c.Status(statusCode).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		return c.JSON(fiber.Map{
			"message":     "Campaign profile deleted",
			"campaign_id": c.Params("id"),
			"deleted":     deleted,
			"name":        strings.ToLower(strings.TrimSpace(c.Params("name"))),
		})
	}
}

// RerunFailedCampaignTargets queues failed targets again.
func RerunFailedCampaignTargets(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		campaign, err := database.GetCampaignByID(ctx, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Campaign not found",
			})
		}
		runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		latestRuns := latestCampaignRuns(runs)
		var queued int
		for _, run := range latestRuns {
			if run.Status != "failed" || run.TriggerType == "campaign-rerun" {
				continue
			}
			clone := &database.Run{
				RunUUID:       uuid.New().String(),
				WorkflowName:  run.WorkflowName,
				WorkflowKind:  run.WorkflowKind,
				Target:        run.Target,
				Params:        run.Params,
				Status:        "queued",
				TriggerType:   "campaign-rerun",
				RunGroupID:    campaign.ID,
				RunPriority:   run.RunPriority,
				RunMode:       "queue",
				IsQueued:      true,
				Workspace:     run.Workspace,
				InputIsFile:   run.InputIsFile,
				InputFilePath: run.InputFilePath,
			}
			if err := database.CreateRun(ctx, clone); err != nil {
				continue
			}
			queued++
		}

		return c.JSON(fiber.Map{
			"message":      "Failed targets queued for rerun",
			"campaign_id":  campaign.ID,
			"queued_count": queued,
		})
	}
}

// QueueCampaignDeepScan queues deep-scan tasks for currently high-risk targets.
func QueueCampaignDeepScan(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		campaign, err := database.GetCampaignByID(ctx, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Campaign not found",
			})
		}
		queued, targets, err := queueCampaignDeepScanRuns(ctx, campaign)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		return c.JSON(fiber.Map{
			"message":      "Deep scan evaluation completed",
			"campaign_id":  campaign.ID,
			"queued_count": queued,
			"targets":      targets,
		})
	}
}

func resolveCampaignWorkflow(flow, module string) (string, string) {
	if strings.TrimSpace(flow) != "" {
		return strings.TrimSpace(flow), "flow"
	}
	if strings.TrimSpace(module) != "" {
		return strings.TrimSpace(module), "module"
	}
	return "", ""
}

func normalizeCampaignWorkflowKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "flow" {
		return "flow"
	}
	return "module"
}

func normalizeCampaignPriority(priority string) string {
	switch strings.TrimSpace(strings.ToLower(priority)) {
	case "low", "normal", "high", "critical":
		return strings.TrimSpace(strings.ToLower(priority))
	default:
		return "high"
	}
}

func normalizeHighRiskSeverities(values []string) []string {
	if len(values) == 0 {
		return []string{"critical", "high"}
	}
	var normalized []string
	seen := make(map[string]struct{})
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return []string{"critical", "high"}
	}
	return normalized
}

func collectCampaignTargets(targets []string, targetFile string) ([]string, error) {
	var allTargets []string
	allTargets = append(allTargets, targets...)
	if strings.TrimSpace(targetFile) != "" {
		fileTargets, err := readTargetsFromFile(targetFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read target file: %w", err)
		}
		allTargets = append(allTargets, fileTargets...)
	}
	return deduplicateTargets(allTargets), nil
}

func normalizeCampaignInputFile(targetFile string) string {
	if strings.TrimSpace(targetFile) == "" {
		return ""
	}
	absPath, err := filepath.Abs(targetFile)
	if err != nil {
		return targetFile
	}
	return absPath
}

func buildCampaignStatusResponse(ctx context.Context, campaign *database.Campaign) (*CampaignStatusResponse, error) {
	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	if err != nil {
		return nil, err
	}

	latestRuns := latestCampaignRuns(runs)

	progress := JobProgress{Total: len(latestRuns)}
	var (
		targets []CampaignTargetStatus
		summary CampaignSignalSummary
	)
	var highRiskTargets []string

	for _, run := range latestRuns {
		switch run.Status {
		case "pending", "queued":
			progress.Pending++
		case "running":
			progress.Running++
		case "completed":
			progress.Completed++
		case "failed":
			progress.Failed++
		}

		vulnSummary, _ := database.GetActiveVulnerabilitySummaryForTarget(ctx, run.Workspace, run.Target)
		attackChainSummary, _ := attackchain.GetTargetSummary(ctx, run.Workspace, run.Target)
		riskLevel := deriveCampaignRiskLevel(vulnSummary, attackChainSummary)
		deepScanQueued, _ := database.HasCampaignDeepScanRun(ctx, campaign.ID, run.Workspace, campaign.DeepScanWorkflow, run.Target)
		summary.TargetsTotal++
		if deepScanQueued {
			summary.DeepScanQueuedTargets++
		}
		if attackChainSummary != nil && attackChainSummary.ReportCount > 0 {
			summary.AttackChainAwareTargets++
		}
		if attackChainSummary != nil && attackChainSummary.VerifiedHits > 0 {
			summary.VerifiedAttackChainTargets++
		}
		targets = append(targets, CampaignTargetStatus{
			Target:             run.Target,
			Workspace:          run.Workspace,
			Status:             run.Status,
			RiskLevel:          riskLevel,
			VulnSummary:        vulnSummary,
			AttackChainSummary: attackChainSummary,
			DeepScanQueued:     deepScanQueued,
			RunUUID:            run.RunUUID,
		})
		if campaignTargetIsHighRisk(campaign, vulnSummary, attackChainSummary) {
			highRiskTargets = append(highRiskTargets, run.Target)
		}
	}
	summary.HighRiskTargets = len(deduplicateTargets(highRiskTargets))

	status := aggregateStatus(progress)
	if err := database.UpdateCampaignStatus(ctx, campaign.ID, status); err != nil {
		return nil, err
	}
	campaign.Status = status

	return &CampaignStatusResponse{
		Campaign:        campaign,
		Status:          status,
		Progress:        progress,
		Summary:         summary,
		Targets:         targets,
		HighRiskTargets: deduplicateTargets(highRiskTargets),
		Runs:            runs,
	}, nil
}

func buildCampaignReportResponse(ctx context.Context, campaign *database.Campaign) (*CampaignReportResponse, error) {
	statusResponse, err := buildCampaignStatusResponse(ctx, campaign)
	if err != nil {
		return nil, err
	}

	report := &CampaignReportResponse{
		Campaign:                    statusResponse.Campaign,
		GeneratedAt:                 time.Now().UTC(),
		Status:                      statusResponse.Status,
		Progress:                    statusResponse.Progress,
		Summary:                     statusResponse.Summary,
		RiskDistribution:            make(map[string]int),
		LatestRunStatusDistribution: make(map[string]int),
		TriggerDistribution:         make(map[string]int),
	}

	targetRows := make(map[string]*CampaignReportTargetRow, len(statusResponse.Targets))
	for _, target := range statusResponse.Targets {
		report.RiskDistribution[target.RiskLevel]++
		report.LatestRunStatusDistribution[target.Status]++
		row := &CampaignReportTargetRow{
			Target:               target.Target,
			Workspace:            target.Workspace,
			Status:               target.Status,
			RiskLevel:            target.RiskLevel,
			LatestRunUUID:        target.RunUUID,
			DeepScanQueued:       target.DeepScanQueued,
			CriticalFindings:     target.VulnSummary["critical"],
			HighFindings:         target.VulnSummary["high"],
			MediumFindings:       target.VulnSummary["medium"],
			LowFindings:          target.VulnSummary["low"],
			OpenHighRiskFindings: target.VulnSummary["critical"] + target.VulnSummary["high"],
		}
		if target.AttackChainSummary != nil {
			row.AttackChainOperationalHits = target.AttackChainSummary.OperationalHits
			row.AttackChainVerifiedHits = target.AttackChainSummary.VerifiedHits
		}
		targetRows[campaignTargetKey(target.Workspace, target.Target)] = row
	}

	latestRuns := latestCampaignRuns(statusResponse.Runs)
	rerunTargets := make(map[string]struct{})

	report.DeepScan.Configured = strings.TrimSpace(statusResponse.Campaign.DeepScanWorkflow) != ""
	report.DeepScan.EligibleTargets = len(statusResponse.HighRiskTargets)
	report.DeepScan.QueuedTargets = statusResponse.Summary.DeepScanQueuedTargets

	for _, run := range statusResponse.Runs {
		report.TriggerDistribution[run.TriggerType]++
		key := campaignTargetKey(run.Workspace, run.Target)
		row, ok := targetRows[key]
		if !ok {
			continue
		}
		row.TotalRuns++

		switch run.TriggerType {
		case "campaign-rerun":
			row.RerunRuns++
			report.RerunHistory.TotalRuns++
			rerunTargets[key] = struct{}{}
			updateCampaignRowTime(&row.LastRerunAt, run.CreatedAt)
			updateCampaignRowTime(&report.RerunHistory.LastRerunAt, run.CreatedAt)
		case "campaign-deep-scan":
			row.DeepScanRuns++
			report.DeepScan.TotalRuns++
			updateCampaignRowTime(&row.LastDeepScanAt, run.CreatedAt)
			switch run.Status {
			case "queued", "pending":
				report.DeepScan.QueuedRuns++
			case "running":
				report.DeepScan.RunningRuns++
			case "completed":
				report.DeepScan.CompletedRuns++
			case "failed":
				report.DeepScan.FailedRuns++
			}
		}
	}
	report.RerunHistory.UniqueTargets = len(rerunTargets)

	for key, row := range targetRows {
		if latest := latestRuns[key]; latest != nil {
			row.LatestRunUUID = latest.RunUUID
			row.LatestTriggerType = latest.TriggerType
			row.LatestRunAt = latest.CreatedAt
		}
		if row.RerunRuns > 0 && latestCampaignTargetRecovered(statusResponse.Runs, key, latestRuns[key]) {
			row.Recovered = true
			report.RerunHistory.RecoveredTargets++
		}
		report.Targets = append(report.Targets, *row)
	}

	if report.DeepScan.EligibleTargets > 0 {
		report.DeepScan.ConversionRate = float64(report.DeepScan.QueuedTargets) / float64(report.DeepScan.EligibleTargets)
	}

	sort.Slice(report.Targets, func(i, j int) bool {
		left := report.Targets[i]
		right := report.Targets[j]
		if campaignRiskRank(left.RiskLevel) == campaignRiskRank(right.RiskLevel) {
			if left.OpenHighRiskFindings == right.OpenHighRiskFindings {
				return left.Target < right.Target
			}
			return left.OpenHighRiskFindings > right.OpenHighRiskFindings
		}
		return campaignRiskRank(left.RiskLevel) > campaignRiskRank(right.RiskLevel)
	})

	report.TotalTargets = len(report.Targets)
	report.ResultCount = len(report.Targets)

	return report, nil
}

func campaignReportFiltersFromQuery(c *fiber.Ctx) (CampaignReportFilters, error) {
	return normalizeCampaignReportFilters(
		splitCampaignFilterValues(c.Query("risk")),
		splitCampaignFilterValues(c.Query("status")),
		splitCampaignFilterValues(c.Query("trigger")),
		c.Query("preset"),
	)
}

func campaignReportSortFromQuery(c *fiber.Ctx) (CampaignReportSort, error) {
	return normalizeCampaignReportSort(c.Query("sort_by"), c.Query("sort_order"))
}

func resolveCampaignReportQueryOptions(c *fiber.Ctx, campaign *database.Campaign) (CampaignReportFilters, CampaignReportSort, string, string, int, error) {
	profileApplied := strings.TrimSpace(c.Query("profile"))
	profileFilters := CampaignReportFilters{}
	profileSort := CampaignReportSort{}
	profileFormat := ""
	if profileApplied != "" {
		profile, err := database.GetCampaignReportProfile(campaign, profileApplied)
		if err != nil {
			statusCode := fiber.StatusInternalServerError
			switch {
			case errors.Is(err, database.ErrCampaignReportProfileNotFound):
				statusCode = fiber.StatusNotFound
			case strings.Contains(err.Error(), "invalid"):
				statusCode = fiber.StatusBadRequest
			}
			return CampaignReportFilters{}, CampaignReportSort{}, "", "", statusCode, err
		}
		profileApplied = profile.Name
		profileFilters = CampaignReportFilters{
			RiskLevels:   append([]string(nil), profile.Filters.RiskLevels...),
			Statuses:     append([]string(nil), profile.Filters.Statuses...),
			TriggerTypes: append([]string(nil), profile.Filters.TriggerTypes...),
			Preset:       profile.Filters.Preset,
		}
		profileSort = CampaignReportSort{
			By:    profile.Sort.By,
			Order: profile.Sort.Order,
		}
		profileFormat = profile.Format
	}

	if raw := c.Query("risk"); strings.TrimSpace(raw) != "" {
		profileFilters.RiskLevels = splitCampaignFilterValues(raw)
	}
	if raw := c.Query("status"); strings.TrimSpace(raw) != "" {
		profileFilters.Statuses = splitCampaignFilterValues(raw)
	}
	if raw := c.Query("trigger"); strings.TrimSpace(raw) != "" {
		profileFilters.TriggerTypes = splitCampaignFilterValues(raw)
	}
	if raw := c.Query("preset"); strings.TrimSpace(raw) != "" {
		profileFilters.Preset = raw
	}
	filters, err := normalizeCampaignReportFilters(profileFilters.RiskLevels, profileFilters.Statuses, profileFilters.TriggerTypes, profileFilters.Preset)
	if err != nil {
		return CampaignReportFilters{}, CampaignReportSort{}, "", "", fiber.StatusBadRequest, err
	}

	sortBy := profileSort.By
	sortOrder := profileSort.Order
	if raw := c.Query("sort_by"); strings.TrimSpace(raw) != "" {
		sortBy = raw
	}
	if raw := c.Query("sort_order"); strings.TrimSpace(raw) != "" {
		sortOrder = raw
	}
	sortSpec, err := normalizeCampaignReportSort(sortBy, sortOrder)
	if err != nil {
		return CampaignReportFilters{}, CampaignReportSort{}, "", "", fiber.StatusBadRequest, err
	}

	format := strings.TrimSpace(profileFormat)
	if raw := c.Query("format"); strings.TrimSpace(raw) != "" {
		format = strings.TrimSpace(raw)
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "" && format != "csv" && format != "json" {
		return CampaignReportFilters{}, CampaignReportSort{}, "", "", fiber.StatusBadRequest, fmt.Errorf("unsupported campaign export format: %s", format)
	}
	return filters, sortSpec, format, profileApplied, fiber.StatusOK, nil
}

func campaignReportPageFromQuery(c *fiber.Ctx) (int, int) {
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "0"))
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if limit > 1000 {
		limit = 1000
	}
	return offset, limit
}

func normalizeCampaignReportFilters(riskLevels, statuses, triggerTypes []string, preset string) (CampaignReportFilters, error) {
	filters := CampaignReportFilters{
		RiskLevels:   normalizeCampaignFilterList(riskLevels),
		Statuses:     normalizeCampaignFilterList(statuses),
		TriggerTypes: normalizeCampaignFilterList(triggerTypes),
		Preset:       normalizeCampaignReportPreset(preset),
	}
	if filters.Preset == "" || filters.Preset == "high-risk" || filters.Preset == "recovered" || filters.Preset == "failed" {
		return filters, nil
	}
	return CampaignReportFilters{}, fmt.Errorf("unsupported campaign report preset: %s", preset)
}

func splitCampaignFilterValues(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func normalizeCampaignFilterList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var normalized []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.ToLower(strings.TrimSpace(item))
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			normalized = append(normalized, item)
		}
	}
	return normalized
}

func normalizeCampaignReportPreset(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func normalizeCampaignReportSort(by, order string) (CampaignReportSort, error) {
	sortSpec := CampaignReportSort{
		By:    normalizeCampaignReportSortBy(by),
		Order: normalizeCampaignReportSortOrder(by, order),
	}
	switch sortSpec.By {
	case "risk", "target", "latest_run", "open_high_risk":
	default:
		return CampaignReportSort{}, fmt.Errorf("unsupported campaign report sort_by: %s", by)
	}
	if sortSpec.Order != "asc" && sortSpec.Order != "desc" {
		return CampaignReportSort{}, fmt.Errorf("unsupported campaign report sort_order: %s", order)
	}
	return sortSpec, nil
}

func normalizeCampaignReportSortBy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "", "risk", "target", "latest_run", "latest_run_at", "open_high_risk", "open_high_risk_findings":
	default:
		return value
	}
	switch value {
	case "", "risk":
		return "risk"
	case "latest_run_at":
		return "latest_run"
	case "open_high_risk_findings":
		return "open_high_risk"
	default:
		return value
	}
}

func normalizeCampaignReportSortOrder(by, order string) string {
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "asc" || order == "desc" {
		return order
	}
	switch normalizeCampaignReportSortBy(by) {
	case "target":
		return "asc"
	default:
		return "desc"
	}
}

func applyCampaignReportFilters(report *CampaignReportResponse, campaign *database.Campaign, filters CampaignReportFilters) *CampaignReportResponse {
	if report == nil {
		return nil
	}

	filtered := *report
	filtered.FiltersApplied = filters
	filtered.TotalTargets = len(report.Targets)
	filtered.ResultCount = len(report.Targets)

	if len(filters.RiskLevels) == 0 && len(filters.Statuses) == 0 && len(filters.TriggerTypes) == 0 && filters.Preset == "" {
		return &filtered
	}

	filtered.Targets = nil
	for _, row := range report.Targets {
		if !campaignReportRowMatchesFilters(campaign, row, filters) {
			continue
		}
		filtered.Targets = append(filtered.Targets, row)
	}
	filtered.ResultCount = len(filtered.Targets)
	return &filtered
}

func sortCampaignReport(report *CampaignReportResponse, sortSpec CampaignReportSort) *CampaignReportResponse {
	if report == nil {
		return nil
	}

	sorted := *report
	sorted.SortApplied = sortSpec
	sorted.Targets = append([]CampaignReportTargetRow(nil), report.Targets...)
	sort.SliceStable(sorted.Targets, func(i, j int) bool {
		return campaignReportLess(sorted.Targets[i], sorted.Targets[j], sortSpec)
	})
	return &sorted
}

func paginateCampaignReport(report *CampaignReportResponse, offset, limit int) *CampaignReportResponse {
	if report == nil {
		return nil
	}

	totalMatched := len(report.Targets)
	if offset < 0 {
		offset = 0
	}
	if offset > totalMatched {
		offset = totalMatched
	}
	end := totalMatched
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	paginated := *report
	paginated.Targets = append([]CampaignReportTargetRow(nil), report.Targets[offset:end]...)
	paginated.Pagination = CampaignReportPagination{
		Offset:        offset,
		Limit:         limit,
		ReturnedCount: len(paginated.Targets),
		HasMore:       end < totalMatched,
	}
	return &paginated
}

func campaignReportLess(left, right CampaignReportTargetRow, sortSpec CampaignReportSort) bool {
	desc := sortSpec.Order == "desc"
	switch sortSpec.By {
	case "target":
		if left.Target == right.Target {
			return campaignReportDefaultLess(left, right)
		}
		return campaignCompareString(left.Target, right.Target, desc)
	case "latest_run":
		if left.LatestRunAt.Equal(right.LatestRunAt) {
			return campaignReportDefaultLess(left, right)
		}
		return campaignCompareTime(left.LatestRunAt, right.LatestRunAt, desc)
	case "open_high_risk":
		if left.OpenHighRiskFindings == right.OpenHighRiskFindings {
			return campaignReportDefaultLess(left, right)
		}
		return campaignCompareInt(left.OpenHighRiskFindings, right.OpenHighRiskFindings, desc)
	case "risk":
		fallthrough
	default:
		return campaignReportDefaultLessWithOrder(left, right, desc)
	}
}

func campaignReportDefaultLess(left, right CampaignReportTargetRow) bool {
	return campaignReportDefaultLessWithOrder(left, right, true)
}

func campaignReportDefaultLessWithOrder(left, right CampaignReportTargetRow, desc bool) bool {
	leftRisk := campaignRiskRank(left.RiskLevel)
	rightRisk := campaignRiskRank(right.RiskLevel)
	if leftRisk != rightRisk {
		return campaignCompareInt(leftRisk, rightRisk, desc)
	}
	if left.OpenHighRiskFindings != right.OpenHighRiskFindings {
		return campaignCompareInt(left.OpenHighRiskFindings, right.OpenHighRiskFindings, desc)
	}
	if !left.LatestRunAt.Equal(right.LatestRunAt) {
		return campaignCompareTime(left.LatestRunAt, right.LatestRunAt, desc)
	}
	return left.Target < right.Target
}

func campaignCompareString(left, right string, desc bool) bool {
	if desc {
		return left > right
	}
	return left < right
}

func campaignCompareInt(left, right int, desc bool) bool {
	if desc {
		return left > right
	}
	return left < right
}

func campaignCompareTime(left, right time.Time, desc bool) bool {
	if desc {
		return left.After(right)
	}
	return left.Before(right)
}

func campaignReportRowMatchesFilters(campaign *database.Campaign, row CampaignReportTargetRow, filters CampaignReportFilters) bool {
	if len(filters.RiskLevels) > 0 && !campaignFilterContains(filters.RiskLevels, row.RiskLevel) {
		return false
	}
	if len(filters.Statuses) > 0 && !campaignFilterContains(filters.Statuses, row.Status) {
		return false
	}
	if len(filters.TriggerTypes) > 0 && !campaignFilterContains(filters.TriggerTypes, row.LatestTriggerType) {
		return false
	}

	switch filters.Preset {
	case "":
		return true
	case "high-risk":
		return campaignReportRowIsHighRisk(campaign, row)
	case "recovered":
		return row.Recovered
	case "failed":
		return strings.EqualFold(row.Status, "failed")
	default:
		return false
	}
}

func campaignFilterContains(values []string, actual string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	for _, value := range values {
		if actual == strings.ToLower(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func campaignReportRowIsHighRisk(campaign *database.Campaign, row CampaignReportTargetRow) bool {
	for _, severity := range normalizeHighRiskSeverities(campaign.HighRiskSeverities) {
		if strings.EqualFold(strings.TrimSpace(severity), row.RiskLevel) {
			return true
		}
	}
	return false
}

func deriveCampaignRiskLevel(summary map[string]int, attackChainSummary *database.AttackChainWorkspaceSummary) string {
	switch {
	case summary["critical"] > 0:
		return "critical"
	case attackChainSummary != nil && attackChainSummary.CriticalChains > 0 && attackChainSummary.OperationalHits > 0:
		return "critical"
	case summary["high"] > 0:
		return "high"
	case attackChainSummary != nil && attackChainSummary.HighImpactChains > 0 && attackChainSummary.OperationalHits > 0:
		return "high"
	case summary["medium"] > 0:
		return "medium"
	case summary["low"] > 0:
		return "low"
	default:
		return "none"
	}
}

func campaignTargetIsHighRisk(campaign *database.Campaign, summary map[string]int, attackChainSummary *database.AttackChainWorkspaceSummary) bool {
	for _, severity := range normalizeHighRiskSeverities(campaign.HighRiskSeverities) {
		if summary[severity] > 0 {
			return true
		}
		if attackChainSummary == nil || attackChainSummary.OperationalHits <= 0 {
			continue
		}
		switch severity {
		case "critical":
			if attackChainSummary.CriticalChains > 0 {
				return true
			}
		case "high":
			if attackChainSummary.HighImpactChains > 0 {
				return true
			}
		}
	}
	return false
}

func queueCampaignDeepScanRuns(ctx context.Context, campaign *database.Campaign) (int, []string, error) {
	if strings.TrimSpace(campaign.DeepScanWorkflow) == "" {
		return 0, nil, fmt.Errorf("campaign has no deep scan workflow configured")
	}
	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	if err != nil {
		return 0, nil, err
	}

	var queued int
	var targets []string
	for _, run := range latestCampaignRuns(runs) {
		if run.TriggerType == "campaign-deep-scan" {
			continue
		}
		vulnSummary, _ := database.GetActiveVulnerabilitySummaryForTarget(ctx, run.Workspace, run.Target)
		attackChainSummary, _ := attackchain.GetTargetSummary(ctx, run.Workspace, run.Target)
		if !campaignTargetIsHighRisk(campaign, vulnSummary, attackChainSummary) {
			continue
		}
		exists, err := database.HasCampaignDeepScanRun(ctx, campaign.ID, run.Workspace, campaign.DeepScanWorkflow, run.Target)
		if err != nil || exists {
			continue
		}

		params := cloneRunParams(run.Params)
		params["campaign_stage"] = "deep_scan"
		params["campaign_source_run_uuid"] = run.RunUUID

		deepScanRun := &database.Run{
			RunUUID:      uuid.New().String(),
			WorkflowName: campaign.DeepScanWorkflow,
			WorkflowKind: normalizeCampaignWorkflowKind(campaign.DeepScanWorkflowKind),
			Target:       run.Target,
			Params:       params,
			Status:       "queued",
			TriggerType:  "campaign-deep-scan",
			RunGroupID:   campaign.ID,
			RunPriority:  "critical",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    run.Workspace,
		}
		if err := database.CreateRun(ctx, deepScanRun); err != nil {
			continue
		}
		queued++
		targets = append(targets, run.Target)
	}
	return queued, deduplicateTargets(targets), nil
}

func renderCampaignReportCSV(report *CampaignReportResponse) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)

	header := []string{
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
	}
	if err := writer.Write(header); err != nil {
		return nil, err
	}

	for _, target := range report.Targets {
		row := []string{
			target.Target,
			target.Workspace,
			target.Status,
			target.RiskLevel,
			target.LatestRunUUID,
			target.LatestTriggerType,
			formatCampaignCSVTime(target.LatestRunAt),
			strconv.FormatBool(target.DeepScanQueued),
			strconv.Itoa(target.TotalRuns),
			strconv.Itoa(target.RerunRuns),
			strconv.Itoa(target.DeepScanRuns),
			strconv.FormatBool(target.Recovered),
			strconv.Itoa(target.CriticalFindings),
			strconv.Itoa(target.HighFindings),
			strconv.Itoa(target.MediumFindings),
			strconv.Itoa(target.LowFindings),
			strconv.Itoa(target.OpenHighRiskFindings),
			strconv.Itoa(target.AttackChainOperationalHits),
			strconv.Itoa(target.AttackChainVerifiedHits),
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func latestCampaignRuns(runs []*database.Run) map[string]*database.Run {
	latestRuns := make(map[string]*database.Run)
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			continue
		}
		key := run.Workspace + "::" + run.Target
		existing := latestRuns[key]
		if existing == nil || run.CreatedAt.After(existing.CreatedAt) {
			latestRuns[key] = run
		}
	}
	return latestRuns
}

func campaignTargetKey(workspace, target string) string {
	return strings.TrimSpace(workspace) + "::" + strings.TrimSpace(target)
}

func latestCampaignTargetRecovered(runs []*database.Run, key string, latest *database.Run) bool {
	if latest == nil || latest.Status != "completed" {
		return false
	}

	hadFailure := false
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			continue
		}
		if campaignTargetKey(run.Workspace, run.Target) != key {
			continue
		}
		if run.Status == "failed" && run.CreatedAt.Before(latest.CreatedAt) {
			hadFailure = true
		}
	}
	return hadFailure
}

func updateCampaignRowTime(target **time.Time, value time.Time) {
	if value.IsZero() {
		return
	}
	if *target == nil || value.After(**target) {
		copyValue := value
		*target = &copyValue
	}
}

func campaignRiskRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "none":
		return 1
	default:
		return 0
	}
}

func formatCampaignCSVTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func cloneRunParams(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	clone := make(map[string]interface{}, len(input))
	for key, value := range input {
		clone[key] = value
	}
	return clone
}
