package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/knowledge"
	"github.com/j3ssie/osmedeus/v5/internal/vectorkb"
)

// KnowledgeIngestRequest triggers local document ingestion into the knowledge base.
type KnowledgeIngestRequest struct {
	Path      string `json:"path"`
	Workspace string `json:"workspace,omitempty"`
	Recursive *bool  `json:"recursive,omitempty"`
}

// KnowledgeSearchRequest performs keyword search across ingested knowledge chunks.
type KnowledgeSearchRequest struct {
	Query           string   `json:"query"`
	Workspace       string   `json:"workspace,omitempty"`
	WorkspaceLayers []string `json:"workspace_layers,omitempty"`
	ScopeLayers     []string `json:"scope_layers,omitempty"`
	Limit           int      `json:"limit,omitempty"`
}

// KnowledgeVectorIndexRequest indexes a workspace into the standalone vector DB.
type KnowledgeVectorIndexRequest struct {
	Workspace string `json:"workspace,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	MaxChunks int    `json:"max_chunks,omitempty"`
}

// KnowledgeVectorSearchRequest performs semantic search against the standalone vector DB.
type KnowledgeVectorSearchRequest struct {
	Query           string   `json:"query"`
	Workspace       string   `json:"workspace,omitempty"`
	WorkspaceLayers []string `json:"workspace_layers,omitempty"`
	ScopeLayers     []string `json:"scope_layers,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Model           string   `json:"model,omitempty"`
	Limit           int      `json:"limit,omitempty"`
}

// KnowledgeLearnRequest synthesizes learned knowledge from an existing workspace.
type KnowledgeLearnRequest struct {
	Workspace          string `json:"workspace"`
	Scope              string `json:"scope,omitempty"`
	MaxAssets          int    `json:"max_assets,omitempty"`
	MaxVulnerabilities int    `json:"max_vulnerabilities,omitempty"`
	MaxRuns            int    `json:"max_runs,omitempty"`
	IncludeAIAnalysis  *bool  `json:"include_ai_analysis,omitempty"`
}

// IngestKnowledge handles knowledge-base ingestion requests.
func IngestKnowledge(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req KnowledgeIngestRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}
		if req.Path == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "path is required",
			})
		}

		recursive := true
		if req.Recursive != nil {
			recursive = *req.Recursive
		}

		ctx := context.Background()
		summary, err := knowledge.IngestPath(ctx, cfg, req.Path, req.Workspace, recursive)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
				"data":    summary,
			})
		}
		if err := maybeAutoIndexVectorKnowledge(ctx, cfg, summary.Workspace, "ingest"); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
				"data":    summary,
			})
		}

		return c.JSON(fiber.Map{
			"data":    summary,
			"message": "Knowledge ingestion completed",
		})
	}
}

// ListKnowledgeDocuments handles paginated document listing.
func ListKnowledgeDocuments(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		limit, _ := strconv.Atoi(c.Query("limit", "20"))
		if offset < 0 {
			offset = 0
		}
		if limit <= 0 {
			limit = 20
		}
		if limit > 1000 {
			limit = 1000
		}

		ctx := context.Background()
		result, err := knowledge.ListDocuments(ctx, c.Query("workspace"), offset, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"data": result.Data,
			"pagination": fiber.Map{
				"total":  result.TotalCount,
				"offset": result.Offset,
				"limit":  result.Limit,
			},
		})
	}
}

// SearchKnowledge handles keyword search requests.
func SearchKnowledge(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req KnowledgeSearchRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}
		if req.Query == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "query is required",
			})
		}
		if req.Limit <= 0 {
			req.Limit = 10
		}

		ctx := context.Background()
		results, err := knowledge.SearchWithOptions(ctx, knowledge.SearchOptions{
			Workspace:       req.Workspace,
			WorkspaceLayers: req.WorkspaceLayers,
			ScopeLayers:     req.ScopeLayers,
			Query:           req.Query,
			Limit:           req.Limit,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"query":     req.Query,
			"workspace": formatKnowledgeWorkspaceLabel(req.Workspace),
			"total":     len(results),
			"data":      results,
		})
	}
}

