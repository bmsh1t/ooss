package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
)

var (
	db         *bun.DB
	lazyConfig *config.Config
	lazyMu     sync.Mutex
)

// Connect establishes a database connection based on configuration
func Connect(cfg *config.Config) (*bun.DB, error) {
	switch {
	case cfg.IsPostgres():
		return connectPostgres(cfg)
	case cfg.IsSQLite():
		return connectSQLite(cfg)
	default:
		return nil, fmt.Errorf("unsupported database engine: %s", cfg.Database.DBEngine)
	}
}

// connectSQLite establishes a SQLite connection
func connectSQLite(cfg *config.Config) (*bun.DB, error) {
	dbPath := cfg.GetDBPath()

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Build DSN with pragmas for better performance
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)

	sqldb, err := sql.Open(sqliteshim.ShimName, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// SQLite connection pooling settings
	sqldb.SetMaxOpenConns(1) // SQLite only supports one writer at a time
	sqldb.SetMaxIdleConns(1)

	db = bun.NewDB(sqldb, sqlitedialect.New())

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping SQLite database: %w", err)
	}

	// Apply additional performance pragmas
	applySQLitePerformancePragmas(context.Background())

	// Initialize cache after successful connection
	_ = InitCache(nil)

	return db, nil
}

// applySQLitePerformancePragmas applies additional SQLite performance settings
func applySQLitePerformancePragmas(ctx context.Context) {
	if db == nil {
		return
	}

	pragmas := []string{
		"PRAGMA synchronous = NORMAL", // Faster writes, safe with WAL
		"PRAGMA cache_size = -10000",  // 10MB cache (negative = KB)
		"PRAGMA temp_store = MEMORY",  // Temp tables in memory
		"PRAGMA mmap_size = 83886080", // 80MB memory-mapped I/O
	}

	for _, pragma := range pragmas {
		_, _ = db.ExecContext(ctx, pragma)
	}
}

// connectPostgres establishes a PostgreSQL connection
func connectPostgres(cfg *config.Config) (*bun.DB, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.Database.Username,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.DBName,
		getSSLMode(cfg.Database.SSLMode),
	)

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))

	// PostgreSQL connection pool settings
	sqldb.SetMaxOpenConns(25)                  // Limit concurrent connections
	sqldb.SetMaxIdleConns(5)                   // Keep some connections ready
	sqldb.SetConnMaxLifetime(time.Hour)        // Recycle connections periodically
	sqldb.SetConnMaxIdleTime(10 * time.Minute) // Close idle connections

	db = bun.NewDB(sqldb, pgdialect.New())

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	return db, nil
}

// getSSLMode returns the SSL mode or default
func getSSLMode(mode string) string {
	if mode == "" {
		return "disable"
	}
	return mode
}

// SetLazyConfig stores a config for deferred database connection.
// The actual connection is established on the first GetDB() call.
func SetLazyConfig(cfg *config.Config) {
	lazyMu.Lock()
	defer lazyMu.Unlock()
	lazyConfig = cfg
}

// GetDB returns the global database instance, connecting lazily if
// SetLazyConfig was called but Connect has not yet been invoked.
func GetDB() *bun.DB {
	if db != nil {
		return db
	}
	lazyMu.Lock()
	defer lazyMu.Unlock()
	if db != nil {
		return db
	}
	if lazyConfig == nil {
		return nil
	}
	cfg := lazyConfig
	lazyConfig = nil // prevent retry loops on persistent failure
	if _, err := Connect(cfg); err != nil {
		return nil
	}
	return db
}

// SetDB sets the global database instance (for testing)
func SetDB(newDB *bun.DB) {
	db = newDB
}

// Close closes the database connection and cache
func Close() error {
	// Close cache first
	closeGlobalCache()

	if db != nil {
		return db.Close()
	}
	return nil
}

