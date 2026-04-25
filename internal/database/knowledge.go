package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

var (
	knowledgeCWEIdentifierPattern    = regexp.MustCompile(`^cwe-\d+$`)
	knowledgeCAPECIdentifierPattern  = regexp.MustCompile(`^capec-\d+$`)
	knowledgeAttackIdentifierPattern = regexp.MustCompile(`^t\d{4}(?:\.\d{3})?$`)
	knowledgeASIIdentifierPattern    = regexp.MustCompile(`^asi\d{2,}$`)
	knowledgeMCPIdentifierPattern    = regexp.MustCompile(`^mcp\d{2,}(?::\d{4})?$`)
	knowledgeOWASPIdentifierPattern  = regexp.MustCompile(`^a\d{2}(?:[@:]\d{4})?$`)
	knowledgeSTRIDEIdentifierPattern = regexp.MustCompile(`^stride[\s:_-]+([stride])$`)
)

// KnowledgeDocumentQuery controls knowledge document list queries.
type KnowledgeDocumentQuery struct {
	Workspace string
	Offset    int
	Limit     int
}

// KnowledgeDocumentResult contains paginated documents.
type KnowledgeDocumentResult struct {
	Data       []KnowledgeDocument `json:"data"`
	TotalCount int                 `json:"total_count"`
	Offset     int                 `json:"offset"`
	Limit      int                 `json:"limit"`
}

// KnowledgeSearchHit is a scored keyword-search hit for a chunk.
type KnowledgeSearchHit struct {
	DocumentID  int64                     `json:"document_id"`
	ChunkID     int64                     `json:"chunk_id"`
	Workspace   string                    `json:"workspace"`
	Title       string                    `json:"title"`
	SourcePath  string                    `json:"source_path"`
	DocType     string                    `json:"doc_type"`
	Section     string                    `json:"section,omitempty"`
	Snippet     string                    `json:"snippet"`
	Score       float64                   `json:"score"`
	Metadata    *KnowledgeMetadataSummary `json:"metadata,omitempty"`
	ContentHash string                    `json:"-"`
}

// KnowledgeSearchOptions controls layered keyword search.
type KnowledgeSearchOptions struct {
	Workspace           string
	WorkspaceLayers     []string
	ScopeLayers         []string
	Query               string
	Limit               int
	ExactID             bool
	MinSourceConfidence float64
	SampleTypes         []string
	ExcludeSampleTypes  []string
}

type knowledgeSearchCandidate struct {
	DocumentID    int64  `bun:"document_id"`
	ChunkID       int64  `bun:"chunk_id"`
	Workspace     string `bun:"workspace"`
	Title         string `bun:"title"`
	SourcePath    string `bun:"source_path"`
	DocType       string `bun:"doc_type"`
	Section       string `bun:"section"`
	Content       string `bun:"content"`
	ContentHash   string `bun:"content_hash"`
	ChunkMetadata string `bun:"chunk_metadata"`
	DocMetadata   string `bun:"doc_metadata"`
}

type knowledgeIdentifierQuery struct {
	Family    string
	Canonical string
	BaseID    string
	PathID    string
	Year      string
}

// KnowledgeChunkExportRow contains a normalized chunk plus its parent document metadata.
type KnowledgeChunkExportRow struct {
	DocumentID int64  `bun:"document_id"`
	ChunkID    int64  `bun:"chunk_id"`
	Workspace  string `bun:"workspace"`
	ChunkIndex int    `bun:"chunk_index"`
	Title      string `bun:"title"`
	SourcePath string `bun:"source_path"`
	DocType    string `bun:"doc_type"`
	DocHash    string `bun:"doc_hash"`
	Section    string `bun:"section"`
	Content    string `bun:"content"`
	ChunkHash  string `bun:"chunk_hash"`
	Metadata   string `bun:"metadata_json"`
}

