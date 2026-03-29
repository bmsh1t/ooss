package vectorkb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/uptrace/bun"
)

type mainKnowledgeDocState struct {
	Workspace   string `bun:"workspace"`
	SourcePath  string `bun:"source_path"`
	ContentHash string `bun:"content_hash"`
	ChunkCount  int    `bun:"chunk_count"`
}

type vectorDocState struct {
	ID          int64  `bun:"id"`
	Workspace   string `bun:"workspace"`
	SourcePath  string `bun:"source_path"`
	ContentHash string `bun:"content_hash"`
	ChunkCount  int    `bun:"chunk_count"`
}

func Doctor(ctx context.Context, cfg *config.Config, opts DoctorOptions) (*DoctorReport, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	if database.GetDB() == nil {
		return nil, fmt.Errorf("database not connected")
	}

	providerConfig := inspectDoctorProviderConfig(cfg, strings.TrimSpace(opts.Provider), strings.TrimSpace(opts.Model))
	pathState := inspectDoctorDBPath(cfg.GetKnowledgeVectorDBPath())

	report := &DoctorReport{
		Path:                    cfg.GetKnowledgeVectorDBPath(),
		Workspace:               strings.TrimSpace(opts.Workspace),
		Provider:                providerConfig.Provider,
		Model:                   providerConfig.Model,
		VectorEnabled:           cfg.IsKnowledgeVectorEnabled(),
		DBPathExists:            pathState.Exists,
		DBPathWritable:          pathState.Writable,
		ProviderConfigured:      providerConfig.ProviderConfigured,
		ModelConfigured:         providerConfig.ModelConfigured,
		ProviderAvailable:       providerConfig.ProviderAvailable,
		ProviderEndpoint:        providerConfig.ProviderEndpoint,
		AvailableProviders:      providerConfig.AvailableProviders,
		AvailableProviderModels: providerConfig.AvailableProviderModels,
		SemanticStatus:          providerConfig.Status,
		SemanticStatusMessage:   providerConfig.Message,
	}

	if !cfg.IsKnowledgeVectorEnabled() {
		report.Issues = append(report.Issues, DoctorIssue{
			Type:    "vector_disabled",
			Message: "knowledge_vector.enabled is false",
		})
	}
	if !pathState.Writable {
		report.Issues = append(report.Issues, DoctorIssue{
			Type:    "vector_db_path_unwritable",
			Message: pathState.Message,
		})
		if report.SemanticStatus == "" || report.SemanticStatus == "ready" {
			report.SemanticStatus = "vector_db_unavailable"
			report.SemanticStatusMessage = pathState.Message
		}
	}
	if providerConfig.Issue != nil {
		report.Issues = append(report.Issues, *providerConfig.Issue)
	}

	mainDocs, err := loadMainKnowledgeDocs(ctx, strings.TrimSpace(opts.Workspace))
	if err != nil {
		return nil, err
	}

	for _, doc := range mainDocs {
		report.MainDocuments++
		report.MainChunks += doc.ChunkCount
	}

	store, err := Open(cfg)
	if err != nil {
		report.Issues = append(report.Issues, DoctorIssue{
			Type:    "vector_db_open_failed",
			Message: fmt.Sprintf("failed to open vector DB: %v", err),
		})
		if report.SemanticStatus == "" || report.SemanticStatus == "ready" {
			report.SemanticStatus = "vector_db_unavailable"
			report.SemanticStatusMessage = fmt.Sprintf("failed to open vector DB: %v", err)
		}
		finalizeDoctorReport(report)
		return report, nil
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		report.Issues = append(report.Issues, DoctorIssue{
			Type:    "vector_db_migrate_failed",
			Message: fmt.Sprintf("failed to migrate vector DB: %v", err),
		})
		if report.SemanticStatus == "" || report.SemanticStatus == "ready" {
			report.SemanticStatus = "vector_db_unavailable"
			report.SemanticStatusMessage = fmt.Sprintf("failed to migrate vector DB: %v", err)
		}
		finalizeDoctorReport(report)
		return report, nil
	}
	report.Path = store.Path()

	vectorDocs, err := store.loadVectorDocs(ctx, strings.TrimSpace(opts.Workspace))
	if err != nil {
		return nil, err
	}
	for _, doc := range vectorDocs {
		report.VectorDocuments++
		report.VectorChunks += doc.ChunkCount
	}
	if report.VectorEmbeddings, err = store.countEmbeddingsAll(ctx, strings.TrimSpace(opts.Workspace)); err != nil {
		return nil, err
	}
	if report.ProviderConfigured && report.ModelConfigured && report.ProviderAvailable {
		if report.SelectedEmbeddings, err = store.countEmbeddings(ctx, strings.TrimSpace(opts.Workspace), report.Provider, report.Model); err != nil {
			return nil, err
		}
	}
	if report.OrphanChunks, err = store.countOrphanChunks(ctx, strings.TrimSpace(opts.Workspace)); err != nil {
		return nil, err
	}
	if report.OrphanEmbeddings, err = store.countOrphanEmbeddings(ctx, strings.TrimSpace(opts.Workspace)); err != nil {
		return nil, err
	}

	vectorByKey := make(map[string]vectorDocState, len(vectorDocs))
	for _, doc := range vectorDocs {
		vectorByKey[doc.Workspace+"::"+doc.SourcePath] = doc
	}
	mainByKey := make(map[string]mainKnowledgeDocState, len(mainDocs))
	for _, doc := range mainDocs {
		mainByKey[doc.Workspace+"::"+doc.SourcePath] = doc
		if vectorDoc, ok := vectorByKey[doc.Workspace+"::"+doc.SourcePath]; !ok {
			report.MissingDocuments++
			report.Issues = append(report.Issues, DoctorIssue{
				Type:       "missing_vector_document",
				Workspace:  doc.Workspace,
				SourcePath: doc.SourcePath,
				Message:    "main knowledge document is not indexed in vector DB",
			})
		} else {
			if vectorDoc.ContentHash != doc.ContentHash || vectorDoc.ChunkCount != doc.ChunkCount {
				report.StaleDocuments++
				report.Issues = append(report.Issues, DoctorIssue{
					Type:       "stale_vector_document",
					Workspace:  doc.Workspace,
					SourcePath: doc.SourcePath,
					Message:    "vector document hash/chunk_count differs from main knowledge DB",
				})
			}
			if report.ProviderConfigured && report.ModelConfigured && report.ProviderAvailable {
				embedCount, countErr := store.countDocumentEmbeddings(ctx, vectorDoc.ID, report.Provider, report.Model)
				if countErr != nil {
					return nil, countErr
				}
				if embedCount != doc.ChunkCount {
					report.DocumentsMissingEmbeddings++
					report.Issues = append(report.Issues, DoctorIssue{
						Type:       "embedding_count_mismatch",
						Workspace:  doc.Workspace,
						SourcePath: doc.SourcePath,
						Message:    fmt.Sprintf("expected %d embeddings for %s/%s but found %d", doc.ChunkCount, report.Provider, report.Model, embedCount),
					})
				}
			}
		}
	}

	for _, doc := range vectorDocs {
		if _, ok := mainByKey[doc.Workspace+"::"+doc.SourcePath]; !ok {
			report.StaleDocuments++
			report.Issues = append(report.Issues, DoctorIssue{
				Type:       "vector_document_missing_in_main_db",
				Workspace:  doc.Workspace,
				SourcePath: doc.SourcePath,
				Message:    "vector DB contains a document that no longer exists in the main knowledge DB",
			})
		}
	}

	if report.OrphanChunks > 0 {
		report.Issues = append(report.Issues, DoctorIssue{
			Type:    "orphan_chunks",
			Message: fmt.Sprintf("vector DB contains %d chunks without parent documents", report.OrphanChunks),
		})
	}
	if report.OrphanEmbeddings > 0 {
		report.Issues = append(report.Issues, DoctorIssue{
			Type:    "orphan_embeddings",
			Message: fmt.Sprintf("vector DB contains %d embeddings without parent chunks", report.OrphanEmbeddings),
		})
	}

	finalizeDoctorReport(report)
	return report, nil
}

