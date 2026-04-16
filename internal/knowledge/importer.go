package knowledge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/config"
)

const securitySQLiteImportType = "security-sqlite"

// ImportOptions controls importing external knowledge into the local KB.
type ImportOptions struct {
	Type      string `json:"type"`
	Path      string `json:"path"`
	Workspace string `json:"workspace"`
}

// ImportSummary reports the result of an external KB import job.
type ImportSummary struct {
	Type      string   `json:"type"`
	Path      string   `json:"path"`
	Workspace string   `json:"workspace"`
	Documents int      `json:"documents"`
	Chunks    int      `json:"chunks"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
}

// Import dispatches an external knowledge source into the main KB schema.
func Import(ctx context.Context, cfg *config.Config, opts ImportOptions) (*ImportSummary, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration not loaded")
	}

	importType := strings.ToLower(strings.TrimSpace(opts.Type))
	if importType == "" {
		return nil, fmt.Errorf("type is required")
	}

	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	summary := &ImportSummary{
		Type:      importType,
		Path:      absPath,
		Workspace: normalizeWorkspace(workspace),
	}

	switch importType {
	case securitySQLiteImportType:
		return importSecuritySQLite(ctx, ImportOptions{
			Type:      importType,
			Path:      absPath,
			Workspace: workspace,
		}, summary)
	default:
		return nil, fmt.Errorf("unsupported importer type: %s", importType)
	}
}

func appendImportError(summary *ImportSummary, format string, args ...interface{}) {
	if summary == nil {
		return
	}
	summary.Failed++
	if len(summary.Errors) >= 20 {
		return
	}
	summary.Errors = append(summary.Errors, fmt.Sprintf(format, args...))
}
