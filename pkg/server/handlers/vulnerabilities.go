package handlers

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/uptrace/bun"
)

type vulnerabilityRunSummary struct {
	RunUUID      string     `json:"run_uuid"`
	WorkflowName string     `json:"workflow_name,omitempty"`
	WorkflowKind string     `json:"workflow_kind,omitempty"`
	Status       string     `json:"status,omitempty"`
	TriggerType  string     `json:"trigger_type,omitempty"`
	Target       string     `json:"target,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type vulnerabilityAssetRecord struct {
	ID         int64     `json:"id"`
	AssetValue string    `json:"asset_value"`
	URL        string    `json:"url,omitempty"`
	AssetType  string    `json:"asset_type,omitempty"`
	Source     string    `json:"source,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type vulnerabilityAttackChainSummary struct {
	ReportID    int64   `json:"report_id"`
	ChainID     string  `json:"chain_id"`
	ChainName   string  `json:"chain_name,omitempty"`
	Target      string  `json:"target,omitempty"`
	RunUUID     string  `json:"run_uuid,omitempty"`
	SourcePath  string  `json:"source_path,omitempty"`
	MatchedBy   string  `json:"matched_by,omitempty"`
	Impact      string  `json:"impact,omitempty"`
	Difficulty  string  `json:"difficulty,omitempty"`
	Probability float64 `json:"success_probability,omitempty"`
}

type vulnerabilityStatusTimelineItem struct {
	EvidenceVersion int       `json:"evidence_version"`
	ObservedAt      time.Time `json:"observed_at"`
	VulnStatus      string    `json:"vuln_status,omitempty"`
	RetestStatus    string    `json:"retest_status,omitempty"`
	Severity        string    `json:"severity,omitempty"`
	Confidence      string    `json:"confidence,omitempty"`
	SourceRunUUID   string    `json:"source_run_uuid,omitempty"`
	AttackChainRef  string    `json:"attack_chain_ref,omitempty"`
}

type vulnerabilityRetestTimelineItem struct {
	RunUUID      string     `json:"run_uuid"`
	WorkflowName string     `json:"workflow_name,omitempty"`
	WorkflowKind string     `json:"workflow_kind,omitempty"`
	Status       string     `json:"status,omitempty"`
	TriggerType  string     `json:"trigger_type,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type vulnerabilityGroupSummary struct {
	ID                int64      `json:"id"`
	Workspace         string     `json:"workspace"`
	FingerprintKey    string     `json:"fingerprint_key,omitempty"`
	VulnInfo          string     `json:"vuln_info,omitempty"`
	VulnTitle         string     `json:"vuln_title,omitempty"`
	AssetType         string     `json:"asset_type,omitempty"`
	AssetValue        string     `json:"asset_value,omitempty"`
	Severity          string     `json:"severity,omitempty"`
	Confidence        string     `json:"confidence,omitempty"`
	VulnStatus        string     `json:"vuln_status,omitempty"`
	RetestStatus      string     `json:"retest_status,omitempty"`
	AIVerdict         string     `json:"ai_verdict,omitempty"`
	AnalystVerdict    string     `json:"analyst_verdict,omitempty"`
	EvidenceVersions  int        `json:"evidence_versions"`
	DistinctRuns      int        `json:"distinct_runs"`
	AssetCount        int        `json:"asset_count"`
	ReportRefCount    int        `json:"report_ref_count"`
	AttackChainLinked bool       `json:"attack_chain_linked"`
	FirstSeenAt       time.Time  `json:"first_seen_at,omitempty"`
	LastSeenAt        time.Time  `json:"last_seen_at,omitempty"`
	VerifiedAt        *time.Time `json:"verified_at,omitempty"`
	ClosedAt          *time.Time `json:"closed_at,omitempty"`
}

type vulnerabilityDetailResponse struct {
	database.Vulnerability
	EvidenceTimeline    []database.VulnerabilityEvidence  `json:"evidence_timeline,omitempty"`
	StatusTimeline      []vulnerabilityStatusTimelineItem `json:"status_timeline,omitempty"`
	RetestTimeline      []vulnerabilityRetestTimelineItem `json:"retest_timeline,omitempty"`
	RelatedRuns         []vulnerabilityRunSummary         `json:"related_runs,omitempty"`
	RelatedAssetRecords []vulnerabilityAssetRecord        `json:"related_asset_records,omitempty"`
	RelatedAttackChains []vulnerabilityAttackChainSummary `json:"related_attack_chains,omitempty"`
}

// ListVulnerabilities handles listing vulnerabilities with pagination and filtering
// @Summary List vulnerabilities
// @Description Get a paginated list of vulnerabilities with optional workspace, severity, and confidence filtering
// @Tags Vulnerabilities
// @Produce json
// @Param workspace query string false "Filter by workspace name"
// @Param severity query string false "Filter by severity (critical, high, medium, low, info)"
// @Param confidence query string false "Filter by confidence (certain, firm, tentative, manual review required)"
// @Param asset_value query string false "Filter by asset value (partial match)"
// @Param offset query int false "Number of records to skip" default(0)
// @Param limit query int false "Maximum number of records to return" default(20)
// @Success 200 {object} map[string]interface{} "List of vulnerabilities with pagination"
// @Failure 500 {object} map[string]interface{} "Failed to fetch vulnerabilities"
// @Security BearerAuth
// @Router /osm/api/vulnerabilities [get]
func ListVulnerabilities(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query := buildVulnerabilityQuery(c)
		ctx := context.Background()

		// Get vulnerabilities from database
		result, err := database.ListVulnerabilities(ctx, query)
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

// ListVulnerabilityGroups returns fingerprint-level vulnerability groups with derived evidence counts.
func ListVulnerabilityGroups(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		query := buildVulnerabilityQuery(c)
		ctx := context.Background()

		result, err := database.ListVulnerabilities(ctx, query)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		groups := make([]vulnerabilityGroupSummary, 0, len(result.Data))
		for i := range result.Data {
			groups = append(groups, buildVulnerabilityGroupSummary(&result.Data[i]))
		}

		return c.JSON(fiber.Map{
			"data": groups,
			"pagination": fiber.Map{
				"total":  result.TotalCount,
				"offset": result.Offset,
				"limit":  result.Limit,
			},
		})
	}
}

// GetVulnerability handles getting a single vulnerability by ID
// @Summary Get vulnerability by ID
// @Description Get a single vulnerability by its ID
// @Tags Vulnerabilities
// @Produce json
// @Param id path int true "Vulnerability ID"
// @Success 200 {object} map[string]interface{} "Vulnerability details"
// @Failure 400 {object} map[string]interface{} "Invalid ID"
// @Failure 404 {object} map[string]interface{} "Vulnerability not found"
// @Failure 500 {object} map[string]interface{} "Failed to fetch vulnerability"
// @Security BearerAuth
// @Router /osm/api/vulnerabilities/{id} [get]
func GetVulnerability(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Parse ID
		idStr := c.Params("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid vulnerability ID",
			})
		}

		ctx := context.Background()

		// Get vulnerability
		vuln, err := database.GetVulnerabilityByID(ctx, id)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Vulnerability not found",
			})
		}

		detail := buildVulnerabilityDetail(ctx, vuln)

		return c.JSON(fiber.Map{
			"data": detail,
		})
	}
}

// CreateVulnerabilityInput represents the input for creating a vulnerability
type CreateVulnerabilityInput struct {
	Workspace          string   `json:"workspace"`
	VulnInfo           string   `json:"vuln_info"`
	VulnTitle          string   `json:"vuln_title"`
	VulnDesc           string   `json:"vuln_desc"`
	VulnPOC            string   `json:"vuln_poc"`
	Severity           string   `json:"severity"`
	Confidence         string   `json:"confidence,omitempty"`
	AssetType          string   `json:"asset_type"`
	AssetValue         string   `json:"asset_value"`
	Tags               []string `json:"tags"`
	DetailHTTPRequest  string   `json:"detail_http_request"`
	DetailHTTPResponse string   `json:"detail_http_response"`
	RawVulnJSON        string   `json:"raw_vuln_json"`
	VulnStatus         string   `json:"vuln_status,omitempty"`
	SourceRunUUID      string   `json:"source_run_uuid,omitempty"`
	AIVerdict          string   `json:"ai_verdict,omitempty"`
	AISummary          string   `json:"ai_summary,omitempty"`
	AnalystVerdict     string   `json:"analyst_verdict,omitempty"`
	AnalystNotes       string   `json:"analyst_notes,omitempty"`
	AttackChainRef     string   `json:"attack_chain_ref,omitempty"`
	RelatedAssets      []string `json:"related_assets,omitempty"`
	ReportRefs         []string `json:"report_refs,omitempty"`
}

// UpdateVulnerabilityInput updates lifecycle, review, and linkage fields.
type UpdateVulnerabilityInput struct {
	VulnInfo           string   `json:"vuln_info,omitempty"`
	VulnTitle          string   `json:"vuln_title,omitempty"`
	VulnDesc           string   `json:"vuln_desc,omitempty"`
	VulnPOC            string   `json:"vuln_poc,omitempty"`
	Severity           string   `json:"severity,omitempty"`
	Confidence         string   `json:"confidence,omitempty"`
	AssetType          string   `json:"asset_type,omitempty"`
	AssetValue         string   `json:"asset_value,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	DetailHTTPRequest  string   `json:"detail_http_request,omitempty"`
	DetailHTTPResponse string   `json:"detail_http_response,omitempty"`
	RawVulnJSON        string   `json:"raw_vuln_json,omitempty"`
	VulnStatus         string   `json:"vuln_status,omitempty"`
	AIVerdict          string   `json:"ai_verdict,omitempty"`
	AISummary          string   `json:"ai_summary,omitempty"`
	AnalystVerdict     string   `json:"analyst_verdict,omitempty"`
	AnalystNotes       string   `json:"analyst_notes,omitempty"`
	AttackChainRef     string   `json:"attack_chain_ref,omitempty"`
	RelatedAssets      []string `json:"related_assets,omitempty"`
	ReportRefs         []string `json:"report_refs,omitempty"`
}

