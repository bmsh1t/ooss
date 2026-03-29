package knowledge

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

const (
	yakitRAGMagic          = "YAKRAG"
	yakitRAGInspectBytes   = 256 * 1024
	yakitDefaultPluginDB   = "yakit-profile-plugin.db"
	yakitDefaultProjectDB  = "default-yakit.db"
	yakitBridgeFormatJSONL = "jsonl"
	yakitBridgeFormatMD    = "md"
)

var (
	yakitUUIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	yakitSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)+$`)
)

// YakitRAGPackageInfo describes the minimal metadata extracted from a Yakit RAG package header.
type YakitRAGPackageInfo struct {
	PackagePath     string `json:"package_path"`
	PackageUUID     string `json:"package_uuid,omitempty"`
	CollectionUUID  string `json:"collection_uuid,omitempty"`
	Slug            string `json:"slug,omitempty"`
	Description     string `json:"description,omitempty"`
	ModelName       string `json:"model_name,omitempty"`
	Compressed      bool   `json:"compressed"`
	InspectedHeader bool   `json:"inspected_header"`
}

// YakitRAGBridgeOptions controls exporting an imported Yakit KB into open text formats.
type YakitRAGBridgeOptions struct {
	PackagePath       string `json:"package_path,omitempty"`
	YakitDBPath       string `json:"yakit_db_path,omitempty"`
	KnowledgeBaseName string `json:"knowledge_base_name,omitempty"`
	OutputPath        string `json:"output_path"`
	Format            string `json:"format,omitempty"`
}

// YakitRAGBridgeSummary reports the result of a Yakit KB bridge export.
type YakitRAGBridgeSummary struct {
	PackagePath       string `json:"package_path,omitempty"`
	YakitDBPath       string `json:"yakit_db_path"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
	OutputPath        string `json:"output_path"`
	Format            string `json:"format"`
	Entries           int    `json:"entries"`
	CollectionUUID    string `json:"collection_uuid,omitempty"`
	ModelName         string `json:"model_name,omitempty"`
	Slug              string `json:"slug,omitempty"`
	ResolvedBy        string `json:"resolved_by,omitempty"`
}

type yakitKnowledgeBaseRecord struct {
	ID               int64
	Name             string
	Type             string
	RAGID            string
	SerialVersionUID string
	CollectionUUID   string
	CollectionName   string
	ModelName        string
	ExportedAt       string
}

type yakitKnowledgeEntryRecord struct {
	ID                 int64
	Title              string
	KnowledgeType      string
	ImportanceScore    int
	Keywords           string
	Summary            string
	Details            string
	PotentialQuestions string
	RelatedEntities    string
	SourcePage         int
	HiddenIndex        string
	HasQuestionIndex   bool
}

type yakitKnowledgeJSONLRecord struct {
	KnowledgeBaseName string   `json:"knowledge_base_name"`
	KnowledgeBaseType string   `json:"knowledge_base_type,omitempty"`
	KnowledgeTitle    string   `json:"knowledge_title"`
	KnowledgeType     string   `json:"knowledge_type"`
	ImportanceScore   int      `json:"importance_score,omitempty"`
	Keywords          []string `json:"keywords,omitempty"`
	Questions         []string `json:"questions,omitempty"`
	Summary           string   `json:"summary,omitempty"`
	Details           string   `json:"details,omitempty"`
	Content           string   `json:"content"`
	SourcePage        int      `json:"source_page,omitempty"`
	RelatedEntities   []string `json:"related_entities,omitempty"`
	HasQuestionIndex  bool     `json:"has_question_index,omitempty"`
	Bridge            struct {
		Kind           string `json:"kind"`
		PackagePath    string `json:"package_path,omitempty"`
		YakitDBPath    string `json:"yakit_db_path"`
		CollectionUUID string `json:"collection_uuid,omitempty"`
		ModelName      string `json:"model_name,omitempty"`
		Slug           string `json:"slug,omitempty"`
		ExportedAt     string `json:"exported_at,omitempty"`
	} `json:"bridge"`
}

