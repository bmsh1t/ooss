package knowledge

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchURLPreview_ExtractsArticleContent(t *testing.T) {
	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head>
    <title>Threat Hunting Notes</title>
    <style>.nav { display:none; }</style>
    <script>console.log("ignore")</script>
  </head>
  <body>
    <nav>Home Archive Contact</nav>
    <main>
      <article>
        <h1>Threat Hunting Notes</h1>
        <p>Authentication bypass investigations should start with session fixation and token confusion checks.</p>
        <p>Validate redirect behavior, compare cookie scope, and capture reproducible proof before escalation.</p>
        <p>Review admin preview routes and fallback handlers when privilege boundaries appear inconsistent.</p>
      </article>
    </main>
    <footer>Copyright</footer>
  </body>
</html>`))
	}))
	defer server.Close()

	preview, err := FetchURLPreview(context.Background(), server.URL)
	require.NoError(t, err)
	require.NotNil(t, preview)

	assert.Equal(t, "Threat Hunting Notes", preview.Title)
	assert.Equal(t, server.URL, preview.URL)
	assert.Equal(t, server.URL, preview.FinalURL)
	assert.Equal(t, http.StatusOK, preview.StatusCode)
	assert.Contains(t, preview.Content, "token confusion checks")
	assert.GreaterOrEqual(t, preview.QualityScore, 70)
	assert.GreaterOrEqual(t, preview.Paragraphs, 3)
	assert.Empty(t, preview.Warnings)
	assert.Equal(t, "public-reference", preview.SuggestedSampleType)
	assert.Greater(t, preview.SuggestedSourceConfidence, 0.5)
	assert.Contains(t, preview.SuggestedLabels, "security-reference")
	assert.Contains(t, preview.SuggestedTargetTypes, "web")
	assert.Contains(t, preview.Markdown, "# Threat Hunting Notes")
	assert.Contains(t, preview.Markdown, "## Suggested Metadata")
	assert.Contains(t, preview.Markdown, "## Article")
	assert.Contains(t, preview.Markdown, urlPreviewMetaBeginMarker)
	assert.Contains(t, preview.Markdown, urlPreviewArticleBeginMarker)
	assert.Contains(t, preview.Markdown, "- Quality Score: ")
	assert.NotContains(t, preview.Content, "console.log")
}

func TestFetchURLPreview_RejectsNonHTMLResponses(t *testing.T) {
	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	preview, err := FetchURLPreview(context.Background(), server.URL)
	require.Error(t, err)
	assert.Nil(t, preview)
	assert.Contains(t, err.Error(), "unsupported content type")
}

func TestFetchURLPreview_RejectsBlockedPage(t *testing.T) {
	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head><title>Just a moment...</title></head>
  <body>
    <main>
      <p>Please enable JavaScript and cookies to continue.</p>
      <p>Cloudflare Ray ID: 1234</p>
    </main>
  </body>
</html>`))
	}))
	defer server.Close()

	preview, err := FetchURLPreview(context.Background(), server.URL)
	require.Error(t, err)
	assert.Nil(t, preview)
	assert.Contains(t, err.Error(), "bot-check")
}