// LearnKnowledge builds a learned knowledge document from existing workspace findings.
func LearnKnowledge(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req KnowledgeLearnRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}
		if strings.TrimSpace(req.Workspace) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "workspace is required",
			})
		}

		ctx := context.Background()
		summary, err := knowledge.LearnWorkspace(ctx, cfg, knowledge.LearnOptions{
			Workspace:          req.Workspace,
			Scope:              req.Scope,
			MaxAssets:          req.MaxAssets,
			MaxVulnerabilities: req.MaxVulnerabilities,
			MaxRuns:            req.MaxRuns,
			IncludeAIAnalysis:  req.IncludeAIAnalysis,
		})
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		if err := maybeAutoIndexVectorKnowledge(ctx, cfg, summary.Workspace, "learn"); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
				"data":    summary,
			})
		}

		return c.JSON(fiber.Map{
			"data":    summary,
			"message": "Knowledge learning completed",
		})
	}
}

// IndexVectorKnowledge indexes main knowledge DB content into the standalone vector DB.
func IndexVectorKnowledge(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req KnowledgeVectorIndexRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}

		ctx := context.Background()
		summary, err := vectorkb.IndexWorkspace(ctx, cfg, vectorkb.IndexOptions{
			Workspace: strings.TrimSpace(req.Workspace),
			Provider:  strings.TrimSpace(req.Provider),
			Model:     strings.TrimSpace(req.Model),
			MaxChunks: req.MaxChunks,
		})
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"data":    summary,
			"message": "Vector knowledge indexing completed",
		})
	}
}

// SearchVectorKnowledge handles semantic search requests against the standalone vector DB.
func SearchVectorKnowledge(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req KnowledgeVectorSearchRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "Invalid request body",
			})
		}
		if strings.TrimSpace(req.Query) == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": "query is required",
			})
		}
		if req.Limit <= 0 {
			req.Limit = 10
		}

		ctx := context.Background()
		results, err := vectorkb.Search(ctx, cfg, vectorkb.SearchOptions{
			Workspace:       strings.TrimSpace(req.Workspace),
			WorkspaceLayers: req.WorkspaceLayers,
			ScopeLayers:     req.ScopeLayers,
			Provider:        strings.TrimSpace(req.Provider),
			Model:           strings.TrimSpace(req.Model),
			Limit:           req.Limit,
		}, req.Query)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"query":     req.Query,
			"workspace": formatKnowledgeWorkspaceLabel(req.Workspace),
			"total":     len(results),
			"data":      results,
		})
	}
}

// VectorKnowledgeStats returns high-level statistics for the standalone vector DB.
func VectorKnowledgeStats(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		store, err := vectorkb.Open(cfg)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}
		defer func() { _ = store.Close() }()

		if err := store.Migrate(ctx); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		stats, err := store.GetStats(ctx)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"data": stats,
		})
	}
}

func formatKnowledgeWorkspaceLabel(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "all"
	}
	return workspace
}

func maybeAutoIndexVectorKnowledge(ctx context.Context, cfg *config.Config, workspace, mode string) error {
	if cfg == nil || !cfg.IsKnowledgeVectorEnabled() {
		return nil
	}
	switch strings.TrimSpace(mode) {
	case "ingest":
		if !cfg.IsKnowledgeVectorAutoIndexOnIngest() {
			return nil
		}
	case "learn":
		if !cfg.IsKnowledgeVectorAutoIndexOnLearn() {
			return nil
		}
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	_, err := vectorkb.IndexWorkspace(ctx, cfg, vectorkb.IndexOptions{
		Workspace: workspace,
	})
	if err != nil {
		return fmt.Errorf("knowledge operation succeeded but vector auto-index failed for workspace %s: %w", workspace, err)
	}
	return nil
}
