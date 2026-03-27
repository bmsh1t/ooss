package attackchain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/uptrace/bun"
)

type summaryChain struct {
	EntryPoint struct {
		Vulnerability string `json:"vulnerability"`
		URL           string `json:"url"`
	} `json:"entry_point"`
}

// FindLinkedVulnerabilities returns the current vulnerability rows that match an
// attack-chain entry point. This is the shared matching layer used by both the
// workbench and campaign/deep-scan risk logic.
func FindLinkedVulnerabilities(ctx context.Context, workspace, vulnerabilityName, entryURL string, limit int) ([]database.Vulnerability, error) {
	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	workspace = strings.TrimSpace(workspace)
	vulnerabilityName = strings.ToLower(strings.TrimSpace(vulnerabilityName))
	entryURL = strings.TrimSpace(entryURL)
	if workspace == "" || (vulnerabilityName == "" && entryURL == "") {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	var vulns []database.Vulnerability
	query := db.NewSelect().
		Model(&vulns).
		Where("workspace = ?", workspace)

	if vulnerabilityName != "" {
		query = query.WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			pattern := "%" + vulnerabilityName + "%"
			return q.
				WhereOr("LOWER(vuln_title) LIKE ?", pattern).
				WhereOr("LOWER(vuln_info) LIKE ?", pattern)
		})
	}
	if entryURL != "" {
		query = query.Where("asset_value LIKE ?", "%"+entryURL+"%")
	}

	if err := query.
		Order("updated_at DESC").
		Limit(limit).
		Scan(ctx); err != nil {
		return nil, err
	}
	return vulns, nil
}

// GetTargetSummary computes live attack-chain risk summary for a single target
// using current linked vulnerability state instead of stale persisted metrics.
func GetTargetSummary(ctx context.Context, workspace, target string) (*database.AttackChainWorkspaceSummary, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return &database.AttackChainWorkspaceSummary{}, nil
	}

	reports, err := listAttackChainReports(ctx, workspace, strings.TrimSpace(target))
	if err != nil {
		return nil, err
	}

	summary := &database.AttackChainWorkspaceSummary{
		Workspace: workspace,
	}
	for _, report := range reports {
		summary.ReportCount++
		summary.TotalChains += report.TotalChains
		summary.CriticalChains += report.CriticalChains
		summary.HighImpactChains += report.HighImpactChains
		summary.QueueHits += report.QueueHits
		summary.VerifiedHits += countVerifiedHitsForReport(ctx, report)
		summary.OperationalHits += countOperationalHitsForReport(ctx, report)
		if report.LastQueuedAt != nil {
			if summary.LastQueuedAt == nil || report.LastQueuedAt.After(*summary.LastQueuedAt) {
				copyTime := *report.LastQueuedAt
				summary.LastQueuedAt = &copyTime
			}
		}
	}
	return summary, nil
}

func listAttackChainReports(ctx context.Context, workspace, target string) ([]database.AttackChainReport, error) {
	const pageSize = 200

	db := database.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	var reports []database.AttackChainReport
	for offset := 0; ; offset += pageSize {
		var page []database.AttackChainReport
		query := db.NewSelect().
			Model(&page).
			Where("workspace = ?", workspace).
			Order("updated_at DESC").
			Offset(offset).
			Limit(pageSize)
		if target != "" {
			query = query.Where("target = ?", target)
		}
		if err := query.Scan(ctx); err != nil {
			return nil, err
		}

		reports = append(reports, page...)
		if len(page) < pageSize {
			break
		}
	}
	return reports, nil
}

func countVerifiedHitsForReport(ctx context.Context, report database.AttackChainReport) int {
	return countMatchedHitsForReport(ctx, report, false)
}

func countOperationalHitsForReport(ctx context.Context, report database.AttackChainReport) int {
	return countMatchedHitsForReport(ctx, report, true)
}

func countMatchedHitsForReport(ctx context.Context, report database.AttackChainReport, includeRetest bool) int {
	chains := decodeSummaryChains(report.AttackChainsJSON)
	if len(chains) == 0 {
		return 0
	}

	total := 0
	for _, chain := range chains {
		linked, err := FindLinkedVulnerabilities(ctx, report.Workspace, chain.EntryPoint.Vulnerability, chain.EntryPoint.URL, 10)
		if err != nil {
			continue
		}
		for _, vuln := range linked {
			status := strings.TrimSpace(vuln.VulnStatus)
			if strings.EqualFold(status, "verified") || (includeRetest && strings.EqualFold(status, "retest")) {
				total++
			}
		}
	}
	return total
}

func decodeSummaryChains(raw string) []summaryChain {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var chains []summaryChain
	if err := json.Unmarshal([]byte(raw), &chains); err != nil {
		return nil
	}
	return chains
}
