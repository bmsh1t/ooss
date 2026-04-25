package knowledge

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	htmlstd "html"
	"io"
	"io/fs"
	"mime"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	xhtml "golang.org/x/net/html"
)

const (
	maxChunkChars        = 1200
	minChunkChars        = 120
	maxFetchURLBytes     = 2 * 1024 * 1024
	maxFetchURLRedirects = 4
	fetchURLTimeout      = 15 * time.Second

	urlPreviewFormatKind    = "osmedeus-url-preview"
	urlPreviewFormatVersion = 1

	urlPreviewMetaBeginMarker    = "<!-- OSMEDEUS_URL_PREVIEW_META_BEGIN"
	urlPreviewMetaEndMarker      = "OSMEDEUS_URL_PREVIEW_META_END -->"
	urlPreviewArticleBeginMarker = "<!-- OSMEDEUS_URL_PREVIEW_ARTICLE_BEGIN -->"
	urlPreviewArticleEndMarker   = "<!-- OSMEDEUS_URL_PREVIEW_ARTICLE_END -->"
)

// IngestSummary reports the outcome of a knowledge import job.
type IngestSummary struct {
	Workspace     string   `json:"workspace"`
	RootPath      string   `json:"root_path"`
	Documents     int      `json:"documents"`
	Chunks        int      `json:"chunks"`
	Skipped       int      `json:"skipped"`
	Failed        int      `json:"failed"`
	Errors        []string `json:"errors,omitempty"`
	VectorIndexed *bool    `json:"vector_indexed,omitempty"`
	VectorError   string   `json:"vector_error,omitempty"`
}

type extractedDocument struct {
	Title    string
	DocType  string
	Content  string
	Metadata map[string]interface{}
}

type extractedChunk struct {
	Section string
	Content string
}

// URLFetchPreview is a non-persistent preview of a fetched article page.
type URLFetchPreview struct {
	URL                       string   `json:"url"`
	FinalURL                  string   `json:"final_url"`
	Title                     string   `json:"title"`
	Content                   string   `json:"content"`
	Markdown                  string   `json:"markdown"`
	ContentType               string   `json:"content_type"`
	StatusCode                int      `json:"status_code"`
	TotalBytes                int      `json:"total_bytes"`
	FetchedAt                 string   `json:"fetched_at"`
	Paragraphs                int      `json:"paragraphs"`
	QualityScore              int      `json:"quality_score"`
	Warnings                  []string `json:"warnings,omitempty"`
	SuggestedSampleType       string   `json:"suggested_sample_type,omitempty"`
	SuggestedSourceConfidence float64  `json:"suggested_source_confidence,omitempty"`
	SuggestedLabels           []string `json:"suggested_labels,omitempty"`
	SuggestedTargetTypes      []string `json:"suggested_target_types,omitempty"`
}

// URLPreviewIngestSummary reports the result of confirming a fetched preview into the KB.
type URLPreviewIngestSummary struct {
	Workspace                 string   `json:"workspace"`
	PreviewPath               string   `json:"preview_path"`
	SourcePath                string   `json:"source_path"`
	SourceType                string   `json:"source_type"`
	DocType                   string   `json:"doc_type"`
	Title                     string   `json:"title"`
	Documents                 int      `json:"documents"`
	Chunks                    int      `json:"chunks"`
	SuggestedSampleType       string   `json:"suggested_sample_type,omitempty"`
	SuggestedSourceConfidence float64  `json:"suggested_source_confidence,omitempty"`
	SuggestedLabels           []string `json:"suggested_labels,omitempty"`
	SuggestedTargetTypes      []string `json:"suggested_target_types,omitempty"`
	VectorIndexed             *bool    `json:"vector_indexed,omitempty"`
	VectorError               string   `json:"vector_error,omitempty"`
}

type urlPreviewSuggestion struct {
	SampleType       string
	SourceConfidence float64
	Labels           []string
	TargetTypes      []string
}

type urlPreviewManifest struct {
	Kind                      string   `json:"kind"`
	Version                   int      `json:"version"`
	Title                     string   `json:"title,omitempty"`
	SourceURL                 string   `json:"source_url"`
	FinalURL                  string   `json:"final_url,omitempty"`
	FetchedAt                 string   `json:"fetched_at,omitempty"`
	ContentType               string   `json:"content_type,omitempty"`
	Paragraphs                int      `json:"paragraphs,omitempty"`
	QualityScore              int      `json:"quality_score,omitempty"`
	Warnings                  []string `json:"warnings,omitempty"`
	SuggestedSampleType       string   `json:"suggested_sample_type,omitempty"`
	SuggestedSourceConfidence float64  `json:"suggested_source_confidence,omitempty"`
	SuggestedLabels           []string `json:"suggested_labels,omitempty"`
	SuggestedTargetTypes      []string `json:"suggested_target_types,omitempty"`
}

type parsedURLPreview struct {
	Title    string
	Content  string
	Manifest urlPreviewManifest
}

// SearchOptions controls layered knowledge retrieval.
type SearchOptions struct {
	Workspace           string   `json:"workspace,omitempty"`
	WorkspaceLayers     []string `json:"workspace_layers,omitempty"`
	ScopeLayers         []string `json:"scope_layers,omitempty"`
	Query               string   `json:"query"`
	Limit               int      `json:"limit,omitempty"`
	ExactID             bool     `json:"exact_id,omitempty"`
	MinSourceConfidence float64  `json:"min_source_confidence,omitempty"`
	SampleTypes         []string `json:"sample_types,omitempty"`
	ExcludeSampleTypes  []string `json:"exclude_sample_types,omitempty"`
}

// IngestPath ingests a single file or a directory tree into the local knowledge base.
func IngestPath(ctx context.Context, cfg *config.Config, rootPath, workspace string, recursive bool) (*IngestSummary, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	_ = cfg
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return nil, fmt.Errorf("path is required")
	}

	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	summary := &IngestSummary{
		Workspace: normalizeWorkspace(workspace),
		RootPath:  absPath,
	}

	if info.IsDir() {
		if err := ingestDirectory(ctx, absPath, summary, recursive); err != nil {
			return summary, err
		}
		return summary, nil
	}

	if err := ingestFile(ctx, absPath, summary); err != nil {
		summary.Failed++
		summary.Errors = append(summary.Errors, err.Error())
	}
	return summary, nil
}

