package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/terminal"
	"github.com/j3ssie/osmedeus/v5/internal/vectorkb"
	"github.com/spf13/cobra"
)

var (
	kbVectorProvider      string
	kbVectorModel         string
	kbProbeProvider       bool
	kbEnableRerank        bool
	kbRerankProvider      string
	kbRerankModel         string
	kbRerankTopN          int
	kbRerankMaxCandidates int
)

var kbVectorCmd = &cobra.Command{
	Use:   "vector",
	Short: "Manage the independent vector knowledge database",
}

var kbVectorIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Index knowledge chunks into the vector knowledge database",
	RunE:  runKBVectorIndex,
}

var kbVectorSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search the vector knowledge database",
	RunE:  runKBVectorSearch,
}

var kbVectorStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show vector knowledge database statistics",
	RunE:  runKBVectorStats,
}

var kbVectorDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check vector knowledge database consistency",
	RunE:  runKBVectorDoctor,
}

var kbVectorRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild vector knowledge database content from the main knowledge DB",
	RunE:  runKBVectorRebuild,
}

var kbVectorPurgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge stale and orphaned vector knowledge records",
	RunE:  runKBVectorPurge,
}

var kbVectorSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize vector knowledge DB with the main knowledge DB",
	RunE:  runKBVectorSync,
}

func init() {
	kbVectorIndexCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty indexes all workspaces)")
	kbVectorIndexCmd.Flags().StringVar(&kbVectorProvider, "provider", "", "embedding provider override")
	kbVectorIndexCmd.Flags().StringVar(&kbVectorModel, "model", "", "embedding model override")
	kbVectorIndexCmd.Flags().IntVar(&kbLimit, "limit", 0, "maximum chunks to index (0 uses config default)")

	kbVectorSearchCmd.Flags().StringVar(&kbQuery, "query", "", "semantic query")
	kbVectorSearchCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty searches all workspaces)")
	kbVectorSearchCmd.Flags().StringVar(&kbVectorProvider, "provider", "", "embedding provider override")
	kbVectorSearchCmd.Flags().StringVar(&kbVectorModel, "model", "", "embedding model override")
	kbVectorSearchCmd.Flags().IntVar(&kbLimit, "limit", 10, "maximum number of results")
	kbVectorSearchCmd.Flags().StringSliceVar(&kbWorkspaceLayers, "workspace-layer", nil, "preferred workspace layers in ranking order")
	kbVectorSearchCmd.Flags().StringSliceVar(&kbScopeLayers, "scope-layer", nil, "preferred scope layers in ranking order")
	kbVectorSearchCmd.Flags().Float64Var(&kbMinSourceConfidence, "min-confidence", 0, "skip learned results below this source confidence")
	kbVectorSearchCmd.Flags().StringSliceVar(&kbSampleTypes, "sample-type", nil, "include only specific learned sample types")
	kbVectorSearchCmd.Flags().StringSliceVar(&kbExcludeSampleTypes, "exclude-sample-type", nil, "exclude specific learned sample types")
	kbVectorSearchCmd.Flags().BoolVar(&kbEnableRerank, "rerank", false, "apply rerank on top of hybrid vector results")
	kbVectorSearchCmd.Flags().StringVar(&kbRerankProvider, "rerank-provider", "", "rerank provider override")
	kbVectorSearchCmd.Flags().StringVar(&kbRerankModel, "rerank-model", "", "rerank model override")
	kbVectorSearchCmd.Flags().IntVar(&kbRerankTopN, "rerank-top-n", 0, "maximum number of reranked results (0 uses config default)")
	kbVectorSearchCmd.Flags().IntVar(&kbRerankMaxCandidates, "rerank-max-candidates", 0, "maximum recall candidates sent to rerank (0 uses config default)")
	_ = kbVectorSearchCmd.MarkFlagRequired("query")

	kbVectorDoctorCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty checks all workspaces)")
	kbVectorDoctorCmd.Flags().StringVar(&kbVectorProvider, "provider", "", "embedding provider override")
	kbVectorDoctorCmd.Flags().StringVar(&kbVectorModel, "model", "", "embedding model override")
	kbVectorDoctorCmd.Flags().BoolVar(&kbProbeProvider, "probe-provider", false, "issue a live embedding probe against the configured provider")

	kbVectorRebuildCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty rebuilds all workspaces)")
	kbVectorRebuildCmd.Flags().StringVar(&kbVectorProvider, "provider", "", "embedding provider override")
	kbVectorRebuildCmd.Flags().StringVar(&kbVectorModel, "model", "", "embedding model override")
	kbVectorRebuildCmd.Flags().IntVar(&kbLimit, "limit", 0, "maximum chunks to rebuild (0 rebuilds all available chunks)")

	kbVectorPurgeCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty purges all workspaces)")

	kbVectorSyncCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty syncs all workspaces)")
	kbVectorSyncCmd.Flags().StringVar(&kbVectorProvider, "provider", "", "embedding provider override")
	kbVectorSyncCmd.Flags().StringVar(&kbVectorModel, "model", "", "embedding model override")
	kbVectorSyncCmd.Flags().IntVar(&kbLimit, "limit", 0, "maximum chunks to sync (0 syncs all available chunks)")

	kbVectorCmd.AddCommand(kbVectorIndexCmd)
	kbVectorCmd.AddCommand(kbVectorSearchCmd)
	kbVectorCmd.AddCommand(kbVectorStatsCmd)
	kbVectorCmd.AddCommand(kbVectorDoctorCmd)
	kbVectorCmd.AddCommand(kbVectorRebuildCmd)
	kbVectorCmd.AddCommand(kbVectorPurgeCmd)
	kbVectorCmd.AddCommand(kbVectorSyncCmd)
	kbCmd.AddCommand(kbVectorCmd)
}