func TestFetchURLPreview_FlagsShortPreviewWithWarnings(t *testing.T) {
	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head><title>Short Note</title></head>
  <body>
    <article>
      <p>Quick operator note about preview validation and login state.</p>
      <p>Review manually.</p>
    </article>
  </body>
</html>`))
	}))
	defer server.Close()

	preview, err := FetchURLPreview(context.Background(), server.URL)
	require.NoError(t, err)
	require.NotNil(t, preview)
	assert.NotEmpty(t, preview.Warnings)
	assert.Less(t, preview.QualityScore, 70)
	assert.Contains(t, preview.Markdown, "- Warnings: ")
}

func TestFetchURLPreview_CleansCommonPageBoilerplate(t *testing.T) {
	server := testutil.NewLoopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html>
  <head><title>CN Style Article</title></head>
  <body>
    <article>
      <h1>CN Style Article</h1>
      <p>admin</p>
      <p>168967 文章 130 评论 13 views 阅读模式</p>
      <p>真实正文第一段，描述登录逻辑和访问控制的问题。</p>
      <p>真实正文第二段，说明如何确认漏洞与复测路径。</p>
      <p>下方可添加作者微信</p>
      <p>免责声明: 仅供研究使用</p>
      <p>微信扫一扫</p>
    </article>
  </body>
</html>`))
	}))
	defer server.Close()

	preview, err := FetchURLPreview(context.Background(), server.URL)
	require.NoError(t, err)
	require.NotNil(t, preview)
	assert.Contains(t, preview.Content, "真实正文第一段")
	assert.Contains(t, preview.Content, "真实正文第二段")
	assert.NotContains(t, preview.Content, "168967 文章")
	assert.NotContains(t, preview.Content, "下方可添加作者微信")
	assert.NotContains(t, preview.Content, "免责声明")
	assert.NotContains(t, preview.Content, "微信扫一扫")
}

func TestRenderURLPreviewMarkdown_IncludesMetadata(t *testing.T) {
	preview := &URLFetchPreview{
		URL:                       "https://example.com/post",
		FinalURL:                  "https://example.com/post?view=full",
		Title:                     "Example Post",
		Content:                   "Operational note body.",
		ContentType:               "text/html; charset=utf-8",
		FetchedAt:                 "2026-03-28T12:00:00Z",
		Paragraphs:                2,
		QualityScore:              78,
		Warnings:                  []string{"content body is short"},
		SuggestedSampleType:       "public-reference",
		SuggestedSourceConfidence: 0.73,
		SuggestedLabels:           []string{"external-reference", "web-article"},
		SuggestedTargetTypes:      []string{"url", "web"},
	}

	markdown := RenderURLPreviewMarkdown(preview)
	assert.Contains(t, markdown, "# Example Post")
	assert.Contains(t, markdown, "- Source URL: https://example.com/post")
	assert.Contains(t, markdown, "- Final URL: https://example.com/post?view=full")
	assert.Contains(t, markdown, "- Fetched At: 2026-03-28T12:00:00Z")
	assert.Contains(t, markdown, "- Content Type: text/html; charset=utf-8")
	assert.Contains(t, markdown, "- Paragraphs: 2")
	assert.Contains(t, markdown, "- Quality Score: 78")
	assert.Contains(t, markdown, "- Warnings: content body is short")
	assert.Contains(t, markdown, "## Suggested Metadata")
	assert.Contains(t, markdown, "- Sample Type: public-reference")
	assert.Contains(t, markdown, "- Source Confidence: 0.73")
	assert.Contains(t, markdown, "- Labels: external-reference, web-article")
	assert.Contains(t, markdown, "- Target Types: url, web")
	assert.Contains(t, markdown, urlPreviewMetaBeginMarker)
	assert.Contains(t, markdown, urlPreviewArticleBeginMarker)
	assert.Contains(t, markdown, "Operational note body.")
}

func TestParseURLPreviewMarkdown_AcceptsLegacyPreview(t *testing.T) {
	markdown := strings.TrimSpace(`
# Legacy Preview

- Source URL: https://example.com/post
- Fetched At: 2026-03-28T12:00:00Z
- Content Type: text/html; charset=utf-8
- Paragraphs: 2
- Quality Score: 76

## Article

Legacy operator notes about authentication checks and session boundaries.
`)

	parsed, err := parseURLPreviewMarkdown(markdown)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	assert.Equal(t, "Legacy Preview", parsed.Title)
	assert.Equal(t, "https://example.com/post", parsed.Manifest.SourceURL)
	assert.Equal(t, "public-reference", parsed.Manifest.SuggestedSampleType)
	assert.Contains(t, parsed.Manifest.SuggestedLabels, "auth")
	assert.Contains(t, parsed.Content, "session boundaries")
}