// IngestURLPreview confirms a generated URL preview markdown file into the knowledge base.
func IngestURLPreview(ctx context.Context, previewPath, workspace string) (*URLPreviewIngestSummary, error) {
	previewPath = strings.TrimSpace(previewPath)
	if previewPath == "" {
		return nil, fmt.Errorf("path is required")
	}

	absPath, err := filepath.Abs(previewPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read preview file: %w", err)
	}

	parsed, err := parseURLPreviewMarkdown(string(content))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", absPath, err)
	}

	sourcePath := strings.TrimSpace(parsed.Manifest.FinalURL)
	if sourcePath == "" {
		sourcePath = strings.TrimSpace(parsed.Manifest.SourceURL)
	}
	if sourcePath == "" {
		return nil, fmt.Errorf("%s: source url is required", absPath)
	}

	metadata := map[string]interface{}{
		"source":                  "url-preview",
		"source_url":              strings.TrimSpace(parsed.Manifest.SourceURL),
		"final_url":               strings.TrimSpace(parsed.Manifest.FinalURL),
		"fetched_at":              strings.TrimSpace(parsed.Manifest.FetchedAt),
		"content_type":            strings.TrimSpace(parsed.Manifest.ContentType),
		"paragraphs":              parsed.Manifest.Paragraphs,
		"quality_score":           parsed.Manifest.QualityScore,
		"warnings":                append([]string(nil), parsed.Manifest.Warnings...),
		"preview_path":            absPath,
		"preview_kind":            urlPreviewFormatKind,
		"preview_version":         urlPreviewFormatVersion,
		"sample_type":             strings.TrimSpace(parsed.Manifest.SuggestedSampleType),
		"source_confidence":       clampURLPreviewConfidence(parsed.Manifest.SuggestedSourceConfidence),
		"labels":                  normalizeURLPreviewSuggestionTokens(parsed.Manifest.SuggestedLabels...),
		"target_types":            normalizeURLPreviewSuggestionTokens(parsed.Manifest.SuggestedTargetTypes...),
		"retrieval_fingerprint":   buildURLPreviewRetrievalFingerprint(sourcePath, parsed.Title, parsed.Content),
		"confidence_observed_at":  strings.TrimSpace(parsed.Manifest.FetchedAt),
		"ingest_confirmation_via": "kb-ingest-preview",
	}

	chunkCount, err := saveKnowledgeContent(ctx, workspace, sourcePath, "url-preview", "web-article", parsed.Title, parsed.Content, metadata, true)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to save preview document: %w", absPath, err)
	}

	return &URLPreviewIngestSummary{
		Workspace:                 normalizeWorkspace(workspace),
		PreviewPath:               absPath,
		SourcePath:                sourcePath,
		SourceType:                "url-preview",
		DocType:                   "web-article",
		Title:                     parsed.Title,
		Documents:                 1,
		Chunks:                    chunkCount,
		SuggestedSampleType:       strings.TrimSpace(parsed.Manifest.SuggestedSampleType),
		SuggestedSourceConfidence: clampURLPreviewConfidence(parsed.Manifest.SuggestedSourceConfidence),
		SuggestedLabels:           normalizeURLPreviewSuggestionTokens(parsed.Manifest.SuggestedLabels...),
		SuggestedTargetTypes:      normalizeURLPreviewSuggestionTokens(parsed.Manifest.SuggestedTargetTypes...),
	}, nil
}

// FetchURLPreview fetches a public HTML article page and returns a reviewable preview.
func FetchURLPreview(ctx context.Context, rawURL string) (*URLFetchPreview, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}

	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme: %s", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("url host is required")
	}

	client := &http.Client{
		Timeout: fetchURLTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxFetchURLRedirects {
				return fmt.Errorf("stopped after %d redirects", maxFetchURLRedirects)
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("User-Agent", "OsmedeusKBFetcher/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch url: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected http status: %d", resp.StatusCode)
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	mediaType := ""
	if contentType != "" {
		parsedType, _, err := mime.ParseMediaType(contentType)
		if err == nil {
			mediaType = strings.TrimSpace(parsedType)
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchURLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > maxFetchURLBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", maxFetchURLBytes)
	}

	if mediaType != "" && mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		return nil, fmt.Errorf("unsupported content type: %s", mediaType)
	}

	htmlInput := string(body)
	if mediaType == "" && !looksLikeHTMLDocument(htmlInput) {
		return nil, fmt.Errorf("response does not look like html content")
	}

	content := extractTextFromHTML(htmlInput)
	content = normalizeContent(content)
	content = cleanURLPreviewContent(content)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("extracted content is empty")
	}

	title := extractHTMLDocumentTitle(htmlInput)
	if strings.TrimSpace(title) == "" {
		title = inferTitle(parsed.Path, content)
	}
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSpace(parsed.Host)
	}

	paragraphs := countURLPreviewParagraphs(content)
	qualityScore, warnings, badReason := evaluateURLPreviewQuality(title, content, htmlInput, resp.Request.URL.String(), paragraphs)
	if badReason != "" {
		return nil, fmt.Errorf("%s", badReason)
	}

	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	preview := &URLFetchPreview{
		URL:          rawURL,
		FinalURL:     resp.Request.URL.String(),
		Title:        title,
		Content:      content,
		ContentType:  normalizeFetchedContentType(contentType, mediaType),
		StatusCode:   resp.StatusCode,
		TotalBytes:   len(body),
		FetchedAt:    fetchedAt,
		Paragraphs:   paragraphs,
		QualityScore: qualityScore,
		Warnings:     warnings,
	}
	suggestion := resolveURLPreviewSuggestion(preview)
	preview.SuggestedSampleType = suggestion.SampleType
	preview.SuggestedSourceConfidence = suggestion.SourceConfidence
	preview.SuggestedLabels = suggestion.Labels
	preview.SuggestedTargetTypes = suggestion.TargetTypes
	preview.Markdown = RenderURLPreviewMarkdown(preview)
	return preview, nil
}

func ingestDirectory(ctx context.Context, rootPath string, summary *IngestSummary, recursive bool) error {
	if !recursive {
		entries, err := os.ReadDir(rootPath)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if err := ingestFile(ctx, filepath.Join(rootPath, entry.Name()), summary); err != nil {
				summary.Failed++
				summary.Errors = append(summary.Errors, err.Error())
			}
		}
		return nil
	}

	return filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			summary.Failed++
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") && path != rootPath {
				return filepath.SkipDir
			}
			return nil
		}
		if err := ingestFile(ctx, path, summary); err != nil {
			summary.Failed++
			summary.Errors = append(summary.Errors, err.Error())
		}
		return nil
	})
}

func ingestFile(ctx context.Context, path string, summary *IngestSummary) error {
	if !isSupportedKnowledgeFile(path) {
		summary.Skipped++
		return nil
	}

	doc, err := extractDocument(ctx, path)
	if err != nil {
		summary.Skipped++
		return fmt.Errorf("%s: %w", path, err)
	}

	content := normalizeContent(doc.Content)
	if strings.TrimSpace(content) == "" {
		summary.Skipped++
		return fmt.Errorf("%s: extracted content is empty", path)
	}

	chunkCount, err := saveKnowledgeContent(ctx, summary.Workspace, path, "file", doc.DocType, doc.Title, content, doc.Metadata, false)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	summary.Documents++
	summary.Chunks += chunkCount
	return nil
}