// BridgeYakitRAG exports a previously imported Yakit knowledge base into jsonl or markdown.
func BridgeYakitRAG(ctx context.Context, opts YakitRAGBridgeOptions) (*YakitRAGBridgeSummary, error) {
	outputPath := strings.TrimSpace(opts.OutputPath)
	if outputPath == "" {
		return nil, fmt.Errorf("output path is required")
	}

	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve output path: %w", err)
	}

	format := normalizeYakitBridgeFormat(opts.Format, absOutput)
	if format == "" {
		return nil, fmt.Errorf("unsupported format: %s", strings.TrimSpace(opts.Format))
	}

	var pkg *YakitRAGPackageInfo
	if strings.TrimSpace(opts.PackagePath) != "" {
		pkg, err = InspectYakitRAGPackage(opts.PackagePath)
		if err != nil {
			return nil, err
		}
	}

	dbPath, err := resolveYakitKnowledgeDB(opts.YakitDBPath)
	if err != nil {
		return nil, err
	}

	db, err := openYakitBridgeDB(dbPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	record, resolvedBy, err := lookupYakitKnowledgeBase(ctx, db, strings.TrimSpace(opts.KnowledgeBaseName), pkg)
	if err != nil {
		return nil, err
	}

	entries, err := loadYakitKnowledgeEntries(ctx, db, record.ID)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("knowledge base %q has no exportable entries", record.Name)
	}

	if err := os.MkdirAll(filepath.Dir(absOutput), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	switch format {
	case yakitBridgeFormatJSONL:
		err = writeYakitBridgeJSONL(absOutput, *record, entries, pkg, dbPath)
	case yakitBridgeFormatMD:
		err = writeYakitBridgeMarkdown(absOutput, *record, entries, pkg, dbPath)
	default:
		err = fmt.Errorf("unsupported format: %s", format)
	}
	if err != nil {
		return nil, err
	}

	summary := &YakitRAGBridgeSummary{
		YakitDBPath:       dbPath,
		KnowledgeBaseName: record.Name,
		OutputPath:        absOutput,
		Format:            format,
		Entries:           len(entries),
		CollectionUUID:    record.CollectionUUID,
		ModelName:         firstNonEmpty(record.ModelName, packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.ModelName })),
		ResolvedBy:        resolvedBy,
	}
	if pkg != nil {
		summary.PackagePath = pkg.PackagePath
		summary.Slug = pkg.Slug
	}

	return summary, nil
}

// InspectYakitRAGPackage extracts lightweight metadata from a Yakit .rag or .rag.gz package.
func InspectYakitRAGPackage(path string) (*YakitRAGPackageInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("package path is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve package path: %w", err)
	}

	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open package: %w", err)
	}
	defer func() { _ = file.Close() }()

	header := make([]byte, 2)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read package header: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to rewind package: %w", err)
	}

	var reader io.Reader = file
	compressed := n == 2 && header[0] == 0x1f && header[1] == 0x8b
	if compressed {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to open gzip package: %w", err)
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}

	buffer := make([]byte, yakitRAGInspectBytes)
	readBytes, err := io.ReadFull(reader, buffer)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("failed to inspect package: %w", err)
	}
	buffer = buffer[:readBytes]
	if len(buffer) < len(yakitRAGMagic) || string(buffer[:len(yakitRAGMagic)]) != yakitRAGMagic {
		return nil, fmt.Errorf("%s is not a Yakit YAKRAG package", absPath)
	}

	tokens := extractYakitPrintableTokens(buffer)
	info := &YakitRAGPackageInfo{
		PackagePath:     absPath,
		Compressed:      compressed,
		InspectedHeader: true,
	}

	uuids := make([]string, 0, 4)
	for _, token := range tokens {
		clean := strings.TrimSpace(strings.TrimPrefix(token, "$"))
		switch {
		case clean == "":
			continue
		case yakitUUIDPattern.MatchString(clean):
			uuids = append(uuids, clean)
		case info.ModelName == "" && looksLikeEmbeddingModel(clean):
			info.ModelName = clean
		case info.Slug == "" && looksLikeYakitSlug(clean):
			info.Slug = clean
		case info.Description == "" && !strings.HasPrefix(clean, ".") && clean != yakitRAGMagic:
			info.Description = clean
		}

		if strings.TrimSpace(token) == ".YAKHNSW" {
			break
		}
	}

	if len(uuids) > 0 {
		info.PackageUUID = uuids[0]
	}
	if len(uuids) > 1 {
		info.CollectionUUID = uuids[1]
	}

	return info, nil
}

