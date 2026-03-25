package vectorkb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	illm "github.com/j3ssie/osmedeus/v5/internal/llm"
)

type documentGroup struct {
	workspace  string
	sourcePath string
	docType    string
	title      string
	docHash    string
	sourceType string
	rows       []database.KnowledgeChunkExportRow
}

// IndexWorkspace indexes knowledge chunks from the main DB into the independent vector DB.
func IndexWorkspace(ctx context.Context, cfg *config.Config, opts IndexOptions) (*IndexSummary, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	if !cfg.IsKnowledgeVectorEnabled() {
		return nil, fmt.Errorf("knowledge vector DB is disabled")
	}
	if database.GetDB() == nil {
		return nil, fmt.Errorf("database not connected")
	}

	provider := strings.TrimSpace(opts.Provider)
	if provider == "" {
		provider = strings.TrimSpace(cfg.KnowledgeVector.DefaultProvider)
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.KnowledgeVector.DefaultModel)
	}
	if provider == "" || model == "" {
		return nil, fmt.Errorf("vector provider/model is not configured")
	}

	maxChunks := opts.MaxChunks
	if maxChunks <= 0 {
		maxChunks = cfg.KnowledgeVector.MaxIndexingChunks
	}
	if maxChunks <= 0 {
		maxChunks = 5000
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = cfg.GetKnowledgeVectorBatchSize()
	}

	store, err := Open(cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		return nil, err
	}

	groups, chunkCount, err := loadDocumentGroups(ctx, strings.TrimSpace(opts.Workspace), maxChunks)
	if err != nil {
		return nil, err
	}

	summary := &IndexSummary{
		Workspace:     strings.TrimSpace(opts.Workspace),
		Provider:      provider,
		Model:         model,
		DocumentsSeen: len(groups),
		ChunksSeen:    chunkCount,
	}

	for _, group := range groups {
		existing, err := store.getDocument(ctx, group.workspace, group.sourcePath)
		if err == nil && existing.ContentHash == group.docHash && existing.ChunkCount == len(group.rows) {
			count, countErr := store.countDocumentEmbeddings(ctx, existing.ID, provider, model)
			if countErr == nil && count == len(group.rows) {
				summary.DocumentsSkipped++
				continue
			}
		}

		inputs := make([]string, 0, len(group.rows))
		chunks := make([]VectorChunk, 0, len(group.rows))
		for _, row := range group.rows {
			chunks = append(chunks, VectorChunk{
				Workspace:   row.Workspace,
				ChunkIndex:  row.ChunkIndex,
				Section:     row.Section,
				Content:     row.Content,
				ContentHash: row.ChunkHash,
				Metadata:    row.Metadata,
				CreatedAt:   time.Now(),
			})
			inputs = append(inputs, row.Content)
		}

		var embeddings [][]float64
		for start := 0; start < len(inputs); start += batchSize {
			end := start + batchSize
			if end > len(inputs) {
				end = len(inputs)
			}
			batchEmbeddings, _, err := illm.GenerateEmbeddings(ctx, cfg, inputs[start:end], model)
			if err != nil {
				return summary, fmt.Errorf("failed to generate embeddings for %s: %w", group.sourcePath, err)
			}
			embeddings = append(embeddings, batchEmbeddings...)
		}
		if len(embeddings) != len(chunks) {
			return summary, fmt.Errorf("embedding count mismatch for %s", group.sourcePath)
		}

		doc := &VectorDocument{
			Workspace:   group.workspace,
			SourcePath:  group.sourcePath,
			SourceType:  group.sourceType,
			DocType:     group.docType,
			Title:       group.title,
			ContentHash: group.docHash,
			Status:      "ready",
			ChunkCount:  len(chunks),
			Metadata:    "",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		vectorEmbeddings := make([]VectorEmbedding, 0, len(embeddings))
		for _, item := range embeddings {
			payload, err := json.Marshal(item)
			if err != nil {
				return summary, err
			}
			vectorEmbeddings = append(vectorEmbeddings, VectorEmbedding{
				Provider:      provider,
				Model:         model,
				Dimension:     len(item),
				EmbeddingJSON: string(payload),
				EmbeddingHash: hashEmbedding(payload),
				CreatedAt:     time.Now(),
			})
		}

		if err := store.replaceDocument(ctx, doc, chunks, vectorEmbeddings); err != nil {
			return summary, err
		}
		summary.DocumentsIndexed++
		summary.ChunksEmbedded += len(chunks)
	}

	return summary, nil
}

func loadDocumentGroups(ctx context.Context, workspace string, maxChunks int) ([]documentGroup, int, error) {
	limit := 500
	if maxChunks < limit {
		limit = maxChunks
	}
	if limit <= 0 {
		limit = 500
	}

	groupMap := make(map[string]*documentGroup)
	totalChunks := 0
	for offset := 0; totalChunks < maxChunks; offset += limit {
		pageLimit := limit
		if remaining := maxChunks - totalChunks; remaining < pageLimit {
			pageLimit = remaining
		}
		rows, err := database.ListKnowledgeChunksPage(ctx, workspace, pageLimit, offset)
		if err != nil {
			return nil, totalChunks, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			key := row.Workspace + "::" + row.SourcePath
			group := groupMap[key]
			if group == nil {
				group = &documentGroup{
					workspace:  row.Workspace,
					sourcePath: row.SourcePath,
					docType:    row.DocType,
					title:      row.Title,
					docHash:    row.DocHash,
					sourceType: "file",
				}
				groupMap[key] = group
			}
			group.rows = append(group.rows, row)
			totalChunks++
			if totalChunks >= maxChunks {
				break
			}
		}
		if len(rows) < pageLimit {
			break
		}
	}

	groups := make([]documentGroup, 0, len(groupMap))
	for _, group := range groupMap {
		groups = append(groups, *group)
	}
	return groups, totalChunks, nil
}

func hashEmbedding(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
