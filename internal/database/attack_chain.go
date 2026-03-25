package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// AttackChainQuery filters attack-chain report listing.
type AttackChainQuery struct {
	Workspace string
	Target    string
	RunUUID   string
	Offset    int
	Limit     int
}

// AttackChainReportResult holds paginated attack-chain reports.
type AttackChainReportResult struct {
	Data       []AttackChainReport `json:"data"`
	TotalCount int                 `json:"total_count"`
	Offset     int                 `json:"offset"`
	Limit      int                 `json:"limit"`
}

// UpsertAttackChainReport creates or updates an attack-chain report keyed by workspace + source path.
func UpsertAttackChainReport(ctx context.Context, report *AttackChainReport) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}
	if report == nil {
		return fmt.Errorf("attack chain report is required")
	}

	report.Workspace = strings.TrimSpace(report.Workspace)
	report.SourcePath = strings.TrimSpace(report.SourcePath)
	if report.Workspace == "" || report.SourcePath == "" {
		return fmt.Errorf("workspace and source_path are required")
	}

	var existing AttackChainReport
	err := db.NewSelect().
		Model(&existing).
		Where("workspace = ? AND source_path = ?", report.Workspace, report.SourcePath).
		Scan(ctx)
	if err == nil {
		if strings.TrimSpace(report.Target) == "" {
			report.Target = existing.Target
		}
		if strings.TrimSpace(report.RunUUID) == "" {
			report.RunUUID = existing.RunUUID
		}
		if strings.TrimSpace(report.MermaidPath) == "" {
			report.MermaidPath = existing.MermaidPath
		}
		if strings.TrimSpace(report.TextPath) == "" {
			report.TextPath = existing.TextPath
		}
		if report.QueueHits == 0 {
			report.QueueHits = existing.QueueHits
		}
		if report.VerifiedHits == 0 {
			report.VerifiedHits = existing.VerifiedHits
		}
		if report.LastQueuedAt == nil {
			report.LastQueuedAt = existing.LastQueuedAt
		}
		report.ID = existing.ID
		report.CreatedAt = existing.CreatedAt
		report.UpdatedAt = time.Now()

		_, err = db.NewUpdate().
			Model(report).
			Column(
				"target",
				"run_uuid",
				"source_hash",
				"status",
				"total_chains",
				"critical_chains",
				"high_impact_chains",
				"most_likely_entry_points",
				"attack_chains_json",
				"critical_paths_json",
				"defense_recommendations",
				"mermaid_path",
				"text_path",
				"queue_hits",
				"verified_hits",
				"last_queued_at",
				"updated_at",
			).
			WherePK().
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update attack chain report: %w", err)
		}
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to load existing attack chain report: %w", err)
	}

	now := time.Now()
	report.CreatedAt = now
	report.UpdatedAt = now
	_, err = db.NewInsert().Model(report).Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create attack chain report: %w", err)
	}
	return nil
}

// ListAttackChainReports returns paginated attack-chain reports.
func ListAttackChainReports(ctx context.Context, query AttackChainQuery) (*AttackChainReportResult, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	result := &AttackChainReportResult{
		Offset: query.Offset,
		Limit:  query.Limit,
	}

	baseQuery := db.NewSelect().Model((*AttackChainReport)(nil))
	if query.Workspace != "" {
		baseQuery = baseQuery.Where("workspace = ?", query.Workspace)
	}
	if query.Target != "" {
		baseQuery = baseQuery.Where("target LIKE ?", "%"+query.Target+"%")
	}
	if query.RunUUID != "" {
		baseQuery = baseQuery.Where("run_uuid = ?", query.RunUUID)
	}

	totalCount, err := baseQuery.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count attack chain reports: %w", err)
	}
	result.TotalCount = totalCount

	var reports []AttackChainReport
	err = db.NewSelect().
		Model(&reports).
		ExcludeColumn("attack_chains_json", "critical_paths_json").
		Apply(func(q *bun.SelectQuery) *bun.SelectQuery {
			if query.Workspace != "" {
				q = q.Where("workspace = ?", query.Workspace)
			}
			if query.Target != "" {
				q = q.Where("target LIKE ?", "%"+query.Target+"%")
			}
			if query.RunUUID != "" {
				q = q.Where("run_uuid = ?", query.RunUUID)
			}
			return q
		}).
		Order("updated_at DESC").
		Offset(query.Offset).
		Limit(query.Limit).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch attack chain reports: %w", err)
	}

	result.Data = reports
	return result, nil
}

// GetAttackChainReportByID fetches a single attack-chain report by ID.
func GetAttackChainReportByID(ctx context.Context, id int64) (*AttackChainReport, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}

	var report AttackChainReport
	if err := db.NewSelect().
		Model(&report).
		Where("id = ?", id).
		Scan(ctx); err != nil {
		return nil, fmt.Errorf("attack chain report not found: %w", err)
	}

	return &report, nil
}

// RecordAttackChainQueueActivity updates queue/relevance metrics after queue generation.
func RecordAttackChainQueueActivity(ctx context.Context, id int64, queuedHits, verifiedHits int) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}
	now := time.Now()
	_, err := db.NewUpdate().
		Model((*AttackChainReport)(nil)).
		Set("queue_hits = queue_hits + ?", queuedHits).
		Set("verified_hits = ?", verifiedHits).
		Set("last_queued_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update attack chain queue metrics: %w", err)
	}
	return nil
}
