package database

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// CampaignResult holds paginated campaigns.
type CampaignResult struct {
	Data       []Campaign `json:"data"`
	TotalCount int        `json:"total_count"`
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
}

// CampaignQuery controls campaign list queries.
type CampaignQuery struct {
	Status string
	Offset int
	Limit  int
}

// CreateCampaign stores a new campaign record.
func CreateCampaign(ctx context.Context, campaign *Campaign) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}
	now := time.Now()
	campaign.CreatedAt = now
	campaign.UpdatedAt = now
	if strings.TrimSpace(campaign.Status) == "" {
		campaign.Status = "queued"
	}
	if _, err := db.NewInsert().Model(campaign).Exec(ctx); err != nil {
		return fmt.Errorf("failed to create campaign: %w", err)
	}
	return nil
}

// GetCampaignByID returns a campaign by ID.
func GetCampaignByID(ctx context.Context, id string) (*Campaign, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	var campaign Campaign
	if err := db.NewSelect().Model(&campaign).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, fmt.Errorf("campaign not found: %w", err)
	}
	return &campaign, nil
}

// ListCampaigns lists campaigns with optional status filtering.
func ListCampaigns(ctx context.Context, query CampaignQuery) (*CampaignResult, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 1000 {
		query.Limit = 1000
	}

	base := db.NewSelect().Model((*Campaign)(nil))
	if strings.TrimSpace(query.Status) != "" {
		base = base.Where("status = ?", query.Status)
	}

	total, err := base.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count campaigns: %w", err)
	}

	var campaigns []Campaign
	listQuery := db.NewSelect().Model(&campaigns)
	if strings.TrimSpace(query.Status) != "" {
		listQuery = listQuery.Where("status = ?", query.Status)
	}
	if err := listQuery.Order("updated_at DESC").Offset(query.Offset).Limit(query.Limit).Scan(ctx); err != nil {
		return nil, fmt.Errorf("failed to list campaigns: %w", err)
	}

	return &CampaignResult{
		Data:       campaigns,
		TotalCount: total,
		Offset:     query.Offset,
		Limit:      query.Limit,
	}, nil
}

// UpdateCampaignStatus updates a campaign status.
func UpdateCampaignStatus(ctx context.Context, id, status string) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}
	_, err := db.NewUpdate().
		Model((*Campaign)(nil)).
		Set("status = ?", status).
		Set("updated_at = CASE WHEN status = ? THEN updated_at ELSE ? END", status, time.Now()).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update campaign status: %w", err)
	}
	return nil
}

// UpdateCampaignParams updates the params payload for a campaign.
func UpdateCampaignParams(ctx context.Context, id string, params map[string]interface{}) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}
	_, err := db.NewUpdate().
		Model((*Campaign)(nil)).
		Set("params = ?", params).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update campaign params: %w", err)
	}
	return nil
}

// HasCampaignDeepScanRun checks whether a deep-scan run has already been queued for a target.
func HasCampaignDeepScanRun(ctx context.Context, campaignID, workspace, workflowName, target string) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("database not connected")
	}
	query := db.NewSelect().
		Model((*Run)(nil)).
		Where("run_group_id = ?", campaignID).
		Where("workspace = ?", workspace).
		Where("workflow_name = ?", workflowName).
		Where("trigger_type = ?", "campaign-deep-scan")
	if strings.TrimSpace(target) != "" {
		query = query.Where("target = ?", strings.TrimSpace(target))
	}
	count, err := query.Count(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to query deep scan runs: %w", err)
	}
	return count > 0, nil
}
