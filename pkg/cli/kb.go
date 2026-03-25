package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/knowledge"
	"github.com/j3ssie/osmedeus/v5/internal/terminal"
	"github.com/spf13/cobra"
)

var (
	kbPath      string
	kbWorkspace string
	kbRecursive bool
	kbQuery     string
	kbLimit     int
	kbOffset    int
	kbScope     string
	kbMaxAssets int
	kbMaxVulns  int
	kbMaxRuns   int
	kbIncludeAI bool
	kbOutput    string
)

var kbCmd = &cobra.Command{
	Use:   "kb",
	Short: "Manage the local document knowledge base",
}

var kbIngestCmd = &cobra.Command{
	Use:   "ingest",
	Short: "Ingest a local file or directory into the knowledge base",
	RunE:  runKBIngest,
}

var kbSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search ingested knowledge chunks",
	RunE:  runKBSearch,
}

var kbDocsCmd = &cobra.Command{
	Use:   "docs",
	Short: "List ingested knowledge documents",
	RunE:  runKBDocs,
}

var kbLearnCmd = &cobra.Command{
	Use:   "learn",
	Short: "Build learned knowledge from an existing workspace",
	RunE:  runKBLearn,
}

var kbExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export knowledge chunks into a line-oriented corpus for vector indexing",
	RunE:  runKBExport,
}

func init() {
	kbIngestCmd.Flags().StringVar(&kbPath, "path", "", "file or directory path to ingest")
	kbIngestCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "global", "knowledge workspace name")
	kbIngestCmd.Flags().BoolVar(&kbRecursive, "recursive", true, "recurse into subdirectories when ingesting a directory")
	_ = kbIngestCmd.MarkFlagRequired("path")

	kbSearchCmd.Flags().StringVar(&kbQuery, "query", "", "search query")
	kbSearchCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty searches all workspaces)")
	kbSearchCmd.Flags().IntVar(&kbLimit, "limit", 10, "maximum number of results")
	_ = kbSearchCmd.MarkFlagRequired("query")

	kbDocsCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty lists all workspaces)")
	kbDocsCmd.Flags().IntVar(&kbLimit, "limit", 20, "maximum number of documents")
	kbDocsCmd.Flags().IntVar(&kbOffset, "offset", 0, "offset for pagination")

	kbLearnCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "workspace name to learn from")
	kbLearnCmd.Flags().StringVar(&kbScope, "scope", "workspace", "knowledge scope: workspace, project, public")
	kbLearnCmd.Flags().IntVar(&kbMaxAssets, "max-assets", 20, "maximum assets to include in learned summary")
	kbLearnCmd.Flags().IntVar(&kbMaxVulns, "max-vulns", 20, "maximum vulnerabilities to include in learned summary")
	kbLearnCmd.Flags().IntVar(&kbMaxRuns, "max-runs", 10, "maximum recent runs to include in learned summary")
	kbLearnCmd.Flags().BoolVar(&kbIncludeAI, "include-ai", true, "include ai-analysis artifacts when available")
	_ = kbLearnCmd.MarkFlagRequired("workspace")

	kbExportCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty exports all workspaces)")
	kbExportCmd.Flags().StringVarP(&kbOutput, "output", "o", "", "output file path")
	kbExportCmd.Flags().IntVar(&kbLimit, "limit", 400, "maximum chunks to export")
	_ = kbExportCmd.MarkFlagRequired("output")

	kbCmd.AddCommand(kbIngestCmd)
	kbCmd.AddCommand(kbSearchCmd)
	kbCmd.AddCommand(kbDocsCmd)
	kbCmd.AddCommand(kbLearnCmd)
	kbCmd.AddCommand(kbExportCmd)
	rootCmd.AddCommand(kbCmd)
}

