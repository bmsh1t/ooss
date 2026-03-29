package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPersistentPreRun_JSONOutputWithWorkflowFolderStaysMachineReadable(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	writeTestWorkflow(t, cfg.WorkflowsPath, "noop", "module")

	settings := fmt.Sprintf(`base_folder: %q
environments:
  workflows: %q
  workspaces: %q
database:
  db_engine: sqlite
  db_path: %q
server:
  enabled_auth_api: false
`, cfg.BaseFolder, cfg.WorkflowsPath, cfg.WorkspacesPath, cfg.Database.DBPath)
	require.NoError(t, os.WriteFile(filepath.Join(cfg.BaseFolder, "osm-settings.yaml"), []byte(settings), 0o644))

	now := time.Now()
	require.NoError(t, database.UpsertKnowledgeDocument(context.Background(), &database.KnowledgeDocument{
		Workspace:   "example.com",
		SourcePath:  "kb://workspace/example.com/auth-playbook.md",
		SourceType:  "generated",
		DocType:     "md",
		Title:       "Workspace Auth Playbook",
		ContentHash: "doc-hash-example-auth",
		Status:      "ready",
		ChunkCount:  1,
		TotalBytes:  96,
		Metadata:    `{"scope":"workspace","sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []database.KnowledgeChunk{{
		Workspace:   "example.com",
		ChunkIndex:  0,
		Section:     "Primary",
		Content:     "Token confusion admin panel preview route investigation notes",
		ContentHash: "chunk-hash-example-auth",
		Metadata:    `{"scope":"workspace","sample_type":"verified","source_confidence":0.95}`,
		CreatedAt:   now,
	}}))

	oldSettingsFile := settingsFile
	oldBaseFolder := baseFolder
	oldWorkflowFolder := workflowFolder
	oldSilent := silent
	oldDisableLogging := disableLogging
	oldDisableColor := disableColor
	oldDisableNotification := disableNotification
	oldCIOutputFormat := ciOutputFormat
	oldGlobalJSON := globalJSON
	oldKBWorkspace := kbWorkspace
	oldKBQuery := kbQuery
	oldKBLimit := kbLimit
	oldKBWorkspaceLayers := kbWorkspaceLayers
	oldKBScopeLayers := kbScopeLayers
	oldKBMinSourceConfidence := kbMinSourceConfidence
	oldKBSampleTypes := kbSampleTypes
	oldKBExcludeSampleTypes := kbExcludeSampleTypes
	oldOSMSilent := os.Getenv("OSMEDEUS_SILENT")

	t.Cleanup(func() {
		settingsFile = oldSettingsFile
		baseFolder = oldBaseFolder
		workflowFolder = oldWorkflowFolder
		silent = oldSilent
		disableLogging = oldDisableLogging
		disableColor = oldDisableColor
		disableNotification = oldDisableNotification
		ciOutputFormat = oldCIOutputFormat
		globalJSON = oldGlobalJSON
		kbWorkspace = oldKBWorkspace
		kbQuery = oldKBQuery
		kbLimit = oldKBLimit
		kbWorkspaceLayers = oldKBWorkspaceLayers
		kbScopeLayers = oldKBScopeLayers
		kbMinSourceConfidence = oldKBMinSourceConfidence
		kbSampleTypes = oldKBSampleTypes
		kbExcludeSampleTypes = oldKBExcludeSampleTypes
		if oldOSMSilent == "" {
			_ = os.Unsetenv("OSMEDEUS_SILENT")
		} else {
			_ = os.Setenv("OSMEDEUS_SILENT", oldOSMSilent)
		}
	})

	settingsFile = ""
	baseFolder = cfg.BaseFolder
	workflowFolder = cfg.WorkflowsPath
	silent = false
	disableLogging = false
	disableColor = true
	disableNotification = true
	ciOutputFormat = false
	globalJSON = true
	kbWorkspace = "example.com"
	kbQuery = "token confusion admin panel preview route"
	kbLimit = 1
	kbWorkspaceLayers = nil
	kbScopeLayers = nil
	kbMinSourceConfidence = 0
	kbSampleTypes = nil
	kbExcludeSampleTypes = nil

	output := captureStdout(t, func() {
		require.NoError(t, rootCmd.PersistentPreRunE(kbSearchCmd, nil))
		require.NoError(t, runKBSearch(kbSearchCmd, nil))
	})

	var hits []map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &hits))
	require.Len(t, hits, 1)
	assert.Equal(t, "example.com", hits[0]["workspace"])
	assert.Equal(t, "Workspace Auth Playbook", hits[0]["title"])
	assert.Equal(t, "1", os.Getenv("OSMEDEUS_SILENT"))
}
