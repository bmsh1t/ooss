package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const campaignReportProfilesParamKey = "campaign_report_profiles"

var (
	ErrCampaignReportProfileNotFound = errors.New("campaign report profile not found")
	campaignReportProfileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// CampaignReportProfile stores a reusable campaign report/export view.
type CampaignReportProfile struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	Filters     CampaignReportProfileFilters `json:"filters,omitempty"`
	Sort        CampaignReportProfileSort    `json:"sort,omitempty"`
	Format      string                       `json:"format,omitempty"`
	CreatedAt   time.Time                    `json:"created_at,omitempty"`
	UpdatedAt   time.Time                    `json:"updated_at,omitempty"`
}

// CampaignReportProfileFilters stores reusable report filters.
type CampaignReportProfileFilters struct {
	RiskLevels   []string `json:"risk_levels,omitempty"`
	Statuses     []string `json:"statuses,omitempty"`
	TriggerTypes []string `json:"trigger_types,omitempty"`
	Preset       string   `json:"preset,omitempty"`
}

// CampaignReportProfileSort stores reusable report ordering.
type CampaignReportProfileSort struct {
	By    string `json:"by,omitempty"`
	Order string `json:"order,omitempty"`
}

// NormalizeCampaignReportProfileName validates and normalizes a profile name.
func NormalizeCampaignReportProfileName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !campaignReportProfileNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid campaign report profile name: %s", name)
	}
	return name, nil
}

// ListCampaignReportProfiles returns sorted report/export profiles for a campaign.
func ListCampaignReportProfiles(campaign *Campaign) ([]CampaignReportProfile, error) {
	profiles, err := decodeCampaignReportProfiles(campaign)
	if err != nil {
		return nil, err
	}
	result := make([]CampaignReportProfile, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, profile)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// GetCampaignReportProfile returns a named report/export profile.
func GetCampaignReportProfile(campaign *Campaign, name string) (*CampaignReportProfile, error) {
	profiles, err := decodeCampaignReportProfiles(campaign)
	if err != nil {
		return nil, err
	}
	normalizedName, err := NormalizeCampaignReportProfileName(name)
	if err != nil {
		return nil, err
	}
	profile, ok := profiles[normalizedName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCampaignReportProfileNotFound, normalizedName)
	}
	return &profile, nil
}

// UpsertCampaignReportProfile stores or updates a reusable report/export profile.
func UpsertCampaignReportProfile(ctx context.Context, campaignID string, profile CampaignReportProfile) (*CampaignReportProfile, error) {
	campaign, err := GetCampaignByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	profiles, err := decodeCampaignReportProfiles(campaign)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeCampaignReportProfile(profile)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if existing, ok := profiles[normalized.Name]; ok && !existing.CreatedAt.IsZero() {
		normalized.CreatedAt = existing.CreatedAt
	} else if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = now
	}
	normalized.UpdatedAt = now
	profiles[normalized.Name] = normalized

	params, err := encodeCampaignReportProfiles(campaign.Params, profiles)
	if err != nil {
		return nil, err
	}
	if err := UpdateCampaignParams(ctx, campaign.ID, params); err != nil {
		return nil, err
	}
	return &normalized, nil
}

// DeleteCampaignReportProfile removes a saved report/export profile.
func DeleteCampaignReportProfile(ctx context.Context, campaignID, name string) (bool, error) {
	campaign, err := GetCampaignByID(ctx, campaignID)
	if err != nil {
		return false, err
	}
	profiles, err := decodeCampaignReportProfiles(campaign)
	if err != nil {
		return false, err
	}
	normalizedName, err := NormalizeCampaignReportProfileName(name)
	if err != nil {
		return false, err
	}
	if _, ok := profiles[normalizedName]; !ok {
		return false, fmt.Errorf("%w: %s", ErrCampaignReportProfileNotFound, normalizedName)
	}
	delete(profiles, normalizedName)
	params, err := encodeCampaignReportProfiles(campaign.Params, profiles)
	if err != nil {
		return false, err
	}
	if err := UpdateCampaignParams(ctx, campaign.ID, params); err != nil {
		return false, err
	}
	return true, nil
}