func TestParseURLPreviewMarkdown_RejectsGenericMarkdown(t *testing.T) {
	parsed, err := parseURLPreviewMarkdown("# Notes\n\nThis is just a normal markdown file.")
	require.Error(t, err)
	assert.Nil(t, parsed)
	assert.Contains(t, err.Error(), "not a generated osmedeus url preview")
}

func TestIngestURLPreview_PersistsReviewedPreviewMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   filepath.Join(tmpDir, "preview-ingest.sqlite"),
		},
	}

	_, err := database.Connect(cfg)
	require.NoError(t, err)
	require.NoError(t, database.Migrate(context.Background()))
	defer func() {
		_ = database.Close()
		database.SetDB(nil)
	}()

	preview := &URLFetchPreview{
		URL:                       "https://example.com/post",
		FinalURL:                  "https://example.com/post",
		Title:                     "Example Post",
		Content:                   "Operator review paragraph one.\n\nParagraph two with auth flow notes.",
		ContentType:               "text/html; charset=utf-8",
		FetchedAt:                 "2026-03-28T12:00:00Z",
		Paragraphs:                2,
		QualityScore:              78,
		SuggestedSampleType:       "public-reference",
		SuggestedSourceConfidence: 0.74,
		SuggestedLabels:           []string{"external-reference", "web-article"},
		SuggestedTargetTypes:      []string{"url", "web"},
	}

	markdown := RenderURLPreviewMarkdown(preview)
	markdown = strings.Replace(markdown, "- Sample Type: public-reference", "- Sample Type: operator-followup", 1)
	markdown = strings.Replace(markdown, "- Source Confidence: 0.74", "- Source Confidence: 0.91", 1)
	markdown = strings.Replace(markdown, "- Labels: external-reference, web-article", "- Labels: operator-followup, manual-review", 1)
	markdown = strings.Replace(markdown, "- Target Types: url, web", "- Target Types: url, api", 1)
	markdown = strings.Replace(markdown, "Operator review paragraph one.", "Edited operator review paragraph one.", 1)

	previewPath := filepath.Join(tmpDir, "article-preview.md")
	require.NoError(t, os.WriteFile(previewPath, []byte(markdown), 0o644))

	summary, err := IngestURLPreview(context.Background(), previewPath, "acme")
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, "acme", summary.Workspace)
	assert.Equal(t, "https://example.com/post", summary.SourcePath)
	assert.Equal(t, "operator-followup", summary.SuggestedSampleType)
	assert.Equal(t, 0.91, summary.SuggestedSourceConfidence)
	assert.Contains(t, summary.SuggestedLabels, "manual-review")
	assert.Contains(t, summary.SuggestedTargetTypes, "api")

	docs, err := ListDocuments(context.Background(), "acme", 0, 10)
	require.NoError(t, err)
	require.Len(t, docs.Data, 1)
	assert.Equal(t, "url-preview", docs.Data[0].SourceType)
	assert.Equal(t, "web-article", docs.Data[0].DocType)

	docMetadata := database.ParseKnowledgeMetadata(docs.Data[0].Metadata)
	require.NotNil(t, docMetadata)
	assert.Equal(t, "operator-followup", docMetadata.SampleType)
	assert.Equal(t, 0.91, docMetadata.SourceConfidence)
	assert.Contains(t, docMetadata.Labels, "manual-review")
	assert.Contains(t, docMetadata.TargetTypes, "api")

	var chunks []database.KnowledgeChunk
	err = database.GetDB().NewSelect().
		Model(&chunks).
		Where("workspace = ?", "acme").
		Order("chunk_index ASC").
		Scan(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	assert.Contains(t, chunks[0].Content, "Edited operator review paragraph one.")

	chunkMetadata := database.ParseKnowledgeMetadata(chunks[0].Metadata)
	require.NotNil(t, chunkMetadata)
	assert.Equal(t, "operator-followup", chunkMetadata.SampleType)
}
