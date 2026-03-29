package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/knowledge"
	"github.com/j3ssie/osmedeus/v5/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunKBFetchURL_JSONAndFilePreview(t *testing.T) {
	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head><title>KB Preview Article</title></head>
  <body>
    <article>
      <h1>KB Preview Article</h1>
      <p>Token confusion validation requires reviewing preview handlers and session boundaries.</p>
      <p>Capture proof before escalating to exploitation or campaign follow-up.</p>
      <p>Keep the preview clean enough for manual review prior to ingestion.</p>
    </article>
  </body>
</html>`))
	}))
	defer server.Close()

	oldURL := kbURL
	oldURLFile := kbURLFile
	oldOutput := kbOutput
	oldOutputDir := kbOutputDir
	oldPrint := kbPrint
	oldJSON := globalJSON
	t.Cleanup(func() {
		kbURL = oldURL
		kbURLFile = oldURLFile
		kbOutput = oldOutput
		kbOutputDir = oldOutputDir
		kbPrint = oldPrint
		globalJSON = oldJSON
	})

	kbURL = server.URL
	kbOutput = ""
	kbPrint = false
	globalJSON = true

	jsonOutput := captureStdout(t, func() {
		require.NoError(t, runKBFetchURL(kbFetchURLCmd, nil))
	})

	var preview map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOutput), &preview))
	assert.Equal(t, "KB Preview Article", preview["title"])
	assert.Equal(t, server.URL, preview["url"])
	assert.Contains(t, preview["markdown"].(string), "## Article")
	assert.Equal(t, "public-reference", preview["suggested_sample_type"])
	assert.NotEmpty(t, preview["suggested_labels"])

	outputPath := filepath.Join(t.TempDir(), "preview.md")
	kbOutput = outputPath
	globalJSON = false

	textOutput := captureStdout(t, func() {
		require.NoError(t, runKBFetchURL(kbFetchURLCmd, nil))
	})

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# KB Preview Article")
	assert.Contains(t, string(data), "## Suggested Metadata")
	assert.Contains(t, string(data), "Token confusion validation")
	assert.Contains(t, textOutput, "URL preview saved")
	assert.Contains(t, textOutput, "osmedeus kb ingest-preview --path")
}

func TestRunKBFetchURL_BatchPreviewFromURLFile(t *testing.T) {
	serverA := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head><title>Article Alpha</title></head>
  <body>
    <article>
      <p>Alpha article focuses on redirect validation, token confusion, and preview endpoints.</p>
      <p>Operators should review this manually before any knowledge-base ingest step.</p>
      <p>Batch fetch mode should save a markdown preview file for this entry.</p>
    </article>
  </body>
</html>`))
	}))
	defer serverA.Close()

	serverB := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head><title>Article Beta</title></head>
  <body>
    <main>
      <p>Beta article focuses on admin preview handlers and session-boundary proof capture.</p>
      <p>Batch preview output should stay operator-review-first and avoid direct KB writes.</p>
      <p>The generated markdown file should contain the article body.</p>
    </main>
  </body>
