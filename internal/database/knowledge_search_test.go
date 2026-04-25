package database

import (
	"context"
	"path/filepath"
	"strings"
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

func TestSearchKnowledgeWithOptions_RanksExactSecurityIdentifiers(t *testing.T) {
	testCases := []struct {
		name       string
		query      string
		exactID    string
		collisions []string
	}{
		{
			name:       "cwe short id does not rank longer cwe prefixes first",
			query:      "CWE-79",
			exactID:    "CWE-79",
			collisions: []string{"CWE-790", "CWE-791", "CWE-792", "CWE-793", "CWE-794", "CWE-795", "CWE-796", "CWE-797", "CWE-798"},
		},
		{
			name:       "cwe two digit id does not rank three digit cwe prefixes first",
			query:      "CWE-22",
			exactID:    "CWE-22",
			collisions: []string{"CWE-220", "CWE-221", "CWE-222", "CWE-223", "CWE-224", "CWE-225", "CWE-226", "CWE-227", "CWE-228"},
		},
		{
			name:       "capec one digit id does not rank capec hundreds first",
			query:      "CAPEC-1",
			exactID:    "CAPEC-1",
			collisions: []string{"CAPEC-100", "CAPEC-101", "CAPEC-102", "CAPEC-103", "CAPEC-104", "CAPEC-105", "CAPEC-106", "CAPEC-107", "CAPEC-108"},
		},
		{
			name:       "capec two digit id does not rank capec hundreds first",
			query:      "CAPEC-10",
			exactID:    "CAPEC-10",
			collisions: []string{"CAPEC-100", "CAPEC-101", "CAPEC-102", "CAPEC-103", "CAPEC-104", "CAPEC-105", "CAPEC-106", "CAPEC-107", "CAPEC-108"},
		},
		{
			name:       "attack parent technique ranks before subtechniques",
			query:      "T1059",
			exactID:    "T1059",
			collisions: []string{"T1059.001", "T1059.002", "T1059.003", "T1059.004", "T1059.005", "T1059.006", "T1059.007", "T1059.008", "T1059.009"},
		},
		{
			name:       "attack subtechnique exact id ranks before longer prefixes",
			query:      "T1059.001",
			exactID:    "T1059.001",
			collisions: []string{"T1059.0010", "T1059.0011", "T1059.0012", "T1059.0013", "T1059.0014", "T1059.0015", "T1059.0016", "T1059.0017", "T1059.0018"},
		},
		{
			name:       "owasp short id ranks exact year record first",
			query:      "A01",
			exactID:    "A01@2025",
			collisions: []string{"A010", "A011", "A012", "A013", "A014", "A015", "A016", "A017", "A018"},
		},
		{
			name:       "owasp id with year ranks exact record first",
			query:      "A01@2025",
			exactID:    "A01@2025",
			collisions: []string{"A01@2020", "A01@2021", "A01@2022", "A01@2023", "A01@2024", "A01@20250", "A01@20251", "A01@20252", "A01@20253"},
		},
		{
			name:       "agentic asi id ranks exact record first",
			query:      "ASI01",
			exactID:    "ASI01",
			collisions: []string{"ASI010", "ASI011", "ASI012", "ASI013", "ASI014", "ASI015", "ASI016", "ASI017", "ASI018"},
		},
		{
			name:       "mcp year id ranks exact record first",
			query:      "MCP01:2025",
			exactID:    "MCP01:2025",
			collisions: []string{"MCP01:2020", "MCP01:2021", "MCP01:2022", "MCP01:2023", "MCP01:2024", "MCP01:20250", "MCP01:20251", "MCP01:20252", "MCP01:20253"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cleanup := setupKnowledgeSearchTestDB(t)
			defer cleanup()
			_ = cfg

			ctx := context.Background()
			expectedPath := seedSecurityIdentifierCollisionFixture(t, ctx, tc.exactID, tc.collisions)

			results, err := SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
				Workspace: "security-kb",
				Query:     tc.query,
				Limit:     1,
			})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, expectedPath, results[0].SourcePath)
		})
	}
}

func TestSearchKnowledgeWithOptions_ExactSecurityIdentifierFiltersPrefixCollisions(t *testing.T) {
	testCases := []struct {
		name       string
		query      string
		exactID    string
		collisions []string
	}{
		{
			name:       "cwe exact id filters longer prefixes",
			query:      "CWE-79",
			exactID:    "CWE-79",
			collisions: []string{"CWE-790", "CWE-791", "CWE-792"},
		},
		{
			name:       "capec exact id filters longer prefixes",
			query:      "CAPEC-1",
			exactID:    "CAPEC-1",
			collisions: []string{"CAPEC-100", "CAPEC-101", "CAPEC-102"},
		},
		{
			name:       "attack parent exact id filters subtechniques",
			query:      "T1059",
			exactID:    "T1059",
			collisions: []string{"T1059.001", "T1059.002", "T1059.003"},
		},
		{
			name:       "attack subtechnique exact id filters longer prefixes",
			query:      "T1059.001",
			exactID:    "T1059.001",
			collisions: []string{"T1059.0010", "T1059.0011", "T1059.0012"},
		},
		{
			name:       "owasp id with year exact filters adjacent years",
			query:      "A01@2025",
			exactID:    "A01@2025",
			collisions: []string{"A01@2024", "A01@20250", "A01@20251"},
		},
		{
			name:       "agentic asi exact id filters longer prefixes",
			query:      "ASI01",
			exactID:    "ASI01",
			collisions: []string{"ASI010", "ASI011", "ASI012"},
		},
		{
			name:       "mcp exact id filters adjacent years and longer prefixes",
			query:      "MCP01:2025",
			exactID:    "MCP01:2025",
			collisions: []string{"MCP01:2024", "MCP01:20250", "MCP01:20251"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, cleanup := setupKnowledgeSearchTestDB(t)
			defer cleanup()
			_ = cfg

			ctx := context.Background()
			expectedPath := seedSecurityIdentifierCollisionFixture(t, ctx, tc.exactID, tc.collisions)

			results, err := SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
				Workspace: "security-kb",
				Query:     tc.query,
				Limit:     10,
				ExactID:   true,
			})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, expectedPath, results[0].SourcePath)
		})
	}
}