func saveKnowledgeContent(ctx context.Context, workspace, sourcePath, sourceType, docType, title, content string, metadata map[string]interface{}, includeMetadataInChunks bool) (int, error) {
	content = normalizeContent(content)
	if strings.TrimSpace(content) == "" {
		return 0, fmt.Errorf("extracted content is empty")
	}

	chunks := chunkContent(content)
	if len(chunks) == 0 {
		return 0, fmt.Errorf("no searchable chunks generated")
	}

	now := time.Now()
	record := &database.KnowledgeDocument{
		Workspace:   normalizeWorkspace(workspace),
		SourcePath:  strings.TrimSpace(sourcePath),
		SourceType:  strings.TrimSpace(sourceType),
		DocType:     strings.TrimSpace(docType),
		Title:       strings.TrimSpace(title),
		ContentHash: hashString(content),
		Status:      "ready",
		ChunkCount:  len(chunks),
		TotalBytes:  int64(len(content)),
		Metadata:    marshalMetadata(metadata),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if record.SourceType == "" {
		record.SourceType = "file"
	}

	chunkRows := make([]database.KnowledgeChunk, 0, len(chunks))
	for i, chunk := range chunks {
		chunkMeta := map[string]interface{}{
			"source_path":   record.SourcePath,
			"section":       chunk.Section,
			"chunk_index":   i,
			"document_type": record.DocType,
			"doc_type":      record.DocType,
		}
		if includeMetadataInChunks {
			for key, value := range metadata {
				chunkMeta[key] = value
			}
		}
		chunkRows = append(chunkRows, database.KnowledgeChunk{
			Workspace:   record.Workspace,
			ChunkIndex:  i,
			Section:     chunk.Section,
			Content:     chunk.Content,
			ContentHash: hashString(chunk.Content),
			Metadata:    marshalMetadata(chunkMeta),
			CreatedAt:   now,
		})
	}

	if err := database.UpsertKnowledgeDocument(ctx, record, chunkRows); err != nil {
		return 0, fmt.Errorf("failed to save document: %w", err)
	}

	return len(chunkRows), nil
}

func extractDocument(ctx context.Context, path string) (*extractedDocument, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".markdown", ".log", ".yaml", ".yml", ".csv":
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &extractedDocument{
			Title:   inferTitle(path, string(content)),
			DocType: strings.TrimPrefix(ext, "."),
			Content: string(content),
			Metadata: map[string]interface{}{
				"extractor": "plain",
			},
		}, nil
	case ".json":
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		normalized := normalizeJSONContent(content)
		return &extractedDocument{
			Title:   inferTitle(path, normalized),
			DocType: "json",
			Content: normalized,
			Metadata: map[string]interface{}{
				"extractor": "json",
			},
		}, nil
	case ".jsonl":
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return &extractedDocument{
			Title:   inferTitle(path, string(content)),
			DocType: "jsonl",
			Content: string(content),
			Metadata: map[string]interface{}{
				"extractor": "jsonl",
			},
		}, nil
	case ".html", ".htm":
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		text := extractTextFromHTML(string(content))
		return &extractedDocument{
			Title:   inferTitle(path, text),
			DocType: strings.TrimPrefix(ext, "."),
			Content: text,
			Metadata: map[string]interface{}{
				"extractor": "html",
			},
		}, nil
	case ".epub":
		text, err := extractEPUB(path)
		if err != nil {
			return nil, err
		}
		return &extractedDocument{
			Title:   inferTitle(path, text),
			DocType: "epub",
			Content: text,
			Metadata: map[string]interface{}{
				"extractor": "epub",
			},
		}, nil
	case ".doc":
		text, err := extractLegacyDoc(ctx, path)
		if err != nil {
			return nil, err
		}
		return &extractedDocument{
			Title:   inferTitle(path, text),
			DocType: "doc",
			Content: text,
			Metadata: map[string]interface{}{
				"extractor": "antiword",
			},
		}, nil
	case ".pdf", ".docx", ".pptx", ".xlsx":
		text, err := extractWithDocling(ctx, path)
		if err != nil {
			return nil, err
		}
		return &extractedDocument{
			Title:   inferTitle(path, text),
			DocType: strings.TrimPrefix(ext, "."),
			Content: text,
			Metadata: map[string]interface{}{
				"extractor": "docling",
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}
}

func isSupportedKnowledgeFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".markdown", ".log", ".yaml", ".yml", ".csv", ".json", ".jsonl", ".html", ".htm", ".epub", ".doc", ".docx", ".pdf", ".pptx", ".xlsx":
		return true
	default:
		return false
	}
}

func inferTitle(path, content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if title != "" {
				return title
			}
		}
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func normalizeJSONContent(content []byte) string {
	var obj interface{}
	if err := json.Unmarshal(content, &obj); err != nil {
		return string(content)
	}
	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return string(content)
	}
	return string(pretty)
}

func normalizeContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	clean := make([]string, 0, len(lines))
	blankCount := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blankCount++
			if blankCount > 1 {
				continue
			}
		} else {
			blankCount = 0
		}
		clean = append(clean, line)
	}
	return strings.TrimSpace(strings.Join(clean, "\n"))
}

func chunkContent(content string) []extractedChunk {
	paragraphs := strings.Split(content, "\n\n")
	section := ""
	var chunks []extractedChunk
	var builder strings.Builder

	flush := func() {
		text := strings.TrimSpace(builder.String())
		if len(text) < minChunkChars {
			return
		}
		chunks = append(chunks, extractedChunk{
			Section: section,
			Content: text,
		})
		builder.Reset()
	}

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if strings.HasPrefix(paragraph, "#") {
			title := strings.TrimSpace(strings.TrimLeft(paragraph, "#"))
			if title != "" {
				section = title
			}
		}
		if builder.Len() > 0 && builder.Len()+len(paragraph)+2 > maxChunkChars {
			flush()
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(paragraph)
	}

	if builder.Len() > 0 {
		flush()
	}

	if len(chunks) == 0 && strings.TrimSpace(content) != "" {
		chunks = append(chunks, extractedChunk{Section: section, Content: strings.TrimSpace(content)})
	}

	return chunks
}

