package handlers

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
)

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
			Workspace:  workspace,
			Severity:   severity,
			Confidence: confidence,
			AssetValue: assetValue,
			VulnStatus: vulnStatus,
			Offset:     offset,
			Limit:      limit,
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

		return c.JSON(fiber.Map{
			"data": vuln,
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