type doctorProviderConfig struct {
	Provider                string
	Model                   string
	ProviderConfigured      bool
	ModelConfigured         bool
	ProviderAvailable       bool
	ProviderEndpoint        string
	AvailableProviders      []string
	AvailableProviderModels []string
	Status                  string
	Message                 string
	Issue                   *DoctorIssue
}

type doctorPathState struct {
	Exists   bool
	Writable bool
	Message  string
}

func inspectDoctorProviderConfig(cfg *config.Config, providerOverride, modelOverride string) doctorProviderConfig {
	result := doctorProviderConfig{
		Provider:                strings.TrimSpace(providerOverride),
		Model:                   strings.TrimSpace(modelOverride),
		AvailableProviders:      listDoctorProviders(cfg),
		AvailableProviderModels: listDoctorProviderModels(cfg),
		Status:                  "ready",
	}
	if result.Provider == "" {
		result.Provider = strings.TrimSpace(cfg.GetKnowledgeVectorProvider())
	}
	if result.Model == "" {
		result.Model = strings.TrimSpace(cfg.GetKnowledgeVectorModel(result.Provider))
	}
	result.ProviderConfigured = result.Provider != ""
	result.ModelConfigured = result.Model != ""

	switch {
	case !result.ProviderConfigured:
		result.Status = "provider_not_configured"
		result.Message = "knowledge_vector.default_provider and embeddings_config.provider are empty and no --provider override was supplied"
		result.Issue = &DoctorIssue{Type: result.Status, Message: result.Message}
		return result
	case !result.ModelConfigured:
		result.Status = "model_not_bound"
		result.Message = "knowledge_vector.default_model is empty and no provider-specific model could be resolved from embeddings_config or llm.llm_providers"
		result.Issue = &DoctorIssue{Type: result.Status, Message: result.Message}
		return result
	}

	provider, source, err := cfg.ResolveEmbeddingProvider(result.Provider)
	if err != nil {
		result.Status = "provider_not_available"
		result.Message = err.Error()
		result.Issue = &DoctorIssue{Type: result.Status, Message: result.Message}
		return result
	}
	result.ProviderAvailable = true
	result.ProviderEndpoint = strings.TrimSpace(provider.BaseURL)
	if result.ProviderEndpoint == "" {
		result.Status = "provider_endpoint_missing"
		result.Message = fmt.Sprintf("embedding provider %q resolved from %s has no base_url/api_url configured", result.Provider, source)
		result.Issue = &DoctorIssue{Type: result.Status, Message: result.Message}
		return result
	}

	result.Message = fmt.Sprintf("provider %s with model %s is configured via %s", result.Provider, result.Model, source)
	return result
}