func runKBVectorIndex(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	ctx := context.Background()
	summary, err := vectorkb.IndexWorkspace(ctx, cfg, vectorkb.IndexOptions{
		Workspace: strings.TrimSpace(kbWorkspace),
		Provider:  strings.TrimSpace(kbVectorProvider),
		Model:     strings.TrimSpace(kbVectorModel),
		MaxChunks: kbLimit,
	})
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	printer := terminal.NewPrinter()
	printer.Success("Vector knowledge index completed")
	fmt.Printf("Workspace:          %s\n", formatKnowledgeWorkspaceLabel(summary.Workspace))
	fmt.Printf("Provider/Model:     %s / %s\n", summary.Provider, summary.Model)
	fmt.Printf("Documents seen:     %d\n", summary.DocumentsSeen)
	fmt.Printf("Documents indexed:  %d\n", summary.DocumentsIndexed)
	fmt.Printf("Documents skipped:  %d\n", summary.DocumentsSkipped)
	fmt.Printf("Chunks seen:        %d\n", summary.ChunksSeen)
	fmt.Printf("Chunks embedded:    %d\n", summary.ChunksEmbedded)
	return nil
}

func runKBVectorSearch(cmd *cobra.Command, args []string) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	ctx := context.Background()
	results, err := vectorkb.Search(ctx, cfg, vectorkb.SearchOptions{
		Workspace:           strings.TrimSpace(kbWorkspace),
		WorkspaceLayers:     kbWorkspaceLayers,
		ScopeLayers:         kbScopeLayers,
		Provider:            strings.TrimSpace(kbVectorProvider),
		Model:               strings.TrimSpace(kbVectorModel),
		Limit:               kbLimit,
		MinSourceConfidence: kbMinSourceConfidence,
		SampleTypes:         kbSampleTypes,
		ExcludeSampleTypes:  kbExcludeSampleTypes,
		EnableRerank:        kbEnableRerank,
		RerankProvider:      strings.TrimSpace(kbRerankProvider),
		RerankModel:         strings.TrimSpace(kbRerankModel),
		RerankTopN:          kbRerankTopN,
		RerankMaxCandidates: kbRerankMaxCandidates,
	}, kbQuery)
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(results)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(results) == 0 {
		terminal.NewPrinter().Info("No vector knowledge hits found")
		return nil
	}

	for _, result := range results {
		fmt.Printf("[%.3f] %s\n", result.RelevanceScore, result.Title)
		fmt.Printf("  Workspace: %s\n", result.Workspace)
		fmt.Printf("  Path: %s\n", result.SourcePath)
		if strings.TrimSpace(result.Section) != "" {
			fmt.Printf("  Section: %s\n", result.Section)
		}
		fmt.Printf("  Vector/Keyword: %.3f / %.3f\n", result.VectorScore, result.KeywordScore)
		fmt.Printf("  Snippet: %s\n\n", result.Snippet)
	}
	return nil
}