func resolveYakitKnowledgeDB(explicitPath string) (string, error) {
	if trimmed := strings.TrimSpace(explicitPath); trimmed != "" {
		absPath, err := filepath.Abs(trimmed)
		if err != nil {
			return "", fmt.Errorf("failed to resolve Yakit DB path: %w", err)
		}
		if err := validateYakitKnowledgeDB(absPath); err != nil {
			return "", err
		}
		return absPath, nil
	}

	candidates := make([]string, 0, 8)
	for _, envName := range []string{"YAKIT_PLUGIN_DB_PATH", "YAKIT_DB_PATH"} {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			candidates = append(candidates, value)
		}
	}

	if yakitHome := strings.TrimSpace(os.Getenv("YAKIT_HOME")); yakitHome != "" {
		candidates = append(candidates,
			filepath.Join(yakitHome, yakitDefaultPluginDB),
			filepath.Join(yakitHome, yakitDefaultProjectDB),
		)
	}

	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates,
			filepath.Join(home, "yakit-projects", yakitDefaultPluginDB),
			filepath.Join(home, "yakit-projects", yakitDefaultProjectDB),
		)
	}

	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		absPath, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}
		if err := validateYakitKnowledgeDB(absPath); err == nil {
			return absPath, nil
		}
	}

	return "", fmt.Errorf("unable to locate a Yakit knowledge database; set --db or YAKIT_PLUGIN_DB_PATH")
}

func validateYakitKnowledgeDB(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to access Yakit DB %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("Yakit DB path is a directory: %s", path)
	}

	db, err := openYakitBridgeDB(path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	var count int
	row := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rag_knowledge_base_v1'`)
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("failed to inspect Yakit DB %s: %w", path, err)
	}
	if count == 0 {
		return fmt.Errorf("Yakit DB %s does not contain rag_knowledge_base_v1", path)
	}
	return nil
}

func openYakitBridgeDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", filepath.ToSlash(path))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open Yakit DB %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to read Yakit DB %s: %w", path, err)
	}
	return db, nil
}

func lookupYakitKnowledgeBase(ctx context.Context, db *sql.DB, explicitName string, pkg *YakitRAGPackageInfo) (*yakitKnowledgeBaseRecord, string, error) {
	if strings.TrimSpace(explicitName) != "" {
		record, err := queryYakitKnowledgeBaseByName(ctx, db, explicitName)
		if err != nil {
			return nil, "", err
		}
		return record, "knowledge_base_name", nil
	}

	if pkg != nil && strings.TrimSpace(pkg.CollectionUUID) != "" {
		record, err := queryYakitKnowledgeBaseByCollectionUUID(ctx, db, pkg.CollectionUUID)
		if err == nil {
			return record, "collection_uuid", nil
		}
	}

	return nil, "", fmt.Errorf("unable to resolve Yakit knowledge base; provide --kb-name or a package whose collection UUID exists in the Yakit DB")
}

func queryYakitKnowledgeBaseByName(ctx context.Context, db *sql.DB, name string) (*yakitKnowledgeBaseRecord, error) {
	const query = `
SELECT
	kb.id,
	kb.knowledge_base_name,
	kb.knowledge_base_type,
	COALESCE(kb.rag_id, ''),
	COALESCE(kb.serial_version_uid, ''),
	COALESCE(vc.uuid, ''),
	COALESCE(vc.name, ''),
	COALESCE(vc.model_name, ''),
	COALESCE(vc.exported_at, '')
FROM rag_knowledge_base_v1 kb
LEFT JOIN rag_vector_collection_v1 vc ON vc.rag_id = kb.rag_id
WHERE lower(kb.knowledge_base_name) = lower(?)
ORDER BY kb.id
LIMIT 1
`

	record := &yakitKnowledgeBaseRecord{}
	err := db.QueryRowContext(ctx, query, strings.TrimSpace(name)).Scan(
		&record.ID,
		&record.Name,
		&record.Type,
		&record.RAGID,
		&record.SerialVersionUID,
		&record.CollectionUUID,
		&record.CollectionName,
		&record.ModelName,
		&record.ExportedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("knowledge base %q was not found in the Yakit DB", strings.TrimSpace(name))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query knowledge base %q: %w", strings.TrimSpace(name), err)
	}
	return record, nil
}