func inspectDoctorDBPath(path string) doctorPathState {
	path = strings.TrimSpace(path)
	if path == "" {
		return doctorPathState{
			Writable: false,
			Message:  "knowledge_vector.db_path resolves to an empty path",
		}
	}

	state := doctorPathState{}
	if _, err := os.Stat(path); err == nil {
		state.Exists = true
	}

	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		state.Writable = false
		state.Message = fmt.Sprintf("failed to create vector DB directory %s: %v", dir, err)
		return state
	}

	tempFile, err := os.CreateTemp(dir, ".vectorkb-doctor-*")
	if err != nil {
		state.Writable = false
		state.Message = fmt.Sprintf("vector DB directory %s is not writable: %v", dir, err)
		return state
	}
	_ = tempFile.Close()
	_ = os.Remove(tempFile.Name())

	state.Writable = true
	state.Message = fmt.Sprintf("vector DB path %s is writable", path)
	return state
}

func listDoctorProviders(cfg *config.Config) []string {
	return cfg.ListEmbeddingProviders()
}

func listDoctorProviderModels(cfg *config.Config) []string {
	return cfg.ListEmbeddingProviderModels()
}

func finalizeDoctorReport(report *DoctorReport) {
	if report == nil {
		return
	}
	switch {
	case report.SemanticStatus == "" || report.SemanticStatus == "ready":
		switch {
		case report.MainDocuments > 0 && (report.MissingDocuments > 0 || report.DocumentsMissingEmbeddings > 0 || report.SelectedEmbeddings == 0):
			report.SemanticStatus = "index_missing"
			report.SemanticStatusMessage = "main knowledge documents exist but the selected provider/model does not have a complete vector index"
		case report.StaleDocuments > 0 || report.OrphanChunks > 0 || report.OrphanEmbeddings > 0:
			report.SemanticStatus = "consistency_issues"
			report.SemanticStatusMessage = "vector DB contains stale or orphaned records"
		default:
			report.SemanticStatus = "ready"
			if report.MainDocuments == 0 {
				report.SemanticStatusMessage = "vector stack is configured; no main knowledge documents were found for this workspace"
			} else {
				report.SemanticStatusMessage = "semantic vector retrieval is ready"
			}
		}
	}

	report.SemanticSearchReady = report.SemanticStatus == "ready"
	report.Healthy = report.SemanticSearchReady &&
		report.MissingDocuments == 0 &&
		report.StaleDocuments == 0 &&
		report.DocumentsMissingEmbeddings == 0 &&
		report.OrphanChunks == 0 &&
		report.OrphanEmbeddings == 0
}