func runKBVectorStats(cmd *cobra.Command, args []string) error {
	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	store, err := vectorkb.Open(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		return err
	}

	stats, err := store.GetStats(ctx)
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Path:        %s\n", stats.Path)
	fmt.Printf("Documents:   %d\n", stats.Documents)
	fmt.Printf("Chunks:      %d\n", stats.Chunks)
	fmt.Printf("Embeddings:  %d\n", stats.Embeddings)
	fmt.Printf("Workspaces:  %s\n", strings.Join(stats.Workspaces, ", "))
	fmt.Printf("Models:      %s\n", strings.Join(stats.Models, ", "))
	return nil
}

func runKBVectorDoctor(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	report, err := vectorkb.Doctor(context.Background(), cfg, vectorkb.DoctorOptions{
		Workspace:     strings.TrimSpace(kbWorkspace),
		Provider:      strings.TrimSpace(kbVectorProvider),
		Model:         strings.TrimSpace(kbVectorModel),
		ProbeProvider: kbProbeProvider,
	})
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(report)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Path:                       %s\n", report.Path)
	fmt.Printf("Workspace:                  %s\n", formatKnowledgeWorkspaceLabel(report.Workspace))
	fmt.Printf("Vector enabled:             %t\n", report.VectorEnabled)
	fmt.Printf("DB path exists/writable:    %t / %t\n", report.DBPathExists, report.DBPathWritable)
	fmt.Printf("Provider/Model:             %s / %s\n", report.Provider, report.Model)
	fmt.Printf("Provider configured/live:   %t / %t\n", report.ProviderConfigured, report.ProviderAvailable)
	if strings.TrimSpace(report.ProviderEndpoint) != "" {
		fmt.Printf("Provider endpoint:          %s\n", report.ProviderEndpoint)
	}
	if len(report.AvailableProviders) > 0 {
		fmt.Printf("Configured providers:       %s\n", strings.Join(report.AvailableProviders, ", "))
	}
	if len(report.AvailableProviderModels) > 0 {
		fmt.Printf("Configured provider-models: %s\n", strings.Join(report.AvailableProviderModels, ", "))
	}
	fmt.Printf("Semantic status:            %s\n", report.SemanticStatus)
	if strings.TrimSpace(report.SemanticStatusMessage) != "" {
		fmt.Printf("Status reason:              %s\n", report.SemanticStatusMessage)
	}
	fmt.Printf("Main documents/chunks:      %d / %d\n", report.MainDocuments, report.MainChunks)
	fmt.Printf("Vector docs/chunks/embed:   %d / %d / %d\n", report.VectorDocuments, report.VectorChunks, report.VectorEmbeddings)
	fmt.Printf("Selected embeddings:        %d\n", report.SelectedEmbeddings)
	fmt.Printf("Missing documents:          %d\n", report.MissingDocuments)
	fmt.Printf("Stale documents:            %d\n", report.StaleDocuments)
	fmt.Printf("Embedding mismatches:       %d\n", report.DocumentsMissingEmbeddings)
	fmt.Printf("Orphan chunks/embeddings:   %d / %d\n", report.OrphanChunks, report.OrphanEmbeddings)
	if report.Healthy {
		terminal.NewPrinter().Success("Vector knowledge DB is healthy")
		return nil
	}
	terminal.NewPrinter().Warning("Vector knowledge DB is not fully ready")
	for _, issue := range report.Issues {
		fmt.Printf("- [%s] %s", issue.Type, issue.Message)
		if issue.SourcePath != "" {
			fmt.Printf(" (%s)", issue.SourcePath)
		}
		fmt.Println()
	}
	return nil
}