func TestSearchKnowledgeWithOptions_STRIDEExactIdentifier(t *testing.T) {
	cfg, cleanup := setupKnowledgeSearchTestDB(t)
	defer cleanup()
	_ = cfg

	ctx := context.Background()
	now := time.Now()
	for _, item := range []struct {
		id   string
		name string
	}{
		{id: "T", name: "Tampering"},
		{id: "R", name: "Repudiation"},
		{id: "I", name: "Information Disclosure"},
		{id: "D", name: "Denial of Service"},
		{id: "E", name: "Elevation of Privilege"},
		{id: "S", name: "Spoofing"},
	} {
		require.NoError(t, UpsertKnowledgeDocument(ctx, &KnowledgeDocument{
			Workspace:   "security-kb",
			SourcePath:  "security-sqlite://stride_cwe/" + item.id,
			SourceType:  "sqlite-import",
			DocType:     "md",
			Title:       "STRIDE " + item.id + ": " + item.name,
			ContentHash: "doc-hash-stride-" + item.id,
			Status:      "ready",
			ChunkCount:  1,
			TotalBytes:  64,
			Metadata:    `{"source_table":"stride_cwe","source_id":"` + item.id + `","labels":["stride"]}`,
			CreatedAt:   now,
			UpdatedAt:   now,
		}, []KnowledgeChunk{{
			Workspace:   "security-kb",
			ChunkIndex:  0,
			Section:     "Overview",
			Content:     "STRIDE " + item.id + " category maps related CWE entries.",
			ContentHash: "chunk-hash-stride-" + item.id,
			Metadata:    `{"source_table":"stride_cwe","source_id":"` + item.id + `","labels":["stride"]}`,
			CreatedAt:   now,
		}}))
	}

	results, err := SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
		Workspace: "security-kb",
		Query:     "STRIDE:S",
		Limit:     1,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "security-sqlite://stride_cwe/S", results[0].SourcePath)

	results, err = SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
		Workspace: "security-kb",
		Query:     "STRIDE S",
		Limit:     1,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "security-sqlite://stride_cwe/S", results[0].SourcePath)

	results, err = SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
		Workspace: "security-kb",
		Query:     "S",
		Limit:     10,
		ExactID:   true,
	})
	require.NoError(t, err)
	require.Empty(t, results)

	results, err = SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
		Workspace: "security-kb",
		Query:     "STRIDES",
		Limit:     10,
		ExactID:   true,
	})
	require.NoError(t, err)
	require.Empty(t, results)
}

func seedSecurityIdentifierCollisionFixture(t *testing.T, ctx context.Context, exactID string, collisions []string) string {
	t.Helper()

	for _, id := range collisions {
		seedSecurityIdentifierDocument(t, ctx, id, false)
	}
	return seedSecurityIdentifierDocument(t, ctx, exactID, true)
}

func seedSecurityIdentifierDocument(t *testing.T, ctx context.Context, id string, exact bool) string {
	t.Helper()

	sourcePath, title := securityIdentifierTestPathAndTitle(id)
	now := time.Now()
	content := id + " reference content mentioning the requested prefix for collision testing."
	if exact {
		content = id + " is the exact requested security knowledge identifier."
	}

	require.NoError(t, UpsertKnowledgeDocument(ctx, &KnowledgeDocument{
		Workspace:   "security-kb",
		SourcePath:  sourcePath,
		SourceType:  "sqlite-import",
		DocType:     "md",
		Title:       title,
		ContentHash: "doc-hash-" + id,
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  int64(len(content)),
		Metadata:    `{"source_id":"` + id + `","labels":["security-test"]}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []KnowledgeChunk{{
		Workspace:   "security-kb",
		ChunkIndex:  0,
		Section:     "Overview",
		Content:     content,
		ContentHash: "chunk-hash-" + id,
		Metadata:    `{"source_id":"` + id + `","labels":["security-test"]}`,
		CreatedAt:   now,
	}}))

	return sourcePath
}

func securityIdentifierTestPathAndTitle(id string) (string, string) {
	upperID := strings.ToUpper(id)
	switch {
	case strings.HasPrefix(upperID, "CWE-"):
		return "security-sqlite://cwe/" + id, id + ": Test Weakness"
	case strings.HasPrefix(upperID, "CAPEC-"):
		return "security-sqlite://capec/" + id, id + ": Test Attack Pattern"
	case strings.HasPrefix(upperID, "T"):
		return "security-sqlite://attack_technique/" + id, id + ": Test Technique"
	case strings.HasPrefix(upperID, "A") && len(id) >= 3 && id[1] >= '0' && id[1] <= '9' && id[2] >= '0' && id[2] <= '9':
		titleID := id
		if at := strings.Index(id, "@"); at >= 0 {
			titleID = id[:at] + " (" + id[at+1:] + ")"
		}
		return "security-sqlite://owasp_top10/" + id, titleID + ": Test OWASP Category"
	default:
		return "security-sqlite://agentic_threat/" + id, id + ": Test Agentic Threat"
	}
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
