package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/logger"
	"github.com/j3ssie/osmedeus/v5/internal/parser"
	"github.com/j3ssie/osmedeus/v5/internal/terminal"
	"github.com/spf13/cobra"
)

var (
	campaignName                 string
	campaignFlow                 string
	campaignModule               string
	campaignTargets              []string
	campaignTargetFile           string
	campaignParams               []string
	campaignRole                 string
	campaignSkills               []string
	campaignStrategy             string
	campaignPriority             string
	campaignDeepScanWorkflow     string
	campaignDeepScanWorkflowKind string
	campaignAutoDeepScan         bool
	campaignHighRiskSeverities   []string
	campaignNotes                string
	campaignStatusFilter         string
	campaignOffset               int
	campaignLimit                int
)

var campaignCmd = &cobra.Command{
	Use:   "campaign",
	Short: "Manage queued campaign batches",
}

var campaignCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a campaign batch and queue its runs",
	RunE:  runCampaignCreate,
}

var campaignListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List campaigns",
	RunE:    runCampaignList,
}

var campaignStatusCmd = &cobra.Command{
	Use:   "status <campaign-id>",
	Short: "Show campaign status and target-level progress",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignStatus,
}

var campaignDeepScanCmd = &cobra.Command{
	Use:   "deep-scan <campaign-id>",
	Short: "Queue deep-scan runs for current high-risk campaign targets",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignDeepScan,
}

var campaignRerunFailedCmd = &cobra.Command{
	Use:   "rerun-failed <campaign-id>",
	Short: "Queue failed campaign targets again",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignRerunFailed,
}

func init() {
	campaignCreateCmd.Flags().StringVar(&campaignName, "name", "", "campaign name")
	campaignCreateCmd.Flags().StringVarP(&campaignFlow, "flow", "f", "", "flow workflow name")
	campaignCreateCmd.Flags().StringVarP(&campaignModule, "module", "m", "", "module workflow name")
	campaignCreateCmd.Flags().StringArrayVarP(&campaignTargets, "target", "t", nil, "target(s) to queue")
	campaignCreateCmd.Flags().StringVarP(&campaignTargetFile, "target-file", "T", "", "file containing campaign targets")
	campaignCreateCmd.Flags().StringArrayVarP(&campaignParams, "params", "p", nil, "additional parameters (key=value)")
	campaignCreateCmd.Flags().StringVar(&campaignRole, "role", "", "campaign role label")
	campaignCreateCmd.Flags().StringArrayVar(&campaignSkills, "skills", nil, "campaign skills (repeatable)")
	campaignCreateCmd.Flags().StringVar(&campaignStrategy, "strategy", "", "campaign strategy label")
	campaignCreateCmd.Flags().StringVar(&campaignPriority, "priority", "high", "campaign priority (low, normal, high, critical)")
	campaignCreateCmd.Flags().StringVar(&campaignDeepScanWorkflow, "deep-scan-workflow", "", "workflow used for deep-scan follow-up")
	campaignCreateCmd.Flags().StringVar(&campaignDeepScanWorkflowKind, "deep-scan-workflow-kind", "flow", "deep-scan workflow kind (flow or module)")
	campaignCreateCmd.Flags().BoolVar(&campaignAutoDeepScan, "auto-deep-scan", false, "auto-queue deep scans for high-risk targets")
	campaignCreateCmd.Flags().StringArrayVar(&campaignHighRiskSeverities, "high-risk-severity", nil, "severity values considered high risk (repeatable)")
	campaignCreateCmd.Flags().StringVar(&campaignNotes, "notes", "", "optional campaign notes")

	campaignListCmd.Flags().StringVar(&campaignStatusFilter, "status", "", "filter by campaign status")
	campaignListCmd.Flags().IntVar(&campaignOffset, "offset", 0, "pagination offset")
	campaignListCmd.Flags().IntVar(&campaignLimit, "limit", 20, "pagination limit")

	campaignCmd.AddCommand(campaignCreateCmd)
	campaignCmd.AddCommand(campaignListCmd)
	campaignCmd.AddCommand(campaignStatusCmd)
	campaignCmd.AddCommand(campaignDeepScanCmd)
	campaignCmd.AddCommand(campaignRerunFailedCmd)
	rootCmd.AddCommand(campaignCmd)
}

