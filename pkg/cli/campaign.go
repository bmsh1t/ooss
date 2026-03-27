package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/j3ssie/osmedeus/v5/internal/attackchain"
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
	campaignReportRiskLevels     []string
	campaignReportStatuses       []string
	campaignReportTriggerTypes   []string
	campaignReportPreset         string
	campaignReportOffset         int
	campaignReportLimit          int
	campaignReportSortBy         string
	campaignReportSortOrder      string
	campaignReportProfileName    string
	campaignProfileDescription   string
	campaignExportFormat         string
	campaignExportOutput         string
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

var campaignReportCmd = &cobra.Command{
	Use:   "report <campaign-id>",
	Short: "Show campaign analytics, rerun history, and target-level findings",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignReport,
}

var campaignExportCmd = &cobra.Command{
	Use:   "export <campaign-id>",
	Short: "Export campaign analytics as CSV or JSON",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignExport,
}

var campaignProfileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage saved campaign report/export profiles",
}

var campaignProfileListCmd = &cobra.Command{
	Use:   "list <campaign-id>",
	Short: "List saved campaign report/export profiles",
	Args:  cobra.ExactArgs(1),
	RunE:  runCampaignProfileList,
}

var campaignProfileSaveCmd = &cobra.Command{
	Use:   "save <campaign-id> <profile-name>",
	Short: "Save a reusable campaign report/export profile",
	Args:  cobra.ExactArgs(2),
	RunE:  runCampaignProfileSave,
}