func marshalMetadata(metadata map[string]interface{}) string {
	if len(metadata) == 0 {
		return ""
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	return string(data)
}

func hashString(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func normalizeWorkspace(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "global"
	}
	return workspace
}

func extractWithDocling(ctx context.Context, path string) (string, error) {
	doclingPath, err := exec.LookPath("docling")
	if err != nil {
		return "", fmt.Errorf("docling not found in PATH")
	}

	// Prefer stdout first since it works across newer docling variants.
	if output, err := runDocling(ctx, doclingPath, path); err == nil {
		return output, nil
	}

	outputDir, err := os.MkdirTemp("", "osmedeus-docling-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(outputDir) }()

	outputFile := filepath.Join(outputDir, "converted.md")
	if _, err := runDoclingToFile(ctx, doclingPath, outputFile, path, "-o"); err != nil {
		if _, fallbackErr := runDoclingToDirectory(ctx, doclingPath, outputDir, path); fallbackErr != nil {
			return "", fallbackErr
		}
	}

	if _, err := os.Stat(outputFile); err != nil {
		outputFile, err = findFirstFileWithExt(outputDir, ".md")
		if err != nil {
			return "", err
		}
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func runDocling(ctx context.Context, doclingPath, sourcePath string) (string, error) {
	cmd := exec.CommandContext(ctx, doclingPath, sourcePath, "--to", "md")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docling failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	text := strings.TrimSpace(string(output))
	if text == "" {
		return "", fmt.Errorf("docling produced empty output")
	}
	return text, nil
}

func runDoclingToFile(ctx context.Context, doclingPath, outputFile, sourcePath, outputFlag string) (string, error) {
	cmd := exec.CommandContext(ctx, doclingPath, sourcePath, "--to", "md", outputFlag, outputFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docling failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func runDoclingToDirectory(ctx context.Context, doclingPath, outputDir, sourcePath string) (string, error) {
	cmd := exec.CommandContext(ctx, doclingPath, sourcePath, "--to", "md", "--output", outputDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docling failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func extractLegacyDoc(ctx context.Context, path string) (string, error) {
	antiwordPath, err := exec.LookPath("antiword")
	if err != nil {
		return "", fmt.Errorf("antiword not found in PATH")
	}

	cmd := exec.CommandContext(ctx, antiwordPath, path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("antiword failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func findFirstFileWithExt(root, ext string) (string, error) {
	var matches []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			matches = append(matches, path)
		}
		return nil
	})
	if len(matches) == 0 {
		return "", fmt.Errorf("no %s output found", ext)
	}
	sort.Strings(matches)
	return matches[0], nil
}

func extractEPUB(path string) (string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	orderedNames := readEPUBReadingOrder(reader.File)
	names := make([]string, 0)
	files := make(map[string]*zip.File)
	for _, file := range reader.File {
		name := strings.ToLower(file.Name)
		if strings.HasSuffix(name, ".xhtml") || strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".htm") {
			names = append(names, file.Name)
			files[file.Name] = file
		}
	}
	if len(orderedNames) > 0 {
		names = mergeOrderedEPUBNames(orderedNames, names)
	} else {
		sort.Strings(names)
	}

	var parts []string
	for _, name := range names {
		file := files[name]
		rc, err := file.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			continue
		}
		text := extractTextFromHTML(string(data))
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("# %s\n\n%s", filepath.Base(name), text))
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("no readable EPUB content found")
	}

	return strings.Join(parts, "\n\n"), nil
}

type epubContainer struct {
	Rootfiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type epubPackage struct {
	Manifest []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

func readEPUBReadingOrder(files []*zip.File) []string {
	containerPath := ""
	for _, file := range files {
		if strings.EqualFold(file.Name, "META-INF/container.xml") {
			containerPath = file.Name
			break
		}
	}
	if containerPath == "" {
		return nil
	}

	containerData, err := readZIPFile(files, containerPath)
	if err != nil {
		return nil
	}

	var container epubContainer
	if err := xml.Unmarshal(containerData, &container); err != nil {
		return nil
	}
	if len(container.Rootfiles) == 0 || strings.TrimSpace(container.Rootfiles[0].FullPath) == "" {
		return nil
	}

	opfPath := container.Rootfiles[0].FullPath
	opfData, err := readZIPFile(files, opfPath)
	if err != nil {
		return nil
	}

	var pkg epubPackage
	if err := xml.Unmarshal(opfData, &pkg); err != nil {
		return nil
	}

	opfDir := pathpkg.Dir(opfPath)
	manifest := make(map[string]string, len(pkg.Manifest))
	for _, item := range pkg.Manifest {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Href) == "" {
			continue
		}
		manifest[item.ID] = pathpkg.Clean(pathpkg.Join(opfDir, item.Href))
	}

	ordered := make([]string, 0, len(pkg.Spine))
	for _, item := range pkg.Spine {
		target := manifest[item.IDRef]
		if target == "" {
			continue
		}
		lower := strings.ToLower(target)
		if strings.HasSuffix(lower, ".xhtml") || strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") {
			ordered = append(ordered, target)
		}
	}
	return ordered
}

func readZIPFile(files []*zip.File, targetPath string) ([]byte, error) {
	for _, file := range files {
		if !strings.EqualFold(file.Name, targetPath) {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("zip entry not found: %s", targetPath)
}

func mergeOrderedEPUBNames(ordered, discovered []string) []string {
	remaining := make(map[string]string, len(discovered))
	for _, name := range discovered {
		remaining[strings.ToLower(name)] = name
	}

	merged := make([]string, 0, len(discovered))
	for _, name := range ordered {
		actual, ok := remaining[strings.ToLower(name)]
		if !ok {
			continue
		}
		merged = append(merged, actual)
		delete(remaining, strings.ToLower(name))
	}

	var leftovers []string
	for _, name := range remaining {
		leftovers = append(leftovers, name)
	}
	sort.Strings(leftovers)
	return append(merged, leftovers...)
}

func extractTextFromHTML(input string) string {
	if focused := extractFocusedHTMLText(input); focused != "" {
		return focused
	}

	tokenizer := xhtml.NewTokenizer(strings.NewReader(input))
	var builder strings.Builder
	skipDepth := 0

	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case xhtml.ErrorToken:
			if tokenizer.Err() == io.EOF {
				return normalizeHTMLText(builder.String())
			}
			return normalizeHTMLText(builder.String())
		case xhtml.StartTagToken:
			token := tokenizer.Token()
			switch token.Data {
			case "script", "style", "noscript":
				skipDepth++
			case "p", "div", "section", "article", "li", "br", "h1", "h2", "h3", "h4", "h5", "h6":
				builder.WriteString("\n")
			}
		case xhtml.EndTagToken:
			token := tokenizer.Token()
			switch token.Data {
			case "script", "style", "noscript":
				if skipDepth > 0 {
					skipDepth--
				}
			case "p", "div", "section", "article", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				builder.WriteString("\n")
			}
		case xhtml.TextToken:
			if skipDepth > 0 {
				continue
			}
			text := strings.TrimSpace(htmlstd.UnescapeString(string(tokenizer.Text())))
			if text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteString(" ")
			}
			builder.WriteString(text)
		}
	}
}

func extractFocusedHTMLText(input string) string {
	root, err := xhtml.Parse(strings.NewReader(input))
	if err != nil {
		return ""
	}

	target := findPreferredHTMLContentNode(root)
	if target == nil {
		return ""
	}

	var rendered bytes.Buffer
	if err := xhtml.Render(&rendered, target); err != nil {
		return ""
	}
	text := normalizeHTMLText(extractTokenizerText(rendered.String()))
	if len(text) < 280 {
		return ""
	}
	return text
}

func extractTokenizerText(input string) string {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(input))
	var builder strings.Builder
	skipDepth := 0

	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case xhtml.ErrorToken:
			if tokenizer.Err() == io.EOF {
				return builder.String()
			}
			return builder.String()
		case xhtml.StartTagToken:
			token := tokenizer.Token()
			switch token.Data {
			case "script", "style", "noscript":
				skipDepth++
			case "p", "div", "section", "article", "li", "br", "h1", "h2", "h3", "h4", "h5", "h6":
				builder.WriteString("\n")
			}
		case xhtml.EndTagToken:
			token := tokenizer.Token()
			switch token.Data {
			case "script", "style", "noscript":
				if skipDepth > 0 {
					skipDepth--
				}
			case "p", "div", "section", "article", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				builder.WriteString("\n")
			}
		case xhtml.TextToken:
			if skipDepth > 0 {
				continue
			}
			text := strings.TrimSpace(htmlstd.UnescapeString(string(tokenizer.Text())))
			if text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteString(" ")
			}
			builder.WriteString(text)
		}
	}
}