func Purge(ctx context.Context, cfg *config.Config, workspace string) (*PurgeSummary, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	if database.GetDB() == nil {
		return nil, fmt.Errorf("database not connected")
	}

	store, err := Open(cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		return nil, err
	}

	workspace = strings.TrimSpace(workspace)
	mainDocs, err := loadMainKnowledgeDocs(ctx, workspace)
	if err != nil {
		return nil, err
	}
	vectorDocs, err := store.loadVectorDocs(ctx, workspace)
	if err != nil {
		return nil, err
	}

	mainByKey := make(map[string]mainKnowledgeDocState, len(mainDocs))
	for _, doc := range mainDocs {
		mainByKey[doc.Workspace+"::"+doc.SourcePath] = doc
	}

	var staleIDs []int64
	for _, doc := range vectorDocs {
		mainDoc, ok := mainByKey[doc.Workspace+"::"+doc.SourcePath]
		if !ok || mainDoc.ContentHash != doc.ContentHash || mainDoc.ChunkCount != doc.ChunkCount {
			staleIDs = append(staleIDs, doc.ID)
		}
	}

	summary := &PurgeSummary{
		Path:      store.Path(),
		Workspace: workspace,
	}
	if len(staleIDs) > 0 {
		docCount, chunkCount, embeddingCount, err := store.deleteDocumentIDs(ctx, staleIDs)
		if err != nil {
			return nil, err
		}
		summary.RemovedDocuments += docCount
		summary.RemovedChunks += chunkCount
		summary.RemovedEmbeddings += embeddingCount
		summary.RemovedStaleDocuments += docCount
	}

	removedEmbeddings, err := store.deleteOrphanEmbeddings(ctx, workspace)
	if err != nil {
		return nil, err
	}
	summary.RemovedEmbeddings += removedEmbeddings
	summary.RemovedOrphanEmbeddings += removedEmbeddings

	removedOrphanEmbeddings, removedOrphanChunks, err := store.deleteOrphanChunks(ctx, workspace)
	if err != nil {
		return nil, err
	}
	summary.RemovedEmbeddings += removedOrphanEmbeddings
	summary.RemovedOrphanEmbeddings += removedOrphanEmbeddings
	summary.RemovedChunks += removedOrphanChunks
	summary.RemovedOrphanChunks += removedOrphanChunks

	return summary, nil
}

func Rebuild(ctx context.Context, cfg *config.Config, opts IndexOptions) (*RebuildSummary, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	if database.GetDB() == nil {
		return nil, fmt.Errorf("database not connected")
	}

	store, err := Open(cfg)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		return nil, err
	}

	workspace := strings.TrimSpace(opts.Workspace)
	var purge *PurgeSummary
	if workspace == "" {
		purge, err = store.clearAll(ctx)
		if err != nil {
			return nil, err
		}
	} else {
		purge, err = store.deleteWorkspace(ctx, workspace)
		if err != nil {
			return nil, err
		}
	}

	if opts.MaxChunks <= 0 {
		opts.MaxChunks = maxInt()
	}
	indexSummary, err := IndexWorkspace(ctx, cfg, opts)
	if err != nil {
		return nil, err
	}

	return &RebuildSummary{
		Workspace: workspace,
		Provider:  indexSummary.Provider,
		Model:     indexSummary.Model,
		Purge:     purge,
		Index:     indexSummary,
	}, nil
}

