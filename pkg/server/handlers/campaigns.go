package handlers

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
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
	Target         string         `json:"target"`
	Workspace      string         `json:"workspace"`
	Status         string         `json:"status"`
	RiskLevel      string         `json:"risk_level"`
	VulnSummary    map[string]int `json:"vuln_summary"`
	DeepScanQueued bool           `json:"deep_scan_queued"`
	RunUUID        string         `json:"run_uuid"`
}

// CampaignStatusResponse returns an aggregated campaign view.
type CampaignStatusResponse struct {
	Campaign        *database.Campaign     `json:"campaign"`
	Status          string                 `json:"status"`
	Progress        JobProgress            `json:"progress"`
	Targets         []CampaignTargetStatus `json:"targets"`
	HighRiskTargets []string               `json:"high_risk_targets"`
	Runs            []*database.Run        `json:"runs"`
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
		for i := range result.Data {
			campaign := result.Data[i]
			if response, err := buildCampaignStatusResponse(ctx, &campaign); err == nil && response.Campaign != nil {
				result.Data[i].Status = response.Campaign.Status
				result.Data[i].UpdatedAt = response.Campaign.UpdatedAt
			}
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

		var queued int
		for _, run := range runs {
			if run.Status != "failed" {
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

	progress := JobProgress{Total: len(latestRuns)}
	var targets []CampaignTargetStatus
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

		vulnSummary, _ := database.GetVulnerabilitySummary(ctx, run.Workspace)
		riskLevel := deriveCampaignRiskLevel(vulnSummary)
		deepScanQueued, _ := database.HasCampaignDeepScanRun(ctx, campaign.ID, run.Workspace, campaign.DeepScanWorkflow)
		targets = append(targets, CampaignTargetStatus{
			Target:         run.Target,
			Workspace:      run.Workspace,
			Status:         run.Status,
			RiskLevel:      riskLevel,
			VulnSummary:    vulnSummary,
			DeepScanQueued: deepScanQueued,
			RunUUID:        run.RunUUID,
		})
		if campaignTargetIsHighRisk(campaign, vulnSummary) {
			highRiskTargets = append(highRiskTargets, run.Target)
		}
	}

	status := aggregateStatus(progress)
	_ = database.UpdateCampaignStatus(ctx, campaign.ID, status)
	campaign.Status = status

	return &CampaignStatusResponse{
		Campaign:        campaign,
		Status:          status,
		Progress:        progress,
		Targets:         targets,
		HighRiskTargets: deduplicateTargets(highRiskTargets),
		Runs:            runs,
	}, nil
}

func deriveCampaignRiskLevel(summary map[string]int) string {
	switch {
	case summary["critical"] > 0:
		return "critical"
	case summary["high"] > 0:
		return "high"
	case summary["medium"] > 0:
		return "medium"
	case summary["low"] > 0:
		return "low"
	default:
		return "none"
	}
}

func campaignTargetIsHighRisk(campaign *database.Campaign, summary map[string]int) bool {
	for _, severity := range normalizeHighRiskSeverities(campaign.HighRiskSeverities) {
		if summary[severity] > 0 {
			return true
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
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			continue
		}
		vulnSummary, _ := database.GetVulnerabilitySummary(ctx, run.Workspace)
		if !campaignTargetIsHighRisk(campaign, vulnSummary) {
			continue
		}
		exists, err := database.HasCampaignDeepScanRun(ctx, campaign.ID, run.Workspace, campaign.DeepScanWorkflow)
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