// RetestVulnerabilityInput creates a queued retest task for a vulnerability.
type RetestVulnerabilityInput struct {
	Flow     string            `json:"flow,omitempty"`
	Module   string            `json:"module,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Priority string            `json:"priority,omitempty"`
}

// BulkVulnerabilityActionInput applies the same lifecycle or retest action to multiple findings.
type BulkVulnerabilityActionInput struct {
	IDs             []int64           `json:"ids,omitempty"`
	FingerprintKeys []string          `json:"fingerprint_keys,omitempty"`
	Workspace       string            `json:"workspace,omitempty"`
	Action          string            `json:"action"`
	Status          string            `json:"status,omitempty"`
	AIVerdict       string            `json:"ai_verdict,omitempty"`
	AISummary       string            `json:"ai_summary,omitempty"`
	AnalystVerdict  string            `json:"analyst_verdict,omitempty"`
	AnalystNotes    string            `json:"analyst_notes,omitempty"`
	AttackChainRef  string            `json:"attack_chain_ref,omitempty"`
	RelatedAssets   []string          `json:"related_assets,omitempty"`
	ReportRefs      []string          `json:"report_refs,omitempty"`
	Flow            string            `json:"flow,omitempty"`
	Module          string            `json:"module,omitempty"`
	Params          map[string]string `json:"params,omitempty"`
	Priority        string            `json:"priority,omitempty"`
}

type bulkVulnerabilityActionResult struct {
	ID             int64  `json:"id"`
	FingerprintKey string `json:"fingerprint_key,omitempty"`
	Action         string `json:"action"`
	Outcome        string `json:"outcome"`
	Message        string `json:"message,omitempty"`
	VulnStatus     string `json:"vuln_status,omitempty"`
	RetestStatus   string `json:"retest_status,omitempty"`
	RunUUID        string `json:"run_uuid,omitempty"`
}

// CreateVulnerability handles creating a new vulnerability
// @Summary Create vulnerability
// @Description Create a new vulnerability record
// @Tags Vulnerabilities
// @Accept json
// @Produce json
// @Param vulnerability body CreateVulnerabilityInput true "Vulnerability data"
// @Success 201 {object} map[string]interface{} "Created vulnerability"
// @Failure 400 {object} map[string]interface{} "Invalid input"
// @Failure 500 {object} map[string]interface{} "Failed to create vulnerability"
// @Security BearerAuth
// @Router /osm/api/vulnerabilities [post]
func CreateVulnerability(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Parse input
		var input CreateVulnerabilityInput
		if err := c.BodyParser(&input); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}

		// Validate required fields
		if input.Workspace == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Workspace is required",
			})
		}

		ctx := context.Background()

		// Create vulnerability
		vuln := &database.Vulnerability{
			Workspace:          input.Workspace,
			VulnInfo:           input.VulnInfo,
			VulnTitle:          input.VulnTitle,
			VulnDesc:           input.VulnDesc,
			VulnPOC:            input.VulnPOC,
			Severity:           strings.ToLower(strings.TrimSpace(input.Severity)),
			Confidence:         strings.TrimSpace(input.Confidence),
			AssetType:          input.AssetType,
			AssetValue:         input.AssetValue,
			Tags:               input.Tags,
			DetailHTTPRequest:  input.DetailHTTPRequest,
			DetailHTTPResponse: input.DetailHTTPResponse,
			RawVulnJSON:        input.RawVulnJSON,
			VulnStatus:         normalizeVulnerabilityStatus(input.VulnStatus),
			SourceRunUUID:      input.SourceRunUUID,
			AIVerdict:          normalizeVulnerabilityVerdict(input.AIVerdict),
			AISummary:          input.AISummary,
			AnalystVerdict:     normalizeVulnerabilityVerdict(input.AnalystVerdict),
			AnalystNotes:       input.AnalystNotes,
			AttackChainRef:     input.AttackChainRef,
			RelatedAssets:      input.RelatedAssets,
			ReportRefs:         input.ReportRefs,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}
		applyVulnerabilityStatusTimestamps(vuln)

		result, err := database.CreateVulnerabilityRecord(ctx, vuln)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		statusCode := fiber.StatusCreated
		message := "Vulnerability created successfully"
		if result != nil && result.Merged {
			statusCode = fiber.StatusOK
			message = "Vulnerability merged into existing record"
		}

		return c.Status(statusCode).JSON(fiber.Map{
			"data":    vuln,
			"message": message,
			"merged":  result != nil && result.Merged,
			"created": result != nil && result.Created,
		})
	}
}

// UpdateVulnerability updates review and lifecycle data for a vulnerability.
func UpdateVulnerability(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid vulnerability ID",
			})
		}

		var input UpdateVulnerabilityInput
		if err := c.BodyParser(&input); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}

		ctx := context.Background()
		vuln, err := database.GetVulnerabilityByID(ctx, id)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Vulnerability not found",
			})
		}

		if err := applyVulnerabilityUpdateInput(vuln, input); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		applyVulnerabilityStatusTimestamps(vuln)
		if err := database.UpdateVulnerabilityRecord(ctx, vuln); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"data":    vuln,
			"message": "Vulnerability updated successfully",
		})
	}
}

// BulkVulnerabilityAction applies a shared lifecycle update or retest request to multiple findings.
func BulkVulnerabilityAction(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var input BulkVulnerabilityActionInput
		if err := c.BodyParser(&input); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}

		action := normalizeBulkVulnerabilityAction(input.Action)
		if action == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid bulk action",
			})
		}

		ctx := context.Background()
		vulns, err := resolveBulkVulnerabilities(ctx, input)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		if len(vulns) == 0 {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "No matching vulnerabilities found",
			})
		}

		var (
			applied int
			queued  int
			skipped int
			failed  int
			items   []bulkVulnerabilityActionResult
		)

		updateInput, retestInput, err := buildBulkActionPayloads(action, input)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		for _, vuln := range vulns {
			item := bulkVulnerabilityActionResult{
				ID:             vuln.ID,
				FingerprintKey: vuln.FingerprintKey,
				Action:         action,
				VulnStatus:     vuln.VulnStatus,
				RetestStatus:   vuln.RetestStatus,
			}

			switch action {
			case "retest":
				runUUID, err := queueVulnerabilityRetestTask(ctx, vuln, *retestInput)
				switch {
				case err == nil:
					item.Outcome = "queued"
					item.RunUUID = runUUID
					item.VulnStatus = vuln.VulnStatus
					item.RetestStatus = vuln.RetestStatus
					queued++
				case errors.Is(err, database.ErrVulnerabilityRetestInProgress):
					item.Outcome = "skipped"
					item.Message = err.Error()
					skipped++
				default:
					item.Outcome = "failed"
					item.Message = err.Error()
					failed++
				}
			default:
				if err := applyVulnerabilityUpdateInput(vuln, *updateInput); err != nil {
					item.Outcome = "skipped"
					item.Message = err.Error()
					skipped++
					items = append(items, item)
					continue
				}
				applyVulnerabilityStatusTimestamps(vuln)
				if err := database.UpdateVulnerabilityRecord(ctx, vuln); err != nil {
					item.Outcome = "failed"
					item.Message = err.Error()
					failed++
				} else {
					item.Outcome = "updated"
					item.VulnStatus = vuln.VulnStatus
					item.RetestStatus = vuln.RetestStatus
					applied++
				}
			}
			items = append(items, item)
		}

		return c.JSON(fiber.Map{
			"summary": fiber.Map{
				"selected": len(vulns),
				"updated":  applied,
				"queued":   queued,
				"skipped":  skipped,
				"failed":   failed,
				"action":   action,
			},
			"data": items,
		})
	}
}

// RetestVulnerability queues a retest run for a vulnerability.
func RetestVulnerability(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid vulnerability ID",
			})
		}

		var input RetestVulnerabilityInput
		if err := c.BodyParser(&input); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}

		workflowName, _ := resolveRetestWorkflow(input.Flow, input.Module)
		if workflowName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Either flow or module is required for retest",
			})
		}

		ctx := context.Background()
		vuln, err := database.GetVulnerabilityByID(ctx, id)
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": "Vulnerability not found",
			})
		}

		retestRunUUID, err := queueVulnerabilityRetestTask(ctx, vuln, input)
		if err != nil {
			if errors.Is(err, database.ErrVulnerabilityRetestInProgress) {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"error":   true,
					"message": err.Error(),
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"message":  "Retest task queued",
			"run_uuid": retestRunUUID,
			"data":     vuln,
		})
	}
}

// DeleteVulnerability handles deleting a vulnerability
// @Summary Delete vulnerability
// @Description Delete a vulnerability by ID
// @Tags Vulnerabilities
// @Produce json
// @Param id path int true "Vulnerability ID"
// @Success 200 {object} map[string]interface{} "Vulnerability deleted"
// @Failure 400 {object} map[string]interface{} "Invalid ID"
// @Failure 404 {object} map[string]interface{} "Vulnerability not found"
// @Failure 500 {object} map[string]interface{} "Failed to delete vulnerability"
// @Security BearerAuth
// @Router /osm/api/vulnerabilities/{id} [delete]
func DeleteVulnerability(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Parse ID
		idStr := c.Params("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid vulnerability ID",
			})
		}

		ctx := context.Background()

		// Delete vulnerability
		if err := database.DeleteVulnerabilityByID(ctx, id); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"message": "Vulnerability deleted successfully",
		})
	}
}

// GetVulnerabilitySummary returns severity summary for a workspace
// @Summary Get vulnerability summary
// @Description Get a summary of vulnerabilities grouped by severity
// @Tags Vulnerabilities
// @Produce json
// @Param workspace query string false "Filter by workspace name"
// @Success 200 {object} map[string]interface{} "Vulnerability summary by severity"
// @Failure 500 {object} map[string]interface{} "Failed to get summary"
// @Security BearerAuth
// @Router /osm/api/vulnerabilities/summary [get]
func GetVulnerabilitySummary(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspace := c.Query("workspace")

		ctx := context.Background()

		// Get summary
		summary, err := database.GetVulnerabilitySummary(ctx, workspace)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		// Calculate total
		total := 0
		for _, count := range summary {
			total += count
		}

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"by_severity": summary,
				"total":       total,
				"workspace":   workspace,
			},
		})
	}
}

// GetVulnerabilityBoard returns workspace-level vulnerability closure metrics.
func GetVulnerabilityBoard(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		topTargetsLimit, _ := strconv.Atoi(c.Query("top_targets_limit", "10"))
		if topTargetsLimit <= 0 {
			topTargetsLimit = 10
		}
		if topTargetsLimit > 100 {
			topTargetsLimit = 100
		}
		board, err := database.GetVulnerabilityBoardWithOptions(ctx, c.Query("workspace"), topTargetsLimit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		return c.JSON(fiber.Map{"data": board})
	}
}

func normalizeVulnerabilityStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "new":
		return "new"
	case "triaged":
		return "triaged"
	case "verified":
		return "verified"
	case "false_positive":
		return "false_positive"
	case "retest":
		return "retest"
	case "closed":
		return "closed"
	default:
		return "new"
	}
}

func buildVulnerabilityQuery(c *fiber.Ctx) database.VulnerabilityQuery {
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 10000 {
		limit = 10000
	}

	status := strings.TrimSpace(c.Query("status"))
	if status != "" {
		status = normalizeVulnerabilityStatus(status)
	}
	aiVerdict := strings.TrimSpace(c.Query("ai_verdict"))
	if aiVerdict != "" {
		aiVerdict = normalizeVulnerabilityVerdict(aiVerdict)
	}
	analystVerdict := strings.TrimSpace(c.Query("analyst_verdict"))
	if analystVerdict != "" {
		analystVerdict = normalizeVulnerabilityVerdict(analystVerdict)
	}

	return database.VulnerabilityQuery{
		Workspace:      strings.TrimSpace(c.Query("workspace")),
		Severity:       strings.ToLower(strings.TrimSpace(c.Query("severity"))),
		Confidence:     strings.TrimSpace(c.Query("confidence")),
		AssetValue:     strings.TrimSpace(c.Query("asset_value")),
		VulnStatus:     status,
		RetestStatus:   strings.ToLower(strings.TrimSpace(c.Query("retest_status"))),
		AIVerdict:      aiVerdict,
		AnalystVerdict: analystVerdict,
		FingerprintKey: strings.TrimSpace(c.Query("fingerprint_key")),
		SourceRunUUID:  strings.TrimSpace(c.Query("source_run_uuid")),
		ActiveOnly:     c.QueryBool("active_only", false),
		HasAttackChain: c.QueryBool("has_attack_chain", false),
		Offset:         offset,
		Limit:          limit,
	}
}

func buildVulnerabilityGroupSummary(vuln *database.Vulnerability) vulnerabilityGroupSummary {
	history := buildVulnerabilityEvidenceTimeline(vuln)

	runSet := make(map[string]struct{})
	assetSet := make(map[string]struct{})
	reportRefSet := make(map[string]struct{})
	attackChainLinked := strings.TrimSpace(vuln.AttackChainRef) != ""

	addStringToSet(runSet, vuln.SourceRunUUID)
	addStringToSet(runSet, vuln.RetestRunUUID)
	addStringToSet(assetSet, vuln.AssetValue)
	for _, item := range vuln.RelatedAssets {
		addStringToSet(assetSet, item)
	}
	for _, item := range vuln.ReportRefs {
		addStringToSet(reportRefSet, item)
	}

	for _, item := range history {
		addStringToSet(runSet, item.SourceRunUUID)
		addStringToSet(assetSet, item.AssetValue)
		for _, reportRef := range item.ReportRefs {
			addStringToSet(reportRefSet, reportRef)
		}
		if strings.TrimSpace(item.AttackChainRef) != "" {
			attackChainLinked = true
		}
	}

	evidenceVersions := vuln.EvidenceVersion
	if len(history) > evidenceVersions {
		evidenceVersions = len(history)
	}

	return vulnerabilityGroupSummary{
		ID:                vuln.ID,
		Workspace:         vuln.Workspace,
		FingerprintKey:    vuln.FingerprintKey,
		VulnInfo:          vuln.VulnInfo,
		VulnTitle:         vuln.VulnTitle,
		AssetType:         vuln.AssetType,
		AssetValue:        vuln.AssetValue,
		Severity:          vuln.Severity,
		Confidence:        vuln.Confidence,
		VulnStatus:        vuln.VulnStatus,
		RetestStatus:      vuln.RetestStatus,
		AIVerdict:         vuln.AIVerdict,
		AnalystVerdict:    vuln.AnalystVerdict,
		EvidenceVersions:  evidenceVersions,
		DistinctRuns:      len(runSet),
		AssetCount:        len(assetSet),
		ReportRefCount:    len(reportRefSet),
		AttackChainLinked: attackChainLinked,
		FirstSeenAt:       vuln.FirstSeenAt,
		LastSeenAt:        vuln.LastSeenAt,
		VerifiedAt:        vuln.VerifiedAt,
		ClosedAt:          vuln.ClosedAt,
	}
}

func addStringToSet(target map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	target[value] = struct{}{}
}

func normalizeVulnerabilityVerdict(verdict string) string {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "", "unknown":
		return ""
	case "confirmed":
		return "confirmed"
	case "false_positive":
		return "false_positive"
	case "needs_verification":
		return "needs_verification"
	case "retest_required":
		return "retest_required"
	default:
		return strings.ToLower(strings.TrimSpace(verdict))
	}
}

func normalizeBulkVulnerabilityAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(strings.ReplaceAll(action, "-", "_"))) {
	case "triage":
		return "triage"
	case "false_positive":
		return "false_positive"
	case "close":
		return "close"
	case "retest":
		return "retest"
	case "update":
		return "update"
	default:
		return ""
	}
}

func isValidVulnerabilityTransition(current, next string) bool {
	current = normalizeVulnerabilityStatus(current)
	next = normalizeVulnerabilityStatus(next)
	if current == next {
		return true
	}
	allowed := map[string]map[string]bool{
		"new": {
			"triaged":        true,
			"verified":       true,
			"false_positive": true,
			"retest":         true,
			"closed":         true,
		},
		"triaged": {
			"verified":       true,
			"false_positive": true,
			"retest":         true,
			"closed":         true,
		},
		"verified": {
			"retest": true,
			"closed": true,
		},
		"false_positive": {
			"triaged": true,
			"closed":  true,
		},
		"retest": {
			"triaged":        true,
			"verified":       true,
			"false_positive": true,
			"closed":         true,
		},
		"closed": {
			"retest":  true,
			"triaged": true,
		},
	}
	return allowed[current][next]
}

func applyVulnerabilityStatusTimestamps(vuln *database.Vulnerability) {
	now := time.Now()
	switch vuln.VulnStatus {
	case "verified":
		if vuln.VerifiedAt == nil {
			vuln.VerifiedAt = &now
		}
		vuln.ClosedAt = nil
		if vuln.RetestStatus == "queued" || vuln.RetestStatus == "running" {
			vuln.RetestStatus = "completed"
		}
	case "false_positive":
		vuln.ClosedAt = nil
		if vuln.RetestStatus == "queued" || vuln.RetestStatus == "running" {
			vuln.RetestStatus = "completed"
		}
	case "closed":
		if vuln.ClosedAt == nil {
			vuln.ClosedAt = &now
		}
		if vuln.RetestStatus == "queued" || vuln.RetestStatus == "running" {
			vuln.RetestStatus = "completed"
		}
	case "retest":
		vuln.ClosedAt = nil
	default:
		if vuln.VulnStatus != "closed" {
			vuln.ClosedAt = nil
		}
	}
	if vuln.VulnStatus != "verified" && vuln.VulnStatus != "closed" && vuln.VulnStatus != "retest" {
		vuln.RetestStatus = ""
	}
}

func resolveRetestWorkflow(flow, module string) (string, string) {
	if strings.TrimSpace(flow) != "" {
		return strings.TrimSpace(flow), "flow"
	}
	if strings.TrimSpace(module) != "" {
		return strings.TrimSpace(module), "module"
	}
	return "", ""
}

func normalizeRetestPriority(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "low", "normal", "high", "critical":
		return strings.ToLower(strings.TrimSpace(priority))
	default:
		return "high"
	}
}

func applyVulnerabilityUpdateInput(vuln *database.Vulnerability, input UpdateVulnerabilityInput) error {
	if input.VulnInfo != "" {
		vuln.VulnInfo = input.VulnInfo
	}
	if input.VulnTitle != "" {
		vuln.VulnTitle = input.VulnTitle
	}
	if input.VulnDesc != "" {
		vuln.VulnDesc = input.VulnDesc
	}
	if input.VulnPOC != "" {
		vuln.VulnPOC = input.VulnPOC
	}
	if input.Severity != "" {
		vuln.Severity = strings.ToLower(strings.TrimSpace(input.Severity))
	}
	if input.Confidence != "" {
		vuln.Confidence = strings.TrimSpace(input.Confidence)
	}
	if input.AssetType != "" {
		vuln.AssetType = input.AssetType
	}
	if input.AssetValue != "" {
		vuln.AssetValue = input.AssetValue
	}
	if input.Tags != nil {
		vuln.Tags = input.Tags
	}
	if input.DetailHTTPRequest != "" {
		vuln.DetailHTTPRequest = input.DetailHTTPRequest
	}
	if input.DetailHTTPResponse != "" {
		vuln.DetailHTTPResponse = input.DetailHTTPResponse
	}
	if input.RawVulnJSON != "" {
		vuln.RawVulnJSON = input.RawVulnJSON
	}
	if input.VulnStatus != "" {
		nextStatus := normalizeVulnerabilityStatus(input.VulnStatus)
		if !isValidVulnerabilityTransition(vuln.VulnStatus, nextStatus) {
			return errors.New("invalid vulnerability status transition")
		}
		vuln.VulnStatus = nextStatus
	}
	if input.AIVerdict != "" {
		vuln.AIVerdict = normalizeVulnerabilityVerdict(input.AIVerdict)
	}
	if input.AISummary != "" {
		vuln.AISummary = input.AISummary
	}
	if input.AnalystVerdict != "" {
		vuln.AnalystVerdict = normalizeVulnerabilityVerdict(input.AnalystVerdict)
	}
	if input.AnalystNotes != "" {
		vuln.AnalystNotes = input.AnalystNotes
	}
	if input.AttackChainRef != "" {
		vuln.AttackChainRef = input.AttackChainRef
	}
	if input.RelatedAssets != nil {
		vuln.RelatedAssets = input.RelatedAssets
	}
	if input.ReportRefs != nil {
		vuln.ReportRefs = input.ReportRefs
	}
	return nil
}

func buildBulkActionPayloads(action string, input BulkVulnerabilityActionInput) (*UpdateVulnerabilityInput, *RetestVulnerabilityInput, error) {
	switch action {
	case "triage":
		updateInput := UpdateVulnerabilityInput{
			VulnStatus:     "triaged",
			AIVerdict:      input.AIVerdict,
			AISummary:      input.AISummary,
			AnalystVerdict: input.AnalystVerdict,
			AnalystNotes:   input.AnalystNotes,
			AttackChainRef: input.AttackChainRef,
			RelatedAssets:  input.RelatedAssets,
			ReportRefs:     input.ReportRefs,
		}
		return &updateInput, nil, nil
	case "false_positive":
		updateInput := UpdateVulnerabilityInput{
			VulnStatus:     "false_positive",
			AIVerdict:      input.AIVerdict,
			AISummary:      input.AISummary,
			AnalystVerdict: input.AnalystVerdict,
			AnalystNotes:   input.AnalystNotes,
			AttackChainRef: input.AttackChainRef,
			RelatedAssets:  input.RelatedAssets,
			ReportRefs:     input.ReportRefs,
		}
		return &updateInput, nil, nil
	case "close":
		updateInput := UpdateVulnerabilityInput{
			VulnStatus:     "closed",
			AIVerdict:      input.AIVerdict,
			AISummary:      input.AISummary,
			AnalystVerdict: input.AnalystVerdict,
			AnalystNotes:   input.AnalystNotes,
			AttackChainRef: input.AttackChainRef,
			RelatedAssets:  input.RelatedAssets,
			ReportRefs:     input.ReportRefs,
		}
		return &updateInput, nil, nil
	case "update":
		updateInput := UpdateVulnerabilityInput{
			VulnStatus:     input.Status,
			AIVerdict:      input.AIVerdict,
			AISummary:      input.AISummary,
			AnalystVerdict: input.AnalystVerdict,
			AnalystNotes:   input.AnalystNotes,
			AttackChainRef: input.AttackChainRef,
			RelatedAssets:  input.RelatedAssets,
			ReportRefs:     input.ReportRefs,
		}
		if strings.TrimSpace(updateInput.VulnStatus) == "" &&
			strings.TrimSpace(updateInput.AIVerdict) == "" &&
			strings.TrimSpace(updateInput.AISummary) == "" &&
			strings.TrimSpace(updateInput.AnalystVerdict) == "" &&
			strings.TrimSpace(updateInput.AnalystNotes) == "" &&
			strings.TrimSpace(updateInput.AttackChainRef) == "" &&
			updateInput.RelatedAssets == nil &&
			updateInput.ReportRefs == nil {
			return nil, nil, errors.New("bulk update requires at least one mutable field")
		}
		return &updateInput, nil, nil
	case "retest":
		retestInput := RetestVulnerabilityInput{
			Flow:     input.Flow,
			Module:   input.Module,
			Params:   input.Params,
			Priority: input.Priority,
		}
		workflowName, _ := resolveRetestWorkflow(retestInput.Flow, retestInput.Module)
		if workflowName == "" {
			return nil, nil, errors.New("either flow or module is required for retest")
		}
		return nil, &retestInput, nil
	default:
		return nil, nil, errors.New("invalid bulk action")
	}
}

func resolveBulkVulnerabilities(ctx context.Context, input BulkVulnerabilityActionInput) ([]*database.Vulnerability, error) {
	if len(input.IDs) == 0 && len(input.FingerprintKeys) == 0 {
		return nil, errors.New("either ids or fingerprint_keys is required")
	}

	seen := make(map[int64]struct{})
	result := make([]*database.Vulnerability, 0, len(input.IDs)+len(input.FingerprintKeys))
	for _, id := range input.IDs {
		if id <= 0 {
			continue
		}
		vuln, err := database.GetVulnerabilityByID(ctx, id)
		if err != nil || vuln == nil {
			continue
		}
		if workspace := strings.TrimSpace(input.Workspace); workspace != "" && !strings.EqualFold(strings.TrimSpace(vuln.Workspace), workspace) {
			continue
		}
		if _, ok := seen[vuln.ID]; ok {
			continue
		}
		seen[vuln.ID] = struct{}{}
		result = append(result, vuln)
	}

	for _, fingerprintKey := range input.FingerprintKeys {
		fingerprintKey = strings.TrimSpace(fingerprintKey)
		if fingerprintKey == "" {
			continue
		}
		items, err := database.ListVulnerabilities(ctx, database.VulnerabilityQuery{
			Workspace:      strings.TrimSpace(input.Workspace),
			FingerprintKey: fingerprintKey,
			Limit:          100,
		})
		if err != nil {
			continue
		}
		for i := range items.Data {
			vuln := items.Data[i]
			if _, ok := seen[vuln.ID]; ok {
				continue
			}
			seen[vuln.ID] = struct{}{}
			copyVuln := vuln
			result = append(result, &copyVuln)
		}
	}

	return result, nil
}

func queueVulnerabilityRetestTask(ctx context.Context, vuln *database.Vulnerability, input RetestVulnerabilityInput) (string, error) {
	workflowName, workflowKind := resolveRetestWorkflow(input.Flow, input.Module)
	if workflowName == "" {
		return "", errors.New("either flow or module is required for retest")
	}

	target := strings.TrimSpace(vuln.AssetValue)
	if target == "" {
		target = vuln.Workspace
	}

	params := make(map[string]interface{})
	for key, value := range input.Params {
		params[key] = value
	}
	workspace := strings.TrimSpace(vuln.Workspace)
	if workspace == "" {
		workspace = computeWorkspace(target, input.Params)
	}
	params["target"] = target
	params["workspace"] = workspace
	params["space_name"] = workspace
	params["retest_vulnerability_id"] = strconv.FormatInt(vuln.ID, 10)

	retestRunUUID := uuid.New().String()
	run := &database.Run{
		RunUUID:      retestRunUUID,
		WorkflowName: workflowName,
		WorkflowKind: workflowKind,
		Target:       target,
		Params:       params,
		Status:       "queued",
		TriggerType:  "vuln-retest",
		RunPriority:  normalizeRetestPriority(input.Priority),
		RunMode:      "queue",
		IsQueued:     true,
		Workspace:    workspace,
	}

	vuln.VulnStatus = "retest"
	vuln.RetestStatus = "queued"
	vuln.RetestRunUUID = retestRunUUID
	applyVulnerabilityStatusTimestamps(vuln)
	if err := database.QueueVulnerabilityRetest(ctx, vuln, run); err != nil {
		return "", err
	}
	return retestRunUUID, nil
}

func buildVulnerabilityDetail(ctx context.Context, vuln *database.Vulnerability) vulnerabilityDetailResponse {
	detail := vulnerabilityDetailResponse{
		Vulnerability: *vuln,
	}
	detail.EvidenceTimeline = buildVulnerabilityEvidenceTimeline(vuln)
	detail.RelatedRuns = findVulnerabilityRelatedRuns(ctx, vuln, detail.EvidenceTimeline)
	detail.StatusTimeline = buildVulnerabilityStatusTimeline(vuln, detail.EvidenceTimeline, detail.RelatedRuns)
	detail.RetestTimeline = buildVulnerabilityRetestTimeline(detail.RelatedRuns)
	detail.RelatedAssetRecords = findVulnerabilityRelatedAssets(ctx, vuln, detail.EvidenceTimeline)
	detail.RelatedAttackChains = findVulnerabilityRelatedAttackChains(ctx, vuln, detail.EvidenceTimeline)
	return detail
}

func buildVulnerabilityEvidenceTimeline(vuln *database.Vulnerability) []database.VulnerabilityEvidence {
	history := database.ParseVulnerabilityEvidenceHistory(vuln.EvidenceHistory)
	if len(history) == 0 {
		history = append(history, database.VulnerabilityEvidence{
			ObservedAt:     maxTime(vuln.LastSeenAt, vuln.UpdatedAt, vuln.CreatedAt),
			SourceRunUUID:  strings.TrimSpace(vuln.SourceRunUUID),
			Severity:       strings.TrimSpace(vuln.Severity),
			Confidence:     strings.TrimSpace(vuln.Confidence),
			VulnStatus:     strings.TrimSpace(vuln.VulnStatus),
			AssetValue:     strings.TrimSpace(vuln.AssetValue),
			ReportRefs:     append([]string(nil), vuln.ReportRefs...),
			AttackChainRef: strings.TrimSpace(vuln.AttackChainRef),
		})
	}
	sort.Slice(history, func(i, j int) bool {
		return history[i].ObservedAt.Before(history[j].ObservedAt)
	})
	return history
}

func buildVulnerabilityStatusTimeline(vuln *database.Vulnerability, history []database.VulnerabilityEvidence, runs []vulnerabilityRunSummary) []vulnerabilityStatusTimelineItem {
	if vuln == nil {
		return nil
	}

	runByUUID := make(map[string]vulnerabilityRunSummary, len(runs))
	for _, run := range runs {
		runByUUID[strings.TrimSpace(run.RunUUID)] = run
	}

	result := make([]vulnerabilityStatusTimelineItem, 0, len(history))
	for idx, item := range history {
		retestStatus := ""
		runUUID := strings.TrimSpace(item.SourceRunUUID)
		if run, ok := runByUUID[runUUID]; ok && strings.Contains(strings.ToLower(strings.TrimSpace(run.TriggerType)), "retest") {
			retestStatus = strings.TrimSpace(run.Status)
		}
		if idx == len(history)-1 && strings.TrimSpace(vuln.RetestStatus) != "" {
			retestStatus = strings.TrimSpace(vuln.RetestStatus)
		}
		result = append(result, vulnerabilityStatusTimelineItem{
			EvidenceVersion: idx + 1,
			ObservedAt:      item.ObservedAt,
			VulnStatus:      strings.TrimSpace(item.VulnStatus),
			RetestStatus:    retestStatus,
			Severity:        strings.TrimSpace(item.Severity),
			Confidence:      strings.TrimSpace(item.Confidence),
			SourceRunUUID:   runUUID,
			AttackChainRef:  strings.TrimSpace(item.AttackChainRef),
		})
	}
	return result
}

func buildVulnerabilityRetestTimeline(runs []vulnerabilityRunSummary) []vulnerabilityRetestTimelineItem {
	result := make([]vulnerabilityRetestTimelineItem, 0, len(runs))
	for _, run := range runs {
		if !strings.Contains(strings.ToLower(strings.TrimSpace(run.TriggerType)), "retest") {
			continue
		}
		result = append(result, vulnerabilityRetestTimelineItem{
			RunUUID:      run.RunUUID,
			WorkflowName: run.WorkflowName,
			WorkflowKind: run.WorkflowKind,
			Status:       run.Status,
			TriggerType:  run.TriggerType,
			CreatedAt:    run.CreatedAt,
			StartedAt:    run.StartedAt,
			CompletedAt:  run.CompletedAt,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func findVulnerabilityRelatedRuns(ctx context.Context, vuln *database.Vulnerability, history []database.VulnerabilityEvidence) []vulnerabilityRunSummary {
	runUUIDs := make([]string, 0, len(history)+2)
	runUUIDs = append(runUUIDs, vuln.SourceRunUUID, vuln.RetestRunUUID)
	for _, item := range history {
		runUUIDs = append(runUUIDs, item.SourceRunUUID)
	}

	seen := make(map[string]struct{})
	result := make([]vulnerabilityRunSummary, 0)
	for _, runUUID := range runUUIDs {
		runUUID = strings.TrimSpace(runUUID)
		if runUUID == "" {
			continue
		}
		if _, ok := seen[runUUID]; ok {
			continue
		}
		seen[runUUID] = struct{}{}

		run, err := database.GetRunByID(ctx, runUUID, false, false)
		if err != nil || run == nil {
			continue
		}
		result = append(result, vulnerabilityRunSummary{
			RunUUID:      run.RunUUID,
			WorkflowName: run.WorkflowName,
			WorkflowKind: run.WorkflowKind,
			Status:       run.Status,
			TriggerType:  run.TriggerType,
			Target:       run.Target,
			CreatedAt:    run.CreatedAt,
			StartedAt:    run.StartedAt,
			CompletedAt:  run.CompletedAt,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func findVulnerabilityRelatedAssets(ctx context.Context, vuln *database.Vulnerability, history []database.VulnerabilityEvidence) []vulnerabilityAssetRecord {
	db := database.GetDB()
	if db == nil {
		return nil
	}

	candidates := make([]string, 0, len(vuln.RelatedAssets)+len(history)+1)
	candidates = append(candidates, vuln.AssetValue)
	candidates = append(candidates, vuln.RelatedAssets...)
	for _, item := range history {
		candidates = append(candidates, item.AssetValue)
	}
	candidates = dedupeNonEmptyStrings(candidates)
	if len(candidates) == 0 {
		return nil
	}

	seen := make(map[int64]struct{})
	result := make([]vulnerabilityAssetRecord, 0)
	for _, candidate := range candidates {
		var assets []database.Asset
		if err := db.NewSelect().
			Model(&assets).
			Column("id", "asset_value", "url", "asset_type", "source", "updated_at").
			Where("workspace = ?", vuln.Workspace).
			WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
				return q.
					WhereOr("asset_value = ?", candidate).
					WhereOr("url = ?", candidate).
					WhereOr("external_url = ?", candidate)
			}).
			Order("updated_at DESC").
			Limit(10).
			Scan(ctx); err != nil {
			continue
		}
		for _, asset := range assets {
			if _, ok := seen[asset.ID]; ok {
				continue
			}
			seen[asset.ID] = struct{}{}
			result = append(result, vulnerabilityAssetRecord{
				ID:         asset.ID,
				AssetValue: asset.AssetValue,
				URL:        asset.URL,
				AssetType:  asset.AssetType,
				Source:     asset.Source,
				UpdatedAt:  asset.UpdatedAt,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result
}

func findVulnerabilityRelatedAttackChains(ctx context.Context, vuln *database.Vulnerability, history []database.VulnerabilityEvidence) []vulnerabilityAttackChainSummary {
	db := database.GetDB()
	if db == nil || strings.TrimSpace(vuln.Workspace) == "" {
		return nil
	}

	var reports []database.AttackChainReport
	if err := db.NewSelect().
		Model(&reports).
		Where("workspace = ?", vuln.Workspace).
		Order("updated_at DESC").
		Limit(50).
		Scan(ctx); err != nil {
		return nil
	}

	result := make([]vulnerabilityAttackChainSummary, 0)
	seen := make(map[string]struct{})
	for _, report := range reports {
		for _, chain := range decodeAttackChains(report.AttackChainsJSON) {
			matchedBy := matchVulnerabilityToAttackChain(vuln, history, report.ID, chain)
			if matchedBy == "" {
				continue
			}
			key := strconv.FormatInt(report.ID, 10) + ":" + chain.ChainID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, vulnerabilityAttackChainSummary{
				ReportID:    report.ID,
				ChainID:     chain.ChainID,
				ChainName:   chain.ChainName,
				Target:      report.Target,
				RunUUID:     report.RunUUID,
				SourcePath:  report.SourcePath,
				MatchedBy:   matchedBy,
				Impact:      chain.Impact,
				Difficulty:  chain.Difficulty,
				Probability: chain.SuccessProbability,
			})
		}
	}
	return result
}

func matchVulnerabilityToAttackChain(vuln *database.Vulnerability, history []database.VulnerabilityEvidence, reportID int64, chain attackChainItem) string {
	if matchesAttackChainReference(vuln.AttackChainRef, reportID, chain.ChainID) {
		return "attack_chain_ref"
	}

	urlCandidates := make([]string, 0, len(vuln.RelatedAssets)+len(history)+1)
	urlCandidates = append(urlCandidates, vuln.AssetValue)
	urlCandidates = append(urlCandidates, vuln.RelatedAssets...)
	for _, item := range history {
		urlCandidates = append(urlCandidates, item.AssetValue)
	}
	if url := strings.TrimSpace(chain.EntryPoint.URL); url != "" {
		for _, candidate := range dedupeNonEmptyStrings(urlCandidates) {
			if strings.Contains(normalizeURLMatchValue(candidate), normalizeURLMatchValue(url)) || strings.Contains(normalizeURLMatchValue(url), normalizeURLMatchValue(candidate)) {
				return "entry_point_url"
			}
		}
	}

	nameCandidates := dedupeNonEmptyStrings([]string{
		vuln.VulnTitle,
		vuln.VulnInfo,
	})
	entryName := strings.ToLower(strings.TrimSpace(chain.EntryPoint.Vulnerability))
	if entryName != "" {
		for _, candidate := range nameCandidates {
			normalized := strings.ToLower(strings.TrimSpace(candidate))
			if normalized == "" {
				continue
			}
			if strings.Contains(normalized, entryName) || strings.Contains(entryName, normalized) {
				return "entry_point_vulnerability"
			}
		}
	}

	return ""
}

func matchesAttackChainReference(ref string, reportID int64, chainID string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	if ref == "" {
		return false
	}
	return strings.Contains(ref, strings.ToLower(strings.TrimSpace(chainID))) &&
		strings.Contains(ref, strconv.FormatInt(reportID, 10))
}

func normalizeURLMatchValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, "/")
	return value
}

func dedupeNonEmptyStrings(values []string) []string {
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

func maxTime(values ...time.Time) time.Time {
	var result time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if result.IsZero() || value.After(result) {
			result = value
		}
	}
	return result
}