</html>`))
	}))
	defer serverB.Close()

	urlFile := filepath.Join(t.TempDir(), "urls.txt")
	require.NoError(t, os.WriteFile(urlFile, []byte(serverA.URL+"\n"+serverB.URL+"\n"), 0o644))

	outputDir := filepath.Join(t.TempDir(), "previews")

	oldURL := kbURL
	oldURLFile := kbURLFile
	oldOutput := kbOutput
	oldOutputDir := kbOutputDir
	oldPrint := kbPrint
	oldJSON := globalJSON
	t.Cleanup(func() {
		kbURL = oldURL
		kbURLFile = oldURLFile
		kbOutput = oldOutput
		kbOutputDir = oldOutputDir
		kbPrint = oldPrint
		globalJSON = oldJSON
	})

	kbURL = ""
	kbURLFile = urlFile
	kbOutput = ""
	kbOutputDir = outputDir
	kbPrint = false
	globalJSON = false

	textOutput := captureStdout(t, func() {
		require.NoError(t, runKBFetchURL(kbFetchURLCmd, nil))
	})

	entries, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	firstPreview, err := os.ReadFile(filepath.Join(outputDir, entries[0].Name()))
	require.NoError(t, err)
	secondPreview, err := os.ReadFile(filepath.Join(outputDir, entries[1].Name()))
	require.NoError(t, err)

	combined := string(firstPreview) + "\n" + string(secondPreview)
	assert.Contains(t, combined, "Article Alpha")
	assert.Contains(t, combined, "Article Beta")
	assert.Contains(t, textOutput, "Batch URL previews saved")
	assert.Contains(t, textOutput, "Succeeded:  2")
	assert.Contains(t, textOutput, "Failed:     0")
	assert.Contains(t, textOutput, "osmedeus kb ingest-preview --path")
}

func TestRunKBIngestPreview_JSONSummary(t *testing.T) {
	_ = setupCampaignTestEnv(t)

	previewPath := filepath.Join(t.TempDir(), "article-preview.md")
	preview := &knowledge.URLFetchPreview{
		URL:                       "https://example.com/post",
		FinalURL:                  "https://example.com/post",
		Title:                     "Previewed Article",
		Content:                   "Reviewed article body about session controls.\n\nSecond paragraph about operator follow-up.",
		ContentType:               "text/html; charset=utf-8",
		FetchedAt:                 "2026-03-28T12:00:00Z",
		Paragraphs:                2,
		QualityScore:              81,
		SuggestedSampleType:       "public-reference",
		SuggestedSourceConfidence: 0.76,
		SuggestedLabels:           []string{"external-reference", "web-article"},
		SuggestedTargetTypes:      []string{"url", "web"},
	}
	require.NoError(t, os.WriteFile(previewPath, []byte(knowledge.RenderURLPreviewMarkdown(preview)), 0o644))

	oldPath := kbPath
	oldWorkspace := kbWorkspace
	oldJSON := globalJSON
	t.Cleanup(func() {
		kbPath = oldPath
		kbWorkspace = oldWorkspace
		globalJSON = oldJSON
	})

	kbPath = previewPath
	kbWorkspace = "acme"
	globalJSON = true

	output := captureStdout(t, func() {
		require.NoError(t, runKBIngestPreview(kbIngestPreviewCmd, nil))
	})

	var summary map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &summary))
	assert.Equal(t, "acme", summary["workspace"])
	assert.Equal(t, "https://example.com/post", summary["source_path"])
	assert.Equal(t, "web-article", summary["doc_type"])
	assert.Equal(t, false, summary["vector_indexed"])
	assert.Contains(t, summary["vector_error"], "vector provider/model is not configured")

	require.NoError(t, connectDB())
	defer func() { _ = database.Close() }()

	docs, err := database.ListKnowledgeDocuments(context.Background(), database.KnowledgeDocumentQuery{
		Workspace: "acme",
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, docs.Data, 1)
	assert.Equal(t, "url-preview", docs.Data[0].SourceType)
}

func TestRunKBIngestPreview_TextOutputWarnsOnVectorFailure(t *testing.T) {
	_ = setupCampaignTestEnv(t)

	previewPath := filepath.Join(t.TempDir(), "article-preview.md")
	preview := &knowledge.URLFetchPreview{
		URL:                       "https://example.com/post",
		FinalURL:                  "https://example.com/post",
		Title:                     "Previewed Article",
		Content:                   "Reviewed article body about session controls.\n\nSecond paragraph about operator follow-up.",
		ContentType:               "text/html; charset=utf-8",
		FetchedAt:                 "2026-03-28T12:00:00Z",
		Paragraphs:                2,
		QualityScore:              81,
		SuggestedSampleType:       "public-reference",
		SuggestedSourceConfidence: 0.76,
		SuggestedLabels:           []string{"external-reference", "web-article"},
		SuggestedTargetTypes:      []string{"url", "web"},
	}
	require.NoError(t, os.WriteFile(previewPath, []byte(knowledge.RenderURLPreviewMarkdown(preview)), 0o644))

	oldPath := kbPath
	oldWorkspace := kbWorkspace
	oldJSON := globalJSON
	t.Cleanup(func() {
		kbPath = oldPath
		kbWorkspace = oldWorkspace
		globalJSON = oldJSON
	})

	kbPath = previewPath
	kbWorkspace = "acme"
	globalJSON = false

	output := captureStdout(t, func() {
		require.NoError(t, runKBIngestPreview(kbIngestPreviewCmd, nil))
	})

	assert.Contains(t, output, "Knowledge preview ingest completed")
	assert.Contains(t, output, "Vector auto-index failed")
	assert.Contains(t, output, "Primary KB write completed")

	require.NoError(t, connectDB())
	defer func() { _ = database.Close() }()

	docs, err := database.ListKnowledgeDocuments(context.Background(), database.KnowledgeDocumentQuery{
		Workspace: "acme",
		Limit:     10,
	})
	require.NoError(t, err)
	require.Len(t, docs.Data, 1)
}

func TestRunKBIngestPreview_RejectsOrdinaryMarkdown(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	disabled := false
	cfg.KnowledgeVector.Enabled = &disabled
	cfg.KnowledgeVector.AutoIndexOnIngest = &disabled

	plainPath := filepath.Join(t.TempDir(), "notes.md")
	require.NoError(t, os.WriteFile(plainPath, []byte("# Notes\n\nJust normal markdown."), 0o644))

	oldPath := kbPath
	oldWorkspace := kbWorkspace
	oldJSON := globalJSON
	t.Cleanup(func() {
		kbPath = oldPath
		kbWorkspace = oldWorkspace
		globalJSON = oldJSON
	})

	kbPath = plainPath
	kbWorkspace = "acme"
	globalJSON = false

	err := runKBIngestPreview(kbIngestPreviewCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a generated osmedeus url preview")
}