func runCampaignCreate(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
	}

	cfg := config.Get()
	if cfg == nil {
		return errConfigNotLoaded
	}

	loader := parser.NewLoader(cfg.WorkflowsPath)
	workflowName, workflowKind := resolveCampaignWorkflowCLI(campaignFlow, campaignModule)
	if workflowName == "" {
		return fmt.Errorf("workflow name required (use -f or -m)")
	}

	if _, err := loader.LoadWorkflow(workflowName); err != nil {
		return fmt.Errorf("workflow not found: %w", err)
	}
	if strings.TrimSpace(campaignDeepScanWorkflow) != "" {
		if _, err := loader.LoadWorkflow(campaignDeepScanWorkflow); err != nil {
			return fmt.Errorf("deep scan workflow not found: %w", err)
		}
	}

	targets, err := collectCampaignTargetsCLI(campaignTargets, campaignTargetFile)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}

	paramsInterface := make(map[string]interface{})
	paramsString := make(map[string]string)
	for _, flag := range campaignParams {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		paramsInterface[key] = value
		paramsString[key] = value
	}
	if strings.TrimSpace(campaignRole) != "" {
		paramsInterface["campaign_role"] = campaignRole
	}
	if len(campaignSkills) > 0 {
		paramsInterface["campaign_skills"] = strings.Join(campaignSkills, ",")
	}
	if strings.TrimSpace(campaignStrategy) != "" {
		paramsInterface["campaign_strategy"] = campaignStrategy
	}

	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	printer := terminal.NewPrinter()
	log := logger.Get()

	campaignID := uuid.NewString()
	name := strings.TrimSpace(campaignName)
	if name == "" {
		name = fmt.Sprintf("%s-%s", workflowName, time.Now().Format("20060102-150405"))
	}

	campaign := &database.Campaign{
		ID:                   campaignID,
		Name:                 name,
		WorkflowName:         workflowName,
		WorkflowKind:         workflowKind,
		Status:               "queued",
		Role:                 campaignRole,
		Strategy:             campaignStrategy,
		Skills:               campaignSkills,
		Params:               paramsInterface,
		DeepScanWorkflow:     strings.TrimSpace(campaignDeepScanWorkflow),
		DeepScanWorkflowKind: normalizeCampaignWorkflowKindCLI(campaignDeepScanWorkflowKind),
		AutoDeepScan:         campaignAutoDeepScan,
		HighRiskSeverities:   normalizeHighRiskSeveritiesCLI(campaignHighRiskSeverities),
		TargetCount:          len(targets),
		Notes:                campaignNotes,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	if err := database.CreateCampaign(ctx, campaign); err != nil {
		return err
	}

	priority := normalizeCampaignPriorityCLI(campaignPriority)
	var queued []string
	for _, target := range targets {
		run := &database.Run{
			RunUUID:       uuid.NewString(),
			WorkflowName:  workflowName,
			WorkflowKind:  workflowKind,
			Target:        target,
			Params:        paramsInterface,
			Status:        "queued",
			TriggerType:   "campaign",
			RunGroupID:    campaignID,
			RunPriority:   priority,
			RunMode:       "queue",
			IsQueued:      true,
			Workspace:     computeWorkspace(target, paramsString),
			InputIsFile:   strings.TrimSpace(campaignTargetFile) != "",
			InputFilePath: normalizeCampaignInputFileCLI(campaignTargetFile),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := database.CreateRun(ctx, run); err != nil {
			continue
		}
		queued = append(queued, run.RunUUID)
		pushQueuedRunToRedis(ctx, cfg, run, printer, log)
	}

	if len(queued) == 0 {
		return fmt.Errorf("failed to queue campaign runs")
	}

	if globalJSON {
		resp := map[string]interface{}{
			"message":      "Campaign created",
			"campaign_id":  campaignID,
			"queued_runs":  len(queued),
			"target_count": len(targets),
			"data":         campaign,
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	printer.Success("Campaign created")
	fmt.Printf("Campaign ID: %s\n", campaignID)
	fmt.Printf("Name:        %s\n", campaign.Name)
	fmt.Printf("Workflow:    %s (%s)\n", workflowName, workflowKind)
	fmt.Printf("Targets:     %d\n", len(targets))
	fmt.Printf("Queued Runs: %d\n", len(queued))
	return nil
}

func runCampaignList(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	result, err := database.ListCampaigns(ctx, database.CampaignQuery{
		Status: campaignStatusFilter,
		Offset: campaignOffset,
		Limit:  campaignLimit,
	})
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	printer := terminal.NewPrinter()
	if len(result.Data) == 0 {
		printer.Info("No campaigns found")
		return nil
	}

	printer.Section("Campaigns")
	for _, campaign := range result.Data {
		fmt.Printf("[%s] %s\n", campaign.ID, campaign.Name)
		fmt.Printf("  Workflow: %s (%s)\n", campaign.WorkflowName, campaign.WorkflowKind)
		fmt.Printf("  Status:   %s\n", campaign.Status)
		fmt.Printf("  Targets:  %d\n", campaign.TargetCount)
		fmt.Printf("  Updated:  %s\n\n", campaign.UpdatedAt.Format("2006-01-02 15:04:05"))
	}
	fmt.Printf("Total: %d\n", result.TotalCount)
	return nil
}

func runCampaignStatus(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	campaign, err := database.GetCampaignByID(ctx, args[0])
	if err != nil {
		return err
	}

	response, err := buildCampaignStatusResponseCLI(ctx, campaign)
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(response)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Campaign: %s\n", response.Campaign.Name)
	fmt.Printf("ID:       %s\n", response.Campaign.ID)
	fmt.Printf("Workflow: %s (%s)\n", response.Campaign.WorkflowName, response.Campaign.WorkflowKind)
	fmt.Printf("Status:   %s\n", response.Status)
	fmt.Printf("Progress: total=%d pending=%d running=%d completed=%d failed=%d\n",
		response.Progress.Total, response.Progress.Pending, response.Progress.Running, response.Progress.Completed, response.Progress.Failed)
	if len(response.HighRiskTargets) > 0 {
		fmt.Printf("High Risk Targets: %s\n", strings.Join(response.HighRiskTargets, ", "))
	}
	fmt.Println("")

	sort.Slice(response.Targets, func(i, j int) bool {
		if response.Targets[i].RiskLevel == response.Targets[j].RiskLevel {
			return response.Targets[i].Target < response.Targets[j].Target
		}
		return response.Targets[i].RiskLevel > response.Targets[j].RiskLevel
	})
	for _, target := range response.Targets {
		fmt.Printf("- %s\n", target.Target)
		fmt.Printf("  Workspace: %s\n", target.Workspace)
		fmt.Printf("  Status: %s | Risk: %s | DeepScanQueued: %t\n", target.Status, target.RiskLevel, target.DeepScanQueued)
		fmt.Printf("  Vulns: critical=%d high=%d medium=%d low=%d | RunUUID: %s\n",
			target.VulnSummary["critical"], target.VulnSummary["high"], target.VulnSummary["medium"], target.VulnSummary["low"], target.RunUUID)
	}
	return nil
}

func runCampaignDeepScan(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
	}
	cfg := config.Get()
	if cfg == nil {
		return errConfigNotLoaded
	}
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	campaign, err := database.GetCampaignByID(ctx, args[0])
	if err != nil {
		return err
	}

	queued, targets, err := queueCampaignDeepScanRunsCLI(ctx, cfg, campaign)
	if err != nil {
		return err
	}

	if globalJSON {
		payload := map[string]interface{}{
			"message":      "Deep scan evaluation completed",
			"campaign_id":  campaign.ID,
			"queued_count": queued,
			"targets":      targets,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Campaign ID: %s\n", campaign.ID)
	fmt.Printf("Queued deep scans: %d\n", queued)
	if len(targets) > 0 {
		fmt.Printf("Targets: %s\n", strings.Join(targets, ", "))
	}
	return nil
}

func runCampaignRerunFailed(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
	}
	cfg := config.Get()
	if cfg == nil {
		return errConfigNotLoaded
	}
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	campaign, err := database.GetCampaignByID(ctx, args[0])
	if err != nil {
		return err
	}

	queued, targets, err := queueCampaignRerunFailedRunsCLI(ctx, cfg, campaign)
	if err != nil {
		return err
	}

	if globalJSON {
		payload := map[string]interface{}{
			"message":      "Failed targets queued for rerun",
			"campaign_id":  campaign.ID,
			"queued_count": queued,
			"targets":      targets,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Campaign ID: %s\n", campaign.ID)
	fmt.Printf("Queued reruns: %d\n", queued)
	if len(targets) > 0 {
		fmt.Printf("Targets: %s\n", strings.Join(targets, ", "))
	}
	return nil
}

func resolveCampaignWorkflowCLI(flow, module string) (string, string) {
	if strings.TrimSpace(flow) != "" {
		return strings.TrimSpace(flow), "flow"
	}
	if strings.TrimSpace(module) != "" {
		return strings.TrimSpace(module), "module"
	}
	return "", ""
}

func normalizeCampaignWorkflowKindCLI(kind string) string {
	if strings.TrimSpace(strings.ToLower(kind)) == "flow" {
		return "flow"
	}
	return "module"
}

func normalizeCampaignPriorityCLI(priority string) string {
	switch strings.TrimSpace(strings.ToLower(priority)) {
	case "low", "normal", "high", "critical":
		return strings.TrimSpace(strings.ToLower(priority))
	default:
		return "high"
	}
}

func normalizeHighRiskSeveritiesCLI(values []string) []string {
	if len(values) == 0 {
		return []string{"critical", "high"}
	}
	seen := make(map[string]struct{})
	var normalized []string
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

func collectCampaignTargetsCLI(targets []string, targetFile string) ([]string, error) {
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

func normalizeCampaignInputFileCLI(targetFile string) string {
	if strings.TrimSpace(targetFile) == "" {
		return ""
	}
	absPath, err := filepath.Abs(targetFile)
	if err != nil {
		return targetFile
	}
	return absPath
}

type campaignJobProgress struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type campaignTargetStatusCLI struct {
	Target             string                                `json:"target"`
	Workspace          string                                `json:"workspace"`
	Status             string                                `json:"status"`
	RiskLevel          string                                `json:"risk_level"`
	VulnSummary        map[string]int                        `json:"vuln_summary"`
	AttackChainSummary *database.AttackChainWorkspaceSummary `json:"attack_chain_summary,omitempty"`
	DeepScanQueued     bool                                  `json:"deep_scan_queued"`
	RunUUID            string                                `json:"run_uuid"`
}

type campaignStatusResponseCLI struct {
	Campaign        *database.Campaign        `json:"campaign"`
	Status          string                    `json:"status"`
	Progress        campaignJobProgress       `json:"progress"`
	Summary         campaignSignalSummaryCLI  `json:"summary"`
	Targets         []campaignTargetStatusCLI `json:"targets"`
	HighRiskTargets []string                  `json:"high_risk_targets"`
	Runs            []*database.Run           `json:"runs"`
}

type campaignSignalSummaryCLI struct {
	TargetsTotal               int `json:"targets_total"`
	HighRiskTargets            int `json:"high_risk_targets"`
	DeepScanQueuedTargets      int `json:"deep_scan_queued_targets"`
	AttackChainAwareTargets    int `json:"attack_chain_aware_targets"`
	VerifiedAttackChainTargets int `json:"verified_attack_chain_targets"`
}

func buildCampaignStatusResponseCLI(ctx context.Context, campaign *database.Campaign) (*campaignStatusResponseCLI, error) {
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

	progress := campaignJobProgress{Total: len(latestRuns)}
	var (
		targets []campaignTargetStatusCLI
		summary campaignSignalSummaryCLI
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

		vulnSummary, _ := database.GetVulnerabilitySummary(ctx, run.Workspace)
		attackChainSummary, _ := database.GetAttackChainWorkspaceSummary(ctx, run.Workspace)
		riskLevel := deriveCampaignRiskLevelCLI(vulnSummary, attackChainSummary)
		deepScanQueued, _ := database.HasCampaignDeepScanRun(ctx, campaign.ID, run.Workspace, campaign.DeepScanWorkflow)
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
		targets = append(targets, campaignTargetStatusCLI{
			Target:             run.Target,
			Workspace:          run.Workspace,
			Status:             run.Status,
			RiskLevel:          riskLevel,
			VulnSummary:        vulnSummary,
			AttackChainSummary: attackChainSummary,
			DeepScanQueued:     deepScanQueued,
			RunUUID:            run.RunUUID,
		})
		if campaignTargetIsHighRiskCLI(campaign, vulnSummary, attackChainSummary) {
			highRiskTargets = append(highRiskTargets, run.Target)
		}
	}
	summary.HighRiskTargets = len(deduplicateTargets(highRiskTargets))

	status := aggregateCampaignStatusCLI(progress)
	_ = database.UpdateCampaignStatus(ctx, campaign.ID, status)
	campaign.Status = status

	return &campaignStatusResponseCLI{
		Campaign:        campaign,
		Status:          status,
		Progress:        progress,
		Summary:         summary,
		Targets:         targets,
		HighRiskTargets: deduplicateTargets(highRiskTargets),
		Runs:            runs,
	}, nil
}

func deriveCampaignRiskLevelCLI(summary map[string]int, attackChainSummary *database.AttackChainWorkspaceSummary) string {
	switch {
	case summary["critical"] > 0:
		return "critical"
	case attackChainSummary != nil && attackChainSummary.CriticalChains > 0 && attackChainSummary.VerifiedHits > 0:
		return "critical"
	case summary["high"] > 0:
		return "high"
	case attackChainSummary != nil && attackChainSummary.HighImpactChains > 0 && attackChainSummary.VerifiedHits > 0:
		return "high"
	case summary["medium"] > 0:
		return "medium"
	case summary["low"] > 0:
		return "low"
	default:
		return "none"
	}
}

func campaignTargetIsHighRiskCLI(campaign *database.Campaign, summary map[string]int, attackChainSummary *database.AttackChainWorkspaceSummary) bool {
	for _, severity := range normalizeHighRiskSeveritiesCLI(campaign.HighRiskSeverities) {
		if summary[severity] > 0 {
			return true
		}
		if attackChainSummary == nil || attackChainSummary.VerifiedHits <= 0 {
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

func aggregateCampaignStatusCLI(progress campaignJobProgress) string {
	if progress.Total == 0 {
		return "pending"
	}
	if progress.Running > 0 {
		return "running"
	}
	if progress.Pending > 0 && progress.Completed == 0 && progress.Failed == 0 {
		return "pending"
	}
	if progress.Completed == progress.Total {
		return "completed"
	}
	if progress.Failed == progress.Total {
		return "failed"
	}
	if progress.Failed > 0 || progress.Completed > 0 {
		return "partial"
	}
	return "pending"
}

func queueCampaignDeepScanRunsCLI(ctx context.Context, cfg *config.Config, campaign *database.Campaign) (int, []string, error) {
	if strings.TrimSpace(campaign.DeepScanWorkflow) == "" {
		return 0, nil, fmt.Errorf("campaign has no deep scan workflow configured")
	}
	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	if err != nil {
		return 0, nil, err
	}

	printer := terminal.NewPrinter()
	log := logger.Get()

	var queued int
	var targets []string
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			continue
		}
		vulnSummary, _ := database.GetVulnerabilitySummary(ctx, run.Workspace)
		attackChainSummary, _ := database.GetAttackChainWorkspaceSummary(ctx, run.Workspace)
		if !campaignTargetIsHighRiskCLI(campaign, vulnSummary, attackChainSummary) {
			continue
		}
		exists, err := database.HasCampaignDeepScanRun(ctx, campaign.ID, run.Workspace, campaign.DeepScanWorkflow)
		if err != nil || exists {
			continue
		}

		params := cloneRunParamsCLI(run.Params)
		params["campaign_stage"] = "deep_scan"
		params["campaign_source_run_uuid"] = run.RunUUID

		deepScanRun := &database.Run{
			RunUUID:      uuid.NewString(),
			WorkflowName: campaign.DeepScanWorkflow,
			WorkflowKind: normalizeCampaignWorkflowKindCLI(campaign.DeepScanWorkflowKind),
			Target:       run.Target,
			Params:       params,
			Status:       "queued",
			TriggerType:  "campaign-deep-scan",
			RunGroupID:   campaign.ID,
			RunPriority:  "critical",
			RunMode:      "queue",
			IsQueued:     true,
			Workspace:    run.Workspace,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := database.CreateRun(ctx, deepScanRun); err != nil {
			continue
		}
		pushQueuedRunToRedis(ctx, cfg, deepScanRun, printer, log)
		queued++
		targets = append(targets, run.Target)
	}
	return queued, deduplicateTargets(targets), nil
}

func cloneRunParamsCLI(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	clone := make(map[string]interface{}, len(input))
	for key, value := range input {
		clone[key] = value
	}
	return clone
}

func queueCampaignRerunFailedRunsCLI(ctx context.Context, cfg *config.Config, campaign *database.Campaign) (int, []string, error) {
	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	if err != nil {
		return 0, nil, err
	}

	printer := terminal.NewPrinter()
	log := logger.Get()

	var queued int
	var targets []string
	for _, run := range runs {
		if run.Status != "failed" || run.TriggerType == "campaign-rerun" {
			continue
		}
		clone := &database.Run{
			RunUUID:       uuid.NewString(),
			WorkflowName:  run.WorkflowName,
			WorkflowKind:  run.WorkflowKind,
			Target:        run.Target,
			Params:        cloneRunParamsCLI(run.Params),
			Status:        "queued",
			TriggerType:   "campaign-rerun",
			RunGroupID:    campaign.ID,
			RunPriority:   run.RunPriority,
			RunMode:       "queue",
			IsQueued:      true,
			Workspace:     run.Workspace,
			InputIsFile:   run.InputIsFile,
			InputFilePath: run.InputFilePath,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := database.CreateRun(ctx, clone); err != nil {
			continue
		}
		pushQueuedRunToRedis(ctx, cfg, clone, printer, log)
		queued++
		targets = append(targets, run.Target)
	}
	return queued, deduplicateTargets(targets), nil
}