// UpsertKnowledgeDocument stores a document and replaces all associated chunks.
func UpsertKnowledgeDocument(ctx context.Context, doc *KnowledgeDocument, chunks []KnowledgeChunk) error {
	if db == nil {
		return fmt.Errorf("database not connected")
	}

	now := time.Now()
	doc.Workspace = normalizeKnowledgeWorkspace(doc.Workspace)
	doc.UpdatedAt = now
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}

	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var existing KnowledgeDocument
		err := tx.NewSelect().
			Model(&existing).
			Where("workspace = ?", doc.Workspace).
			Where("source_path = ?", doc.SourcePath).
			Limit(1).
			Scan(ctx)

		switch {
		case err == nil:
			doc.ID = existing.ID
			doc.CreatedAt = existing.CreatedAt
			if _, err := tx.NewUpdate().
				Model(doc).
				Column("source_type", "doc_type", "title", "content_hash", "status", "chunk_count", "total_bytes", "metadata_json", "error_message", "updated_at").
				WherePK().
				Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewDelete().
				Model((*KnowledgeChunk)(nil)).
				Where("document_id = ?", doc.ID).
				Exec(ctx); err != nil {
				return err
			}
		case err == sql.ErrNoRows:
			if _, err := tx.NewInsert().Model(doc).Exec(ctx); err != nil {
				return err
			}
		default:
			return err
		}

		if len(chunks) == 0 {
			return nil
		}

		for i := range chunks {
			chunks[i].DocumentID = doc.ID
			chunks[i].Workspace = doc.Workspace
			if chunks[i].CreatedAt.IsZero() {
				chunks[i].CreatedAt = now
			}
		}

		_, err = tx.NewInsert().Model(&chunks).Exec(ctx)
		return err
	})
}

// ListKnowledgeDocuments lists knowledge documents with pagination.
func ListKnowledgeDocuments(ctx context.Context, query KnowledgeDocumentQuery) (*KnowledgeDocumentResult, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 1000 {
		query.Limit = 1000
	}

	var docs []KnowledgeDocument
	q := db.NewSelect().Model(&docs)
	if trimmed := strings.TrimSpace(query.Workspace); trimmed != "" {
		q = q.Where("workspace = ?", trimmed)
	}

	total, err := q.
		Order("updated_at DESC").
		Limit(query.Limit).
		Offset(query.Offset).
		ScanAndCount(ctx)
	if err != nil {
		return nil, err
	}

	return &KnowledgeDocumentResult{
		Data:       docs,
		TotalCount: total,
		Offset:     query.Offset,
		Limit:      query.Limit,
	}, nil
}

// SearchKnowledge performs simple keyword search across knowledge chunks.
func SearchKnowledge(ctx context.Context, workspace, query string, limit int) ([]KnowledgeSearchHit, error) {
	return SearchKnowledgeWithOptions(ctx, KnowledgeSearchOptions{
		Workspace: strings.TrimSpace(workspace),
		Query:     query,
		Limit:     limit,
	})
}

