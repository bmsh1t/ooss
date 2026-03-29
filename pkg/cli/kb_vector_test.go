package cli

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunKBVectorDoctor_JSONReportIncludesSemanticStatus(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	cfg.LLM.LLMProviders = []config.LLMProvider{{
		Provider: "openai",
		BaseURL:  "http://127.0.0.1:1/embeddings",
		Model:    "test-embedding-3-small",
	}}

	oldWorkspace := kbWorkspace
	oldProvider := kbVectorProvider
	oldModel := kbVectorModel
	oldJSON := globalJSON
	oldSilent := os.Getenv("OSMEDEUS_SILENT")
	t.Cleanup(func() {
		kbWorkspace = oldWorkspace
		kbVectorProvider = oldProvider
		kbVectorModel = oldModel
		globalJSON = oldJSON
		if oldSilent == "" {
			_ = os.Unsetenv("OSMEDEUS_SILENT")
		} else {
			_ = os.Setenv("OSMEDEUS_SILENT", oldSilent)
		}
	})

	kbWorkspace = "acme"
	kbVectorProvider = ""
	kbVectorModel = ""
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runKBVectorDoctor(kbVectorDoctorCmd, nil))
	})

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.Equal(t, "provider_not_configured", report["semantic_status"])
	assert.Equal(t, false, report["semantic_search_ready"])
	assert.Contains(t, report["semantic_status_message"], "default_provider")
	assert.NotNil(t, report["available_providers"])
}
