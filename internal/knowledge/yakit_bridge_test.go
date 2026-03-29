package knowledge

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectYakitRAGPackage_ExtractsHeaderMetadata(t *testing.T) {
	_, packagePath := setupYakitBridgeFixture(t)

	info, err := InspectYakitRAGPackage(packagePath)
	require.NoError(t, err)
	require.NotNil(t, info)

	assert.Equal(t, packagePath, info.PackagePath)
	assert.True(t, info.Compressed)
	assert.Equal(t, "29ba286d-38bb-49a5-a6c2-d206b6404b9c", info.PackageUUID)
	assert.Equal(t, "7a95ef45-fb38-49cc-8132-4b57b84deb34", info.CollectionUUID)
	assert.Equal(t, "mitre-attack-techniques", info.Slug)
	assert.Equal(t, "default description", info.Description)
	assert.Equal(t, "Qwen3-Embedding-0.6B", info.ModelName)
}

func TestBridgeYakitRAG_WritesJSONLFromImportedYakitDB(t *testing.T) {
	dbPath, packagePath := setupYakitBridgeFixture(t)
	outputPath := filepath.Join(t.TempDir(), "mitre-attack-techniques.jsonl")

	summary, err := BridgeYakitRAG(context.Background(), YakitRAGBridgeOptions{
		PackagePath: packagePath,
		YakitDBPath: dbPath,
		OutputPath:  outputPath,
	})
	require.NoError(t, err)
	require.NotNil(t, summary)

	assert.Equal(t, dbPath, summary.YakitDBPath)
	assert.Equal(t, "MITRE ATT&CK Techniques", summary.KnowledgeBaseName)
	assert.Equal(t, yakitBridgeFormatJSONL, summary.Format)
	assert.Equal(t, 2, summary.Entries)
	assert.Equal(t, "collection_uuid", summary.ResolvedBy)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 2)

	var first map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))
	assert.Equal(t, "MITRE ATT&CK Techniques", first["knowledge_base_name"])
	assert.Equal(t, "question_index", first["knowledge_type"])
	assert.Contains(t, first["content"].(string), "T1057 (Process Discovery) - ATT&CK")
	assert.Contains(t, first["content"].(string), "Questions:")
}

func TestBridgeYakitRAG_WritesMarkdownByKnowledgeBaseName(t *testing.T) {
	dbPath, _ := setupYakitBridgeFixture(t)
	outputPath := filepath.Join(t.TempDir(), "mitre-attack-techniques.md")

	summary, err := BridgeYakitRAG(context.Background(), YakitRAGBridgeOptions{
		YakitDBPath:       dbPath,
		KnowledgeBaseName: "MITRE ATT&CK Techniques",
		OutputPath:        outputPath,
		Format:            "md",
	})
	require.NoError(t, err)
	require.NotNil(t, summary)

	assert.Equal(t, yakitBridgeFormatMD, summary.Format)
	assert.Equal(t, "knowledge_base_name", summary.ResolvedBy)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "# MITRE ATT&CK Techniques")
	assert.Contains(t, content, "## Bridge Metadata")
	assert.Contains(t, content, "## T1057 (Process Discovery) - ATT&CK")
	assert.Contains(t, content, "### Questions")
}

