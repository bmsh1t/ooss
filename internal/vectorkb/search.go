package vectorkb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	illm "github.com/j3ssie/osmedeus/v5/internal/llm"
	"github.com/j3ssie/osmedeus/v5/internal/rerank"
	"github.com/uptrace/bun"
)

type searchRow struct {
	DocumentID    int64  `bun:"document_id"`
	ChunkID       int64  `bun:"chunk_id"`
	Workspace     string `bun:"workspace"`
	Title         string `bun:"title"`
	SourcePath    string `bun:"source_path"`
	DocType       string `bun:"doc_type"`
	Section       string `bun:"section"`
	Content       string `bun:"content"`
	ContentHash   string `bun:"content_hash"`
	Provider      string `bun:"provider"`
	Model         string `bun:"model"`
	EmbeddingJSON string `bun:"embedding_json"`
	ChunkMetadata string `bun:"chunk_metadata"`
	DocMetadata   string `bun:"doc_metadata"`
}

// Search queries the independent vector knowledge DB.
func Search(ctx context.Context, cfg *config.Config, opts SearchOptions, query string) ([]SearchHit, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchHit{}, nil
	}
	workspaces := normalizeVectorWorkspaceLayers(opts.Workspace, opts.WorkspaceLayers)
	scopeLayers := normalizeVectorScopeLayers(opts.ScopeLayers)

	provider := strings.TrimSpace(opts.Provider)
	if provider == "" {
		provider = strings.TrimSpace(cfg.GetKnowledgeVectorProvider())
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.GetKnowledgeVectorModel(provider))
	}

	store, err := Open(cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		return nil, err
	}

	queryEmbeddings, _, err := illm.GenerateEmbeddingsWithProvider(ctx, cfg, provider, []string{query}, model)
	if err != nil {
		return nil, err
	}
	if len(queryEmbeddings) == 0 || len(queryEmbeddings[0]) == 0 {
		return []SearchHit{}, nil
	}

	var rows []searchRow
	q := store.db.NewSelect().
		TableExpr("vector_embeddings AS ve").
		ColumnExpr("vd.id AS document_id").
		ColumnExpr("vc.id AS chunk_id").
		ColumnExpr("vd.workspace AS workspace").
		ColumnExpr("vd.title AS title").
		ColumnExpr("vd.source_path AS source_path").
		ColumnExpr("vd.doc_type AS doc_type").
		ColumnExpr("vc.section AS section").
		ColumnExpr("vc.content AS content").
		ColumnExpr("vc.content_hash AS content_hash").
		ColumnExpr("ve.provider AS provider").
		ColumnExpr("ve.model AS model").
		ColumnExpr("ve.embedding_json AS embedding_json").
		ColumnExpr("COALESCE(vc.metadata_json, '') AS chunk_metadata").
		ColumnExpr("COALESCE(vd.metadata_json, '') AS doc_metadata").
		Join("JOIN vector_chunks AS vc ON vc.id = ve.chunk_id").
		Join("JOIN vector_documents AS vd ON vd.id = vc.document_id").
		Where("ve.provider = ?", provider).
		Where("ve.model = ?", model)
	if len(workspaces) == 1 {
		q = q.Where("vd.workspace = ?", workspaces[0])
	} else if len(workspaces) > 1 {
		q = q.Where("vd.workspace IN (?)", bun.In(workspaces))
	}
	if err := q.Scan(ctx, &rows); err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	hybridWeight := opts.HybridWeight
	keywordWeight := opts.KeywordWeight
	if hybridWeight <= 0 || keywordWeight <= 0 {
		hybridWeight, keywordWeight = cfg.GetKnowledgeVectorHybridWeights()
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = cfg.GetKnowledgeVectorTopK()
	}

	queryVector := queryEmbeddings[0]
	results := make([]SearchHit, 0, len(rows))
	for _, row := range rows {
		var vector []float64
		if err := json.Unmarshal([]byte(row.EmbeddingJSON), &vector); err != nil {
			continue
		}
		vectorScore := cosineSimilarity(queryVector, vector)
		keywordScore := computeKeywordScore(query, row.Title, row.SourcePath, row.Content)
		layerBoost := computeVectorLayerBoost(workspaces, row.Workspace)
		scopeBoost := computeVectorScopeBoost(scopeLayers, row.ChunkMetadata)
		metadata := database.ParseKnowledgeMetadata(row.ChunkMetadata, row.DocMetadata)
		if !database.KnowledgeMetadataMatchesFilters(metadata, opts.MinSourceConfidence, opts.SampleTypes, opts.ExcludeSampleTypes) {
			continue
		}
		metadataBoost := computeVectorMetadataBoost(query, metadata)
		if vectorScore <= 0 && keywordScore <= 0 {
			continue
		}
		results = append(results, SearchHit{
			DocumentID:     row.DocumentID,
			ChunkID:        row.ChunkID,
			Workspace:      row.Workspace,
			Title:          row.Title,
			SourcePath:     row.SourcePath,
			DocType:        row.DocType,
			Section:        row.Section,
			Content:        row.Content,
			Snippet:        buildSnippet(query, row.Content),
			Provider:       row.Provider,
			Model:          row.Model,
			VectorScore:    vectorScore,
			KeywordScore:   keywordScore,
			RelevanceScore: (vectorScore * hybridWeight) + (keywordScore * keywordWeight) + layerBoost + scopeBoost + metadataBoost,
			Type:           "vector_kb",
			Metadata:       metadata,
			ContentHash:    row.ContentHash,
		})
	}

	results = dedupeVectorSearchHits(results)

	sort.Slice(results, func(i, j int) bool {
		if results[i].RelevanceScore == results[j].RelevanceScore {
			return results[i].ChunkID < results[j].ChunkID
		}
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	for i := range results {
		results[i].BaseRelevanceScore = results[i].RelevanceScore
		results[i].RankingSource = "hybrid"
	}

	if opts.EnableRerank && cfg.IsRerankEnabled() && len(results) > 0 {
		reranked, err := applySearchRerank(ctx, cfg, opts, query, results)
		if err == nil && len(reranked) > 0 {
			results = reranked
		} else {
			for i := range results {
				results[i].RankingSource = "fallback_hybrid"
			}
		}
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func applySearchRerank(ctx context.Context, cfg *config.Config, opts SearchOptions, query string, hits []SearchHit) ([]SearchHit, error) {
	provider, err := cfg.ResolveRerankProvider(strings.TrimSpace(opts.RerankProvider))
	if err != nil {
		return nil, err
	}

	maxCandidates := opts.RerankMaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = cfg.GetRerankMaxCandidates()
	}
	if maxCandidates <= 0 || maxCandidates > len(hits) {
		maxCandidates = len(hits)
	}
	if maxCandidates == 0 {
		return nil, nil
	}

	topN := opts.RerankTopN
	if topN <= 0 {
		topN = cfg.GetRerankTopN()
	}

	candidates := hits[:maxCandidates]
	docs := make([]rerank.Document, 0, len(candidates))
	byID := make(map[string]SearchHit, len(candidates))
	for _, hit := range candidates {
		docID := buildSearchRerankDocumentID(hit)
		docs = append(docs, rerank.Document{
			ID:       docID,
			Text:     buildSearchRerankDocumentText(hit),
			Metadata: buildSearchRerankMetadata(hit),
		})
		byID[docID] = hit
	}

	client := rerank.NewClient(provider, cfg.GetRerankTimeout())
	resp, err := client.Rerank(ctx, rerank.Request{
		Query:         query,
		Documents:     docs,
		TopN:          topN,
		MaxCandidates: maxCandidates,
		MinScore:      cfg.Rerank.MinScore,
		ModelOverride: strings.TrimSpace(opts.RerankModel),
	})
	if err != nil {
		return nil, err
	}

	ordered := make([]SearchHit, 0, len(resp.Results))
	for _, item := range resp.Results {
		hit, ok := byID[item.ID]
		if !ok {
			continue
		}
		hit.RelevanceScore = item.Score
		hit.RerankScore = item.Score
		hit.RankingSource = "rerank"
		ordered = append(ordered, hit)
	}

	if maxCandidates < len(hits) {
		ordered = append(ordered, hits[maxCandidates:]...)
	}

	return ordered, nil
}

func buildSearchRerankDocumentID(hit SearchHit) string {
	return fmt.Sprintf("%s#%d", strings.TrimSpace(hit.SourcePath), hit.ChunkID)
}

func buildSearchRerankDocumentText(hit SearchHit) string {
	parts := make([]string, 0, 4)
	if title := strings.TrimSpace(hit.Title); title != "" {
		parts = append(parts, "title: "+title)
	}
	if section := strings.TrimSpace(hit.Section); section != "" {
		parts = append(parts, "section: "+section)
	}
	if snippet := strings.TrimSpace(hit.Snippet); snippet != "" {
		parts = append(parts, "snippet: "+snippet)
	}
	meta := make([]string, 0, 2)
	if workspace := strings.TrimSpace(hit.Workspace); workspace != "" {
		meta = append(meta, "workspace="+workspace)
	}
	if docType := strings.TrimSpace(hit.DocType); docType != "" {
		meta = append(meta, "doc_type="+docType)
	}
	if len(meta) > 0 {
		parts = append(parts, "meta: "+strings.Join(meta, ", "))
	}
	return strings.Join(parts, "\n")
}

func buildSearchRerankMetadata(hit SearchHit) map[string]string {
	metadata := map[string]string{
		"workspace": strings.TrimSpace(hit.Workspace),
		"doc_type":  strings.TrimSpace(hit.DocType),
	}
	if title := strings.TrimSpace(hit.Title); title != "" {
		metadata["title"] = title
	}
	if section := strings.TrimSpace(hit.Section); section != "" {
		metadata["section"] = section
	}
	return metadata
}

func normalizeVectorWorkspaceLayers(workspace string, layers []string) []string {
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

func normalizeVectorScopeLayers(layers []string) []string {
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

func computeVectorLayerBoost(workspaces []string, workspace string) float64 {
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

func computeVectorScopeBoost(scopeLayers []string, metadata string) float64 {
	if len(scopeLayers) == 0 {
		return 0
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(metadata), &parsed); err != nil {
		return 0
	}
	scope, _ := parsed["scope"].(string)
	scope = strings.ToLower(strings.TrimSpace(scope))
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

func computeVectorMetadataBoost(query string, metadata *database.KnowledgeMetadataSummary) float64 {
	return database.ComputeKnowledgeMetadataBoost(query, metadata)
}

func dedupeVectorSearchHits(results []SearchHit) []SearchHit {
	if len(results) <= 1 {
		return results
	}

	bestByKey := make(map[string]SearchHit, len(results))
	order := make([]string, 0, len(results))
	for _, result := range results {
		key := database.KnowledgeMetadataFingerprint(result.Metadata)
		if key == "" {
			key = strings.TrimSpace(result.ContentHash)
		}
		if key == "" {
			key = strings.TrimSpace(result.SourcePath) + "::" + strings.TrimSpace(result.Section) + "::" + strings.TrimSpace(result.Snippet)
		}
		if existing, ok := bestByKey[key]; ok {
			if existing.RelevanceScore >= result.RelevanceScore {
				continue
			}
		} else {
			order = append(order, key)
		}
		bestByKey[key] = result
	}

	deduped := make([]SearchHit, 0, len(bestByKey))
	for _, key := range order {
		deduped = append(deduped, bestByKey[key])
	}
	return deduped
}

func cosineSimilarity(left, right []float64) float64 {
	if len(left) == 0 || len(right) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for i := range left {
		dot += left[i] * right[i]
		leftNorm += left[i] * left[i]
		rightNorm += right[i] * right[i]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

func computeKeywordScore(query, title, sourcePath, content string) float64 {
	queryLower := strings.ToLower(strings.TrimSpace(query))
	if queryLower == "" {
		return 0
	}
	titleLower := strings.ToLower(title)
	pathLower := strings.ToLower(sourcePath)
	contentLower := strings.ToLower(content)
	score := float64(strings.Count(contentLower, queryLower) * 12)
	score += float64(strings.Count(titleLower, queryLower) * 25)
	score += float64(strings.Count(pathLower, queryLower) * 8)
	for _, term := range uniqueTerms(strings.Fields(queryLower)) {
		if len(term) < 2 {
			continue
		}
		score += float64(strings.Count(contentLower, term) * 4)
		score += float64(strings.Count(titleLower, term) * 10)
		score += float64(strings.Count(pathLower, term) * 3)
	}
	return score / 100.0
}

func uniqueTerms(terms []string) []string {
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

func buildSnippet(query, content string) string {
	content = strings.TrimSpace(strings.Join(strings.Fields(content), " "))
	if len(content) <= 220 {
		return content
	}
	queryLower := strings.ToLower(strings.TrimSpace(query))
	contentLower := strings.ToLower(content)
	if queryLower != "" {
		if idx := strings.Index(contentLower, queryLower); idx >= 0 {
			start := idx - 80
			if start < 0 {
				start = 0
			}
			end := start + 220
			if end > len(content) {
				end = len(content)
			}
			return strings.TrimSpace(content[start:end])
		}
	}
	return content[:220]
}
