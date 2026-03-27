package database

import "testing"

import "github.com/stretchr/testify/require"

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