func findPreferredHTMLContentNode(root *xhtml.Node) *xhtml.Node {
	for _, selector := range []func(*xhtml.Node) bool{
		func(node *xhtml.Node) bool { return node.Type == xhtml.ElementNode && node.Data == "article" },
		func(node *xhtml.Node) bool { return node.Type == xhtml.ElementNode && node.Data == "main" },
		func(node *xhtml.Node) bool {
			return node.Type == xhtml.ElementNode && hasHTMLAttrValue(node, "role", "main")
		},
	} {
		if node := walkHTMLNode(root, selector); node != nil {
			return node
		}
	}
	return nil
}

func walkHTMLNode(node *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	if node == nil {
		return nil
	}
	if match(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := walkHTMLNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func hasHTMLAttrValue(node *xhtml.Node, key, expected string) bool {
	if node == nil {
		return false
	}
	expected = strings.ToLower(strings.TrimSpace(expected))
	for _, attr := range node.Attr {
		if strings.ToLower(strings.TrimSpace(attr.Key)) != key {
			continue
		}
		return strings.ToLower(strings.TrimSpace(attr.Val)) == expected
	}
	return false
}

func extractHTMLDocumentTitle(input string) string {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(input))
	inTitle := false

	for {
		switch tokenizer.Next() {
		case xhtml.ErrorToken:
			return ""
		case xhtml.StartTagToken:
			token := tokenizer.Token()
			if token.Data == "title" {
				inTitle = true
			}
		case xhtml.EndTagToken:
			token := tokenizer.Token()
			if token.Data == "title" {
				inTitle = false
			}
		case xhtml.TextToken:
			if !inTitle {
				continue
			}
			title := strings.Join(strings.Fields(htmlstd.UnescapeString(string(tokenizer.Text()))), " ")
			return strings.TrimSpace(title)
		}
	}
}

func looksLikeHTMLDocument(input string) bool {
	lower := strings.ToLower(input)
	for _, marker := range []string{"<html", "<body", "<article", "<main", "<title"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeFetchedContentType(headerValue, mediaType string) string {
	if strings.TrimSpace(headerValue) != "" {
		return strings.TrimSpace(headerValue)
	}
	if strings.TrimSpace(mediaType) != "" {
		return strings.TrimSpace(mediaType)
	}
	return "text/html"
}

func countURLPreviewParagraphs(content string) int {
	parts := strings.Split(strings.TrimSpace(content), "\n\n")
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func cleanURLPreviewContent(content string) string {
	lines := strings.Split(content, "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			clean = append(clean, "")
			continue
		}
		if shouldDropURLPreviewLine(line) {
			continue
		}
		clean = append(clean, line)
	}
	return normalizeContent(strings.Join(clean, "\n"))
}

func shouldDropURLPreviewLine(line string) bool {
	normalized := normalizeURLPreviewText(line)
	if normalized == "" {
		return false
	}

	exactDrops := map[string]struct{}{
		"admin": {},
		"点赞":    {},
		"微信扫一扫": {},
		"左青龙":   {},
		"右白虎":   {},
		"复制链接":  {},
		"移动安全":  {},
	}
	if _, ok := exactDrops[normalized]; ok {
		return true
	}

	prefixDrops := []string{
		"免责声明",
		"原文始发于微信公众号",
		"下方可添加作者微信",
	}
	for _, prefix := range prefixDrops {
		if strings.HasPrefix(normalized, normalizeURLPreviewText(prefix)) {
			return true
		}
	}

	if len([]rune(line)) <= 120 {
		metaHints := 0
		for _, token := range []string{"文章", "评论", "views", "阅读模式", "字数", "复制链接", "微信扫一扫"} {
			if strings.Contains(line, token) {
				metaHints++
			}
		}
		if metaHints >= 2 {
			return true
		}
	}
	if strings.Contains(line, "复制链接") {
		return true
	}
	if len([]rune(line)) <= 40 {
		hasDigit := false
		for _, r := range line {
			if r >= '0' && r <= '9' {
				hasDigit = true
				break
			}
		}
		if hasDigit {
			for _, token := range []string{"文章", "评论", "views"} {
				if strings.Contains(line, token) {
					return true
				}
			}
		}
	}

	return false
}

func evaluateURLPreviewQuality(title, content, htmlInput, finalURL string, paragraphs int) (int, []string, string) {
	if reason := detectBlockedURLPage(title, content, htmlInput); reason != "" {
		return 0, nil, reason
	}

	warnings := make([]string, 0, 3)
	score := 35
	if strings.TrimSpace(title) != "" {
		score += 10
	} else {
		warnings = append(warnings, "missing page title")
	}

	contentLength := len([]rune(content))
	switch {
	case contentLength >= 1600:
		score += 35
	case contentLength >= 900:
		score += 25
	case contentLength >= 500:
		score += 15
	case contentLength >= 220:
		// Concise writeups with a few dense paragraphs are still usable KB material.
		score += 15
	default:
		warnings = append(warnings, "content body is short")
	}

	switch {
	case paragraphs >= 5:
		score += 15
	case paragraphs >= 3:
		score += 10
	case paragraphs >= 2:
		score += 5
	default:
		warnings = append(warnings, "few content sections detected")
	}

	if looksLikeURLPreviewIndexPage(title, content, finalURL, paragraphs) {
		score -= 20
		warnings = append(warnings, "content resembles an index or navigation page")
	}

	if contentLength < 120 && paragraphs <= 1 {
		return 0, nil, "page content is too short to be a stable article preview"
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, uniqueLearnedStrings(warnings), ""
}

func detectBlockedURLPage(title, content, htmlInput string) string {
	titleNorm := normalizeURLPreviewText(title)
	contentNorm := normalizeURLPreviewText(content)
	htmlNorm := normalizeURLPreviewText(htmlInput)

	titleMarkers := []string{
		"just a moment",
		"attention required",
		"access denied",
		"security check",
		"captcha",
		"verify you are human",
		"安全验证",
		"访问受限",
	}
	for _, marker := range titleMarkers {
		if strings.Contains(titleNorm, normalizeURLPreviewText(marker)) {
			return "page appears to be a bot-check or access-control page"
		}
	}

	contentMarkers := []string{
		"enable javascript and cookies to continue",
		"please enable javascript",
		"verify you are human",
		"checking your browser before accessing",
		"complete the security check",
		"ray id",
		"cloudflare",
		"captcha",
		"ddos protection",
		"验证您是真人",
		"请开启 javascript",
	}

	hits := 0
	for _, marker := range contentMarkers {
		token := normalizeURLPreviewText(marker)
		if strings.Contains(contentNorm, token) || strings.Contains(htmlNorm, token) {
			hits++
		}
	}

	if hits >= 2 && len([]rune(content)) < 900 {
		return "page appears to be a bot-check or access-control page"
	}
	return ""
}

func looksLikeURLPreviewIndexPage(title, content, finalURL string, paragraphs int) bool {
	if paragraphs > 2 || len([]rune(content)) > 1200 {
		return false
	}

	combined := normalizeURLPreviewText(title + "\n" + content + "\n" + finalURL)
	navTokens := []string{
		"home", "archive", "archives", "category", "categories", "tag", "tags", "search",
		"menu", "login", "register", "related", "previous", "next", "comments",
		"首页", "归档", "分类", "标签", "搜索", "菜单", "上一篇", "下一篇", "评论",
	}

	hits := 0
	for _, token := range navTokens {
		if strings.Contains(combined, normalizeURLPreviewText(token)) {
			hits++
		}
	}
	return hits >= 4
}

func normalizeURLPreviewText(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	replacer := strings.NewReplacer("\n", " ", "\t", " ", "_", " ", "-", " ", "/", " ", ":", " ")
	input = replacer.Replace(input)
	return strings.Join(strings.Fields(input), " ")
}

// RenderURLPreviewMarkdown converts a fetched webpage preview into a reviewable markdown file.
func RenderURLPreviewMarkdown(preview *URLFetchPreview) string {
	if preview == nil {
		return ""
	}

	manifest := buildURLPreviewManifest(preview)
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		manifestJSON = []byte("{}")
	}

	var builder strings.Builder
	title := strings.TrimSpace(preview.Title)
	if title == "" {
		title = "Fetched Article"
	}

	builder.WriteString("# ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	builder.WriteString(urlPreviewMetaBeginMarker)
	builder.WriteString("\n")
	builder.Write(manifestJSON)
	builder.WriteString("\n")
	builder.WriteString(urlPreviewMetaEndMarker)
	builder.WriteString("\n\n")
	builder.WriteString("- Source URL: ")
	builder.WriteString(strings.TrimSpace(manifest.SourceURL))
	builder.WriteString("\n")
	if finalURL := strings.TrimSpace(manifest.FinalURL); finalURL != "" && finalURL != strings.TrimSpace(manifest.SourceURL) {
		builder.WriteString("- Final URL: ")
		builder.WriteString(finalURL)
		builder.WriteString("\n")
	}
	if fetchedAt := strings.TrimSpace(manifest.FetchedAt); fetchedAt != "" {
		builder.WriteString("- Fetched At: ")
		builder.WriteString(fetchedAt)
		builder.WriteString("\n")
	}
	if contentType := strings.TrimSpace(manifest.ContentType); contentType != "" {
		builder.WriteString("- Content Type: ")
		builder.WriteString(contentType)
		builder.WriteString("\n")
	}
	if manifest.Paragraphs > 0 {
		builder.WriteString("- Paragraphs: ")
		builder.WriteString(fmt.Sprintf("%d", manifest.Paragraphs))
		builder.WriteString("\n")
	}
	if manifest.QualityScore > 0 {
		builder.WriteString("- Quality Score: ")
		builder.WriteString(fmt.Sprintf("%d", manifest.QualityScore))
		builder.WriteString("\n")
	}
	if len(manifest.Warnings) > 0 {
		builder.WriteString("- Warnings: ")
		builder.WriteString(strings.Join(manifest.Warnings, "; "))
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Suggested Metadata\n\n")
	if sampleType := strings.TrimSpace(manifest.SuggestedSampleType); sampleType != "" {
		builder.WriteString("- Sample Type: ")
		builder.WriteString(sampleType)
		builder.WriteString("\n")
	}
	if manifest.SuggestedSourceConfidence > 0 {
		builder.WriteString("- Source Confidence: ")
		builder.WriteString(fmt.Sprintf("%.2f", manifest.SuggestedSourceConfidence))
		builder.WriteString("\n")
	}
	if len(manifest.SuggestedLabels) > 0 {
		builder.WriteString("- Labels: ")
		builder.WriteString(strings.Join(manifest.SuggestedLabels, ", "))
		builder.WriteString("\n")
	}
	if len(manifest.SuggestedTargetTypes) > 0 {
		builder.WriteString("- Target Types: ")
		builder.WriteString(strings.Join(manifest.SuggestedTargetTypes, ", "))
		builder.WriteString("\n")
	}
	builder.WriteString("\n## Article\n\n")
	builder.WriteString(urlPreviewArticleBeginMarker)
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(preview.Content))
	builder.WriteString("\n")
	builder.WriteString(urlPreviewArticleEndMarker)
	builder.WriteString("\n")
	return builder.String()
}

func buildURLPreviewManifest(preview *URLFetchPreview) urlPreviewManifest {
	suggestion := resolveURLPreviewSuggestion(preview)
	return urlPreviewManifest{
		Kind:                      urlPreviewFormatKind,
		Version:                   urlPreviewFormatVersion,
		Title:                     strings.TrimSpace(preview.Title),
		SourceURL:                 strings.TrimSpace(preview.URL),
		FinalURL:                  strings.TrimSpace(preview.FinalURL),
		FetchedAt:                 strings.TrimSpace(preview.FetchedAt),
		ContentType:               strings.TrimSpace(preview.ContentType),
		Paragraphs:                preview.Paragraphs,
		QualityScore:              preview.QualityScore,
		Warnings:                  append([]string(nil), preview.Warnings...),
		SuggestedSampleType:       suggestion.SampleType,
		SuggestedSourceConfidence: suggestion.SourceConfidence,
		SuggestedLabels:           append([]string(nil), suggestion.Labels...),
		SuggestedTargetTypes:      append([]string(nil), suggestion.TargetTypes...),
	}
}

func resolveURLPreviewSuggestion(preview *URLFetchPreview) urlPreviewSuggestion {
	base := suggestURLPreviewMetadata(preview)
	if preview == nil {
		return base
	}

	resolved := urlPreviewSuggestion{
		SampleType:       strings.TrimSpace(preview.SuggestedSampleType),
		SourceConfidence: clampURLPreviewConfidence(preview.SuggestedSourceConfidence),
		Labels:           normalizeURLPreviewSuggestionTokens(preview.SuggestedLabels...),
		TargetTypes:      normalizeURLPreviewSuggestionTokens(preview.SuggestedTargetTypes...),
	}
	if resolved.SampleType == "" {
		resolved.SampleType = base.SampleType
	}
	if resolved.SourceConfidence == 0 {
		resolved.SourceConfidence = base.SourceConfidence
	}
	if len(resolved.Labels) == 0 {
		resolved.Labels = base.Labels
	}
	if len(resolved.TargetTypes) == 0 {
		resolved.TargetTypes = base.TargetTypes
	}
	return resolved
}

func suggestURLPreviewMetadata(preview *URLFetchPreview) urlPreviewSuggestion {
	suggestion := urlPreviewSuggestion{
		SampleType: "public-reference",
		Labels:     []string{"url-preview", "external-reference", "web-article"},
		TargetTypes: []string{
			"url",
			"web",
		},
	}
	if preview == nil {
		suggestion.SourceConfidence = 0.55
		return suggestion
	}

	combined := strings.ToLower(strings.Join([]string{
		strings.TrimSpace(preview.Title),
		strings.TrimSpace(preview.Content),
		strings.TrimSpace(preview.FinalURL),
		strings.TrimSpace(preview.URL),
	}, "\n"))

	securitySignal := urlPreviewContainsAny(combined,
		"vulnerability", "漏洞", "渗透", "security", "攻防", "poc", "writeup", "exploit", "cve", "bypass", "复现",
	)
	if securitySignal {
		suggestion.Labels = append(suggestion.Labels, "security-reference")
	}
	if urlPreviewContainsAny(combined, "writeup", "渗透", "复现", "实战", "poc", "walkthrough") {
		suggestion.Labels = append(suggestion.Labels, "pentest-writeup")
	}
	if urlPreviewContainsAny(combined, "auth", "authentication", "登录", "认证", "jwt", "token", "session", "oauth", "sso") {
		suggestion.Labels = append(suggestion.Labels, "auth")
	}
	if urlPreviewContainsAny(combined, "access control", "privilege", "权限", "越权", "admin", "idor") {
		suggestion.Labels = append(suggestion.Labels, "access-control")
	}
	if urlPreviewContainsAny(combined, "sqli", "sql injection", "sql注入", "联合注入", "布尔注入") {
		suggestion.Labels = append(suggestion.Labels, "sqli")
	}
	if urlPreviewContainsAny(combined, "xss", "cross site scripting", "跨站") {
		suggestion.Labels = append(suggestion.Labels, "xss")
	}
	if urlPreviewContainsAny(combined, "ssrf", "服务端请求伪造") {
		suggestion.Labels = append(suggestion.Labels, "ssrf")
	}
	if urlPreviewContainsAny(combined, "rce", "remote code execution", "命令执行", "代码执行", "远程执行") {
		suggestion.Labels = append(suggestion.Labels, "rce")
	}
	if urlPreviewContainsAny(combined, "waf", "绕过", "bypass") {
		suggestion.Labels = append(suggestion.Labels, "waf-bypass")
	}
	if urlPreviewContainsAny(combined, "upload", "文件上传") {
		suggestion.Labels = append(suggestion.Labels, "file-upload")
	}
	if urlPreviewContainsAny(combined, "lfi", "path traversal", "目录遍历", "文件包含") {
		suggestion.Labels = append(suggestion.Labels, "path-traversal")
	}

	if urlPreviewContainsAny(combined, "app", "android", "ios", "apk", "ipa", "小程序") {
		suggestion.TargetTypes = append(suggestion.TargetTypes, "app")
		suggestion.Labels = append(suggestion.Labels, "app-security")
	}
	if urlPreviewContainsAny(combined, "api", "graphql", "rest") {
		suggestion.TargetTypes = append(suggestion.TargetTypes, "api")
	}

	quality := preview.QualityScore
	if quality < 0 {
		quality = 0
	}
	if quality > 100 {
		quality = 100
	}

	confidence := 0.42 + (float64(quality) / 350.0)
	if preview.Paragraphs >= 6 {
		confidence += 0.04
	}
	if preview.Paragraphs >= 12 {
		confidence += 0.04
	}
	if securitySignal {
		confidence += 0.03
	}
	confidence -= float64(len(preview.Warnings)) * 0.04
	if confidence < 0.35 {
		confidence = 0.35
	}
	suggestion.SourceConfidence = clampURLPreviewConfidence(confidence)
	suggestion.Labels = normalizeURLPreviewSuggestionTokens(suggestion.Labels...)
	suggestion.TargetTypes = normalizeURLPreviewSuggestionTokens(suggestion.TargetTypes...)
	return suggestion
}

func parseURLPreviewMarkdown(markdown string) (*parsedURLPreview, error) {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return nil, fmt.Errorf("preview file is empty")
	}

	var manifest urlPreviewManifest
	if metaJSON, ok := extractURLPreviewMetaJSON(markdown); ok {
		if err := json.Unmarshal([]byte(metaJSON), &manifest); err != nil {
			return nil, fmt.Errorf("failed to parse preview metadata: %w", err)
		}
		if strings.TrimSpace(manifest.Kind) != "" && strings.TrimSpace(manifest.Kind) != urlPreviewFormatKind {
			return nil, fmt.Errorf("unsupported preview kind: %s", manifest.Kind)
		}
	}

	title := extractURLPreviewTitle(markdown)
	if title == "" {
		title = strings.TrimSpace(manifest.Title)
	}

	readVisibleURLPreviewMetadata(markdown, &manifest)
	applyURLPreviewSuggestedOverrides(markdown, &manifest)

	content, ok := extractURLPreviewArticleContent(markdown)
	if !ok {
		return nil, fmt.Errorf("not a generated osmedeus url preview: missing article section")
	}
	content = normalizeContent(content)
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("preview article content is empty")
	}

	if strings.TrimSpace(manifest.SourceURL) == "" {
		return nil, fmt.Errorf("not a generated osmedeus url preview: missing source url")
	}
	if strings.TrimSpace(title) == "" {
		title = strings.TrimSpace(manifest.SourceURL)
	}

	manifest.SourceURL = strings.TrimSpace(manifest.SourceURL)
	manifest.FinalURL = strings.TrimSpace(manifest.FinalURL)
	manifest.Title = title
	manifest.SuggestedSampleType = strings.TrimSpace(manifest.SuggestedSampleType)
	manifest.SuggestedLabels = normalizeURLPreviewSuggestionTokens(manifest.SuggestedLabels...)
	manifest.SuggestedTargetTypes = normalizeURLPreviewSuggestionTokens(manifest.SuggestedTargetTypes...)
	manifest.SuggestedSourceConfidence = clampURLPreviewConfidence(manifest.SuggestedSourceConfidence)

	suggestion := resolveURLPreviewSuggestion(&URLFetchPreview{
		URL:                       manifest.SourceURL,
		FinalURL:                  manifest.FinalURL,
		Title:                     title,
		Content:                   content,
		ContentType:               manifest.ContentType,
		FetchedAt:                 manifest.FetchedAt,
		Paragraphs:                manifest.Paragraphs,
		QualityScore:              manifest.QualityScore,
		Warnings:                  append([]string(nil), manifest.Warnings...),
		SuggestedSampleType:       manifest.SuggestedSampleType,
		SuggestedSourceConfidence: manifest.SuggestedSourceConfidence,
		SuggestedLabels:           append([]string(nil), manifest.SuggestedLabels...),
		SuggestedTargetTypes:      append([]string(nil), manifest.SuggestedTargetTypes...),
	})
	manifest.SuggestedSampleType = suggestion.SampleType
	manifest.SuggestedSourceConfidence = suggestion.SourceConfidence
	manifest.SuggestedLabels = suggestion.Labels
	manifest.SuggestedTargetTypes = suggestion.TargetTypes

	return &parsedURLPreview{
		Title:    title,
		Content:  content,
		Manifest: manifest,
	}, nil
}

func extractURLPreviewMetaJSON(markdown string) (string, bool) {
	start := strings.Index(markdown, urlPreviewMetaBeginMarker)
	if start < 0 {
		return "", false
	}
	start += len(urlPreviewMetaBeginMarker)
	end := strings.Index(markdown[start:], urlPreviewMetaEndMarker)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(markdown[start : start+end]), true
}

func extractURLPreviewTitle(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}

func readVisibleURLPreviewMetadata(markdown string, manifest *urlPreviewManifest) {
	if manifest == nil {
		return
	}

	lines := strings.Split(markdown, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "## Suggested Metadata" || line == "## Article" {
			break
		}
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		key, value, ok := parseURLPreviewKeyValueLine(line)
		if !ok {
			continue
		}
		switch key {
		case "source url":
			if manifest.SourceURL == "" {
				manifest.SourceURL = value
			}
		case "final url":
			if manifest.FinalURL == "" {
				manifest.FinalURL = value
			}
		case "fetched at":
			if manifest.FetchedAt == "" {
				manifest.FetchedAt = value
			}
		case "content type":
			if manifest.ContentType == "" {
				manifest.ContentType = value
			}
		case "paragraphs":
			if manifest.Paragraphs == 0 {
				manifest.Paragraphs = parseURLPreviewInt(value)
			}
		case "quality score":
			if manifest.QualityScore == 0 {
				manifest.QualityScore = parseURLPreviewInt(value)
			}
		case "warnings":
			if len(manifest.Warnings) == 0 {
				manifest.Warnings = splitURLPreviewWarnings(value)
			}
		}
	}
}

func applyURLPreviewSuggestedOverrides(markdown string, manifest *urlPreviewManifest) {
	if manifest == nil {
		return
	}
	section := extractURLPreviewSuggestedSection(markdown)
	if strings.TrimSpace(section) == "" {
		return
	}

	for _, line := range strings.Split(section, "\n") {
		key, value, ok := parseURLPreviewKeyValueLine(strings.TrimSpace(line))
		if !ok {
			continue
		}
		switch key {
		case "sample type":
			if strings.TrimSpace(value) != "" {
				manifest.SuggestedSampleType = strings.TrimSpace(value)
			}
		case "source confidence":
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				manifest.SuggestedSourceConfidence = clampURLPreviewConfidence(parsed)
			}
		case "labels":
			manifest.SuggestedLabels = normalizeURLPreviewSuggestionTokens(splitURLPreviewCSV(value)...)
		case "target types":
			manifest.SuggestedTargetTypes = normalizeURLPreviewSuggestionTokens(splitURLPreviewCSV(value)...)
		}
	}
}

func extractURLPreviewSuggestedSection(markdown string) string {
	lines := strings.Split(markdown, "\n")
	inSection := false
	var collected []string
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "## Suggested Metadata" {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(line, "## ") {
			break
		}
		if inSection {
			collected = append(collected, rawLine)
		}
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

func extractURLPreviewArticleContent(markdown string) (string, bool) {
	if content, ok := extractURLPreviewArticleContentFromMarkers(markdown); ok {
		return content, true
	}
	return extractURLPreviewLegacyArticleContent(markdown)
}

func extractURLPreviewArticleContentFromMarkers(markdown string) (string, bool) {
	start := strings.Index(markdown, urlPreviewArticleBeginMarker)
	if start < 0 {
		return "", false
	}
	start += len(urlPreviewArticleBeginMarker)
	end := strings.Index(markdown[start:], urlPreviewArticleEndMarker)
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(markdown[start : start+end]), true
}

func extractURLPreviewLegacyArticleContent(markdown string) (string, bool) {
	lines := strings.Split(markdown, "\n")
	inArticle := false
	var collected []string
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "## Article" {
			inArticle = true
			continue
		}
		if inArticle {
			collected = append(collected, rawLine)
		}
	}
	if !inArticle {
		return "", false
	}
	return strings.TrimSpace(strings.Join(collected, "\n")), true
}

func parseURLPreviewKeyValueLine(line string) (string, string, bool) {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
	if line == "" {
		return "", "", false
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1]), true
}

func parseURLPreviewInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func splitURLPreviewWarnings(value string) []string {
	parts := strings.Split(strings.TrimSpace(value), ";")
	results := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item != "" {
			results = append(results, item)
		}
	}
	return results
}

