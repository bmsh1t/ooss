package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSearchKnowledgeWithOptions_DedupesByRetrievalFingerprint(t *testing.T) {
	cfg, cleanup := setupKnowledgeSearchTestDB(t)
	defer cleanup()
	_ = cfg

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, UpsertKnowledgeDocument(ctx, &KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/verified-findings.md",
		SourceType:  "generated",
		DocType:     "learned-findings",
		Title:       "Verified Findings - acme",
		ContentHash: "doc-hash-acme",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  64,
		Metadata:    `{"scope":"workspace","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-fp"}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Primary",
		Content:     "Verified sql injection login bypass playbook",
		ContentHash: "chunk-hash-acme",
		Metadata:    `{"scope":"workspace","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-fp"}`,
		CreatedAt:   now,
	}}))

	require.NoError(t, UpsertKnowledgeDocument(ctx, &KnowledgeDocument{
		Workspace:   "public",
		SourcePath:  "kb://learned/public/acme/verified-findings.md",
		SourceType:  "generated",
		DocType:     "learned-findings",
		Title:       "Verified Findings - acme",
		ContentHash: "doc-hash-public",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  64,
		Metadata:    `{"scope":"public","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-fp"}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []KnowledgeChunk{{
		Workspace:   "public",
		ChunkIndex:  0,
		Section:     "Shared",
		Content:     "Verified sql injection login bypass memory",
		ContentHash: "chunk-hash-public",
		Metadata:    `{"scope":"public","sample_type":"verified","source_confidence":0.95,"retrieval_fingerprint":"shared-fp"}`,
		CreatedAt:   now,
	}}))

	results, err := SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
		Workspace: "acme",
		Query:     "verified sql injection login bypass",
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "acme", results[0].Workspace)
}

func TestSearchKnowledgeWithOptions_FiltersByConfidenceAndSampleType(t *testing.T) {
	cfg, cleanup := setupKnowledgeSearchTestDB(t)
	defer cleanup()
	_ = cfg

	ctx := context.Background()
	now := time.Now()

	require.NoError(t, UpsertKnowledgeDocument(ctx, &KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/ai-insights.md",
		SourceType:  "generated",
		DocType:     "learned-ai-insights",
		Title:       "AI Insights - acme",
		ContentHash: "doc-hash-ai",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  64,
		Metadata:    `{"scope":"workspace","sample_type":"ai-analysis","source_confidence":0.52}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "AI",
		Content:     "Auth bypass hypothesis from AI analysis",
		ContentHash: "chunk-hash-ai",
		Metadata:    `{"scope":"workspace","sample_type":"ai-analysis","source_confidence":0.52}`,
		CreatedAt:   now,
	}}))

	require.NoError(t, UpsertKnowledgeDocument(ctx, &KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/false-positive-samples.md",
		SourceType:  "generated",
		DocType:     "learned-false-positives",
		Title:       "False Positive Samples - acme",
		ContentHash: "doc-hash-fp",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  64,
		Metadata:    `{"scope":"workspace","sample_type":"false_positive","source_confidence":0.90}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Noise",
		Content:     "Auth bypass false positive sample caused by redirect behavior",
		ContentHash: "chunk-hash-fp",
		Metadata:    `{"scope":"workspace","sample_type":"false_positive","source_confidence":0.90}`,
		CreatedAt:   now,
	}}))

	require.NoError(t, UpsertKnowledgeDocument(ctx, &KnowledgeDocument{
		Workspace:   "acme",
		SourcePath:  "kb://learned/workspace/acme/verified-findings.md",
		SourceType:  "generated",
		DocType:     "learned-findings",
		Title:       "Verified Findings - acme",
		ContentHash: "doc-hash-verified",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  64,
		Metadata:    `{"scope":"workspace","sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []KnowledgeChunk{{
		Workspace:   "acme",
		ChunkIndex:  0,
		Section:     "Proof",
		Content:     "Verified auth bypass proof and retest notes",
		ContentHash: "chunk-hash-verified",
		Metadata:    `{"scope":"workspace","sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
	}}))

	results, err := SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
		Workspace:           "acme",
		Query:               "auth bypass proof",
		Limit:               10,
		MinSourceConfidence: 0.60,
		ExcludeSampleTypes:  []string{"false_positive"},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].Metadata)
	require.Equal(t, "verified", results[0].Metadata.SampleType)
}

func setupKnowledgeSearchTestDB(t *testing.T) (*config.Config, func()) {
	t.Helper()

	cfg := &config.Config{
		BaseFolder: t.TempDir(),
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(t.TempDir(), "knowledge-search.sqlite"),
		},
	}

	_, err := Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, Migrate(context.Background()))

	return cfg, func() {
		_ = Close()
		SetDB(nil)
	}
}