// Migrate runs database migrations
func Migrate(ctx context.Context) error {
	models := []interface{}{
		(*Run)(nil),
		(*StepResult)(nil),
		(*Artifact)(nil),
		(*Asset)(nil),
		(*EventLog)(nil),
		(*Schedule)(nil),
		(*Workspace)(nil),
		(*WorkflowMeta)(nil),
		(*Vulnerability)(nil),
		(*AttackChainReport)(nil),
		(*AssetDiffSnapshot)(nil),
		(*VulnDiffSnapshot)(nil),
		(*AgentSession)(nil),
		(*KnowledgeDocument)(nil),
		(*KnowledgeChunk)(nil),
		(*Campaign)(nil),
	}

	for _, model := range models {
		_, err := db.NewCreateTable().
			Model(model).
			IfNotExists().
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Create indexes for Run table
	if err := createRunIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for Asset table
	if err := createAssetIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for EventLog table
	if err := createEventLogIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for WorkflowMeta table
	if err := createWorkflowMetaIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for AttackChainReport table
	if err := createAttackChainIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for Workspace table
	if err := createWorkspaceIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for KnowledgeDocument and KnowledgeChunk tables
	if err := createKnowledgeIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for Campaign table
	if err := createCampaignIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for AssetDiffSnapshot table
	if err := createAssetDiffIndexes(ctx); err != nil {
		return err
	}

	// Create indexes for VulnDiffSnapshot table
	if err := createVulnDiffIndexes(ctx); err != nil {
		return err
	}

	// Add current_pid column to runs table if it doesn't exist (for existing databases)
	if err := addRunsPIDColumn(ctx); err != nil {
		return err
	}

	// Add optional column to artifacts table if it doesn't exist (for existing databases)
	if err := addArtifactsOptionalColumn(ctx); err != nil {
		return err
	}

	// Add remarks column to assets table if it doesn't exist (for existing databases)
	if err := addAssetRemarksColumn(ctx); err != nil {
		return err
	}

	// Add language column to assets table if it doesn't exist (for existing databases)
	if err := addAssetLanguageColumn(ctx); err != nil {
		return err
	}

	// Add blob_content column to assets table if it doesn't exist (for existing databases)
	if err := addAssetBlobContentColumn(ctx); err != nil {
		return err
	}

	// Add external_url column to assets table if it doesn't exist (for existing databases)
	if err := addAssetExternalURLColumn(ctx); err != nil {
		return err
	}

	// Add size column to assets table if it doesn't exist (for existing databases)
	if err := addAssetSizeColumn(ctx); err != nil {
		return err
	}

	// Add loc column to assets table if it doesn't exist (for existing databases)
	if err := addAssetLOCColumn(ctx); err != nil {
		return err
	}

	// Add CDN/WAF classification columns to assets table if they don't exist
	if err := addAssetCDNColumns(ctx); err != nil {
		return err
	}

	// Add port tracking columns to assets table if they don't exist
	if err := addAssetPortColumns(ctx); err != nil {
		return err
	}

	// Add queue metadata columns to runs table if they don't exist
	if err := addRunsQueueColumns(ctx); err != nil {
		return err
	}

	// Add webhook columns to runs table if they don't exist
	if err := addRunsWebhookColumns(ctx); err != nil {
		return err
	}

	// Add vulnerability lifecycle columns to vulnerabilities table if they don't exist
	if err := addVulnerabilityLifecycleColumns(ctx); err != nil {
		return err
	}

	// Add vulnerability evidence/dedup columns to vulnerabilities table if they don't exist
	if err := addVulnerabilityEvidenceColumns(ctx); err != nil {
		return err
	}

	// Backfill and reconcile vulnerability fingerprints before unique indexes.
	if err := reconcileVulnerabilityFingerprints(ctx); err != nil {
		return err
	}

	// Create indexes for Vulnerability table after lifecycle/evidence columns exist.
	if err := createVulnerabilityIndexes(ctx); err != nil {
		return err
	}

	// Add attack-chain queue metrics columns if they don't exist
	if err := addAttackChainMetricsColumns(ctx); err != nil {
		return err
	}

	return nil
}

// createRunIndexes creates indexes for the runs table
func createRunIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_runs_run_uuid ON runs(run_uuid)",
		"CREATE INDEX IF NOT EXISTS idx_runs_run_group_id ON runs(run_group_id)",
		"CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status)",
		"CREATE INDEX IF NOT EXISTS idx_runs_workflow_name ON runs(workflow_name)",
		"CREATE INDEX IF NOT EXISTS idx_runs_target ON runs(target)",
		"CREATE INDEX IF NOT EXISTS idx_runs_is_queued ON runs(is_queued)",
		"CREATE INDEX IF NOT EXISTS idx_runs_webhook_uuid ON runs(webhook_uuid)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createKnowledgeIndexes creates indexes for local knowledge base tables.
func createKnowledgeIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_documents_workspace_source_path ON knowledge_documents(workspace, source_path)",
		"CREATE INDEX IF NOT EXISTS idx_knowledge_documents_workspace_updated_at ON knowledge_documents(workspace, updated_at)",
		"CREATE INDEX IF NOT EXISTS idx_knowledge_documents_content_hash ON knowledge_documents(content_hash)",
		"CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_document_id ON knowledge_chunks(document_id)",
		"CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_workspace ON knowledge_chunks(workspace)",
		"CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_workspace_chunk_index ON knowledge_chunks(workspace, chunk_index)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create knowledge index: %w", err)
		}
	}

	return nil
}