func runKBIngest(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	ctx := context.Background()
	summary, err := knowledge.IngestPath(ctx, cfg, kbPath, kbWorkspace, kbRecursive)
	if err != nil {
		if summary != nil && globalJSON {
			data, _ := json.Marshal(summary)
			fmt.Println(string(data))
		}
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
	printer.Success("Knowledge ingest completed")
	fmt.Printf("Workspace: %s\n", summary.Workspace)
	fmt.Printf("Root Path: %s\n", summary.RootPath)
	fmt.Printf("Documents: %d\n", summary.Documents)
	fmt.Printf("Chunks:    %d\n", summary.Chunks)
	fmt.Printf("Skipped:   %d\n", summary.Skipped)
	fmt.Printf("Failed:    %d\n", summary.Failed)
	if len(summary.Errors) > 0 {
		fmt.Println("")
		fmt.Println("Errors:")
		for _, entry := range summary.Errors {
			fmt.Printf("  - %s\n", entry)
		}
	}
	return nil
}

func runKBSearch(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	results, err := knowledge.Search(ctx, kbWorkspace, kbQuery, kbLimit)
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
		terminal.NewPrinter().Info("No knowledge hits found")
		return nil
	}

	for _, result := range results {
		fmt.Printf("[%0.1f] %s\n", result.Score, result.Title)
		fmt.Printf("  Path: %s\n", result.SourcePath)
		if strings.TrimSpace(result.Section) != "" {
			fmt.Printf("  Section: %s\n", result.Section)
		}
		fmt.Printf("  Snippet: %s\n\n", result.Snippet)
	}
	return nil
}

func runKBDocs(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	result, err := knowledge.ListDocuments(ctx, kbWorkspace, kbOffset, kbLimit)
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if len(result.Data) == 0 {
		terminal.NewPrinter().Info("No knowledge documents found")
		return nil
	}

	for _, doc := range result.Data {
		fmt.Printf("[%d] %s\n", doc.ID, doc.Title)
		fmt.Printf("  Workspace: %s\n", doc.Workspace)
		fmt.Printf("  Path: %s\n", doc.SourcePath)
		fmt.Printf("  Type: %s | Chunks: %d | Status: %s\n\n", doc.DocType, doc.ChunkCount, doc.Status)
	}
	return nil
}

func runKBLearn(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	ctx := context.Background()
	includeAI := kbIncludeAI
	summary, err := knowledge.LearnWorkspace(ctx, cfg, knowledge.LearnOptions{
		Workspace:          kbWorkspace,
		Scope:              kbScope,
		MaxAssets:          kbMaxAssets,
		MaxVulnerabilities: kbMaxVulns,
		MaxRuns:            kbMaxRuns,
		IncludeAIAnalysis:  &includeAI,
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
	printer.Success("Knowledge learning completed")
	fmt.Printf("Workspace: %s\n", summary.Workspace)
	fmt.Printf("Scope:     %s\n", summary.Scope)
	fmt.Printf("Document:  %s\n", summary.SourcePath)
	fmt.Printf("Chunks:    %d\n", summary.Chunks)
	fmt.Printf("Assets:    %d\n", summary.AssetsIncluded)
	fmt.Printf("Vulns:     %d\n", summary.VulnsIncluded)
	fmt.Printf("Runs:      %d\n", summary.RunsIncluded)
	if len(summary.AIFilesIncluded) > 0 {
		fmt.Println("")
		fmt.Println("AI Files:")
		for _, path := range summary.AIFilesIncluded {
			fmt.Printf("  - %s\n", path)
		}
	}

	return nil
}

func runKBExport(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	summary, err := knowledge.ExportChunks(ctx, kbWorkspace, kbOutput, kbLimit)
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
	printer.Success("Knowledge export completed")
	fmt.Printf("Workspace: %s\n", strings.TrimSpace(summary.Workspace))
	fmt.Printf("Output:    %s\n", summary.Output)
	fmt.Printf("Documents: %d\n", summary.Documents)
	fmt.Printf("Chunks:    %d\n", summary.Chunks)
	return nil
}