func runKBVectorRebuild(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	summary, err := vectorkb.Rebuild(context.Background(), cfg, vectorkb.IndexOptions{
		Workspace: strings.TrimSpace(kbWorkspace),
		Provider:  strings.TrimSpace(kbVectorProvider),
		Model:     strings.TrimSpace(kbVectorModel),
		MaxChunks: kbLimit,
	})
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	terminal.NewPrinter().Success("Vector knowledge rebuild completed")
	fmt.Printf("Workspace:                  %s\n", formatKnowledgeWorkspaceLabel(summary.Workspace))
	fmt.Printf("Provider/Model:             %s / %s\n", summary.Provider, summary.Model)
	if summary.Purge != nil {
		fmt.Printf("Purged docs/chunks/embed:   %d / %d / %d\n", summary.Purge.RemovedDocuments, summary.Purge.RemovedChunks, summary.Purge.RemovedEmbeddings)
	}
	if summary.Index != nil {
		fmt.Printf("Indexed docs/chunks:        %d / %d\n", summary.Index.DocumentsIndexed, summary.Index.ChunksEmbedded)
		fmt.Printf("Skipped docs:               %d\n", summary.Index.DocumentsSkipped)
	}
	return nil
}

func runKBVectorPurge(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	summary, err := vectorkb.Purge(context.Background(), cfg, strings.TrimSpace(kbWorkspace))
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	terminal.NewPrinter().Success("Vector knowledge purge completed")
	fmt.Printf("Workspace:                  %s\n", formatKnowledgeWorkspaceLabel(summary.Workspace))
	fmt.Printf("Removed stale docs:         %d\n", summary.RemovedStaleDocuments)
	fmt.Printf("Removed docs/chunks/embed:  %d / %d / %d\n", summary.RemovedDocuments, summary.RemovedChunks, summary.RemovedEmbeddings)
	fmt.Printf("Removed orphan chunks:      %d\n", summary.RemovedOrphanChunks)
	fmt.Printf("Removed orphan embeddings:  %d\n", summary.RemovedOrphanEmbeddings)
	return nil
}

func runKBVectorSync(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	summary, err := vectorkb.Sync(context.Background(), cfg, vectorkb.IndexOptions{
		Workspace: strings.TrimSpace(kbWorkspace),
		Provider:  strings.TrimSpace(kbVectorProvider),
		Model:     strings.TrimSpace(kbVectorModel),
		MaxChunks: kbLimit,
	})
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	terminal.NewPrinter().Success("Vector knowledge sync completed")
	fmt.Printf("Workspace:                  %s\n", formatKnowledgeWorkspaceLabel(summary.Workspace))
	fmt.Printf("Provider/Model:             %s / %s\n", summary.Provider, summary.Model)
	if summary.Purge != nil {
		fmt.Printf("Purged docs/chunks/embed:   %d / %d / %d\n", summary.Purge.RemovedDocuments, summary.Purge.RemovedChunks, summary.Purge.RemovedEmbeddings)
	}
	if summary.Index != nil {
		fmt.Printf("Docs indexed/skipped:       %d / %d\n", summary.Index.DocumentsIndexed, summary.Index.DocumentsSkipped)
		fmt.Printf("Chunks seen/embedded:       %d / %d\n", summary.Index.ChunksSeen, summary.Index.ChunksEmbedded)
	}
	return nil
}