// SearchKnowledgeWithOptions performs keyword search across one or more knowledge layers.
func SearchKnowledgeWithOptions(ctx context.Context, opts KnowledgeSearchOptions) ([]KnowledgeSearchHit, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return []KnowledgeSearchHit{}, nil
	}
	identifier, hasIdentifier := detectKnowledgeIdentifier(query)
	if opts.ExactID && !hasIdentifier {
		return []KnowledgeSearchHit{}, nil
	}
	workspaces := normalizeKnowledgeWorkspaceLayers(opts.Workspace, opts.WorkspaceLayers)
	scopeLayers := normalizeKnowledgeScopeLayers(opts.ScopeLayers)
	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	pattern := "%" + escapeKnowledgeLike(strings.ToLower(query)) + "%"

	var candidates []knowledgeSearchCandidate
	q := db.NewSelect().
		TableExpr("knowledge_chunks AS kc").
		ColumnExpr("kc.document_id AS document_id").
		ColumnExpr("kc.id AS chunk_id").
		ColumnExpr("kc.workspace AS workspace").
		ColumnExpr("kd.title AS title").
		ColumnExpr("kd.source_path AS source_path").
		ColumnExpr("kd.doc_type AS doc_type").
		ColumnExpr("kc.section AS section").
		ColumnExpr("kc.content AS content").
		ColumnExpr("kc.content_hash AS content_hash").
		ColumnExpr("COALESCE(kc.metadata_json, '') AS chunk_metadata").
		ColumnExpr("COALESCE(kd.metadata_json, '') AS doc_metadata").
		Join("JOIN knowledge_documents AS kd ON kd.id = kc.document_id").
		WhereGroup(" AND ", func(query *bun.SelectQuery) *bun.SelectQuery {
			query = query.WhereOr("LOWER(kc.content) LIKE ? ESCAPE '\\'", pattern)
			query = query.WhereOr("LOWER(kd.title) LIKE ? ESCAPE '\\'", pattern)
			query = query.WhereOr("LOWER(kd.source_path) LIKE ? ESCAPE '\\'", pattern)
			if hasIdentifier {
				query = addKnowledgeIdentifierCandidateConditions(query, identifier)
			}
			return query
		})

	if opts.ExactID {
		q = addExactKnowledgeIdentifierFilter(q, identifier)
	}
	if hasIdentifier {
		q = orderKnowledgeIdentifierCandidates(q, identifier)
	}

	if len(workspaces) == 1 {
		q = q.Where("kc.workspace = ?", workspaces[0])
	} else if len(workspaces) > 1 {
		q = q.Where("kc.workspace IN (?)", bun.In(workspaces))
	}
	if len(scopeLayers) > 0 {
		q = q.WhereGroup(" AND ", func(query *bun.SelectQuery) *bun.SelectQuery {
			for _, scope := range scopeLayers {
				pattern := "%\"scope\":\"" + escapeKnowledgeLike(scope) + "\"%"
				query = query.WhereOr("LOWER(COALESCE(kc.metadata_json, '')) LIKE ? ESCAPE '\\'", pattern)
				query = query.WhereOr("LOWER(COALESCE(kd.metadata_json, '')) LIKE ? ESCAPE '\\'", pattern)
			}
			return query
		})
	}

	if err := q.Limit(limit*8).Scan(ctx, &candidates); err != nil {
		return nil, err
	}

	results := make([]KnowledgeSearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		metadata := ParseKnowledgeMetadata(candidate.ChunkMetadata, candidate.DocMetadata)
		if !KnowledgeMetadataMatchesFilters(metadata, opts.MinSourceConfidence, opts.SampleTypes, opts.ExcludeSampleTypes) {
			continue
		}
		score := computeKnowledgeScore(query, candidate.Title, candidate.SourcePath, candidate.Content)
		score += computeKnowledgeIdentifierBoost(identifier, hasIdentifier, candidate.Title, candidate.SourcePath, candidate.Content, candidate.ChunkMetadata, candidate.DocMetadata)
		score += computeKnowledgeLayerBoost(workspaces, candidate.Workspace)
		score += computeKnowledgeScopeBoost(scopeLayers, candidate.ChunkMetadata, candidate.DocMetadata)
		score += ComputeKnowledgeMetadataBoost(query, metadata)
		if score <= 0 {
			continue
		}
		results = append(results, KnowledgeSearchHit{
			DocumentID:  candidate.DocumentID,
			ChunkID:     candidate.ChunkID,
			Workspace:   candidate.Workspace,
			Title:       candidate.Title,
			SourcePath:  candidate.SourcePath,
			DocType:     candidate.DocType,
			Section:     candidate.Section,
			Snippet:     buildKnowledgeSnippet(query, candidate.Content),
			Score:       score,
			Metadata:    metadata,
			ContentHash: candidate.ContentHash,
		})
	}

	results = dedupeKnowledgeSearchHits(results)

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].Title == results[j].Title {
				return results[i].ChunkID < results[j].ChunkID
			}
			return results[i].Title < results[j].Title
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func addKnowledgeIdentifierCandidateConditions(q *bun.SelectQuery, identifier knowledgeIdentifierQuery) *bun.SelectQuery {
	// ID 查询要额外把标题、路径、metadata 精确命中的记录拉进候选集。
	// 否则 CWE-79 这类短 ID 可能被大量 CWE-790/CWE-791 前缀碰撞记录挤出 limit*8 窗口。
	for _, pattern := range knowledgeIdentifierTitleLikePatterns(identifier) {
		q = q.WhereOr("LOWER(kd.title) LIKE ? ESCAPE '\\'", pattern)
	}
	for _, pattern := range knowledgeIdentifierSourcePathLikePatterns(identifier) {
		q = q.WhereOr("LOWER(kd.source_path) LIKE ? ESCAPE '\\'", pattern)
	}
	for _, pattern := range knowledgeIdentifierMetadataLikePatterns(identifier) {
		q = q.WhereOr("LOWER(COALESCE(kc.metadata_json, '')) LIKE ? ESCAPE '\\'", pattern)
		q = q.WhereOr("LOWER(COALESCE(kd.metadata_json, '')) LIKE ? ESCAPE '\\'", pattern)
	}
	return q
}