// createCampaignIndexes creates indexes for campaign batch operations.
func createCampaignIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_campaigns_name_created_at ON campaigns(name, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_campaigns_status ON campaigns(status)",
		"CREATE INDEX IF NOT EXISTS idx_campaigns_workflow_name ON campaigns(workflow_name)",
		"CREATE INDEX IF NOT EXISTS idx_campaigns_updated_at ON campaigns(updated_at)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create campaign index: %w", err)
		}
	}

	return nil
}

// addRunsPIDColumn adds the current_pid column to runs table for existing databases
// This is safe to run multiple times - it checks if column exists first
func addRunsPIDColumn(ctx context.Context) error {
	// SQLite and PostgreSQL have different syntax for adding columns
	// We use a simple approach: try to add the column and ignore "already exists" errors
	_, err := db.ExecContext(ctx, "ALTER TABLE runs ADD COLUMN current_pid INTEGER DEFAULT 0")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		// Ignore "column already exists" errors (SQLite and PostgreSQL have different messages)
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add current_pid column: %w", err)
	}
	return nil
}

// addArtifactsOptionalColumn adds the optional column to artifacts table for existing databases
// This is safe to run multiple times - it checks if column exists first
func addArtifactsOptionalColumn(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "ALTER TABLE artifacts ADD COLUMN optional BOOLEAN DEFAULT FALSE")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add optional column: %w", err)
	}
	return nil
}

// addAssetRemarksColumn adds the remarks column to assets table for existing databases
func addAssetRemarksColumn(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "ALTER TABLE assets ADD COLUMN remarks JSON")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add remarks column: %w", err)
	}
	return nil
}

// addAssetLanguageColumn adds the language column to assets table for existing databases
func addAssetLanguageColumn(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "ALTER TABLE assets ADD COLUMN language TEXT DEFAULT ''")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add language column: %w", err)
	}
	return nil
}

// addAssetBlobContentColumn adds the blob_content column to assets table for existing databases
func addAssetBlobContentColumn(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "ALTER TABLE assets ADD COLUMN blob_content TEXT DEFAULT ''")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add blob_content column: %w", err)
	}
	return nil
}

// addAssetSizeColumn adds the size column to assets table for existing databases
func addAssetSizeColumn(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "ALTER TABLE assets ADD COLUMN size INTEGER DEFAULT 0")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add size column: %w", err)
	}
	return nil
}

// addAssetLOCColumn adds the loc column to assets table for existing databases
func addAssetLOCColumn(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "ALTER TABLE assets ADD COLUMN loc INTEGER DEFAULT 0")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add loc column: %w", err)
	}
	return nil
}

// addAssetExternalURLColumn adds the external_url column to assets table for existing databases
func addAssetExternalURLColumn(ctx context.Context) error {
	_, err := db.ExecContext(ctx, "ALTER TABLE assets ADD COLUMN external_url TEXT DEFAULT ''")
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "duplicate column") ||
			strings.Contains(errStr, "already exists") ||
			strings.Contains(errStr, "sqlstate 42701") {
			return nil
		}
		return fmt.Errorf("failed to add external_url column: %w", err)
	}
	return nil
}

// addAssetCDNColumns adds CDN/WAF classification columns to assets table for existing databases
func addAssetCDNColumns(ctx context.Context) error {
	columns := []string{
		"ALTER TABLE assets ADD COLUMN is_cdn BOOLEAN DEFAULT FALSE",
		"ALTER TABLE assets ADD COLUMN is_cloud BOOLEAN DEFAULT FALSE",
		"ALTER TABLE assets ADD COLUMN is_waf BOOLEAN DEFAULT FALSE",
	}
	for _, ddl := range columns {
		_, err := db.ExecContext(ctx, ddl)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate column") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "sqlstate 42701") {
				continue
			}
			return fmt.Errorf("failed to add CDN column: %w", err)
		}
	}
	return nil
}

// addAssetPortColumns adds port tracking columns to assets table for existing databases
func addAssetPortColumns(ctx context.Context) error {
	columns := []string{
		"ALTER TABLE assets ADD COLUMN open_ports TEXT DEFAULT '[]'",
		"ALTER TABLE assets ADD COLUMN port_json_data TEXT DEFAULT ''",
	}
	for _, ddl := range columns {
		_, err := db.ExecContext(ctx, ddl)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate column") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "sqlstate 42701") {
				continue
			}
			return fmt.Errorf("failed to add port column: %w", err)
		}
	}
	return nil
}