func Sync(ctx context.Context, cfg *config.Config, opts IndexOptions) (*SyncSummary, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}
	if database.GetDB() == nil {
		return nil, fmt.Errorf("database not connected")
	}

	workspace := strings.TrimSpace(opts.Workspace)
	purgeSummary, err := Purge(ctx, cfg, workspace)
	if err != nil {
		return nil, err
	}

	indexSummary, err := IndexWorkspace(ctx, cfg, opts)
	if err != nil {
		return nil, err
	}

	return &SyncSummary{
		Workspace: workspace,
		Provider:  indexSummary.Provider,
		Model:     indexSummary.Model,
		Purge:     purgeSummary,
		Index:     indexSummary,
	}, nil
}

func loadMainKnowledgeDocs(ctx context.Context, workspace string) ([]mainKnowledgeDocState, error) {
	var docs []mainKnowledgeDocState
	q := database.GetDB().NewSelect().
		Model((*database.KnowledgeDocument)(nil)).
		Column("workspace", "source_path", "content_hash", "chunk_count")
	if workspace != "" {
		q = q.Where("workspace = ?", workspace)
	}
	if err := q.Scan(ctx, &docs); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return docs, nil
}

func (s *Store) loadVectorDocs(ctx context.Context, workspace string) ([]vectorDocState, error) {
	var docs []vectorDocState
	q := s.db.NewSelect().
		Model((*VectorDocument)(nil)).
		Column("id", "workspace", "source_path", "content_hash", "chunk_count")
	if workspace != "" {
		q = q.Where("workspace = ?", workspace)
	}
	if err := q.Scan(ctx, &docs); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return docs, nil
}

func (s *Store) countEmbeddings(ctx context.Context, workspace, provider, model string) (int, error) {
	q := s.db.NewSelect().
		TableExpr("vector_embeddings AS ve").
		Join("JOIN vector_chunks AS vc ON vc.id = ve.chunk_id").
		Join("JOIN vector_documents AS vd ON vd.id = vc.document_id").
		Where("ve.provider = ?", provider).
		Where("ve.model = ?", model)
	if workspace != "" {
		q = q.Where("vd.workspace = ?", workspace)
	}
	return q.Count(ctx)
}

func (s *Store) countEmbeddingsAll(ctx context.Context, workspace string) (int, error) {
	q := s.db.NewSelect().
		TableExpr("vector_embeddings AS ve").
		Join("JOIN vector_chunks AS vc ON vc.id = ve.chunk_id").
		Join("JOIN vector_documents AS vd ON vd.id = vc.document_id")
	if workspace != "" {
		q = q.Where("vd.workspace = ?", workspace)
	}
	return q.Count(ctx)
}

func (s *Store) countOrphanChunks(ctx context.Context, workspace string) (int, error) {
	q := s.db.NewSelect().
		TableExpr("vector_chunks AS vc").
		Join("LEFT JOIN vector_documents AS vd ON vd.id = vc.document_id").
		Where("vd.id IS NULL")
	if workspace != "" {
		q = q.Where("vc.workspace = ?", workspace)
	}
	return q.Count(ctx)
}

func (s *Store) countOrphanEmbeddings(ctx context.Context, workspace string) (int, error) {
	q := s.db.NewSelect().
		TableExpr("vector_embeddings AS ve").
		Join("LEFT JOIN vector_chunks AS vc ON vc.id = ve.chunk_id").
		Where("vc.id IS NULL")
	return q.Count(ctx)
}