func addExactKnowledgeIdentifierFilter(q *bun.SelectQuery, identifier knowledgeIdentifierQuery) *bun.SelectQuery {
	// --exact-id 是强过滤模式：只保留文档身份本身匹配的记录。
	// 普通正文里提到该 ID 的相关文档仍可在非 exact 模式下返回。
	return q.WhereGroup(" AND ", func(query *bun.SelectQuery) *bun.SelectQuery {
		for _, value := range knowledgeIdentifierTitleExactValues(identifier) {
			query = query.WhereOr("LOWER(kd.title) = ?", value)
		}
		for _, pattern := range knowledgeIdentifierTitleLikePatterns(identifier) {
			query = query.WhereOr("LOWER(kd.title) LIKE ? ESCAPE '\\'", pattern)
		}
		for _, pattern := range knowledgeIdentifierSourcePathLikePatterns(identifier) {
			query = query.WhereOr("LOWER(kd.source_path) LIKE ? ESCAPE '\\'", pattern)
		}
		for _, pattern := range knowledgeIdentifierMetadataLikePatterns(identifier) {
			query = query.WhereOr("LOWER(COALESCE(kc.metadata_json, '')) LIKE ? ESCAPE '\\'", pattern)
			query = query.WhereOr("LOWER(COALESCE(kd.metadata_json, '')) LIKE ? ESCAPE '\\'", pattern)
		}
		return query
	})
}

func orderKnowledgeIdentifierCandidates(q *bun.SelectQuery, identifier knowledgeIdentifierQuery) *bun.SelectQuery {
	// 在 SQL 候选阶段先把精确身份命中排到前面，避免数据库 limit 先截断掉真正目标。
	orderTerms := make([]string, 0)
	args := make([]interface{}, 0)

	for _, value := range knowledgeIdentifierTitleExactValues(identifier) {
		orderTerms = append(orderTerms, "LOWER(kd.title) = ?")
		args = append(args, value)
	}
	for _, pattern := range knowledgeIdentifierTitleLikePatterns(identifier) {
		orderTerms = append(orderTerms, "LOWER(kd.title) LIKE ? ESCAPE '\\'")
		args = append(args, pattern)
	}
	for _, pattern := range knowledgeIdentifierSourcePathLikePatterns(identifier) {
		orderTerms = append(orderTerms, "LOWER(kd.source_path) LIKE ? ESCAPE '\\'")
		args = append(args, pattern)
	}
	for _, pattern := range knowledgeIdentifierMetadataLikePatterns(identifier) {
		orderTerms = append(orderTerms, "LOWER(COALESCE(kd.metadata_json, '')) LIKE ? ESCAPE '\\'")
		args = append(args, pattern)
	}
	if len(orderTerms) == 0 {
		return q
	}

	return q.OrderExpr("CASE WHEN "+strings.Join(orderTerms, " OR ")+" THEN 0 ELSE 1 END ASC", args...)
}