// addRunsQueueColumns adds queue metadata columns to runs table for existing databases
func addRunsQueueColumns(ctx context.Context) error {
	columns := []string{
		"ALTER TABLE runs ADD COLUMN is_queued BOOLEAN DEFAULT FALSE",
		"ALTER TABLE runs ADD COLUMN input_is_file BOOLEAN DEFAULT FALSE",
		"ALTER TABLE runs ADD COLUMN input_file_path TEXT DEFAULT ''",
	}
	for _, ddl := range columns {
		_, err := db.ExecContext(ctx, ddl)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate column") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "sqlstate 42701") {
				continue
			}
			return fmt.Errorf("failed to add queue column: %w", err)
		}
	}
	return nil
}

// addRunsWebhookColumns adds webhook columns to runs table for existing databases
func addRunsWebhookColumns(ctx context.Context) error {
	columns := []string{
		"ALTER TABLE runs ADD COLUMN webhook_uuid TEXT DEFAULT ''",
		"ALTER TABLE runs ADD COLUMN webhook_auth_key TEXT DEFAULT ''",
	}
	for _, ddl := range columns {
		_, err := db.ExecContext(ctx, ddl)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate column") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "sqlstate 42701") {
				continue
			}
			return fmt.Errorf("failed to add webhook column: %w", err)
		}
	}
	return nil
}

// createAssetIndexes creates indexes for the assets table
func createAssetIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_assets_workspace ON assets(workspace)",
		"CREATE INDEX IF NOT EXISTS idx_assets_asset_value ON assets(asset_value)",
		"CREATE INDEX IF NOT EXISTS idx_assets_status_code ON assets(status_code)",
		"CREATE INDEX IF NOT EXISTS idx_assets_host_ip ON assets(host_ip)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_assets_unique ON assets(workspace, asset_value, url)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createEventLogIndexes creates indexes for the event_logs table
func createEventLogIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_event_logs_topic ON event_logs(topic)",
		"CREATE INDEX IF NOT EXISTS idx_event_logs_workspace ON event_logs(workspace)",
		"CREATE INDEX IF NOT EXISTS idx_event_logs_run_id ON event_logs(run_id)",
		"CREATE INDEX IF NOT EXISTS idx_event_logs_created_at ON event_logs(created_at)",
		// Composite index for unprocessed event queries (ListUnprocessed, Search with processed filter)
		"CREATE INDEX IF NOT EXISTS idx_event_logs_processed_created ON event_logs(processed, created_at)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createWorkflowMetaIndexes creates indexes for the workflow_meta table
func createWorkflowMetaIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_workflow_meta_kind ON workflow_meta(kind)",
		"CREATE INDEX IF NOT EXISTS idx_workflow_meta_checksum ON workflow_meta(checksum)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createVulnerabilityIndexes creates indexes for the vulnerabilities table
func createVulnerabilityIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_workspace ON vulnerabilities(workspace)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_severity ON vulnerabilities(severity)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_confidence ON vulnerabilities(confidence)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_asset_value ON vulnerabilities(asset_value)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_fingerprint_key ON vulnerabilities(fingerprint_key)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_vulnerabilities_workspace_fingerprint_unique ON vulnerabilities(workspace, fingerprint_key) WHERE fingerprint_key <> ''",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_vuln_status ON vulnerabilities(vuln_status)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_retest_status ON vulnerabilities(retest_status)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_retest_run_uuid ON vulnerabilities(retest_run_uuid)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_first_seen_at ON vulnerabilities(first_seen_at)",
		"CREATE INDEX IF NOT EXISTS idx_vulnerabilities_last_seen_at ON vulnerabilities(last_seen_at)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createAttackChainIndexes creates indexes for attack-chain reports.
func createAttackChainIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_attack_chain_reports_workspace_source_path ON attack_chain_reports(workspace, source_path)",
		"CREATE INDEX IF NOT EXISTS idx_attack_chain_reports_workspace ON attack_chain_reports(workspace)",
		"CREATE INDEX IF NOT EXISTS idx_attack_chain_reports_run_uuid ON attack_chain_reports(run_uuid)",
		"CREATE INDEX IF NOT EXISTS idx_attack_chain_reports_queue_hits ON attack_chain_reports(queue_hits)",
		"CREATE INDEX IF NOT EXISTS idx_attack_chain_reports_verified_hits ON attack_chain_reports(verified_hits)",
		"CREATE INDEX IF NOT EXISTS idx_attack_chain_reports_last_queued_at ON attack_chain_reports(last_queued_at)",
		"CREATE INDEX IF NOT EXISTS idx_attack_chain_reports_updated_at ON attack_chain_reports(updated_at)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create attack chain index: %w", err)
		}
	}

	return nil
}