func setupYakitBridgeFixture(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yakit-profile-plugin.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	schema := []string{
		`CREATE TABLE rag_knowledge_base_v1 (
			id integer primary key autoincrement,
			rag_id varchar(255),
			knowledge_base_name varchar(255) NOT NULL,
			knowledge_base_description text,
			knowledge_base_type varchar(255) NOT NULL,
			is_default bool DEFAULT false,
			tags text,
			serial_version_uid varchar(255),
			created_from_ui bool
		)`,
		`CREATE TABLE rag_vector_collection_v1 (
			id integer primary key autoincrement,
			name varchar(255),
			description text,
			model_name varchar(255),
			dimension integer NOT NULL,
			uuid varchar(255),
			rag_id varchar(255),
			serial_version_uid varchar(255),
			exported_at datetime
		)`,
		`CREATE TABLE rag_knowledge_entry_v1 (
			id integer primary key autoincrement,
			knowledge_base_id bigint NOT NULL,
			related_entity_uuid_s varchar(255),
			knowledge_title varchar(255) NOT NULL,
			knowledge_type varchar(255) NOT NULL,
			importance_score integer,
			keywords text,
			knowledge_details text,
			summary text,
			source_page integer,
			potential_questions text,
			hidden_index varchar(255),
			has_question_index bool
		)`,
	}
	for _, stmt := range schema {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}

	_, err = db.Exec(`INSERT INTO rag_knowledge_base_v1 (id, rag_id, knowledge_base_name, knowledge_base_type, serial_version_uid) VALUES (?, ?, ?, ?, ?)`,
		16,
		"30a80622-a6bc-4717-9b0e-c1785f98f726",
		"MITRE ATT&CK Techniques",
		"default",
		"2a04d96630a05b1cc6c5e564857c04ef653b8ca44a55634d31656fd9cd3cce0e",
	)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO rag_vector_collection_v1 (id, name, model_name, dimension, uuid, rag_id, serial_version_uid, exported_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		7,
		"MITRE ATT&CK Techniques",
		"Qwen3-Embedding-0.6B",
		1024,
		"7a95ef45-fb38-49cc-8132-4b57b84deb34",
		"30a80622-a6bc-4717-9b0e-c1785f98f726",
		"2a04d96630a05b1cc6c5e564857c04ef653b8ca44a55634d31656fd9cd3cce0e",
		"2026-03-25 01:21:06+00:00",
	)
	require.NoError(t, err)

	entries := []struct {
		id        int
		title     string
		summary   string
		details   string
		questions string
		keywords  string
	}{
		{
			id:        25903,
			title:     "T1057 (Process Discovery) - ATT&CK",
			summary:   "Enumerates running processes to identify security tools and target workloads.",
			details:   "executor: sh\ncommand: ps aux\nsupported_platforms:\n  - linux\n  - macos",
			questions: "How do operators enumerate running processes?\nHow can process discovery reveal defensive tooling?",
			keywords:  "process discovery,linux,macos",
		},
		{
			id:        25904,
			title:     "T1124 (System Time Discovery) - ATT&CK",
			summary:   "Queries system time to align scheduled execution or infer locale information.",
			details:   "executor: sh\ncommand: date\nsupported_platforms:\n  - linux\n  - macos\n  - windows",
			questions: "How can adversaries query system time for discovery?",
			keywords:  "time discovery,date",
		},
	}

	for _, entry := range entries {
		_, err = db.Exec(`INSERT INTO rag_knowledge_entry_v1 (id, knowledge_base_id, related_entity_uuid_s, knowledge_title, knowledge_type, importance_score, keywords, knowledge_details, summary, source_page, potential_questions, hidden_index, has_question_index) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.id,
			16,
			"entity-1,entity-2",
			entry.title,
			"question_index",
			0,
			entry.keywords,
			entry.details,
			entry.summary,
			1,
			entry.questions,
			"",
			true,
		)
		require.NoError(t, err)
	}

	packagePath := filepath.Join(tmpDir, "mitre_attack_techniques.rag.gz")
	raw := []byte("YAKRAG\x02\x00$29ba286d-38bb-49a5-a6c2-d206b6404b9c\x00mitre-attack-techniques\x00default description\x00Qwen3-Embedding-0.6B\x00$7a95ef45-fb38-49cc-8132-4b57b84deb34\x00.YAKHNSW\x00")
	file, err := os.Create(packagePath)
	require.NoError(t, err)
	gz := gzip.NewWriter(file)
	_, err = gz.Write(raw)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	require.NoError(t, file.Close())

	return dbPath, packagePath
}