func decodeCampaignReportProfiles(campaign *Campaign) (map[string]CampaignReportProfile, error) {
	if campaign == nil || campaign.Params == nil {
		return map[string]CampaignReportProfile{}, nil
	}
	raw, ok := campaign.Params[campaignReportProfilesParamKey]
	if !ok || raw == nil {
		return map[string]CampaignReportProfile{}, nil
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to decode campaign report profiles: %w", err)
	}
	rawProfiles := make(map[string]json.RawMessage)
	if err := json.Unmarshal(blob, &rawProfiles); err != nil {
		return nil, fmt.Errorf("failed to decode campaign report profiles: %w", err)
	}
	profiles := make(map[string]CampaignReportProfile)
	for name, payload := range rawProfiles {
		var profile CampaignReportProfile
		if err := json.Unmarshal(payload, &profile); err != nil {
			continue
		}
		if strings.TrimSpace(profile.Name) == "" {
			profile.Name = name
		}
		normalized, err := normalizeCampaignReportProfile(profile)
		if err != nil {
			continue
		}
		if normalized.CreatedAt.IsZero() {
			normalized.CreatedAt = normalized.UpdatedAt
		}
		profiles[normalized.Name] = normalized
	}
	return profiles, nil
}

func encodeCampaignReportProfiles(params map[string]interface{}, profiles map[string]CampaignReportProfile) (map[string]interface{}, error) {
	clone := make(map[string]interface{})
	for key, value := range params {
		clone[key] = value
	}
	if len(profiles) == 0 {
		delete(clone, campaignReportProfilesParamKey)
		return clone, nil
	}
	normalized := make(map[string]CampaignReportProfile, len(profiles))
	for name, profile := range profiles {
		profile.Name = name
		normalized[name] = profile
	}
	clone[campaignReportProfilesParamKey] = normalized
	return clone, nil
}

func normalizeCampaignReportProfile(profile CampaignReportProfile) (CampaignReportProfile, error) {
	name, err := NormalizeCampaignReportProfileName(profile.Name)
	if err != nil {
		return CampaignReportProfile{}, err
	}
	profile.Name = name
	profile.Description = strings.TrimSpace(profile.Description)
	profile.Filters.RiskLevels = normalizeCampaignProfileList(profile.Filters.RiskLevels)
	profile.Filters.Statuses = normalizeCampaignProfileList(profile.Filters.Statuses)
	profile.Filters.TriggerTypes = normalizeCampaignProfileList(profile.Filters.TriggerTypes)
	profile.Filters.Preset = normalizeCampaignReportProfilePreset(profile.Filters.Preset)
	switch profile.Filters.Preset {
	case "", "high-risk", "recovered", "failed":
	default:
		return CampaignReportProfile{}, fmt.Errorf("unsupported campaign report profile preset: %s", profile.Filters.Preset)
	}

	profile.Sort.By = normalizeCampaignReportProfileSortBy(profile.Sort.By)
	profile.Sort.Order = normalizeCampaignReportProfileSortOrder(profile.Sort.By, profile.Sort.Order)
	switch profile.Sort.By {
	case "risk", "target", "latest_run", "open_high_risk":
	default:
		return CampaignReportProfile{}, fmt.Errorf("unsupported campaign report profile sort by: %s", profile.Sort.By)
	}
	switch profile.Sort.Order {
	case "asc", "desc":
	default:
		return CampaignReportProfile{}, fmt.Errorf("unsupported campaign report profile sort order: %s", profile.Sort.Order)
	}

	profile.Format = normalizeCampaignReportProfileFormat(profile.Format)
	switch profile.Format {
	case "", "csv", "json":
	default:
		return CampaignReportProfile{}, fmt.Errorf("unsupported campaign report profile format: %s", profile.Format)
	}
	return profile, nil
}

func normalizeCampaignProfileList(values []string) []string {
	seen := make(map[string]struct{})
	var normalized []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			normalized = append(normalized, part)
		}
	}
	return normalized
}

func normalizeCampaignReportProfilePreset(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

func normalizeCampaignReportProfileSortBy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "", "risk":
		return "risk"
	case "target", "latest_run", "open_high_risk":
		return value
	case "latest_run_at":
		return "latest_run"
	case "open_high_risk_findings":
		return "open_high_risk"
	default:
		return value
	}
}

func normalizeCampaignReportProfileSortOrder(by, order string) string {
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "asc" || order == "desc" {
		return order
	}
	switch normalizeCampaignReportProfileSortBy(by) {
	case "target":
		return "asc"
	default:
		return "desc"
	}
}

func normalizeCampaignReportProfileFormat(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