func queryYakitKnowledgeBaseByCollectionUUID(ctx context.Context, db *sql.DB, collectionUUID string) (*yakitKnowledgeBaseRecord, error) {
	const query = `
SELECT
	kb.id,
	kb.knowledge_base_name,
	kb.knowledge_base_type,
	COALESCE(kb.rag_id, ''),
	COALESCE(kb.serial_version_uid, ''),
	COALESCE(vc.uuid, ''),
	COALESCE(vc.name, ''),
	COALESCE(vc.model_name, ''),
	COALESCE(vc.exported_at, '')
FROM rag_knowledge_base_v1 kb
JOIN rag_vector_collection_v1 vc ON vc.rag_id = kb.rag_id
WHERE lower(vc.uuid) = lower(?)
ORDER BY kb.id
LIMIT 1
`

	record := &yakitKnowledgeBaseRecord{}
	err := db.QueryRowContext(ctx, query, strings.TrimSpace(collectionUUID)).Scan(
		&record.ID,
		&record.Name,
		&record.Type,
		&record.RAGID,
		&record.SerialVersionUID,
		&record.CollectionUUID,
		&record.CollectionName,
		&record.ModelName,
		&record.ExportedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("collection UUID %q was not found in the Yakit DB", strings.TrimSpace(collectionUUID))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query collection UUID %q: %w", strings.TrimSpace(collectionUUID), err)
	}
	return record, nil
}

func loadYakitKnowledgeEntries(ctx context.Context, db *sql.DB, knowledgeBaseID int64) ([]yakitKnowledgeEntryRecord, error) {
	const query = `
SELECT
	e.id,
	COALESCE(e.knowledge_title, ''),
	COALESCE(e.knowledge_type, ''),
	COALESCE(e.importance_score, 0),
	COALESCE(e.keywords, ''),
	COALESCE(e.summary, ''),
	COALESCE(e.knowledge_details, ''),
	COALESCE(e.potential_questions, ''),
	COALESCE(e.related_entity_uuid_s, ''),
	COALESCE(e.source_page, 0),
	COALESCE(e.hidden_index, ''),
	COALESCE(e.has_question_index, 0)
FROM rag_knowledge_entry_v1 e
WHERE e.knowledge_base_id = ?
ORDER BY e.id
`

	rows, err := db.QueryContext(ctx, query, knowledgeBaseID)
	if err != nil {
		return nil, fmt.Errorf("failed to query knowledge entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]yakitKnowledgeEntryRecord, 0, 128)
	for rows.Next() {
		var entry yakitKnowledgeEntryRecord
		if err := rows.Scan(
			&entry.ID,
			&entry.Title,
			&entry.KnowledgeType,
			&entry.ImportanceScore,
			&entry.Keywords,
			&entry.Summary,
			&entry.Details,
			&entry.PotentialQuestions,
			&entry.RelatedEntities,
			&entry.SourcePage,
			&entry.HiddenIndex,
			&entry.HasQuestionIndex,
		); err != nil {
			return nil, fmt.Errorf("failed to scan knowledge entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while reading knowledge entries: %w", err)
	}

	return entries, nil
}

func writeYakitBridgeJSONL(path string, kb yakitKnowledgeBaseRecord, entries []yakitKnowledgeEntryRecord, pkg *YakitRAGPackageInfo, dbPath string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := bufio.NewWriter(file)
	defer func() { _ = writer.Flush() }()

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)

	for _, entry := range entries {
		record := yakitKnowledgeJSONLRecord{
			KnowledgeBaseName: kb.Name,
			KnowledgeBaseType: kb.Type,
			KnowledgeTitle:    strings.TrimSpace(entry.Title),
			KnowledgeType:     strings.TrimSpace(entry.KnowledgeType),
			ImportanceScore:   entry.ImportanceScore,
			Keywords:          splitYakitDelimitedText(entry.Keywords),
			Questions:         splitYakitDelimitedText(entry.PotentialQuestions),
			Summary:           strings.TrimSpace(entry.Summary),
			Details:           strings.TrimSpace(entry.Details),
			Content:           buildYakitBridgeContent(entry),
			SourcePage:        entry.SourcePage,
			RelatedEntities:   splitYakitDelimitedText(entry.RelatedEntities),
			HasQuestionIndex:  entry.HasQuestionIndex,
		}
		record.Bridge.Kind = "yakit-rag-bridge"
		record.Bridge.PackagePath = packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.PackagePath })
		record.Bridge.YakitDBPath = dbPath
		record.Bridge.CollectionUUID = firstNonEmpty(kb.CollectionUUID, packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.CollectionUUID }))
		record.Bridge.ModelName = firstNonEmpty(kb.ModelName, packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.ModelName }))
		record.Bridge.Slug = packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.Slug })
		record.Bridge.ExportedAt = strings.TrimSpace(kb.ExportedAt)

		if err := encoder.Encode(record); err != nil {
			return fmt.Errorf("failed to encode jsonl record: %w", err)
		}
	}

	return nil
}

