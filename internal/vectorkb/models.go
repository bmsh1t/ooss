package vectorkb

import "time"

// VectorDocument stores normalized document metadata in the independent vector DB.
type VectorDocument struct {
	ID          int64     `bun:"id,pk,autoincrement" json:"id"`
	Workspace   string    `bun:"workspace,notnull" json:"workspace"`
	SourcePath  string    `bun:"source_path,notnull" json:"source_path"`
	SourceType  string    `bun:"source_type,notnull,default:'file'" json:"source_type"`
	DocType     string    `bun:"doc_type,notnull" json:"doc_type"`
	Title       string    `bun:"title,notnull" json:"title"`
	ContentHash string    `bun:"content_hash,notnull" json:"content_hash"`
	Status      string    `bun:"status,notnull,default:'ready'" json:"status"`
	ChunkCount  int       `bun:"chunk_count,notnull,default:0" json:"chunk_count"`
	Metadata    string    `bun:"metadata_json" json:"metadata,omitempty"`
	CreatedAt   time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt   time.Time `bun:"updated_at,notnull,default:current_timestamp" json:"updated_at"`
}

// VectorChunk stores searchable chunk content in the vector DB.
type VectorChunk struct {
	ID          int64     `bun:"id,pk,autoincrement" json:"id"`
	DocumentID  int64     `bun:"document_id,notnull" json:"document_id"`
	Workspace   string    `bun:"workspace,notnull" json:"workspace"`
	ChunkIndex  int       `bun:"chunk_index,notnull" json:"chunk_index"`
	Section     string    `bun:"section" json:"section,omitempty"`
	Content     string    `bun:"content,notnull" json:"content"`
	ContentHash string    `bun:"content_hash,notnull" json:"content_hash"`
	Metadata    string    `bun:"metadata_json" json:"metadata,omitempty"`
	CreatedAt   time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at"`
}

// VectorEmbedding stores a generated embedding for a chunk.
type VectorEmbedding struct {
	ID            int64     `bun:"id,pk,autoincrement" json:"id"`
	ChunkID       int64     `bun:"chunk_id,notnull" json:"chunk_id"`
	Provider      string    `bun:"provider,notnull" json:"provider"`
	Model         string    `bun:"model,notnull" json:"model"`
	Dimension     int       `bun:"dimension,notnull" json:"dimension"`
	EmbeddingJSON string    `bun:"embedding_json,notnull" json:"embedding_json"`
	EmbeddingHash string    `bun:"embedding_hash,notnull" json:"embedding_hash"`
	CreatedAt     time.Time `bun:"created_at,notnull,default:current_timestamp" json:"created_at"`
}

// IndexOptions controls vector indexing behavior.
type IndexOptions struct {
	Workspace string
	Provider  string
	Model     string
	BatchSize int
	MaxChunks int
}

// IndexSummary reports vector indexing results.
type IndexSummary struct {
	Workspace        string `json:"workspace,omitempty"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	DocumentsSeen    int    `json:"documents_seen"`
	DocumentsIndexed int    `json:"documents_indexed"`
	DocumentsSkipped int    `json:"documents_skipped"`
	ChunksSeen       int    `json:"chunks_seen"`
	ChunksEmbedded   int    `json:"chunks_embedded"`
}

// SearchOptions controls vector search behavior.
type SearchOptions struct {
	Workspace       string
	WorkspaceLayers []string
	ScopeLayers     []string
	Provider        string
	Model           string
	Limit           int
	HybridWeight    float64
	KeywordWeight   float64
}

// SearchHit is a normalized vector knowledge result.
type SearchHit struct {
	DocumentID     int64   `json:"document_id"`
	ChunkID        int64   `json:"chunk_id"`
	Workspace      string  `json:"workspace"`
	Title          string  `json:"title"`
	SourcePath     string  `json:"source_path"`
	DocType        string  `json:"doc_type"`
	Section        string  `json:"section,omitempty"`
	Content        string  `json:"content"`
	Snippet        string  `json:"snippet"`
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	RelevanceScore float64 `json:"relevance_score"`
	VectorScore    float64 `json:"vector_score"`
	KeywordScore   float64 `json:"keyword_score"`
	Type           string  `json:"type"`
}

// Stats reports high-level vector knowledge DB statistics.
type Stats struct {
	Path       string   `json:"path"`
	Documents  int      `json:"documents"`
	Chunks     int      `json:"chunks"`
	Embeddings int      `json:"embeddings"`
	Workspaces []string `json:"workspaces,omitempty"`
	Models     []string `json:"models,omitempty"`
}

// DoctorOptions controls vector DB consistency checks.
type DoctorOptions struct {
	Workspace string
	Provider  string
	Model     string
}

// DoctorIssue describes a concrete consistency problem.
type DoctorIssue struct {
	Type       string `json:"type"`
	Workspace  string `json:"workspace,omitempty"`
	SourcePath string `json:"source_path,omitempty"`
	Message    string `json:"message"`
}

// DoctorReport summarizes vector DB health against the main knowledge DB.
type DoctorReport struct {
	Path                       string        `json:"path"`
	Workspace                  string        `json:"workspace,omitempty"`
	Provider                   string        `json:"provider"`
	Model                      string        `json:"model"`
	MainDocuments              int           `json:"main_documents"`
	MainChunks                 int           `json:"main_chunks"`
	VectorDocuments            int           `json:"vector_documents"`
	VectorChunks               int           `json:"vector_chunks"`
	VectorEmbeddings           int           `json:"vector_embeddings"`
	MissingDocuments           int           `json:"missing_documents"`
	StaleDocuments             int           `json:"stale_documents"`
	DocumentsMissingEmbeddings int           `json:"documents_missing_embeddings"`
	OrphanChunks               int           `json:"orphan_chunks"`
	OrphanEmbeddings           int           `json:"orphan_embeddings"`
	Healthy                    bool          `json:"healthy"`
	Issues                     []DoctorIssue `json:"issues,omitempty"`
}

// PurgeSummary reports what stale/orphan data was removed from the vector DB.
type PurgeSummary struct {
	Path                    string `json:"path"`
	Workspace               string `json:"workspace,omitempty"`
	RemovedDocuments        int    `json:"removed_documents"`
	RemovedChunks           int    `json:"removed_chunks"`
	RemovedEmbeddings       int    `json:"removed_embeddings"`
	RemovedStaleDocuments   int    `json:"removed_stale_documents"`
	RemovedOrphanChunks     int    `json:"removed_orphan_chunks"`
	RemovedOrphanEmbeddings int    `json:"removed_orphan_embeddings"`
}

// RebuildSummary reports the results of a rebuild operation.
type RebuildSummary struct {
	Workspace string        `json:"workspace,omitempty"`
	Provider  string        `json:"provider"`
	Model     string        `json:"model"`
	Purge     *PurgeSummary `json:"purge,omitempty"`
	Index     *IndexSummary `json:"index,omitempty"`
}

// SyncSummary reports the result of a targeted purge + incremental reindex cycle.
type SyncSummary struct {
	Workspace string        `json:"workspace,omitempty"`
	Provider  string        `json:"provider"`
	Model     string        `json:"model"`
	Purge     *PurgeSummary `json:"purge,omitempty"`
	Index     *IndexSummary `json:"index,omitempty"`
}