func (s *Store) deleteDocumentIDs(ctx context.Context, documentIDs []int64) (int, int, int, error) {
	if len(documentIDs) == 0 {
		return 0, 0, 0, nil
	}

	chunkCount, err := s.db.NewSelect().
		Model((*VectorChunk)(nil)).
		Where("document_id IN (?)", bun.In(documentIDs)).
		Count(ctx)
	if err != nil {
		return 0, 0, 0, err
	}

	embeddingCount, err := s.db.NewSelect().
		TableExpr("vector_embeddings AS ve").
		Join("JOIN vector_chunks AS vc ON vc.id = ve.chunk_id").
		Where("vc.document_id IN (?)", bun.In(documentIDs)).
		Count(ctx)
	if err != nil {
		return 0, 0, 0, err
	}

	return len(documentIDs), chunkCount, embeddingCount, s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM vector_embeddings WHERE chunk_id IN (SELECT id FROM vector_chunks WHERE document_id IN (?))", bun.In(documentIDs)); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*VectorChunk)(nil)).Where("document_id IN (?)", bun.In(documentIDs)).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*VectorDocument)(nil)).Where("id IN (?)", bun.In(documentIDs)).Exec(ctx); err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) deleteWorkspace(ctx context.Context, workspace string) (*PurgeSummary, error) {
	docs, err := s.loadVectorDocs(ctx, workspace)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(docs))
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}

	summary := &PurgeSummary{
		Path:      s.path,
		Workspace: workspace,
	}
	docCount, chunkCount, embeddingCount, err := s.deleteDocumentIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	summary.RemovedDocuments = docCount
	summary.RemovedChunks = chunkCount
	summary.RemovedEmbeddings = embeddingCount
	summary.RemovedStaleDocuments = docCount
	return summary, nil
}

func (s *Store) clearAll(ctx context.Context) (*PurgeSummary, error) {
	stats, err := s.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.NewDelete().Model((*VectorEmbedding)(nil)).Where("1=1").Exec(ctx); err != nil {
		return nil, err
	}
	if _, err := s.db.NewDelete().Model((*VectorChunk)(nil)).Where("1=1").Exec(ctx); err != nil {
		return nil, err
	}
	if _, err := s.db.NewDelete().Model((*VectorDocument)(nil)).Where("1=1").Exec(ctx); err != nil {
		return nil, err
	}
	return &PurgeSummary{
		Path:                  s.path,
		RemovedDocuments:      stats.Documents,
		RemovedChunks:         stats.Chunks,
		RemovedEmbeddings:     stats.Embeddings,
		RemovedStaleDocuments: stats.Documents,
	}, nil
}

func (s *Store) deleteOrphanEmbeddings(ctx context.Context, workspace string) (int, error) {
	count, err := s.countOrphanEmbeddings(ctx, workspace)
	if err != nil || count == 0 {
		return count, err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM vector_embeddings WHERE chunk_id NOT IN (SELECT id FROM vector_chunks)")
	return count, err
}

func (s *Store) deleteOrphanChunks(ctx context.Context, workspace string) (int, int, error) {
	var orphanChunkIDs []int64
	q := s.db.NewSelect().
		TableExpr("vector_chunks AS vc").
		Column("vc.id").
		Join("LEFT JOIN vector_documents AS vd ON vd.id = vc.document_id").
		Where("vd.id IS NULL")
	if workspace != "" {
		q = q.Where("vc.workspace = ?", workspace)
	}
	if err := q.Scan(ctx, &orphanChunkIDs); err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}
	if len(orphanChunkIDs) == 0 {
		return 0, 0, nil
	}

	embeddingCount, err := s.db.NewSelect().
		Model((*VectorEmbedding)(nil)).
		Where("chunk_id IN (?)", bun.In(orphanChunkIDs)).
		Count(ctx)
	if err != nil {
		return 0, 0, err
	}

	if _, err := s.db.NewDelete().Model((*VectorEmbedding)(nil)).Where("chunk_id IN (?)", bun.In(orphanChunkIDs)).Exec(ctx); err != nil {
		return 0, 0, err
	}
	if _, err := s.db.NewDelete().Model((*VectorChunk)(nil)).Where("id IN (?)", bun.In(orphanChunkIDs)).Exec(ctx); err != nil {
		return 0, 0, err
	}
	return embeddingCount, len(orphanChunkIDs), nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
