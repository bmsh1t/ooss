package handlers

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/attackchain"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
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

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"report":              report,
				"chains":              chains,
				"critical_paths":      decodeAttackPaths(report.CriticalPathsJSON),
				"execution_checklist": buildExecutionChecklist(chains),
				"source_files": fiber.Map{
					"json":    report.SourcePath,
					"mermaid": report.MermaidPath,
					"text":    report.TextPath,
				},
			},
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

		summary, err := attackchain.ImportFile(
			context.Background(),
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

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data":    summary,
			"message": "Attack chain report imported successfully",
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