func splitURLPreviewCSV(value string) []string {
	parts := strings.Split(strings.TrimSpace(value), ",")
	results := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item != "" {
			results = append(results, item)
		}
	}
	return results
}

func urlPreviewContainsAny(input string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(input, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func normalizeURLPreviewSuggestionTokens(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	results := make([]string, 0, len(values))
	for _, value := range values {
		token := normalizeURLPreviewSuggestionToken(value)
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		results = append(results, token)
	}
	return results
}

func normalizeURLPreviewSuggestionToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func clampURLPreviewConfidence(value float64) float64 {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return float64(int((value*100)+0.5)) / 100
}

func buildURLPreviewRetrievalFingerprint(sourcePath, title, content string) string {
	return hashString(strings.Join([]string{
		"url-preview",
		normalizeURLPreviewText(sourcePath),
		normalizeURLPreviewText(title),
		hashString(normalizeContent(content)),
	}, "::"))
}

func normalizeHTMLText(input string) string {
	lines := strings.Split(input, "\n")
	var clean []string
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		line = strings.TrimSpace(line)
		if line != "" {
			clean = append(clean, line)
		}
	}
	return strings.TrimSpace(strings.Join(clean, "\n\n"))
}

// Search proxies the stored keyword search and keeps the internal package as the main entrypoint.
func Search(ctx context.Context, workspace, query string, limit int) ([]database.KnowledgeSearchHit, error) {
	return SearchWithOptions(ctx, SearchOptions{
		Workspace: strings.TrimSpace(workspace),
		Query:     query,
		Limit:     limit,
	})
}

// SearchWithOptions proxies layered search to the database-backed implementation.
func SearchWithOptions(ctx context.Context, opts SearchOptions) ([]database.KnowledgeSearchHit, error) {
	return database.SearchKnowledgeWithOptions(ctx, database.KnowledgeSearchOptions{
		Workspace:           strings.TrimSpace(opts.Workspace),
		WorkspaceLayers:     opts.WorkspaceLayers,
		ScopeLayers:         opts.ScopeLayers,
		Query:               opts.Query,
		Limit:               opts.Limit,
		ExactID:             opts.ExactID,
		MinSourceConfidence: opts.MinSourceConfidence,
		SampleTypes:         opts.SampleTypes,
		ExcludeSampleTypes:  opts.ExcludeSampleTypes,
	})
}

// ListDocuments returns paginated knowledge documents.
func ListDocuments(ctx context.Context, workspace string, offset, limit int) (*database.KnowledgeDocumentResult, error) {
	return database.ListKnowledgeDocuments(ctx, database.KnowledgeDocumentQuery{
		Workspace: strings.TrimSpace(workspace),
		Offset:    offset,
		Limit:     limit,
	})
}