// ListKnowledgeChunks returns joined chunk/document rows for export and offline indexing.
func ListKnowledgeChunks(ctx context.Context, workspace string, limit int) ([]KnowledgeChunkExportRow, error) {
	return ListKnowledgeChunksPage(ctx, workspace, limit, 0)
}

// ListKnowledgeChunksPage returns joined chunk/document rows for export and offline indexing with pagination.
func ListKnowledgeChunksPage(ctx context.Context, workspace string, limit, offset int) ([]KnowledgeChunkExportRow, error) {
	if db == nil {
		return nil, fmt.Errorf("database not connected")
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}
	if offset < 0 {
		offset = 0
	}

	var rows []KnowledgeChunkExportRow
	q := db.NewSelect().
		TableExpr("knowledge_chunks AS kc").
		ColumnExpr("kc.document_id AS document_id").
		ColumnExpr("kc.id AS chunk_id").
		ColumnExpr("kc.workspace AS workspace").
		ColumnExpr("kc.chunk_index AS chunk_index").
		ColumnExpr("kd.title AS title").
		ColumnExpr("kd.source_path AS source_path").
		ColumnExpr("kd.doc_type AS doc_type").
		ColumnExpr("kd.content_hash AS doc_hash").
		ColumnExpr("kc.section AS section").
		ColumnExpr("kc.content AS content").
		ColumnExpr("kc.content_hash AS chunk_hash").
		ColumnExpr("kc.metadata_json AS metadata_json").
		Join("JOIN knowledge_documents AS kd ON kd.id = kc.document_id")

	if trimmed := strings.TrimSpace(workspace); trimmed != "" {
		q = q.Where("kc.workspace = ?", trimmed)
	}

	if err := q.
		OrderExpr("kd.updated_at DESC").
		OrderExpr("kc.document_id DESC").
		OrderExpr("kc.chunk_index ASC").
		Limit(limit).
		Offset(offset).
		Scan(ctx, &rows); err != nil {
		return nil, err
	}

	return rows, nil
}

func normalizeKnowledgeWorkspace(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "global"
	}
	return workspace
}

func normalizeKnowledgeWorkspaceLayers(workspace string, layers []string) []string {
	if len(layers) == 0 {
		if trimmed := strings.TrimSpace(workspace); trimmed != "" {
			if trimmed == "public" {
				return []string{"public"}
			}
			return []string{trimmed, "public"}
		}
		return nil
	}
	result := make([]string, 0, len(layers))
	seen := make(map[string]struct{}, len(layers))
	for _, layer := range layers {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			continue
		}
		if _, ok := seen[layer]; ok {
			continue
		}
		seen[layer] = struct{}{}
		result = append(result, layer)
	}
	return result
}

func normalizeKnowledgeScopeLayers(layers []string) []string {
	if len(layers) == 0 {
		return nil
	}
	result := make([]string, 0, len(layers))
	seen := make(map[string]struct{}, len(layers))
	for _, layer := range layers {
		layer = strings.ToLower(strings.TrimSpace(layer))
		if layer == "" {
			continue
		}
		if _, ok := seen[layer]; ok {
			continue
		}
		seen[layer] = struct{}{}
		result = append(result, layer)
	}
	return result
}

func computeKnowledgeScore(query, title, sourcePath, content string) float64 {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if queryLower == "" {
		return 0
	}

	titleLower := strings.ToLower(title)
	pathLower := strings.ToLower(sourcePath)
	contentLower := strings.ToLower(content)

	terms := uniqueKnowledgeTerms(strings.Fields(queryLower))
	score := float64(strings.Count(contentLower, queryLower) * 12)
	score += float64(strings.Count(titleLower, queryLower) * 25)
	score += float64(strings.Count(pathLower, queryLower) * 8)

	for _, term := range terms {
		if len(term) < 2 {
			continue
		}
		score += float64(strings.Count(contentLower, term) * 4)
		score += float64(strings.Count(titleLower, term) * 10)
		score += float64(strings.Count(pathLower, term) * 3)
	}

	return score
}