func writeYakitBridgeMarkdown(path string, kb yakitKnowledgeBaseRecord, entries []yakitKnowledgeEntryRecord, pkg *YakitRAGPackageInfo, dbPath string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	writer := bufio.NewWriter(file)
	defer func() { _ = writer.Flush() }()

	header := []string{
		"# " + kb.Name,
		"",
		"## Bridge Metadata",
		fmt.Sprintf("- Source: Yakit knowledge DB"),
		fmt.Sprintf("- Yakit DB: %s", dbPath),
		fmt.Sprintf("- Knowledge Base Type: %s", strings.TrimSpace(kb.Type)),
	}
	if pkg != nil && strings.TrimSpace(pkg.PackagePath) != "" {
		header = append(header, fmt.Sprintf("- Package: %s", pkg.PackagePath))
	}
	if value := firstNonEmpty(kb.CollectionUUID, packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.CollectionUUID })); value != "" {
		header = append(header, fmt.Sprintf("- Collection UUID: %s", value))
	}
	if value := firstNonEmpty(kb.ModelName, packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.ModelName })); value != "" {
		header = append(header, fmt.Sprintf("- Embedding Model: %s", value))
	}
	if value := packageField(pkg, func(info *YakitRAGPackageInfo) string { return info.Slug }); value != "" {
		header = append(header, fmt.Sprintf("- Package Slug: %s", value))
	}
	if value := strings.TrimSpace(kb.ExportedAt); value != "" {
		header = append(header, fmt.Sprintf("- Exported At: %s", value))
	}
	header = append(header, fmt.Sprintf("- Entries: %d", len(entries)), "")

	if _, err := writer.WriteString(strings.Join(header, "\n")); err != nil {
		return fmt.Errorf("failed to write markdown header: %w", err)
	}

	for _, entry := range entries {
		sections := []string{
			fmt.Sprintf("## %s", sanitizeMarkdownHeading(entry.Title, entry.ID)),
			"",
			fmt.Sprintf("- Entry ID: %d", entry.ID),
		}
		if value := strings.TrimSpace(entry.KnowledgeType); value != "" {
			sections = append(sections, fmt.Sprintf("- Type: %s", value))
		}
		if entry.ImportanceScore != 0 {
			sections = append(sections, fmt.Sprintf("- Importance Score: %d", entry.ImportanceScore))
		}
		if entry.SourcePage > 0 {
			sections = append(sections, fmt.Sprintf("- Source Page: %d", entry.SourcePage))
		}
		if entry.HasQuestionIndex {
			sections = append(sections, "- Has Question Index: true")
		}
		if keywords := splitYakitDelimitedText(entry.Keywords); len(keywords) > 0 {
			sections = append(sections, fmt.Sprintf("- Keywords: %s", strings.Join(keywords, ", ")))
		}
		if related := splitYakitDelimitedText(entry.RelatedEntities); len(related) > 0 {
			sections = append(sections, fmt.Sprintf("- Related Entities: %s", strings.Join(related, ", ")))
		}
		sections = append(sections, "")

		if summary := strings.TrimSpace(entry.Summary); summary != "" {
			sections = append(sections, "### Summary", "", summary, "")
		}
		if details := strings.TrimSpace(entry.Details); details != "" {
			sections = append(sections, "### Details", "", "```text", details, "```", "")
		}
		if questions := splitYakitDelimitedText(entry.PotentialQuestions); len(questions) > 0 {
			sections = append(sections, "### Questions", "")
			for _, question := range questions {
				sections = append(sections, "- "+question)
			}
			sections = append(sections, "")
		}

		if _, err := writer.WriteString(strings.Join(sections, "\n")); err != nil {
			return fmt.Errorf("failed to write markdown entry: %w", err)
		}
	}

	return nil
}

