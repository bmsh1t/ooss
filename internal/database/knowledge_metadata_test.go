package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestComputeKnowledgeMetadataBoost_PrefersOperationalMemoryForOperationalQuery(t *testing.T) {
	query := "manual exploitation retest admin auth bypass followup proof"

	operational := &KnowledgeMetadataSummary{
		Scope:            "workspace",
		SampleType:       "operator-followup",
		SourceConfidence: 0.88,
		Labels:           []string{"operator-followup", "manual-followup", "operator-queue", "retest-plan", "followup-decision", "campaign-handoff"},
		TargetTypes:      []string{"url"},
	}
	verified := &KnowledgeMetadataSummary{
		Scope:            "workspace",
		SampleType:       "verified",
		SourceConfidence: 0.95,
		Labels:           []string{"verified"},
		TargetTypes:      []string{"url"},
	}
	aiAnalysis := &KnowledgeMetadataSummary{
		Scope:            "workspace",
		SampleType:       "ai-analysis",
		SourceConfidence: 0.64,
		Labels:           []string{"auto-learn"},
		TargetTypes:      []string{"url"},
	}

	operationalBoost := ComputeKnowledgeMetadataBoost(query, operational)
	verifiedBoost := ComputeKnowledgeMetadataBoost(query, verified)
	aiBoost := ComputeKnowledgeMetadataBoost(query, aiAnalysis)

	require.Greater(t, operationalBoost, verifiedBoost)
	require.Greater(t, operationalBoost, aiBoost)
}

func TestParseKnowledgeMetadata_ParsesRetrievalFingerprint(t *testing.T) {
	metadata := ParseKnowledgeMetadata(`{"sample_type":"verified","retrieval_fingerprint":"fp-123"}`)
	require.NotNil(t, metadata)
	require.Equal(t, "fp-123", metadata.RetrievalFingerprint)
	require.Equal(t, "fp-123", KnowledgeMetadataFingerprint(metadata))
}

func TestParseKnowledgeMetadata_ParsesConfidenceObservedAt(t *testing.T) {
	metadata := ParseKnowledgeMetadata(`{"sample_type":"ai-analysis","generated_at":"2026-03-01T10:00:00Z"}`)
	require.NotNil(t, metadata)
	require.False(t, metadata.observedAt.IsZero())
	require.Equal(t, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), metadata.observedAt)
}

func TestComputeKnowledgeMetadataBoost_AgesStaleAIAnalysis(t *testing.T) {
	now := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	query := "auth bypass proof"

	freshAI := &KnowledgeMetadataSummary{
		Scope:            "workspace",
		SampleType:       "ai-analysis",
		SourceConfidence: 0.92,
		observedAt:       now.Add(-12 * time.Hour),
	}
	staleAI := &KnowledgeMetadataSummary{
		Scope:            "workspace",
		SampleType:       "ai-analysis",
		SourceConfidence: 0.92,
		observedAt:       now.Add(-90 * 24 * time.Hour),
	}
	staleVerified := &KnowledgeMetadataSummary{
		Scope:            "workspace",
		SampleType:       "verified",
		SourceConfidence: 0.92,
		observedAt:       now.Add(-90 * 24 * time.Hour),
	}

	freshBoost := ComputeKnowledgeMetadataBoostAt(query, freshAI, now)
	staleAIBoost := ComputeKnowledgeMetadataBoostAt(query, staleAI, now)
	staleVerifiedBoost := ComputeKnowledgeMetadataBoostAt(query, staleVerified, now)

	require.Greater(t, freshBoost, staleAIBoost)
	require.Greater(t, staleVerifiedBoost, staleAIBoost)
}

func TestKnowledgeMetadataMatchesFilters_AllowsPlainDocsButFiltersLearnedNoise(t *testing.T) {
	require.True(t, KnowledgeMetadataMatchesFilters(nil, 0.6, nil, nil))
	require.False(t, KnowledgeMetadataMatchesFilters(nil, 0, []string{"verified"}, nil))

	metadata := &KnowledgeMetadataSummary{
		SampleType:       "false_positive",
		SourceConfidence: 0.9,
	}
	require.False(t, KnowledgeMetadataMatchesFilters(metadata, 0, nil, []string{"false_positive"}))

	metadata = &KnowledgeMetadataSummary{
		SampleType:       "ai-analysis",
		SourceConfidence: 0.52,
	}
	require.False(t, KnowledgeMetadataMatchesFilters(metadata, 0.6, nil, nil))
	require.True(t, KnowledgeMetadataMatchesFilters(metadata, 0.5, []string{"ai-analysis"}, nil))
}