func detectKnowledgeIdentifier(query string) (knowledgeIdentifierQuery, bool) {
	// 只识别明确的安全知识 ID。单独的 S/T/D 这类字符不算 ID，
	// 避免普通短词搜索被 STRIDE 单字母分类误伤。
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return knowledgeIdentifierQuery{}, false
	}

	if match := knowledgeSTRIDEIdentifierPattern.FindStringSubmatch(normalized); len(match) == 2 {
		return knowledgeIdentifierQuery{
			Family:    "stride",
			Canonical: "stride:" + match[1],
			BaseID:    match[1],
			PathID:    match[1],
		}, true
	}

	compact := strings.ReplaceAll(normalized, " ", "")
	switch {
	case knowledgeCWEIdentifierPattern.MatchString(compact):
		return knowledgeIdentifierQuery{Family: "cwe", Canonical: compact, BaseID: compact, PathID: compact}, true
	case knowledgeCAPECIdentifierPattern.MatchString(compact):
		return knowledgeIdentifierQuery{Family: "capec", Canonical: compact, BaseID: compact, PathID: compact}, true
	case knowledgeAttackIdentifierPattern.MatchString(compact):
		return knowledgeIdentifierQuery{Family: "attack", Canonical: compact, BaseID: compact, PathID: compact}, true
	case knowledgeASIIdentifierPattern.MatchString(compact):
		return knowledgeIdentifierQuery{Family: "agentic", Canonical: compact, BaseID: compact, PathID: compact}, true
	case knowledgeMCPIdentifierPattern.MatchString(compact):
		return knowledgeIdentifierQuery{Family: "agentic", Canonical: compact, BaseID: compact, PathID: compact}, true
	case knowledgeOWASPIdentifierPattern.MatchString(compact):
		baseID := compact
		year := ""
		if idx := strings.IndexAny(compact, "@:"); idx >= 0 {
			baseID = compact[:idx]
			year = compact[idx+1:]
		}
		pathID := baseID
		canonical := baseID
		if year != "" {
			pathID = baseID + "@" + year
			canonical = pathID
		}
		return knowledgeIdentifierQuery{Family: "owasp", Canonical: canonical, BaseID: baseID, PathID: pathID, Year: year}, true
	default:
		return knowledgeIdentifierQuery{}, false
	}
}

func computeKnowledgeIdentifierBoost(identifier knowledgeIdentifierQuery, ok bool, title, sourcePath, content string, metadata ...string) float64 {
	if !ok {
		return 0
	}

	titleLower := strings.ToLower(title)
	pathLower := strings.ToLower(sourcePath)
	contentLower := strings.ToLower(content)

	score := 0.0
	// 标题、路径、metadata 代表“文档身份”，权重大于正文里的关系引用。
	// 例如 CWE-352 正文提到 CWE-79，不应排在 CWE-79 本体前面。
	for _, value := range knowledgeIdentifierTitleExactValues(identifier) {
		if titleLower == value {
			score += 2000
		}
	}
	for _, pattern := range knowledgeIdentifierTitleBoundaryValues(identifier) {
		if hasKnowledgeIdentifierBoundary(titleLower, pattern) {
			score += 1800
			break
		}
	}
	for _, value := range knowledgeIdentifierPathBoundaryValues(identifier) {
		if hasKnowledgeIdentifierBoundary(pathLower, value) {
			score += 1600
			break
		}
	}
	for _, value := range knowledgeIdentifierMetadataExactValues(identifier) {
		if metadataHasKnowledgeIdentifierValue(value, metadata...) {
			score += 1500
			break
		}
	}
	for _, value := range knowledgeIdentifierContentBoundaryValues(identifier) {
		if hasKnowledgeIdentifierBoundary(contentLower, value) {
			score += 80
			break
		}
	}
	return score
}

