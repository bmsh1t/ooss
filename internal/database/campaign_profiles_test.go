package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCampaignReportProfileCRUD_PersistsInCampaignParams(t *testing.T) {
	cleanup := setupCampaignProfileTestDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	campaign := &Campaign{
		ID:           "camp-profile-db",
		Name:         "profile-db",
		WorkflowName: "general",
		WorkflowKind: "flow",
		Status:       "queued",
		Params: map[string]interface{}{
			"existing_flag": "keep-me",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, CreateCampaign(ctx, campaign))

	saved, err := UpsertCampaignReportProfile(ctx, campaign.ID, CampaignReportProfile{
		Name:        "Ops-Handoff",
		Description: "operator handoff",
		Filters: CampaignReportProfileFilters{
			RiskLevels:   []string{"high", "critical", "high"},
			Statuses:     []string{"completed"},
			TriggerTypes: []string{"campaign-rerun"},
		},
		Sort: CampaignReportProfileSort{
			By:    "target",
			Order: "desc",
		},
		Format: "JSON",
	})
	require.NoError(t, err)
	assert.Equal(t, "ops-handoff", saved.Name)
	assert.Equal(t, "operator handoff", saved.Description)
	assert.Equal(t, []string{"high", "critical"}, saved.Filters.RiskLevels)
	assert.Equal(t, []string{"completed"}, saved.Filters.Statuses)
	assert.Equal(t, []string{"campaign-rerun"}, saved.Filters.TriggerTypes)
	assert.Equal(t, "target", saved.Sort.By)
	assert.Equal(t, "desc", saved.Sort.Order)
	assert.Equal(t, "json", saved.Format)
	assert.False(t, saved.CreatedAt.IsZero())
	assert.False(t, saved.UpdatedAt.IsZero())

	refreshed, err := GetCampaignByID(ctx, campaign.ID)
	require.NoError(t, err)
	assert.Equal(t, "keep-me", refreshed.Params["existing_flag"])
	_, ok := refreshed.Params[campaignReportProfilesParamKey]
	assert.True(t, ok)

	got, err := GetCampaignReportProfile(refreshed, "OPS-HANDOFF")
	require.NoError(t, err)
	assert.Equal(t, "ops-handoff", got.Name)
	assert.Equal(t, "json", got.Format)

	_, err = UpsertCampaignReportProfile(ctx, campaign.ID, CampaignReportProfile{
		Name: "alpha-view",
		Sort: CampaignReportProfileSort{
			By:    "risk",
			Order: "desc",
		},
	})
	require.NoError(t, err)

	refreshed, err = GetCampaignByID(ctx, campaign.ID)
	require.NoError(t, err)
	profiles, err := ListCampaignReportProfiles(refreshed)
	require.NoError(t, err)
	require.Len(t, profiles, 2)
	assert.Equal(t, "alpha-view", profiles[0].Name)
	assert.Equal(t, "ops-handoff", profiles[1].Name)

	deleted, err := DeleteCampaignReportProfile(ctx, campaign.ID, "ops-handoff")
	require.NoError(t, err)
	assert.True(t, deleted)

	deleted, err = DeleteCampaignReportProfile(ctx, campaign.ID, "alpha-view")
	require.NoError(t, err)
	assert.True(t, deleted)

	refreshed, err = GetCampaignByID(ctx, campaign.ID)
	require.NoError(t, err)
	assert.Equal(t, "keep-me", refreshed.Params["existing_flag"])
	_, ok = refreshed.Params[campaignReportProfilesParamKey]
	assert.False(t, ok)
}

func TestNormalizeCampaignReportProfileName_RejectsInvalidInput(t *testing.T) {
	_, err := NormalizeCampaignReportProfileName("ops handoff")
	require.Error(t, err)

	normalized, err := NormalizeCampaignReportProfileName("  OPS-HANDOFF  ")
	require.NoError(t, err)
	assert.Equal(t, "ops-handoff", normalized)
}

func setupCampaignProfileTestDB(t *testing.T) func() {
	t.Helper()

	cfg := &config.Config{
		BaseFolder: t.TempDir(),
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(t.TempDir(), "campaign-profiles.sqlite"),
		},
	}

	_, err := Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, Migrate(context.Background()))

	return func() {
		_ = Close()
		SetDB(nil)
	}
}