// addVulnerabilityLifecycleColumns adds lifecycle and review metadata columns to vulnerabilities.
func addVulnerabilityLifecycleColumns(ctx context.Context) error {
	columns := []string{
		"ALTER TABLE vulnerabilities ADD COLUMN vuln_status TEXT DEFAULT 'new'",
		"ALTER TABLE vulnerabilities ADD COLUMN source_run_uuid TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN ai_verdict TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN ai_summary TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN analyst_verdict TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN analyst_notes TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN retest_status TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN retest_run_uuid TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN attack_chain_ref TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN related_assets TEXT DEFAULT '[]'",
		"ALTER TABLE vulnerabilities ADD COLUMN report_refs TEXT DEFAULT '[]'",
		"ALTER TABLE vulnerabilities ADD COLUMN verified_at TIMESTAMP NULL",
		"ALTER TABLE vulnerabilities ADD COLUMN closed_at TIMESTAMP NULL",
	}

	for _, ddl := range columns {
		_, err := db.ExecContext(ctx, ddl)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate column") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "sqlstate 42701") {
				continue
			}
			return fmt.Errorf("failed to add vulnerability lifecycle column: %w", err)
		}
	}
	return nil
}

func addVulnerabilityEvidenceColumns(ctx context.Context) error {
	columns := []string{
		"ALTER TABLE vulnerabilities ADD COLUMN fingerprint_key TEXT DEFAULT ''",
		"ALTER TABLE vulnerabilities ADD COLUMN evidence_version INTEGER DEFAULT 1",
		"ALTER TABLE vulnerabilities ADD COLUMN evidence_history_json TEXT DEFAULT '[]'",
		"ALTER TABLE vulnerabilities ADD COLUMN first_seen_at TIMESTAMP NULL",
	}

	for _, ddl := range columns {
		_, err := db.ExecContext(ctx, ddl)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate column") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "sqlstate 42701") {
				continue
			}
			return fmt.Errorf("failed to add vulnerability evidence column: %w", err)
		}
	}
	return nil
}

func addAttackChainMetricsColumns(ctx context.Context) error {
	columns := []string{
		"ALTER TABLE attack_chain_reports ADD COLUMN queue_hits INTEGER DEFAULT 0",
		"ALTER TABLE attack_chain_reports ADD COLUMN verified_hits INTEGER DEFAULT 0",
		"ALTER TABLE attack_chain_reports ADD COLUMN last_queued_at TIMESTAMP NULL",
	}

	for _, ddl := range columns {
		_, err := db.ExecContext(ctx, ddl)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "duplicate column") ||
				strings.Contains(errStr, "already exists") ||
				strings.Contains(errStr, "sqlstate 42701") {
				continue
			}
			return fmt.Errorf("failed to add attack chain metrics column: %w", err)
		}
	}
	return nil
}

// createWorkspaceIndexes creates indexes for the workspaces table
func createWorkspaceIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_workspaces_data_source ON workspaces(data_source)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createAssetDiffIndexes creates indexes for the asset_diffs table
func createAssetDiffIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_asset_diffs_workspace ON asset_diffs(workspace_name)",
		"CREATE INDEX IF NOT EXISTS idx_asset_diffs_created_at ON asset_diffs(created_at)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// createVulnDiffIndexes creates indexes for the vuln_diffs table
func createVulnDiffIndexes(ctx context.Context) error {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_vuln_diffs_workspace ON vuln_diffs(workspace_name)",
		"CREATE INDEX IF NOT EXISTS idx_vuln_diffs_created_at ON vuln_diffs(created_at)",
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// Transaction wraps a function in a database transaction
func Transaction(ctx context.Context, fn func(ctx context.Context, tx bun.Tx) error) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(ctx, tx)
	})
}

// IsSQLite returns true if the current database is SQLite
func IsSQLite() bool {
	if db == nil {
		return false
	}
	return db.Dialect().Name().String() == "sqlite"
}

// IsPostgres returns true if the current database is PostgreSQL
func IsPostgres() bool {
	if db == nil {
		return false
	}
	return db.Dialect().Name().String() == "pg"
}
