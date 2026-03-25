package knowledge

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	htmlstd "html"
	"io"
	"io/fs"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	xhtml "golang.org/x/net/html"
)

const (
	maxChunkChars = 1200
	minChunkChars = 120
)

// IngestSummary reports the outcome of a knowledge import job.
type IngestSummary struct {
	Workspace string   `json:"workspace"`
	RootPath  string   `json:"root_path"`
	Documents int      `json:"documents"`
	Chunks    int      `json:"chunks"`
	Skipped   int      `json:"skipped"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
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

// SearchOptions controls layered knowledge retrieval.
type SearchOptions struct {
	Workspace       string   `json:"workspace,omitempty"`
	WorkspaceLayers []string `json:"workspace_layers,omitempty"`
	ScopeLayers     []string `json:"scope_layers,omitempty"`
	Query           string   `json:"query"`
	Limit           int      `json:"limit,omitempty"`
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

	chunks := chunkContent(content)
	if len(chunks) == 0 {
		summary.Skipped++
		return fmt.Errorf("%s: no searchable chunks generated", path)
	}

	metadataJSON := marshalMetadata(doc.Metadata)
	contentHash := hashString(content)

	record := &database.KnowledgeDocument{
		Workspace:   summary.Workspace,
		SourcePath:  path,
		SourceType:  "file",
		DocType:     doc.DocType,
		Title:       doc.Title,
		ContentHash: contentHash,
		Status:      "ready",
		ChunkCount:  len(chunks),
		TotalBytes:  int64(len(content)),
		Metadata:    metadataJSON,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	chunkRows := make([]database.KnowledgeChunk, 0, len(chunks))
	for i, chunk := range chunks {
		chunkMeta := map[string]interface{}{
			"source_path":   path,
			"section":       chunk.Section,
			"chunk_index":   i,
			"document_type": doc.DocType,
		}
		chunkRows = append(chunkRows, database.KnowledgeChunk{
			Workspace:   summary.Workspace,
			ChunkIndex:  i,
			Section:     chunk.Section,
			Content:     chunk.Content,
			ContentHash: hashString(chunk.Content),
			Metadata:    marshalMetadata(chunkMeta),
			CreatedAt:   time.Now(),
		})
	}

	if err := database.UpsertKnowledgeDocument(ctx, record, chunkRows); err != nil {
		return fmt.Errorf("%s: failed to save document: %w", path, err)
	}

	summary.Documents++
	summary.Chunks += len(chunkRows)
	return nil
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
		Workspace:       strings.TrimSpace(opts.Workspace),
		WorkspaceLayers: opts.WorkspaceLayers,
		ScopeLayers:     opts.ScopeLayers,
		Query:           opts.Query,
		Limit:           opts.Limit,
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