var campaignProfileDeleteCmd = &cobra.Command{
	Use:   "delete <campaign-id> <profile-name>",
	Short: "Delete a saved campaign report/export profile",
	Args:  cobra.ExactArgs(2),
	RunE:  runCampaignProfileDelete,
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

	campaignReportCmd.Flags().StringSliceVar(&campaignReportRiskLevels, "risk", nil, "filter target rows by risk level")
	campaignReportCmd.Flags().StringSliceVar(&campaignReportStatuses, "status", nil, "filter target rows by latest status")
	campaignReportCmd.Flags().StringSliceVar(&campaignReportTriggerTypes, "trigger", nil, "filter target rows by latest trigger type")
	campaignReportCmd.Flags().StringVar(&campaignReportPreset, "preset", "", "preset slice (high-risk, recovered, failed)")
	campaignReportCmd.Flags().IntVar(&campaignReportOffset, "offset", 0, "pagination offset after filters")
	campaignReportCmd.Flags().IntVar(&campaignReportLimit, "limit", 0, "pagination limit after filters (0 = all)")
	campaignReportCmd.Flags().StringVar(&campaignReportSortBy, "sort-by", "", "sort target rows by risk, target, latest-run, or open-high-risk")
	campaignReportCmd.Flags().StringVar(&campaignReportSortOrder, "sort-order", "", "sort order (asc or desc)")
	campaignReportCmd.Flags().StringVar(&campaignReportProfileName, "profile", "", "saved report/export profile to apply before explicit flags")

	campaignExportCmd.Flags().StringSliceVar(&campaignReportRiskLevels, "risk", nil, "filter target rows by risk level")
	campaignExportCmd.Flags().StringSliceVar(&campaignReportStatuses, "status", nil, "filter target rows by latest status")
	campaignExportCmd.Flags().StringSliceVar(&campaignReportTriggerTypes, "trigger", nil, "filter target rows by latest trigger type")
	campaignExportCmd.Flags().StringVar(&campaignReportPreset, "preset", "", "preset slice (high-risk, recovered, failed)")
	campaignExportCmd.Flags().IntVar(&campaignReportOffset, "offset", 0, "pagination offset after filters")
	campaignExportCmd.Flags().IntVar(&campaignReportLimit, "limit", 0, "pagination limit after filters (0 = all)")
	campaignExportCmd.Flags().StringVar(&campaignReportSortBy, "sort-by", "", "sort target rows by risk, target, latest-run, or open-high-risk")
	campaignExportCmd.Flags().StringVar(&campaignReportSortOrder, "sort-order", "", "sort order (asc or desc)")
	campaignExportCmd.Flags().StringVar(&campaignReportProfileName, "profile", "", "saved report/export profile to apply before explicit flags")
	campaignExportCmd.Flags().StringVar(&campaignExportFormat, "format", "", "export format (csv or json)")
	campaignExportCmd.Flags().StringVarP(&campaignExportOutput, "output", "o", "", "write export to file instead of stdout")

	campaignProfileSaveCmd.Flags().StringSliceVar(&campaignReportRiskLevels, "risk", nil, "profile risk levels")
	campaignProfileSaveCmd.Flags().StringSliceVar(&campaignReportStatuses, "status", nil, "profile latest statuses")
	campaignProfileSaveCmd.Flags().StringSliceVar(&campaignReportTriggerTypes, "trigger", nil, "profile latest trigger types")
	campaignProfileSaveCmd.Flags().StringVar(&campaignReportPreset, "preset", "", "profile preset (high-risk, recovered, failed)")
	campaignProfileSaveCmd.Flags().StringVar(&campaignReportSortBy, "sort-by", "", "profile sort by risk, target, latest-run, or open-high-risk")
	campaignProfileSaveCmd.Flags().StringVar(&campaignReportSortOrder, "sort-order", "", "profile sort order (asc or desc)")
	campaignProfileSaveCmd.Flags().StringVar(&campaignExportFormat, "format", "", "preferred export format for this profile (csv or json)")
	campaignProfileSaveCmd.Flags().StringVar(&campaignProfileDescription, "description", "", "optional profile description")

	campaignCmd.AddCommand(campaignCreateCmd)
	campaignCmd.AddCommand(campaignListCmd)
	campaignCmd.AddCommand(campaignStatusCmd)
	campaignCmd.AddCommand(campaignReportCmd)
	campaignCmd.AddCommand(campaignExportCmd)
	campaignProfileCmd.AddCommand(campaignProfileListCmd)
	campaignProfileCmd.AddCommand(campaignProfileSaveCmd)
	campaignProfileCmd.AddCommand(campaignProfileDeleteCmd)
	campaignCmd.AddCommand(campaignProfileCmd)
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

func runCampaignReport(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
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

	report, err := buildCampaignReportResponseCLI(ctx, campaign)
	if err != nil {
		return err
	}
	filters, sortSpec, _, profileApplied, err := resolveCampaignReportSelectionCLI(campaign)
	if err != nil {
		return err
	}
	report = applyCampaignReportFiltersCLI(report, campaign, filters)
	report = sortCampaignReportCLI(report, sortSpec)
	report.ProfileApplied = profileApplied
	report = paginateCampaignReportCLI(report, campaignReportOffset, campaignReportLimit)

	if globalJSON {
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Campaign: %s\n", report.Campaign.Name)
	fmt.Printf("ID:       %s\n", report.Campaign.ID)
	fmt.Printf("Workflow: %s (%s)\n", report.Campaign.WorkflowName, report.Campaign.WorkflowKind)
	fmt.Printf("Status:   %s\n", report.Status)
	fmt.Printf("Targets:  returned=%d matched=%d total=%d\n", report.Pagination.ReturnedCount, report.ResultCount, report.TotalTargets)
	if report.Pagination.Offset > 0 || report.Pagination.Limit > 0 {
		fmt.Printf("Page:     offset=%d limit=%d has-more=%t\n", report.Pagination.Offset, report.Pagination.Limit, report.Pagination.HasMore)
	}
	fmt.Printf("Progress: total=%d pending=%d running=%d completed=%d failed=%d\n",
		report.Progress.Total, report.Progress.Pending, report.Progress.Running, report.Progress.Completed, report.Progress.Failed)
	fmt.Printf("Risk Distribution: critical=%d high=%d medium=%d low=%d none=%d\n",
		report.RiskDistribution["critical"], report.RiskDistribution["high"], report.RiskDistribution["medium"], report.RiskDistribution["low"], report.RiskDistribution["none"])
	fmt.Printf("Triggers: campaign=%d rerun=%d deep-scan=%d\n",
		report.TriggerDistribution["campaign"], report.TriggerDistribution["campaign-rerun"], report.TriggerDistribution["campaign-deep-scan"])
	fmt.Printf("Deep Scan: configured=%t eligible=%d queued-targets=%d total-runs=%d queued=%d running=%d completed=%d failed=%d conversion=%.2f\n",
		report.DeepScan.Configured, report.DeepScan.EligibleTargets, report.DeepScan.QueuedTargets, report.DeepScan.TotalRuns,
		report.DeepScan.QueuedRuns, report.DeepScan.RunningRuns, report.DeepScan.CompletedRuns, report.DeepScan.FailedRuns, report.DeepScan.ConversionRate)
	fmt.Printf("Reruns: total=%d unique-targets=%d recovered=%d\n",
		report.RerunHistory.TotalRuns, report.RerunHistory.UniqueTargets, report.RerunHistory.RecoveredTargets)
	if report.RerunHistory.LastRerunAt != nil {
		fmt.Printf("Last Rerun: %s\n", report.RerunHistory.LastRerunAt.Format(time.RFC3339))
	}
	if len(report.FiltersApplied.RiskLevels) > 0 || len(report.FiltersApplied.Statuses) > 0 || len(report.FiltersApplied.TriggerTypes) > 0 || report.FiltersApplied.Preset != "" {
		fmt.Printf("Filters: risk=%s status=%s trigger=%s preset=%s\n",
			strings.Join(report.FiltersApplied.RiskLevels, ","),
			strings.Join(report.FiltersApplied.Statuses, ","),
			strings.Join(report.FiltersApplied.TriggerTypes, ","),
			report.FiltersApplied.Preset)
	}
	if report.ProfileApplied != "" {
		fmt.Printf("Profile: %s\n", report.ProfileApplied)
	}
	if report.SortApplied.By != "risk" || report.SortApplied.Order != "desc" || strings.TrimSpace(campaignReportSortBy) != "" || strings.TrimSpace(campaignReportSortOrder) != "" {
		fmt.Printf("Sort:    by=%s order=%s\n", report.SortApplied.By, report.SortApplied.Order)
	}
	fmt.Println("")

	for _, target := range report.Targets {
		fmt.Printf("- %s\n", target.Target)
		fmt.Printf("  Workspace: %s\n", target.Workspace)
		fmt.Printf("  Status: %s | Risk: %s | Latest Trigger: %s | DeepScanQueued: %t\n",
			target.Status, target.RiskLevel, target.LatestTriggerType, target.DeepScanQueued)
		fmt.Printf("  Runs: total=%d rerun=%d deep-scan=%d recovered=%t\n",
			target.TotalRuns, target.RerunRuns, target.DeepScanRuns, target.Recovered)
		fmt.Printf("  Findings: critical=%d high=%d medium=%d low=%d open-high-risk=%d\n",
			target.CriticalFindings, target.HighFindings, target.MediumFindings, target.LowFindings, target.OpenHighRiskFindings)
		fmt.Printf("  Attack Chains: operational=%d verified=%d\n",
			target.AttackChainOperationalHits, target.AttackChainVerifiedHits)
	}
	return nil
}

func runCampaignExport(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
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

	report, err := buildCampaignReportResponseCLI(ctx, campaign)
	if err != nil {
		return err
	}
	filters, sortSpec, format, profileApplied, err := resolveCampaignReportSelectionCLI(campaign)
	if err != nil {
		return err
	}
	report = applyCampaignReportFiltersCLI(report, campaign, filters)
	report = sortCampaignReportCLI(report, sortSpec)
	report.ProfileApplied = profileApplied
	report = paginateCampaignReportCLI(report, campaignReportOffset, campaignReportLimit)

	format = strings.TrimSpace(strings.ToLower(format))
	if format == "" {
		if globalJSON {
			format = "json"
		} else {
			format = "csv"
		}
	}

	var data []byte
	switch format {
	case "json":
		data, err = json.Marshal(report)
	case "csv":
		data, err = renderCampaignReportCSVCLI(report)
	default:
		return fmt.Errorf("unsupported export format: %s", format)
	}
	if err != nil {
		return err
	}

	if strings.TrimSpace(campaignExportOutput) != "" {
		outputPath := strings.TrimSpace(campaignExportOutput)
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			return err
		}
		fmt.Printf("Exported campaign report to %s\n", outputPath)
		return nil
	}

	if format == "json" {
		fmt.Println(string(data))
		return nil
	}
	fmt.Print(string(data))
	return nil
}

func runCampaignProfileList(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
	}
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	campaign, err := database.GetCampaignByID(context.Background(), args[0])
	if err != nil {
		return err
	}
	profiles, err := database.ListCampaignReportProfiles(campaign)
	if err != nil {
		return err
	}

	if globalJSON {
		payload := map[string]interface{}{
			"campaign_id": campaign.ID,
			"data":        profiles,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Campaign ID: %s\n", campaign.ID)
	if len(profiles) == 0 {
		fmt.Println("No saved profiles")
		return nil
	}
	for _, profile := range profiles {
		fmt.Printf("- %s\n", profile.Name)
		if profile.Description != "" {
			fmt.Printf("  Description: %s\n", profile.Description)
		}
		fmt.Printf("  Filters: risk=%s status=%s trigger=%s preset=%s\n",
			strings.Join(profile.Filters.RiskLevels, ","),
			strings.Join(profile.Filters.Statuses, ","),
			strings.Join(profile.Filters.TriggerTypes, ","),
			profile.Filters.Preset)
		fmt.Printf("  Sort: by=%s order=%s\n", profile.Sort.By, profile.Sort.Order)
		if profile.Format != "" {
			fmt.Printf("  Format: %s\n", profile.Format)
		}
		if !profile.UpdatedAt.IsZero() {
			fmt.Printf("  Updated: %s\n", profile.UpdatedAt.Format(time.RFC3339))
		}
	}
	return nil
}

func runCampaignProfileSave(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
	}
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	sortSpec, err := normalizeCampaignReportSortCLI(campaignReportSortBy, campaignReportSortOrder)
	if err != nil {
		return err
	}
	filters, err := normalizeCampaignReportFiltersCLI(campaignReportRiskLevels, campaignReportStatuses, campaignReportTriggerTypes, campaignReportPreset)
	if err != nil {
		return err
	}
	profile, err := database.UpsertCampaignReportProfile(context.Background(), args[0], database.CampaignReportProfile{
		Name:        args[1],
		Description: campaignProfileDescription,
		Filters: database.CampaignReportProfileFilters{
			RiskLevels:   filters.RiskLevels,
			Statuses:     filters.Statuses,
			TriggerTypes: filters.TriggerTypes,
			Preset:       filters.Preset,
		},
		Sort: database.CampaignReportProfileSort{
			By:    sortSpec.By,
			Order: sortSpec.Order,
		},
		Format: strings.ToLower(strings.TrimSpace(campaignExportFormat)),
	})
	if err != nil {
		return err
	}

	if globalJSON {
		payload := map[string]interface{}{
			"message":     "Campaign profile saved",
			"campaign_id": args[0],
			"data":        profile,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Campaign ID: %s\n", args[0])
	fmt.Printf("Saved profile: %s\n", profile.Name)
	return nil
}

func runCampaignProfileDelete(cmd *cobra.Command, args []string) error {
	if disableDB {
		return fmt.Errorf("campaign commands unavailable: --disable-db flag is set")
	}
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	deleted, err := database.DeleteCampaignReportProfile(context.Background(), args[0], args[1])
	if err != nil {
		return err
	}

	if globalJSON {
		payload := map[string]interface{}{
			"message":     "Campaign profile deleted",
			"campaign_id": args[0],
			"name":        strings.ToLower(strings.TrimSpace(args[1])),
			"deleted":     deleted,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Campaign ID: %s\n", args[0])
	fmt.Printf("Deleted profile: %s\n", strings.ToLower(strings.TrimSpace(args[1])))
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

type campaignReportResponseCLI struct {
	Campaign                    *database.Campaign           `json:"campaign"`
	GeneratedAt                 time.Time                    `json:"generated_at"`
	Status                      string                       `json:"status"`
	Progress                    campaignJobProgress          `json:"progress"`
	Summary                     campaignSignalSummaryCLI     `json:"summary"`
	RiskDistribution            map[string]int               `json:"risk_distribution"`
	LatestRunStatusDistribution map[string]int               `json:"latest_run_status_distribution"`
	TriggerDistribution         map[string]int               `json:"trigger_distribution"`
	DeepScan                    campaignDeepScanReportCLI    `json:"deep_scan"`
	RerunHistory                campaignRerunHistoryCLI      `json:"rerun_history"`
	FiltersApplied              campaignReportFiltersCLI     `json:"filters_applied"`
	SortApplied                 campaignReportSortCLI        `json:"sort_applied"`
	ProfileApplied              string                       `json:"profile_applied,omitempty"`
	TotalTargets                int                          `json:"total_targets"`
	ResultCount                 int                          `json:"result_count"`
	Pagination                  campaignReportPaginationCLI  `json:"pagination"`
	Targets                     []campaignReportTargetRowCLI `json:"targets"`
}

type campaignReportFiltersCLI struct {
	RiskLevels   []string `json:"risk_levels,omitempty"`
	Statuses     []string `json:"statuses,omitempty"`
	TriggerTypes []string `json:"trigger_types,omitempty"`
	Preset       string   `json:"preset,omitempty"`
}

type campaignReportSortCLI struct {
	By    string `json:"by"`
	Order string `json:"order"`
}

type campaignReportPaginationCLI struct {
	Offset        int  `json:"offset"`
	Limit         int  `json:"limit"`
	ReturnedCount int  `json:"returned_count"`
	HasMore       bool `json:"has_more"`
}

type campaignDeepScanReportCLI struct {
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

type campaignRerunHistoryCLI struct {
	TotalRuns        int        `json:"total_runs"`
	UniqueTargets    int        `json:"unique_targets"`
	RecoveredTargets int        `json:"recovered_targets"`
	LastRerunAt      *time.Time `json:"last_rerun_at,omitempty"`
}

type campaignReportTargetRowCLI struct {
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

func buildCampaignStatusResponseCLI(ctx context.Context, campaign *database.Campaign) (*campaignStatusResponseCLI, error) {
	runs, err := database.GetRunsByRunGroupID(ctx, campaign.ID)
	if err != nil {
		return nil, err
	}

	latestRuns := latestCampaignRunsCLI(runs)

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

		vulnSummary, _ := database.GetActiveVulnerabilitySummaryForTarget(ctx, run.Workspace, run.Target)
		attackChainSummary, _ := attackchain.GetTargetSummary(ctx, run.Workspace, run.Target)
		riskLevel := deriveCampaignRiskLevelCLI(vulnSummary, attackChainSummary)
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
	if err := database.UpdateCampaignStatus(ctx, campaign.ID, status); err != nil {
		return nil, err
	}
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

func buildCampaignReportResponseCLI(ctx context.Context, campaign *database.Campaign) (*campaignReportResponseCLI, error) {
	statusResponse, err := buildCampaignStatusResponseCLI(ctx, campaign)
	if err != nil {
		return nil, err
	}

	report := &campaignReportResponseCLI{
		Campaign:                    statusResponse.Campaign,
		GeneratedAt:                 time.Now().UTC(),
		Status:                      statusResponse.Status,
		Progress:                    statusResponse.Progress,
		Summary:                     statusResponse.Summary,
		RiskDistribution:            make(map[string]int),
		LatestRunStatusDistribution: make(map[string]int),
		TriggerDistribution:         make(map[string]int),
	}

	targetRows := make(map[string]*campaignReportTargetRowCLI, len(statusResponse.Targets))
	for _, target := range statusResponse.Targets {
		report.RiskDistribution[target.RiskLevel]++
		report.LatestRunStatusDistribution[target.Status]++
		row := &campaignReportTargetRowCLI{
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
		targetRows[campaignTargetKeyCLI(target.Workspace, target.Target)] = row
	}

	latestRuns := latestCampaignRunsCLI(statusResponse.Runs)
	rerunTargets := make(map[string]struct{})

	report.DeepScan.Configured = strings.TrimSpace(statusResponse.Campaign.DeepScanWorkflow) != ""
	report.DeepScan.EligibleTargets = len(statusResponse.HighRiskTargets)
	report.DeepScan.QueuedTargets = statusResponse.Summary.DeepScanQueuedTargets

	for _, run := range statusResponse.Runs {
		report.TriggerDistribution[run.TriggerType]++
		key := campaignTargetKeyCLI(run.Workspace, run.Target)
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
			updateCampaignRowTimeCLI(&row.LastRerunAt, run.CreatedAt)
			updateCampaignRowTimeCLI(&report.RerunHistory.LastRerunAt, run.CreatedAt)
		case "campaign-deep-scan":
			row.DeepScanRuns++
			report.DeepScan.TotalRuns++
			updateCampaignRowTimeCLI(&row.LastDeepScanAt, run.CreatedAt)
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
		if row.RerunRuns > 0 && latestCampaignTargetRecoveredCLI(statusResponse.Runs, key, latestRuns[key]) {
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
		if campaignRiskRankCLI(left.RiskLevel) == campaignRiskRankCLI(right.RiskLevel) {
			if left.OpenHighRiskFindings == right.OpenHighRiskFindings {
				return left.Target < right.Target
			}
			return left.OpenHighRiskFindings > right.OpenHighRiskFindings
		}
		return campaignRiskRankCLI(left.RiskLevel) > campaignRiskRankCLI(right.RiskLevel)
	})

	report.TotalTargets = len(report.Targets)
	report.ResultCount = len(report.Targets)

	return report, nil
}

func normalizeCampaignReportFiltersCLI(riskLevels, statuses, triggerTypes []string, preset string) (campaignReportFiltersCLI, error) {
	filters := campaignReportFiltersCLI{
		RiskLevels:   normalizeCampaignFilterListCLI(riskLevels),
		Statuses:     normalizeCampaignFilterListCLI(statuses),
		TriggerTypes: normalizeCampaignFilterListCLI(triggerTypes),
		Preset:       normalizeCampaignReportPresetCLI(preset),
	}
	if filters.Preset == "" || filters.Preset == "high-risk" || filters.Preset == "recovered" || filters.Preset == "failed" {
		return filters, nil
	}
	return campaignReportFiltersCLI{}, fmt.Errorf("unsupported campaign report preset: %s", preset)
}

func normalizeCampaignFilterListCLI(values []string) []string {
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

func normalizeCampaignReportPresetCLI(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func normalizeCampaignReportSortCLI(by, order string) (campaignReportSortCLI, error) {
	sortSpec := campaignReportSortCLI{
		By:    normalizeCampaignReportSortByCLI(by),
		Order: normalizeCampaignReportSortOrderCLI(by, order),
	}
	switch sortSpec.By {
	case "risk", "target", "latest_run", "open_high_risk":
	default:
		return campaignReportSortCLI{}, fmt.Errorf("unsupported campaign report sort-by: %s", by)
	}
	if sortSpec.Order != "asc" && sortSpec.Order != "desc" {
		return campaignReportSortCLI{}, fmt.Errorf("unsupported campaign report sort-order: %s", order)
	}
	return sortSpec, nil
}

func normalizeCampaignReportSortByCLI(value string) string {
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

func normalizeCampaignReportSortOrderCLI(by, order string) string {
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "asc" || order == "desc" {
		return order
	}
	switch normalizeCampaignReportSortByCLI(by) {
	case "target":
		return "asc"
	default:
		return "desc"
	}
}

func applyCampaignReportFiltersCLI(report *campaignReportResponseCLI, campaign *database.Campaign, filters campaignReportFiltersCLI) *campaignReportResponseCLI {
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
		if !campaignReportRowMatchesFiltersCLI(campaign, row, filters) {
			continue
		}
		filtered.Targets = append(filtered.Targets, row)
	}
	filtered.ResultCount = len(filtered.Targets)
	return &filtered
}

func sortCampaignReportCLI(report *campaignReportResponseCLI, sortSpec campaignReportSortCLI) *campaignReportResponseCLI {
	if report == nil {
		return nil
	}

	sorted := *report
	sorted.SortApplied = sortSpec
	sorted.Targets = append([]campaignReportTargetRowCLI(nil), report.Targets...)
	sort.SliceStable(sorted.Targets, func(i, j int) bool {
		return campaignReportLessCLI(sorted.Targets[i], sorted.Targets[j], sortSpec)
	})
	return &sorted
}

func paginateCampaignReportCLI(report *campaignReportResponseCLI, offset, limit int) *campaignReportResponseCLI {
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
	if limit < 0 {
		limit = 0
	}
	if limit > 1000 {
		limit = 1000
	}

	end := totalMatched
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	paginated := *report
	paginated.Targets = append([]campaignReportTargetRowCLI(nil), report.Targets[offset:end]...)
	paginated.Pagination = campaignReportPaginationCLI{
		Offset:        offset,
		Limit:         limit,
		ReturnedCount: len(paginated.Targets),
		HasMore:       end < totalMatched,
	}
	return &paginated
}

func resolveCampaignReportSelectionCLI(campaign *database.Campaign) (campaignReportFiltersCLI, campaignReportSortCLI, string, string, error) {
	profileApplied := strings.TrimSpace(campaignReportProfileName)
	profileFilters := campaignReportFiltersCLI{}
	profileSort := campaignReportSortCLI{}
	profileFormat := ""
	if profileApplied != "" {
		profile, err := database.GetCampaignReportProfile(campaign, profileApplied)
		if err != nil {
			return campaignReportFiltersCLI{}, campaignReportSortCLI{}, "", "", err
		}
		profileApplied = profile.Name
		profileFilters = campaignReportFiltersCLI{
			RiskLevels:   append([]string(nil), profile.Filters.RiskLevels...),
			Statuses:     append([]string(nil), profile.Filters.Statuses...),
			TriggerTypes: append([]string(nil), profile.Filters.TriggerTypes...),
			Preset:       profile.Filters.Preset,
		}
		profileSort = campaignReportSortCLI{
			By:    profile.Sort.By,
			Order: profile.Sort.Order,
		}
		profileFormat = profile.Format
	}

	if len(campaignReportRiskLevels) > 0 {
		profileFilters.RiskLevels = campaignReportRiskLevels
	}
	if len(campaignReportStatuses) > 0 {
		profileFilters.Statuses = campaignReportStatuses
	}
	if len(campaignReportTriggerTypes) > 0 {
		profileFilters.TriggerTypes = campaignReportTriggerTypes
	}
	if strings.TrimSpace(campaignReportPreset) != "" {
		profileFilters.Preset = campaignReportPreset
	}
	filters, err := normalizeCampaignReportFiltersCLI(profileFilters.RiskLevels, profileFilters.Statuses, profileFilters.TriggerTypes, profileFilters.Preset)
	if err != nil {
		return campaignReportFiltersCLI{}, campaignReportSortCLI{}, "", "", err
	}

	sortBy := profileSort.By
	sortOrder := profileSort.Order
	if strings.TrimSpace(campaignReportSortBy) != "" {
		sortBy = campaignReportSortBy
	}
	if strings.TrimSpace(campaignReportSortOrder) != "" {
		sortOrder = campaignReportSortOrder
	}
	sortSpec, err := normalizeCampaignReportSortCLI(sortBy, sortOrder)
	if err != nil {
		return campaignReportFiltersCLI{}, campaignReportSortCLI{}, "", "", err
	}

	format := strings.TrimSpace(profileFormat)
	if strings.TrimSpace(campaignExportFormat) != "" {
		format = campaignExportFormat
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "" && format != "csv" && format != "json" {
		return campaignReportFiltersCLI{}, campaignReportSortCLI{}, "", "", fmt.Errorf("unsupported export format: %s", format)
	}
	return filters, sortSpec, format, profileApplied, nil
}

func campaignReportLessCLI(left, right campaignReportTargetRowCLI, sortSpec campaignReportSortCLI) bool {
	desc := sortSpec.Order == "desc"
	switch sortSpec.By {
	case "target":
		if left.Target == right.Target {
			return campaignReportDefaultLessCLI(left, right)
		}
		return campaignCompareStringCLI(left.Target, right.Target, desc)
	case "latest_run":
		if left.LatestRunAt.Equal(right.LatestRunAt) {
			return campaignReportDefaultLessCLI(left, right)
		}
		return campaignCompareTimeCLI(left.LatestRunAt, right.LatestRunAt, desc)
	case "open_high_risk":
		if left.OpenHighRiskFindings == right.OpenHighRiskFindings {
			return campaignReportDefaultLessCLI(left, right)
		}
		return campaignCompareIntCLI(left.OpenHighRiskFindings, right.OpenHighRiskFindings, desc)
	case "risk":
		fallthrough
	default:
		return campaignReportDefaultLessWithOrderCLI(left, right, desc)
	}
}

func campaignReportDefaultLessCLI(left, right campaignReportTargetRowCLI) bool {
	return campaignReportDefaultLessWithOrderCLI(left, right, true)
}

func campaignReportDefaultLessWithOrderCLI(left, right campaignReportTargetRowCLI, desc bool) bool {
	leftRisk := campaignRiskRankCLI(left.RiskLevel)
	rightRisk := campaignRiskRankCLI(right.RiskLevel)
	if leftRisk != rightRisk {
		return campaignCompareIntCLI(leftRisk, rightRisk, desc)
	}
	if left.OpenHighRiskFindings != right.OpenHighRiskFindings {
		return campaignCompareIntCLI(left.OpenHighRiskFindings, right.OpenHighRiskFindings, desc)
	}
	if !left.LatestRunAt.Equal(right.LatestRunAt) {
		return campaignCompareTimeCLI(left.LatestRunAt, right.LatestRunAt, desc)
	}
	return left.Target < right.Target
}

func campaignCompareStringCLI(left, right string, desc bool) bool {
	if desc {
		return left > right
	}
	return left < right
}

func campaignCompareIntCLI(left, right int, desc bool) bool {
	if desc {
		return left > right
	}
	return left < right
}

func campaignCompareTimeCLI(left, right time.Time, desc bool) bool {
	if desc {
		return left.After(right)
	}
	return left.Before(right)
}

func campaignReportRowMatchesFiltersCLI(campaign *database.Campaign, row campaignReportTargetRowCLI, filters campaignReportFiltersCLI) bool {
	if len(filters.RiskLevels) > 0 && !campaignFilterContainsCLI(filters.RiskLevels, row.RiskLevel) {
		return false
	}
	if len(filters.Statuses) > 0 && !campaignFilterContainsCLI(filters.Statuses, row.Status) {
		return false
	}
	if len(filters.TriggerTypes) > 0 && !campaignFilterContainsCLI(filters.TriggerTypes, row.LatestTriggerType) {
		return false
	}

	switch filters.Preset {
	case "":
		return true
	case "high-risk":
		return campaignReportRowIsHighRiskCLI(campaign, row)
	case "recovered":
		return row.Recovered
	case "failed":
		return strings.EqualFold(row.Status, "failed")
	default:
		return false
	}
}

func campaignFilterContainsCLI(values []string, actual string) bool {
	actual = strings.ToLower(strings.TrimSpace(actual))
	for _, value := range values {
		if actual == strings.ToLower(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func campaignReportRowIsHighRiskCLI(campaign *database.Campaign, row campaignReportTargetRowCLI) bool {
	for _, severity := range normalizeHighRiskSeveritiesCLI(campaign.HighRiskSeverities) {
		if strings.EqualFold(strings.TrimSpace(severity), row.RiskLevel) {
			return true
		}
	}
	return false
}

func deriveCampaignRiskLevelCLI(summary map[string]int, attackChainSummary *database.AttackChainWorkspaceSummary) string {
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

func campaignTargetIsHighRiskCLI(campaign *database.Campaign, summary map[string]int, attackChainSummary *database.AttackChainWorkspaceSummary) bool {
	for _, severity := range normalizeHighRiskSeveritiesCLI(campaign.HighRiskSeverities) {
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
	for _, run := range latestCampaignRunsCLI(runs) {
		if run.TriggerType == "campaign-deep-scan" {
			continue
		}
		vulnSummary, _ := database.GetActiveVulnerabilitySummaryForTarget(ctx, run.Workspace, run.Target)
		attackChainSummary, _ := attackchain.GetTargetSummary(ctx, run.Workspace, run.Target)
		if !campaignTargetIsHighRiskCLI(campaign, vulnSummary, attackChainSummary) {
			continue
		}
		exists, err := database.HasCampaignDeepScanRun(ctx, campaign.ID, run.Workspace, campaign.DeepScanWorkflow, run.Target)
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

func latestCampaignRunsCLI(runs []*database.Run) map[string]*database.Run {
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

func campaignTargetKeyCLI(workspace, target string) string {
	return strings.TrimSpace(workspace) + "::" + strings.TrimSpace(target)
}

func latestCampaignTargetRecoveredCLI(runs []*database.Run, key string, latest *database.Run) bool {
	if latest == nil || latest.Status != "completed" {
		return false
	}

	hadFailure := false
	for _, run := range runs {
		if run.TriggerType == "campaign-deep-scan" {
			continue
		}
		if campaignTargetKeyCLI(run.Workspace, run.Target) != key {
			continue
		}
		if run.Status == "failed" && run.CreatedAt.Before(latest.CreatedAt) {
			hadFailure = true
		}
	}
	return hadFailure
}

func updateCampaignRowTimeCLI(target **time.Time, value time.Time) {
	if value.IsZero() {
		return
	}
	if *target == nil || value.After(**target) {
		copyValue := value
		*target = &copyValue
	}
}

func campaignRiskRankCLI(level string) int {
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

func formatCampaignCSVTimeCLI(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func renderCampaignReportCSVCLI(report *campaignReportResponseCLI) ([]byte, error) {
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
			formatCampaignCSVTimeCLI(target.LatestRunAt),
			fmt.Sprintf("%t", target.DeepScanQueued),
			fmt.Sprintf("%d", target.TotalRuns),
			fmt.Sprintf("%d", target.RerunRuns),
			fmt.Sprintf("%d", target.DeepScanRuns),
			fmt.Sprintf("%t", target.Recovered),
			fmt.Sprintf("%d", target.CriticalFindings),
			fmt.Sprintf("%d", target.HighFindings),
			fmt.Sprintf("%d", target.MediumFindings),
			fmt.Sprintf("%d", target.LowFindings),
			fmt.Sprintf("%d", target.OpenHighRiskFindings),
			fmt.Sprintf("%d", target.AttackChainOperationalHits),
			fmt.Sprintf("%d", target.AttackChainVerifiedHits),
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
	for _, run := range latestCampaignRunsCLI(runs) {
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
