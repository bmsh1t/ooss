package cli

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunKBBridgeYakitRAG_JSONSummary(t *testing.T) {
	dbPath, packagePath := setupKBYakitBridgeFixture(t)
	outputPath := filepath.Join(t.TempDir(), "mitre-attack-techniques.jsonl")

	oldPath := kbPath
	oldOutput := kbOutput
	oldYakitDB := kbYakitDB
	oldYakitKBName := kbYakitKBName
	oldYakitFormat := kbYakitFormat
	oldJSON := globalJSON
	t.Cleanup(func() {
		kbPath = oldPath
		kbOutput = oldOutput
		kbYakitDB = oldYakitDB
		kbYakitKBName = oldYakitKBName
		kbYakitFormat = oldYakitFormat
		globalJSON = oldJSON
	})

	kbPath = packagePath
	kbOutput = outputPath
	kbYakitDB = dbPath
	kbYakitKBName = ""
	kbYakitFormat = "auto"
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runKBBridgeYakitRAG(kbBridgeYakitRAGCmd, nil))
	})

	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &summary))
	assert.Equal(t, "MITRE ATT&CK Techniques", summary["knowledge_base_name"])
	assert.Equal(t, "jsonl", summary["format"])
	assert.Equal(t, float64(2), summary["entries"])
	assert.Equal(t, "collection_uuid", summary["resolved_by"])

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "T1057 (Process Discovery) - ATT&CK")
}

func setupKBYakitBridgeFixture(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "yakit-profile-plugin.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	for _, stmt := range []string{
		`CREATE TABLE rag_knowledge_base_v1 (
			id integer primary key autoincrement,
			rag_id varchar(255),
			knowledge_base_name varchar(255) NOT NULL,
			knowledge_base_type varchar(255) NOT NULL,
			serial_version_uid varchar(255)
		)`,
		`CREATE TABLE rag_vector_collection_v1 (
			id integer primary key autoincrement,
			name varchar(255),
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
	} {
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

	for _, stmt := range []struct {
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
			summary:   "Enumerates running processes.",
			details:   "executor: sh\ncommand: ps aux",
			questions: "How do operators enumerate running processes?",
			keywords:  "process discovery,linux",
		},
		{
			id:        25904,
			title:     "T1124 (System Time Discovery) - ATT&CK",
			summary:   "Queries system time.",
			details:   "executor: sh\ncommand: date",
			questions: "How can adversaries query system time?",
			keywords:  "time discovery,date",
		},
	} {
		_, err = db.Exec(`INSERT INTO rag_knowledge_entry_v1 (id, knowledge_base_id, related_entity_uuid_s, knowledge_title, knowledge_type, importance_score, keywords, knowledge_details, summary, source_page, potential_questions, hidden_index, has_question_index) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			stmt.id,
			16,
			"",
			stmt.title,
			"question_index",
			0,
			stmt.keywords,
			stmt.details,
			stmt.summary,
			1,
			stmt.questions,
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
