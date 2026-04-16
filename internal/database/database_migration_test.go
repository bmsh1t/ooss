package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/stretchr/testify/require"
)

func TestMigrateUpgradesLegacyAttackChainReportMetricsColumnsBeforeIndexes(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "legacy-attack-chain.sqlite")
	cfg := &config.Config{
		BaseFolder: tmpDir,
		Database: config.DatabaseConfig{
			DBEngine: "sqlite",
			DBPath:   dbPath,
		},
	}

	_, err := Connect(cfg)
	require.NoError(t, err)
	defer func() {
		_ = Close()
		SetDB(nil)
	}()

	ctx := context.Background()
	_, err = GetDB().ExecContext(ctx, `
		CREATE TABLE attack_chain_reports (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			workspace VARCHAR NOT NULL,
			target VARCHAR,
			run_uuid VARCHAR,
			source_path VARCHAR NOT NULL,
			source_hash VARCHAR NOT NULL,
			status VARCHAR NOT NULL DEFAULT 'ready',
			total_chains INTEGER DEFAULT 0,
			critical_chains INTEGER DEFAULT 0,
			high_impact_chains INTEGER DEFAULT 0,
			most_likely_entry_points json,
			attack_chains_json VARCHAR,
			critical_paths_json VARCHAR,
			defense_recommendations json,
			mermaid_path VARCHAR,
			text_path VARCHAR,
			created_at TIMESTAMP NOT NULL DEFAULT current_timestamp,
			updated_at TIMESTAMP NOT NULL DEFAULT current_timestamp
		)
	`)
	require.NoError(t, err)

	require.NoError(t, Migrate(ctx))

	type tableInfo struct {
		Name string `bun:"name"`
	}
	var columns []tableInfo
	err = GetDB().NewRaw("SELECT name FROM pragma_table_info('attack_chain_reports')").Scan(ctx, &columns)
	require.NoError(t, err)

	columnNames := make([]string, 0, len(columns))
	for _, column := range columns {
		columnNames = append(columnNames, column.Name)
	}
	require.Contains(t, columnNames, "queue_hits")
	require.Contains(t, columnNames, "verified_hits")
	require.Contains(t, columnNames, "last_queued_at")

	type indexInfo struct {
		Name string `bun:"name"`
	}
	var indexes []indexInfo
	err = GetDB().NewRaw("SELECT name FROM pragma_index_list('attack_chain_reports')").Scan(ctx, &indexes)
	require.NoError(t, err)

	indexNames := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		indexNames = append(indexNames, idx.Name)
	}
	require.Contains(t, indexNames, "idx_attack_chain_reports_queue_hits")
	require.Contains(t, indexNames, "idx_attack_chain_reports_verified_hits")
	require.Contains(t, indexNames, "idx_attack_chain_reports_last_queued_at")
}
