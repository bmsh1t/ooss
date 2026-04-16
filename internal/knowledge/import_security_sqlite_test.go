package knowledge

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/database"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportSecuritySQLite_ImportsStructuredKnowledge(t *testing.T) {
	cfg := setupKnowledgeImportDB(t)
	sourcePath := createSecuritySQLiteFixture(t)
	ctx := context.Background()

	summary, err := Import(ctx, cfg, ImportOptions{
		Type:      "security-sqlite",
		Path:      sourcePath,
		Workspace: "security-kb",
	})
	require.NoError(t, err)
	require.NotNil(t, summary)

	assert.Equal(t, "security-sqlite", summary.Type)
	assert.Equal(t, "security-kb", summary.Workspace)
	assert.Equal(t, 6, summary.Documents)
	assert.GreaterOrEqual(t, summary.Chunks, 6)
	assert.Zero(t, summary.Failed)
	assert.Empty(t, summary.Errors)

	docs, err := database.ListKnowledgeDocuments(ctx, database.KnowledgeDocumentQuery{
		Workspace: "security-kb",
		Limit:     20,
	})
	require.NoError(t, err)
	require.Equal(t, 6, docs.TotalCount)

	titles := make([]string, 0, len(docs.Data))
	for _, doc := range docs.Data {
		titles = append(titles, doc.Title)
	}
	sort.Strings(titles)
	assert.Contains(t, titles, "A01 (2025): Broken Access Control")
	assert.Contains(t, titles, "ASI01: Agent Goal Hijack")
	assert.Contains(t, titles, "CAPEC-1: Accessing Functionality Not Properly Constrained by ACLs")
	assert.Contains(t, titles, "CWE-1004: Sensitive Cookie Without 'HttpOnly' Flag")
	assert.Contains(t, titles, "STRIDE S: Spoofing")
	assert.Contains(t, titles, "T1055.011: Extra Window Memory Injection")

	chunks, err := database.ListKnowledgeChunks(ctx, "security-kb", 100)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	var cweChunk database.KnowledgeChunkExportRow
	foundCWE := false
	for _, chunk := range chunks {
		if chunk.SourcePath == "security-sqlite://cwe/CWE-1004" {
			cweChunk = chunk
			foundCWE = true
			break
		}
	}
	require.True(t, foundCWE)
	assert.Contains(t, cweChunk.Content, "Sensitive Cookie Without 'HttpOnly' Flag")
	assert.Contains(t, cweChunk.Content, "Leverage the HttpOnly flag")
	assert.Contains(t, cweChunk.Content, "CWE-732")

	hits, err := SearchWithOptions(ctx, SearchOptions{
		Workspace: "security-kb",
		Query:     "HttpOnly",
		Limit:     5,
	})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	assert.Contains(t, collectKnowledgeHitTitles(hits), "CWE-1004: Sensitive Cookie Without 'HttpOnly' Flag")
}

func TestImportSecuritySQLite_IsIdempotent(t *testing.T) {
	cfg := setupKnowledgeImportDB(t)
	sourcePath := createSecuritySQLiteFixture(t)
	ctx := context.Background()

	first, err := Import(ctx, cfg, ImportOptions{
		Type:      "security-sqlite",
		Path:      sourcePath,
		Workspace: "security-kb",
	})
	require.NoError(t, err)

	docsBefore, err := database.ListKnowledgeDocuments(ctx, database.KnowledgeDocumentQuery{
		Workspace: "security-kb",
		Limit:     20,
	})
	require.NoError(t, err)
	chunksBefore, err := database.ListKnowledgeChunks(ctx, "security-kb", 100)
	require.NoError(t, err)

	second, err := Import(ctx, cfg, ImportOptions{
		Type:      "security-sqlite",
		Path:      sourcePath,
		Workspace: "security-kb",
	})
	require.NoError(t, err)

	docsAfter, err := database.ListKnowledgeDocuments(ctx, database.KnowledgeDocumentQuery{
		Workspace: "security-kb",
		Limit:     20,
	})
	require.NoError(t, err)
	chunksAfter, err := database.ListKnowledgeChunks(ctx, "security-kb", 100)
	require.NoError(t, err)

	assert.Equal(t, first.Documents, second.Documents)
	assert.Equal(t, docsBefore.TotalCount, docsAfter.TotalCount)
	assert.Equal(t, len(chunksBefore), len(chunksAfter))
	assert.Equal(t, mapKnowledgeDocHashes(docsBefore.Data), mapKnowledgeDocHashes(docsAfter.Data))
}