func knowledgeIdentifierTitleExactValues(identifier knowledgeIdentifierQuery) []string {
	switch identifier.Family {
	case "stride":
		return []string{"stride " + identifier.BaseID}
	default:
		return []string{identifier.Canonical}
	}
}

func knowledgeIdentifierTitleBoundaryValues(identifier knowledgeIdentifierQuery) []string {
	switch identifier.Family {
	case "stride":
		return []string{"stride " + identifier.BaseID, "stride:" + identifier.BaseID}
	case "owasp":
		if identifier.Year != "" {
			return []string{identifier.BaseID + " (" + identifier.Year + ")", identifier.PathID}
		}
		return []string{identifier.BaseID}
	default:
		return []string{identifier.Canonical}
	}
}

func knowledgeIdentifierPathBoundaryValues(identifier knowledgeIdentifierQuery) []string {
	switch identifier.Family {
	case "stride":
		return []string{"stride_cwe/" + identifier.PathID}
	case "owasp":
		if identifier.Year == "" {
			return []string{"owasp_top10/" + identifier.BaseID}
		}
		return []string{"owasp_top10/" + identifier.PathID}
	default:
		return []string{identifier.PathID}
	}
}

func knowledgeIdentifierContentBoundaryValues(identifier knowledgeIdentifierQuery) []string {
	values := knowledgeIdentifierTitleBoundaryValues(identifier)
	if identifier.Family == "owasp" && identifier.Year != "" {
		values = append(values, identifier.BaseID+"::"+identifier.Year)
	}
	return values
}

func knowledgeIdentifierMetadataExactValues(identifier knowledgeIdentifierQuery) []string {
	values := []string{identifier.Canonical, identifier.PathID}
	switch identifier.Family {
	case "stride":
		values = append(values, identifier.BaseID)
	case "owasp":
		values = append(values, identifier.BaseID)
		if identifier.Year != "" {
			values = append(values, identifier.BaseID+"::"+identifier.Year)
		}
	}
	return uniqueKnowledgeTerms(values)
}

func knowledgeIdentifierTitleLikePatterns(identifier knowledgeIdentifierQuery) []string {
	values := make([]string, 0)
	for _, titleID := range knowledgeIdentifierTitleBoundaryValues(identifier) {
		escaped := escapeKnowledgeLike(titleID)
		values = append(values,
			escaped+":%",
			escaped+" (%",
			escaped+" -%",
			escaped+" —%",
		)
	}
	return uniqueKnowledgeTerms(values)
}

func knowledgeIdentifierSourcePathLikePatterns(identifier knowledgeIdentifierQuery) []string {
	switch identifier.Family {
	case "stride":
		return []string{"%/stride\\_cwe/" + escapeKnowledgeLike(identifier.PathID)}
	case "owasp":
		if identifier.Year == "" {
			return []string{
				"%/owasp\\_top10/" + escapeKnowledgeLike(identifier.BaseID),
				"%/owasp\\_top10/" + escapeKnowledgeLike(identifier.BaseID) + "@%",
			}
		}
		return []string{"%/owasp\\_top10/" + escapeKnowledgeLike(identifier.PathID)}
	default:
		return []string{"%/" + escapeKnowledgeLike(identifier.PathID)}
	}
}

func knowledgeIdentifierMetadataLikePatterns(identifier knowledgeIdentifierQuery) []string {
	patterns := make([]string, 0)
	for _, value := range knowledgeIdentifierMetadataExactValues(identifier) {
		escaped := escapeKnowledgeLike(value)
		patterns = append(patterns,
			`%"source_id":"`+escaped+`"%`,
			`%"source_id": "`+escaped+`"%`,
			`%"stix_id":"`+escaped+`"%`,
			`%"stix_id": "`+escaped+`"%`,
		)
	}
	return uniqueKnowledgeTerms(patterns)
}

