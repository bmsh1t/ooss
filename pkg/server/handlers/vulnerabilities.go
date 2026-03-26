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
		// Parse query parameters
		workspace := c.Query("workspace")
		severity := c.Query("severity")
		confidence := c.Query("confidence")
		assetValue := c.Query("asset_value")
		vulnStatus := c.Query("status")
		fingerprintKey := c.Query("fingerprint_key")
		sourceRunUUID := c.Query("source_run_uuid")
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		limit, _ := strconv.Atoi(c.Query("limit", "20"))

		// Validate pagination
		if offset < 0 {
			offset = 0
		}
		if limit <= 0 {
			limit = 20
		}
		if limit > 10000 {
			limit = 10000
		}

		ctx := context.Background()

		// Get vulnerabilities from database
		result, err := database.ListVulnerabilities(ctx, database.VulnerabilityQuery{
			Workspace:      workspace,
			Severity:       severity,
			Confidence:     confidence,
			AssetValue:     assetValue,
			VulnStatus:     vulnStatus,
			FingerprintKey: fingerprintKey,
			SourceRunUUID:  sourceRunUUID,
			Offset:         offset,
			Limit:          limit,
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
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error":   true,
					"message": "Invalid vulnerability status transition",
				})
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

		workflowName, workflowKind := resolveRetestWorkflow(input.Flow, input.Module)
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
		board, err := database.GetVulnerabilityBoard(ctx, c.Query("workspace"))
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