func collectKnowledgeHitTitles(hits []database.KnowledgeSearchHit) []string {
	titles := make([]string, 0, len(hits))
	for _, hit := range hits {
		titles = append(titles, hit.Title)
	}
	return titles
}

func mapKnowledgeDocHashes(docs []database.KnowledgeDocument) map[string]string {
	mapped := make(map[string]string, len(docs))
	for _, doc := range docs {
		mapped[doc.SourcePath] = doc.ContentHash
	}
	return mapped
}

func createSecuritySQLiteFixture(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "security_kb.sqlite")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	for _, stmt := range []string{
		`CREATE TABLE cwe (
			id TEXT PRIMARY KEY,
			cwe_num INTEGER NOT NULL,
			name TEXT NOT NULL,
			abstraction TEXT,
			status TEXT,
			description TEXT,
			extended_description TEXT,
			likelihood_of_exploit TEXT,
			embedding_text TEXT
		)`,
		`CREATE TABLE cwe_mitigation (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cwe_id TEXT NOT NULL,
			phase TEXT,
			strategy TEXT,
			description TEXT NOT NULL,
			effectiveness TEXT
		)`,
		`CREATE TABLE cwe_hierarchy (
			child_id TEXT NOT NULL,
			parent_id TEXT NOT NULL,
			nature TEXT,
			ordinal TEXT,
			PRIMARY KEY (child_id, parent_id)
		)`,
		`CREATE TABLE capec (
			id TEXT PRIMARY KEY,
			capec_num INTEGER NOT NULL,
			name TEXT NOT NULL,
			abstraction TEXT,
			status TEXT,
			description TEXT,
			severity TEXT,
			likelihood_of_attack TEXT,
			prerequisites TEXT,
			skills_required TEXT,
			resources_required TEXT,
			embedding_text TEXT
		)`,
		`CREATE TABLE capec_cwe (
			capec_id TEXT NOT NULL,
			cwe_id TEXT NOT NULL,
			nature TEXT,
			PRIMARY KEY (capec_id, cwe_id)
		)`,
		`CREATE TABLE capec_attack (
			capec_id TEXT NOT NULL,
			attack_id TEXT NOT NULL,
			source TEXT,
			PRIMARY KEY (capec_id, attack_id)
		)`,
		`CREATE TABLE attack_technique (
			id TEXT PRIMARY KEY,
			stix_id TEXT UNIQUE,
			name TEXT NOT NULL,
			description TEXT,
			tactics TEXT,
			platforms TEXT,
			detection TEXT,
			is_subtechnique INTEGER DEFAULT 0,
			parent_technique TEXT,
			version TEXT,
			embedding_text TEXT
		)`,
		`CREATE TABLE attack_mitigation (
			id TEXT PRIMARY KEY,
			stix_id TEXT UNIQUE,
			name TEXT NOT NULL,
			description TEXT
		)`,
		`CREATE TABLE attack_tech_mitigation (
			technique_id TEXT NOT NULL,
			mitigation_id TEXT NOT NULL,
			description TEXT,
			PRIMARY KEY (technique_id, mitigation_id)
		)`,
		`CREATE TABLE agentic_threat (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			source TEXT NOT NULL,
			stride_categories TEXT,
			cwes TEXT,
			attack_techniques TEXT,
			atlas_techniques TEXT,
			kill_chain_phases TEXT,
			severity TEXT,
			related_controls TEXT
		)`,
		`CREATE TABLE stride_category (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			security_property TEXT NOT NULL,
			description TEXT
		)`,
		`CREATE TABLE stride_cwe (
			stride_category TEXT NOT NULL,
			cwe_id TEXT NOT NULL,
			relevance_score REAL DEFAULT 1.0,
			source TEXT,
			notes TEXT,
			PRIMARY KEY (stride_category, cwe_id)
		)`,
		`CREATE TABLE owasp_top10 (
			id TEXT PRIMARY KEY,
			year INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			cwe_count INTEGER
		)`,
		`CREATE TABLE owasp_cwe (
			owasp_id TEXT NOT NULL,
			cwe_id TEXT NOT NULL,
			year INTEGER NOT NULL,
			PRIMARY KEY (owasp_id, cwe_id, year)
		)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}

	_, err = db.Exec(`INSERT INTO cwe (id, cwe_num, name, abstraction, status, description, extended_description, likelihood_of_exploit) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"CWE-1004", 1004, "Sensitive Cookie Without 'HttpOnly' Flag", "Variant", "Incomplete",
		"The product uses a cookie to store sensitive information, but the cookie is not marked with the HttpOnly flag.",
		"The HttpOnly flag helps prevent client-side script from reading cookies during XSS.",
		"Medium",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO cwe_mitigation (cwe_id, phase, strategy, description, effectiveness) VALUES (?, ?, ?, ?, ?)`,
		"CWE-1004", "Implementation", "", "Leverage the HttpOnly flag when setting a sensitive cookie in a response.", "High",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO cwe_hierarchy (child_id, parent_id, nature, ordinal) VALUES (?, ?, ?, ?)`,
		"CWE-1004", "CWE-732", "ChildOf", "Primary",
	)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO capec (id, capec_num, name, abstraction, status, description, severity, likelihood_of_attack, prerequisites, skills_required, resources_required) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"CAPEC-1", 1, "Accessing Functionality Not Properly Constrained by ACLs", "Standard", "Draft",
		"Attackers abuse missing or weak access control lists to reach protected functionality.",
		"High", "High",
		"Protected routes must be discoverable; ACLs must be missing or weak.",
		"Low skill; observe URLs and access patterns.",
		"None",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO capec_cwe (capec_id, cwe_id, nature) VALUES (?, ?, ?)`, "CAPEC-1", "CWE-1004", "CanFollow")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO capec_attack (capec_id, attack_id, source) VALUES (?, ?, ?)`, "CAPEC-1", "T1055.011", "stix")
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO attack_technique (id, stix_id, name, description, tactics, platforms, detection, is_subtechnique, parent_technique, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"T1055.011", "attack-pattern--0042a9f5-f053-4769-b3ef-9ad018dfa298", "Extra Window Memory Injection",
		"Adversaries may inject malicious code into process via Extra Window Memory.",
		`["defense-evasion", "privilege-escalation"]`,
		`["Windows"]`,
		"Monitor suspicious window memory abuse patterns.",
		1, "T1055", "1.1",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO attack_mitigation (id, stix_id, name, description) VALUES (?, ?, ?, ?)`,
		"M1021", "course-of-action--00d7d21b-69d6-4797-88a2-c86f3fc97651", "Restrict Registry Permissions", "Limit the ability to change critical notification packages.",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO attack_tech_mitigation (technique_id, mitigation_id, description) VALUES (?, ?, ?)`,
		"T1055.011", "M1021", "Consider restricting registry permissions for notification packages.",
	)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO agentic_threat (id, name, description, source, stride_categories, cwes, attack_techniques, atlas_techniques, kill_chain_phases, severity, related_controls) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"ASI01", "Agent Goal Hijack",
		"Manipulation of an agent's objectives through malicious inputs.",
		"owasp_asi",
		`["tampering", "elevation_of_privilege"]`,
		`["CWE-1004"]`,
		`["T1055.011"]`,
		`["AML.T0051"]`,
		`["delivery", "exploitation"]`,
		"critical",
		`["AGENT-01", "AGENT-06"]`,
	)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO stride_category (id, name, security_property, description) VALUES (?, ?, ?, ?)`,
		"S", "Spoofing", "Authentication", "Pretending to be something or someone other than yourself",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO stride_cwe (stride_category, cwe_id, relevance_score, source, notes) VALUES (?, ?, ?, ?, ?)`,
		"S", "CWE-1004", 1.0, "fixture", "Cookies without HttpOnly can aid spoofing and session theft.",
	)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO owasp_top10 (id, year, name, description, cwe_count) VALUES (?, ?, ?, ?, ?)`,
		"A01", 2025, "Broken Access Control", "Failures in authorization and object-level access control.", 1,
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO owasp_cwe (owasp_id, cwe_id, year) VALUES (?, ?, ?)`, "A01", "CWE-1004", 2025)
	require.NoError(t, err)

	return dbPath
}
