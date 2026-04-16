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
	Query               string   `json:"query"`
	Workspace           string   `json:"workspace,omitempty"`
	WorkspaceLayers     []string `json:"workspace_layers,omitempty"`
	ScopeLayers         []string `json:"scope_layers,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	MinSourceConfidence float64  `json:"min_source_confidence,omitempty"`
	SampleTypes         []string `json:"sample_types,omitempty"`
	ExcludeSampleTypes  []string `json:"exclude_sample_types,omitempty"`
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
	Query               string   `json:"query"`
	Workspace           string   `json:"workspace,omitempty"`
	WorkspaceLayers     []string `json:"workspace_layers,omitempty"`
	ScopeLayers         []string `json:"scope_layers,omitempty"`
	Provider            string   `json:"provider,omitempty"`
	Model               string   `json:"model,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	MinSourceConfidence float64  `json:"min_source_confidence,omitempty"`
	SampleTypes         []string `json:"sample_types,omitempty"`
	ExcludeSampleTypes  []string `json:"exclude_sample_types,omitempty"`
	EnableRerank        bool     `json:"enable_rerank,omitempty"`
	RerankProvider      string   `json:"rerank_provider,omitempty"`
	RerankModel         string   `json:"rerank_model,omitempty"`
	RerankTopN          int      `json:"rerank_top_n,omitempty"`
	RerankMaxCandidates int      `json:"rerank_max_candidates,omitempty"`
}

type vectorAutoIndexResult struct {
	Attempted bool
	Indexed   bool
	Error     string
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
		autoIndexResult := maybeAutoIndexVectorKnowledge(ctx, cfg, summary.Workspace, "ingest")
		applyVectorAutoIndexToIngestSummary(summary, autoIndexResult)

		return c.JSON(buildKnowledgeWriteResponse("Knowledge ingestion completed", summary, autoIndexResult))
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
			Workspace:           req.Workspace,
			WorkspaceLayers:     req.WorkspaceLayers,
			ScopeLayers:         req.ScopeLayers,
			Query:               req.Query,
			Limit:               req.Limit,
			MinSourceConfidence: req.MinSourceConfidence,
			SampleTypes:         req.SampleTypes,
			ExcludeSampleTypes:  req.ExcludeSampleTypes,
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
		indexWorkspace := strings.TrimSpace(summary.StorageWorkspace)
		if indexWorkspace == "" {
			indexWorkspace = strings.TrimSpace(summary.Workspace)
		}
		autoIndexResult := maybeAutoIndexVectorKnowledge(ctx, cfg, indexWorkspace, "learn")
		applyVectorAutoIndexToLearnSummary(summary, autoIndexResult)

		return c.JSON(buildKnowledgeWriteResponse("Knowledge learning completed", summary, autoIndexResult))
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
			Workspace:           strings.TrimSpace(req.Workspace),
			WorkspaceLayers:     req.WorkspaceLayers,
			ScopeLayers:         req.ScopeLayers,
			Provider:            strings.TrimSpace(req.Provider),
			Model:               strings.TrimSpace(req.Model),
			Limit:               req.Limit,
			MinSourceConfidence: req.MinSourceConfidence,
			SampleTypes:         req.SampleTypes,
			ExcludeSampleTypes:  req.ExcludeSampleTypes,
			EnableRerank:        req.EnableRerank,
			RerankProvider:      strings.TrimSpace(req.RerankProvider),
			RerankModel:         strings.TrimSpace(req.RerankModel),
			RerankTopN:          req.RerankTopN,
			RerankMaxCandidates: req.RerankMaxCandidates,
		}, req.Query)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		rankingSource := knowledgeVectorRankingSource(results)

		return c.JSON(fiber.Map{
			"query":           req.Query,
			"workspace":       formatKnowledgeWorkspaceLabel(req.Workspace),
			"total":           len(results),
			"ranking_source":  rankingSource,
			"rerank_applied":  rankingSource == "rerank",
			"rerank_provider": strings.TrimSpace(req.RerankProvider),
			"rerank_model":    strings.TrimSpace(req.RerankModel),
			"data":            results,
		})
	}
}

func knowledgeVectorRankingSource(results []vectorkb.SearchHit) string {
	if len(results) == 0 {
		return "hybrid"
	}
	return results[0].RankingSource
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

// VectorKnowledgeDoctor reports vector readiness, provider binding, and consistency state.
func VectorKnowledgeDoctor(cfg *config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := context.Background()
		report, err := vectorkb.Doctor(ctx, cfg, vectorkb.DoctorOptions{
			Workspace:     strings.TrimSpace(c.Query("workspace")),
			Provider:      strings.TrimSpace(c.Query("provider")),
			Model:         strings.TrimSpace(c.Query("model")),
			ProbeProvider: c.QueryBool("probe", false),
		})
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error":   true,
				"message": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"data": report,
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

func maybeAutoIndexVectorKnowledge(ctx context.Context, cfg *config.Config, workspace, mode string) *vectorAutoIndexResult {
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
	result := &vectorAutoIndexResult{Attempted: true}
	_, err := vectorkb.IndexWorkspace(ctx, cfg, vectorkb.IndexOptions{
		Workspace: workspace,
	})
	if err != nil {
		result.Error = fmt.Sprintf("failed for workspace %s: %v", workspace, err)
		return result
	}
	result.Indexed = true
	return result
}

func applyVectorAutoIndexToIngestSummary(summary *knowledge.IngestSummary, result *vectorAutoIndexResult) {
	if summary == nil || result == nil || !result.Attempted {
		return
	}
	indexed := result.Indexed
	summary.VectorIndexed = &indexed
	summary.VectorError = strings.TrimSpace(result.Error)
}

func applyVectorAutoIndexToLearnSummary(summary *knowledge.LearnSummary, result *vectorAutoIndexResult) {
	if summary == nil || result == nil || !result.Attempted {
		return
	}
	indexed := result.Indexed
	summary.VectorIndexed = &indexed
	summary.VectorError = strings.TrimSpace(result.Error)
}

func buildKnowledgeWriteResponse(message string, data interface{}, result *vectorAutoIndexResult) fiber.Map {
	response := fiber.Map{
		"data":    data,
		"message": strings.TrimSpace(message),
	}
	if result == nil || !result.Attempted || result.Indexed {
		return response
	}
	response["message"] = strings.TrimSpace(message) + " with vector auto-index warning"
	response["warning"] = "Vector auto-index failed: " + strings.TrimSpace(result.Error)
	return response
}
