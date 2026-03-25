package vectorkb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
)

type Store struct {
	db   *bun.DB
	path string
}

// Open creates or opens the independent vector knowledge SQLite database.
func Open(cfg *config.Config) (*Store, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	return OpenPath(cfg.GetKnowledgeVectorDBPath())
}

// OpenPath opens a vector knowledge SQLite database at the provided path.
func OpenPath(dbPath string) (*Store, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("vector DB path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create vector DB directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	sqldb, err := sql.Open(sqliteshim.ShimName, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open vector SQLite database: %w", err)
	}
	sqldb.SetMaxOpenConns(1)
	sqldb.SetMaxIdleConns(1)

	db := bun.NewDB(sqldb, sqlitedialect.New())
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping vector SQLite database: %w", err)
	}

	store := &Store{db: db, path: dbPath}
	if err := store.applyPragmas(context.Background()); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) applyPragmas(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -8000",
		"PRAGMA temp_store = MEMORY",
	}
	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	return nil
}

// Close closes the vector knowledge DB connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path returns the opened vector DB path.
func (s *Store) Path() string {
	return s.path
}

// Migrate ensures the vector knowledge schema exists.
func (s *Store) Migrate(ctx context.Context) error {
	models := []interface{}{
		(*VectorDocument)(nil),
		(*VectorChunk)(nil),
		(*VectorEmbedding)(nil),
	}
	for _, model := range models {
		if _, err := s.db.NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			return err
		}
	}

	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_vk_documents_workspace_source_path ON vector_documents(workspace, source_path)",
		"CREATE INDEX IF NOT EXISTS idx_vk_documents_workspace_updated_at ON vector_documents(workspace, updated_at)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_vk_chunks_document_chunk_index ON vector_chunks(document_id, chunk_index)",
		"CREATE INDEX IF NOT EXISTS idx_vk_chunks_workspace_hash ON vector_chunks(workspace, content_hash)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_vk_embeddings_chunk_provider_model ON vector_embeddings(chunk_id, provider, model)",
		"CREATE INDEX IF NOT EXISTS idx_vk_embeddings_provider_model ON vector_embeddings(provider, model)",
	}
	for _, stmt := range indexes {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) getDocument(ctx context.Context, workspace, sourcePath string) (*VectorDocument, error) {
	var doc VectorDocument
	err := s.db.NewSelect().
		Model(&doc).
		Where("workspace = ?", workspace).
		Where("source_path = ?", sourcePath).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (s *Store) countDocumentEmbeddings(ctx context.Context, documentID int64, provider, model string) (int, error) {
	return s.db.NewSelect().
		TableExpr("vector_embeddings AS ve").
		Join("JOIN vector_chunks AS vc ON vc.id = ve.chunk_id").
		Where("vc.document_id = ?", documentID).
		Where("ve.provider = ?", provider).
		Where("ve.model = ?", model).
		Count(ctx)
}

func (s *Store) replaceDocument(ctx context.Context, document *VectorDocument, chunks []VectorChunk, embeddings []VectorEmbedding) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var existing VectorDocument
		err := tx.NewSelect().
			Model(&existing).
			Where("workspace = ?", document.Workspace).
			Where("source_path = ?", document.SourcePath).
			Limit(1).
			Scan(ctx)
		if err == nil {
			document.ID = existing.ID
			document.CreatedAt = existing.CreatedAt
			if _, err := tx.NewUpdate().
				Model(document).
				Column("source_type", "doc_type", "title", "content_hash", "status", "chunk_count", "metadata_json", "updated_at").
				WherePK().
				Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM vector_embeddings WHERE chunk_id IN (SELECT id FROM vector_chunks WHERE document_id = ?)", document.ID); err != nil {
				return err
			}
			if _, err := tx.NewDelete().Model((*VectorChunk)(nil)).Where("document_id = ?", document.ID).Exec(ctx); err != nil {
				return err
			}
		} else {
			if _, err := tx.NewInsert().Model(document).Exec(ctx); err != nil {
				return err
			}
		}

		for i := range chunks {
			chunks[i].DocumentID = document.ID
		}
		if len(chunks) > 0 {
			if _, err := tx.NewInsert().Model(&chunks).Exec(ctx); err != nil {
				return err
			}
		}

		if len(embeddings) > 0 {
			if err := tx.NewSelect().
				Model(&chunks).
				Where("document_id = ?", document.ID).
				OrderExpr("chunk_index ASC").
				Scan(ctx); err != nil {
				return err
			}
			for i := range embeddings {
				if i >= len(chunks) {
					break
				}
				embeddings[i].ChunkID = chunks[i].ID
			}
			if _, err := tx.NewInsert().Model(&embeddings).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetStats returns high-level vector DB statistics.
func (s *Store) GetStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{Path: s.path}
	var err error
	if stats.Documents, err = s.db.NewSelect().Model((*VectorDocument)(nil)).Count(ctx); err != nil {
		return nil, err
	}
	if stats.Chunks, err = s.db.NewSelect().Model((*VectorChunk)(nil)).Count(ctx); err != nil {
		return nil, err
	}
	if stats.Embeddings, err = s.db.NewSelect().Model((*VectorEmbedding)(nil)).Count(ctx); err != nil {
		return nil, err
	}

	if err := s.db.NewSelect().Model((*VectorDocument)(nil)).Distinct().Column("workspace").Order("workspace ASC").Scan(ctx, &stats.Workspaces); err != nil {
		return nil, err
	}
	if err := s.db.NewSelect().Model((*VectorEmbedding)(nil)).ColumnExpr("DISTINCT provider || ':' || model").OrderExpr("provider || ':' || model ASC").Scan(ctx, &stats.Models); err != nil {
		return nil, err
	}
	sort.Strings(stats.Workspaces)
	sort.Strings(stats.Models)
	return stats, nil
}
