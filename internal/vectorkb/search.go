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
	illm "github.com/j3ssie/osmedeus/v5/internal/llm"
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
	Provider      string `bun:"provider"`
	Model         string `bun:"model"`
	EmbeddingJSON string `bun:"embedding_json"`
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

	provider := strings.TrimSpace(opts.Provider)
	if provider == "" {
		provider = strings.TrimSpace(cfg.KnowledgeVector.DefaultProvider)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.KnowledgeVector.DefaultModel)
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
		ColumnExpr("ve.provider AS provider").
		ColumnExpr("ve.model AS model").
		ColumnExpr("ve.embedding_json AS embedding_json").
		Join("JOIN vector_chunks AS vc ON vc.id = ve.chunk_id").
		Join("JOIN vector_documents AS vd ON vd.id = vc.document_id").
		Where("ve.provider = ?", provider).
		Where("ve.model = ?", model)
	if workspace := strings.TrimSpace(opts.Workspace); workspace != "" {
		q = q.Where("vd.workspace = ?", workspace)
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
			RelevanceScore: (vectorScore * hybridWeight) + (keywordScore * keywordWeight),
			Type:           "vector_kb",
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].RelevanceScore == results[j].RelevanceScore {
			return results[i].ChunkID < results[j].ChunkID
		}
		return results[i].RelevanceScore > results[j].RelevanceScore
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
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