func hasKnowledgeIdentifierBoundary(text, query string) bool {
	text = strings.ToLower(text)
	query = strings.ToLower(strings.TrimSpace(query))
	if text == "" || query == "" {
		return false
	}

	start := 0
	for {
		idx := strings.Index(text[start:], query)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isKnowledgeIdentifierChar(rune(text[idx-1]))
		afterIdx := idx + len(query)
		afterOK := afterIdx >= len(text) || !isKnowledgeIdentifierChar(rune(text[afterIdx]))
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
}

func metadataHasKnowledgeIdentifierValue(value string, metadata ...string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, raw := range metadata {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			continue
		}
		for _, key := range []string{"source_id", "stix_id"} {
			if strings.EqualFold(parseKnowledgeMetadataString(parsed[key]), value) {
				return true
			}
		}
	}
	return false
}

func isKnowledgeIdentifierChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' ||
		r == '.' ||
		r == ':' ||
		r == '_' ||
		r == '@'
}

func computeKnowledgeLayerBoost(workspaces []string, workspace string) float64 {
	if len(workspaces) == 0 {
		return 0
	}
	workspace = strings.TrimSpace(workspace)
	for idx, layer := range workspaces {
		if layer == workspace {
			return float64(len(workspaces)-idx) * 0.35
		}
	}
	return 0
}

func computeKnowledgeScopeBoost(scopeLayers []string, metadata ...string) float64 {
	if len(scopeLayers) == 0 {
		return 0
	}
	scope := extractKnowledgeScope(metadata...)
	if scope == "" {
		return 0
	}
	for idx, layer := range scopeLayers {
		if layer == scope {
			return float64(len(scopeLayers)-idx) * 0.2
		}
	}
	return 0
}

func dedupeKnowledgeSearchHits(results []KnowledgeSearchHit) []KnowledgeSearchHit {
	if len(results) <= 1 {
		return results
	}

	bestByKey := make(map[string]KnowledgeSearchHit, len(results))
	order := make([]string, 0, len(results))
	for _, result := range results {
		key := KnowledgeMetadataFingerprint(result.Metadata)
		if key == "" {
			key = strings.TrimSpace(result.ContentHash)
		}
		if key == "" {
			key = strings.TrimSpace(result.SourcePath) + "::" + strings.TrimSpace(result.Section) + "::" + strings.TrimSpace(result.Snippet)
		}
		if existing, ok := bestByKey[key]; ok {
			if existing.Score >= result.Score {
				continue
			}
		} else {
			order = append(order, key)
		}
		bestByKey[key] = result
	}

	deduped := make([]KnowledgeSearchHit, 0, len(bestByKey))
	for _, key := range order {
		deduped = append(deduped, bestByKey[key])
	}
	return deduped
}

func extractKnowledgeScope(metadata ...string) string {
	for _, item := range metadata {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(item), &parsed); err != nil {
			continue
		}
		scope, _ := parsed["scope"].(string)
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope != "" {
			return scope
		}
	}
	return ""
}

func uniqueKnowledgeTerms(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		result = append(result, term)
	}
	return result
}

func escapeKnowledgeLike(input string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return replacer.Replace(input)
}

func buildKnowledgeSnippet(query, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	lowerContent := strings.ToLower(content)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	index := strings.Index(lowerContent, lowerQuery)
	if index < 0 {
		for _, term := range strings.Fields(lowerQuery) {
			index = strings.Index(lowerContent, term)
			if index >= 0 {
				break
			}
		}
	}
	if index < 0 {
		index = 0
	}

	start := index - 120
	if start < 0 {
		start = 0
	}
	end := index + 240
	if end > len(content) {
		end = len(content)
	}

	snippet := strings.TrimSpace(content[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet += "..."
	}
	return snippet
}
