package cli

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunKBImport_ValidatesRequiredFlags(t *testing.T) {
	_ = setupCampaignTestEnv(t)

	oldType := kbImportType
	oldPath := kbPath
	oldWorkspace := kbWorkspace
	t.Cleanup(func() {
		kbImportType = oldType
		kbPath = oldPath
		kbWorkspace = oldWorkspace
	})

	kbImportType = ""
	kbPath = ""
	kbWorkspace = ""

	err := runKBImport(kbImportCmd, nil)
	require.ErrorContains(t, err, "type is required")
}

func TestRunKBImport_JSONSummary(t *testing.T) {
	_ = setupCampaignTestEnv(t)
	sourcePath := setupKBImportFixture(t)

	oldType := kbImportType
	oldPath := kbPath
	oldWorkspace := kbWorkspace
	oldJSON := globalJSON
	t.Cleanup(func() {
		kbImportType = oldType
		kbPath = oldPath
		kbWorkspace = oldWorkspace
		globalJSON = oldJSON
	})

	kbImportType = "security-sqlite"
	kbPath = sourcePath
	kbWorkspace = "security-kb"
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runKBImport(kbImportCmd, nil))
	})

	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(output), &summary))
	assert.Equal(t, "security-sqlite", summary["type"])
	assert.Equal(t, "security-kb", summary["workspace"])
	assert.Equal(t, float64(1), summary["documents"])
	assert.NotEmpty(t, summary["path"])
}

func setupKBImportFixture(t *testing.T) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "security_kb.sqlite")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	_, err = db.Exec(`CREATE TABLE cwe (
		id TEXT PRIMARY KEY,
		cwe_num INTEGER NOT NULL,
		name TEXT NOT NULL,
		abstraction TEXT,
		status TEXT,
		description TEXT,
		extended_description TEXT,
		likelihood_of_exploit TEXT,
		embedding_text TEXT
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE cwe_mitigation (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cwe_id TEXT NOT NULL,
		phase TEXT,
		strategy TEXT,
		description TEXT NOT NULL,
		effectiveness TEXT
	)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO cwe (id, cwe_num, name, abstraction, status, description, extended_description, likelihood_of_exploit) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"CWE-1004", 1004, "Sensitive Cookie Without 'HttpOnly' Flag", "Variant", "Incomplete",
		"Cookie stores sensitive data without HttpOnly.",
		"HttpOnly helps block client-side scripts from reading cookies.",
		"Medium",
	)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO cwe_mitigation (cwe_id, phase, strategy, description, effectiveness) VALUES (?, ?, ?, ?, ?)`,
		"CWE-1004", "Implementation", "", "Set the HttpOnly flag on sensitive cookies.", "High",
	)
	require.NoError(t, err)

	return dbPath
}