func normalizeYakitBridgeFormat(format, outputPath string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "", "auto":
		ext := strings.ToLower(filepath.Ext(outputPath))
		switch ext {
		case ".md", ".markdown":
			return yakitBridgeFormatMD
		case ".jsonl":
			return yakitBridgeFormatJSONL
		default:
			return yakitBridgeFormatJSONL
		}
	case "jsonl":
		return yakitBridgeFormatJSONL
	case "md", "markdown":
		return yakitBridgeFormatMD
	default:
		return ""
	}
}

func extractYakitPrintableTokens(data []byte) []string {
	tokens := make([]string, 0, 16)
	var builder strings.Builder
	flush := func() {
		token := strings.TrimSpace(builder.String())
		builder.Reset()
		if len(token) >= 3 {
			tokens = append(tokens, token)
		}
	}

	for _, b := range data {
		if b >= 32 && b <= 126 {
			builder.WriteByte(b)
			continue
		}
		flush()
	}
	flush()

	return tokens
}

func looksLikeEmbeddingModel(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "embed") || strings.Contains(lower, "embedding")
}

func looksLikeYakitSlug(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || yakitUUIDPattern.MatchString(value) {
		return false
	}
	if !yakitSlugPattern.MatchString(value) {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool {
		return r >= 'a' && r <= 'z'
	}) >= 0
}

func splitYakitDelimitedText(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var stringList []string
	if json.Valid([]byte(raw)) {
		if err := json.Unmarshal([]byte(raw), &stringList); err == nil {
			return normalizeYakitStringList(stringList)
		}

		var anyList []interface{}
		if err := json.Unmarshal([]byte(raw), &anyList); err == nil {
			values := make([]string, 0, len(anyList))
			for _, item := range anyList {
				if text := strings.TrimSpace(fmt.Sprintf("%v", item)); text != "" {
					values = append(values, text)
				}
			}
			return normalizeYakitStringList(values)
		}

		var single string
		if err := json.Unmarshal([]byte(raw), &single); err == nil {
			raw = single
		}
	}

	splitters := []string{"\n", "||", ";", ","}
	values := []string{raw}
	for _, sep := range splitters {
		if !strings.Contains(raw, sep) {
			continue
		}
		parts := strings.Split(raw, sep)
		if len(parts) > 1 {
			values = parts
			break
		}
	}

	return normalizeYakitStringList(values)
}

func normalizeYakitStringList(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		value = collapseKnowledgeWhitespace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized
}

func buildYakitBridgeContent(entry yakitKnowledgeEntryRecord) string {
	parts := make([]string, 0, 8)
	if title := strings.TrimSpace(entry.Title); title != "" {
		parts = append(parts, title)
	}
	if summary := strings.TrimSpace(entry.Summary); summary != "" {
		parts = append(parts, "Summary:\n"+summary)
	}
	if details := strings.TrimSpace(entry.Details); details != "" {
		parts = append(parts, "Details:\n"+details)
	}
	if keywords := splitYakitDelimitedText(entry.Keywords); len(keywords) > 0 {
		parts = append(parts, "Keywords: "+strings.Join(keywords, ", "))
	}
	if questions := splitYakitDelimitedText(entry.PotentialQuestions); len(questions) > 0 {
		parts = append(parts, "Questions:\n- "+strings.Join(questions, "\n- "))
	}

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func sanitizeMarkdownHeading(title string, id int64) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Sprintf("Entry %d", id)
	}
	return strings.ReplaceAll(title, "\n", " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func packageField(pkg *YakitRAGPackageInfo, getter func(*YakitRAGPackageInfo) string) string {
	if pkg == nil {
		return ""
	}
	return strings.TrimSpace(getter(pkg))
}
