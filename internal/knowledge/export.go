package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/database"
)

// ExportSummary reports the result of exporting knowledge chunks for downstream indexing.
type ExportSummary struct {
	Workspace string `json:"workspace,omitempty"`
	Output    string `json:"output"`
	Documents int    `json:"documents"`
	Chunks    int    `json:"chunks"`
}

// ExportChunks writes normalized knowledge chunks into a line-oriented corpus for vector indexing.
func ExportChunks(ctx context.Context, workspace, outputPath string, limit int) (*ExportSummary, error) {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return nil, fmt.Errorf("output path is required")
	}

	rows, err := database.ListKnowledgeChunks(ctx, strings.TrimSpace(workspace), limit)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, err
	}

	var builder strings.Builder
	documents := make(map[int64]struct{})
	for _, row := range rows {
		documents[row.DocumentID] = struct{}{}
		line := formatKnowledgeExportLine(row)
		if line == "" {
			continue
		}
		builder.WriteString(line)
		builder.WriteString("\n")
	}

	if err := os.WriteFile(outputPath, []byte(builder.String()), 0o644); err != nil {
		return nil, err
	}

	return &ExportSummary{
		Workspace: strings.TrimSpace(workspace),
		Output:    outputPath,
		Documents: len(documents),
		Chunks:    len(rows),
	}, nil
}

func formatKnowledgeExportLine(row database.KnowledgeChunkExportRow) string {
	content := collapseKnowledgeWhitespace(row.Content)
	if content == "" {
		return ""
	}

	var prefix []string
	prefix = append(prefix, "kb")
	if trimmed := strings.TrimSpace(row.Workspace); trimmed != "" {
		prefix = append(prefix, "workspace="+trimmed)
	}
	if trimmed := strings.TrimSpace(row.DocType); trimmed != "" {
		prefix = append(prefix, "type="+trimmed)
	}
	if trimmed := collapseKnowledgeWhitespace(row.Title); trimmed != "" {
		prefix = append(prefix, "title="+trimmed)
	}
	if trimmed := collapseKnowledgeWhitespace(row.Section); trimmed != "" {
		prefix = append(prefix, "section="+trimmed)
	}
	if trimmed := collapseKnowledgeWhitespace(row.SourcePath); trimmed != "" {
		prefix = append(prefix, "path="+trimmed)
	}

	return "[" + strings.Join(prefix, "][") + "] " + content
}

func collapseKnowledgeWhitespace(input string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(input), " "))
}
