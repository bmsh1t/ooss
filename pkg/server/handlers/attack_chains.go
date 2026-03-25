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
	LinkedVulnerabilities []linkedVulnerability `json:"linked_vulnerabilities,omitempty"`
	LinkedAssets          []linkedAsset         `json:"linked_assets,omitempty"`
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

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"report":              report,
				"chains":              enrichedChains,
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
		result = append(result, attackChainWorkbenchItem{
			attackChainItem:       chain,
			LinkedVulnerabilities: findLinkedVulnerabilities(ctx, workspace, chain),
			LinkedAssets:          findLinkedAssets(ctx, workspace, chain),
		})
	}
	return result
}

func findLinkedVulnerabilities(ctx context.Context, workspace string, chain attackChainItem) []linkedVulnerability {
	db := database.GetDB()
	if db == nil {
		return nil
	}

	var vulns []database.Vulnerability
	query := db.NewSelect().
		Model(&vulns).
		Column("id", "vuln_title", "severity", "vuln_status", "asset_value").
		Where("workspace = ?", workspace)

	entryName := strings.ToLower(strings.TrimSpace(chain.EntryPoint.Vulnerability))
	entryURL := strings.TrimSpace(chain.EntryPoint.URL)

	if entryName != "" {
		query = query.Where("LOWER(vuln_title) LIKE ?", "%"+entryName+"%")
	}
	if entryURL != "" {
		query = query.Where("asset_value LIKE ?", "%"+entryURL+"%")
	}

	if entryName == "" && entryURL == "" {
		return nil
	}

	if err := query.Order("updated_at DESC").Limit(10).Scan(ctx); err != nil {
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
