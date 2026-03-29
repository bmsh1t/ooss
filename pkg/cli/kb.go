package cli

import (
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/database"
	"github.com/j3ssie/osmedeus/v5/internal/knowledge"
	"github.com/j3ssie/osmedeus/v5/internal/terminal"
	"github.com/j3ssie/osmedeus/v5/internal/vectorkb"
	"github.com/spf13/cobra"
)

type vectorAutoIndexResult struct {
	Attempted bool
	Indexed   bool
	Error     string
}

var (
	kbPath                string
	kbURL                 string
	kbURLFile             string
	kbWorkspace           string
	kbRecursive           bool
	kbQuery               string
	kbLimit               int
	kbOffset              int
	kbScope               string
	kbMaxAssets           int
	kbMaxVulns            int
	kbMaxRuns             int
	kbIncludeAI           bool
	kbOutput              string
	kbWorkspaceLayers     []string
	kbScopeLayers         []string
	kbMinSourceConfidence float64
	kbSampleTypes         []string
	kbExcludeSampleTypes  []string
	kbPrint               bool
	kbOutputDir           string
	kbYakitDB             string
	kbYakitKBName         string
	kbYakitFormat         string
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

var kbIngestPreviewCmd = &cobra.Command{
	Use:   "ingest-preview",
	Short: "Confirm a generated URL preview markdown file into the knowledge base",
	RunE:  runKBIngestPreview,
}

var kbFetchURLCmd = &cobra.Command{
	Use:   "fetch-url",
	Short: "Fetch a public article page into a reviewable markdown preview",
	RunE:  runKBFetchURL,
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

var kbBridgeYakitRAGCmd = &cobra.Command{
	Use:   "bridge-yakrag",
	Short: "Bridge an imported Yakit .rag/.rag.gz knowledge base into jsonl or markdown",
	RunE:  runKBBridgeYakitRAG,
}

func init() {
	kbIngestCmd.Flags().StringVar(&kbPath, "path", "", "file or directory path to ingest")
	kbIngestCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "global", "knowledge workspace name")
	kbIngestCmd.Flags().BoolVar(&kbRecursive, "recursive", true, "recurse into subdirectories when ingesting a directory")
	_ = kbIngestCmd.MarkFlagRequired("path")

	kbIngestPreviewCmd.Flags().StringVar(&kbPath, "path", "", "generated preview markdown file to confirm into the knowledge base")
	kbIngestPreviewCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "global", "knowledge workspace name")
	_ = kbIngestPreviewCmd.MarkFlagRequired("path")

	kbFetchURLCmd.Flags().StringVar(&kbURL, "url", "", "public article url to fetch")
	kbFetchURLCmd.Flags().StringVar(&kbURLFile, "url-file", "", "file containing one public article url per line")
	kbFetchURLCmd.Flags().StringVarP(&kbOutput, "output", "o", "", "output markdown file path (optional)")
	kbFetchURLCmd.Flags().StringVar(&kbOutputDir, "output-dir", "", "output directory for batch markdown previews")
	kbFetchURLCmd.Flags().BoolVar(&kbPrint, "print", false, "print markdown preview to stdout even when writing to a file")

	kbSearchCmd.Flags().StringVar(&kbQuery, "query", "", "search query")
	kbSearchCmd.Flags().StringVarP(&kbWorkspace, "workspace", "w", "", "knowledge workspace name (empty searches all workspaces)")
	kbSearchCmd.Flags().IntVar(&kbLimit, "limit", 10, "maximum number of results")
	kbSearchCmd.Flags().StringSliceVar(&kbWorkspaceLayers, "workspace-layer", nil, "preferred workspace layers in ranking order")
	kbSearchCmd.Flags().StringSliceVar(&kbScopeLayers, "scope-layer", nil, "preferred scope layers in ranking order")
	kbSearchCmd.Flags().Float64Var(&kbMinSourceConfidence, "min-confidence", 0, "skip learned results below this source confidence")
	kbSearchCmd.Flags().StringSliceVar(&kbSampleTypes, "sample-type", nil, "include only specific learned sample types")
	kbSearchCmd.Flags().StringSliceVar(&kbExcludeSampleTypes, "exclude-sample-type", nil, "exclude specific learned sample types")
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

	kbBridgeYakitRAGCmd.Flags().StringVar(&kbPath, "path", "", "Yakit .rag or .rag.gz package path (optional when --kb-name is provided)")
	kbBridgeYakitRAGCmd.Flags().StringVar(&kbYakitDB, "db", "", "path to the Yakit SQLite database (auto-detects common Yakit paths)")
	kbBridgeYakitRAGCmd.Flags().StringVar(&kbYakitKBName, "kb-name", "", "knowledge base name inside the Yakit DB (optional when inferable from package)")
	kbBridgeYakitRAGCmd.Flags().StringVar(&kbYakitFormat, "format", "auto", "output format: auto, jsonl, md")
	kbBridgeYakitRAGCmd.Flags().StringVarP(&kbOutput, "output", "o", "", "output jsonl or markdown file path")
	_ = kbBridgeYakitRAGCmd.MarkFlagRequired("output")

	kbCmd.AddCommand(kbIngestCmd)
	kbCmd.AddCommand(kbIngestPreviewCmd)
	kbCmd.AddCommand(kbFetchURLCmd)
	kbCmd.AddCommand(kbSearchCmd)
	kbCmd.AddCommand(kbDocsCmd)
	kbCmd.AddCommand(kbLearnCmd)
	kbCmd.AddCommand(kbExportCmd)
	kbCmd.AddCommand(kbBridgeYakitRAGCmd)
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

	vectorResult := maybeAutoIndexVectorKnowledge(ctx, cfg, summary.Workspace, "ingest")
	applyVectorAutoIndexToIngestSummary(summary, vectorResult)

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
	printVectorAutoIndexResult(vectorResult)
	return nil
}

func runKBIngestPreview(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	cfg := config.Get()
	if cfg == nil {
		return fmt.Errorf("configuration not loaded")
	}

	ctx := context.Background()
	summary, err := knowledge.IngestURLPreview(ctx, kbPath, kbWorkspace)
	if err != nil {
		return err
	}

	vectorResult := maybeAutoIndexVectorKnowledge(ctx, cfg, summary.Workspace, "ingest")
	applyVectorAutoIndexToPreviewSummary(summary, vectorResult)

	if globalJSON {
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	printer := terminal.NewPrinter()
	printer.Success("Knowledge preview ingest completed")
	fmt.Printf("Workspace:  %s\n", summary.Workspace)
	fmt.Printf("Preview:    %s\n", summary.PreviewPath)
	fmt.Printf("Source:     %s\n", summary.SourcePath)
	fmt.Printf("Title:      %s\n", summary.Title)
	fmt.Printf("Doc Type:   %s\n", summary.DocType)
	fmt.Printf("Documents:  %d\n", summary.Documents)
	fmt.Printf("Chunks:     %d\n", summary.Chunks)
	if strings.TrimSpace(summary.SuggestedSampleType) != "" {
		fmt.Printf("Sample:     %s\n", summary.SuggestedSampleType)
	}
	if summary.SuggestedSourceConfidence > 0 {
		fmt.Printf("Confidence: %.2f\n", summary.SuggestedSourceConfidence)
	}
	if len(summary.SuggestedLabels) > 0 {
		fmt.Printf("Labels:     %s\n", strings.Join(summary.SuggestedLabels, ", "))
	}
	if len(summary.SuggestedTargetTypes) > 0 {
		fmt.Printf("Targets:    %s\n", strings.Join(summary.SuggestedTargetTypes, ", "))
	}
	printVectorAutoIndexResult(vectorResult)
	return nil
}

func runKBFetchURL(cmd *cobra.Command, args []string) error {
	urlCount := 0
	if strings.TrimSpace(kbURL) != "" {
		urlCount++
	}
	if strings.TrimSpace(kbURLFile) != "" {
		urlCount++
	}
	if urlCount == 0 {
		return fmt.Errorf("either --url or --url-file is required")
	}
	if urlCount > 1 {
		return fmt.Errorf("use only one of --url or --url-file")
	}

	if strings.TrimSpace(kbURLFile) != "" {
		return runKBFetchURLBatch(cmd, args)
	}

	ctx := context.Background()
	preview, err := knowledge.FetchURLPreview(ctx, kbURL)
	if err != nil {
		return err
	}

	if globalJSON {
		data, err := json.Marshal(preview)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	outputPath := strings.TrimSpace(kbOutput)
	if outputPath != "" {
		absOutput, err := filepath.Abs(outputPath)
		if err != nil {
			return fmt.Errorf("failed to resolve output path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absOutput), 0o755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
		if err := os.WriteFile(absOutput, []byte(preview.Markdown), 0o644); err != nil {
			return fmt.Errorf("failed to write preview file: %w", err)
		}

		terminal.NewPrinter().Success("URL preview saved")
		fmt.Printf("Title:  %s\n", preview.Title)
		fmt.Printf("URL:    %s\n", preview.FinalURL)
		fmt.Printf("Quality: %d\n", preview.QualityScore)
		if preview.SuggestedSourceConfidence > 0 {
			fmt.Printf("Suggested Confidence: %.2f\n", preview.SuggestedSourceConfidence)
		}
		if len(preview.SuggestedLabels) > 0 {
			fmt.Printf("Suggested Labels: %s\n", strings.Join(preview.SuggestedLabels, ", "))
		}
		fmt.Printf("Output: %s\n", absOutput)
		if len(preview.Warnings) > 0 {
			fmt.Printf("Warnings: %s\n", strings.Join(preview.Warnings, "; "))
		}
		fmt.Println("")
		fmt.Printf("Review or edit Suggested Metadata / Article, then confirm it with: osmedeus kb ingest-preview --path %s -w <workspace>\n", absOutput)
		if kbPrint {
			fmt.Println("")
			fmt.Println(preview.Markdown)
		}
		return nil
	}

	fmt.Println(preview.Markdown)
	return nil
}

type kbFetchURLBatchItem struct {
	URL                       string   `json:"url"`
	FinalURL                  string   `json:"final_url,omitempty"`
	Title                     string   `json:"title,omitempty"`
	Output                    string   `json:"output,omitempty"`
	QualityScore              int      `json:"quality_score,omitempty"`
	Warnings                  []string `json:"warnings,omitempty"`
	SuggestedSampleType       string   `json:"suggested_sample_type,omitempty"`
	SuggestedSourceConfidence float64  `json:"suggested_source_confidence,omitempty"`
	SuggestedLabels           []string `json:"suggested_labels,omitempty"`
	SuggestedTargetTypes      []string `json:"suggested_target_types,omitempty"`
	Error                     string   `json:"error,omitempty"`
}

type kbFetchURLBatchSummary struct {
	URLFile   string                `json:"url_file"`
	OutputDir string                `json:"output_dir,omitempty"`
	TotalURLs int                   `json:"total_urls"`
	Succeeded int                   `json:"succeeded"`
	Failed    int                   `json:"failed"`
	Generated []string              `json:"generated,omitempty"`
	Items     []kbFetchURLBatchItem `json:"items,omitempty"`
}

func runKBFetchURLBatch(cmd *cobra.Command, args []string) error {
	urls, err := loadKBFetchURLs(kbURLFile)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return fmt.Errorf("no urls found in %s", strings.TrimSpace(kbURLFile))
	}

	outputDir := strings.TrimSpace(kbOutputDir)
	if outputDir == "" {
		return fmt.Errorf("--output-dir is required with --url-file")
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}
	if err := os.MkdirAll(absOutputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	ctx := context.Background()
	summary := kbFetchURLBatchSummary{
		URLFile:   strings.TrimSpace(kbURLFile),
		OutputDir: absOutputDir,
		TotalURLs: len(urls),
		Items:     make([]kbFetchURLBatchItem, 0, len(urls)),
	}

	for idx, rawURL := range urls {
		item := kbFetchURLBatchItem{URL: rawURL}
		preview, err := knowledge.FetchURLPreview(ctx, rawURL)
		if err != nil {
			item.Error = err.Error()
			summary.Failed++
			summary.Items = append(summary.Items, item)
			continue
		}

		filename := buildKBFetchPreviewFileName(idx+1, preview)
		outputPath := filepath.Join(absOutputDir, filename)
		if err := os.WriteFile(outputPath, []byte(preview.Markdown), 0o644); err != nil {
			item.Error = fmt.Sprintf("failed to write preview file: %v", err)
			summary.Failed++
			summary.Items = append(summary.Items, item)
			continue
		}

		item.FinalURL = preview.FinalURL
		item.Title = preview.Title
		item.Output = outputPath
		item.QualityScore = preview.QualityScore
		item.Warnings = preview.Warnings
		item.SuggestedSampleType = preview.SuggestedSampleType
		item.SuggestedSourceConfidence = preview.SuggestedSourceConfidence
		item.SuggestedLabels = preview.SuggestedLabels
		item.SuggestedTargetTypes = preview.SuggestedTargetTypes
		summary.Succeeded++
		summary.Generated = append(summary.Generated, outputPath)
		summary.Items = append(summary.Items, item)
	}

	if globalJSON {
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		if summary.Succeeded == 0 && summary.Failed > 0 {
			return fmt.Errorf("failed to fetch all urls from %s", strings.TrimSpace(kbURLFile))
		}
		return nil
	}

	if summary.Succeeded > 0 {
		terminal.NewPrinter().Success("Batch URL previews saved")
		fmt.Printf("Input:      %s\n", summary.URLFile)
		fmt.Printf("Output Dir: %s\n", summary.OutputDir)
		fmt.Printf("Succeeded:  %d\n", summary.Succeeded)
		fmt.Printf("Failed:     %d\n", summary.Failed)
		fmt.Println("")
		fmt.Printf("Review or edit Suggested Metadata / Article, then confirm each file with: osmedeus kb ingest-preview --path <file> -w <workspace>\n")
		if kbPrint {
			fmt.Println("")
			fmt.Println("Generated Files:")
			for _, item := range summary.Items {
				if strings.TrimSpace(item.Output) == "" {
					continue
				}
				if len(item.Warnings) > 0 {
					fmt.Printf("  - %s (quality=%d, warnings=%s)\n", item.Output, item.QualityScore, strings.Join(item.Warnings, "; "))
					continue
				}
				fmt.Printf("  - %s (quality=%d)\n", item.Output, item.QualityScore)
			}
		}
	}

	if summary.Failed > 0 {
		if summary.Succeeded > 0 {
			terminal.NewPrinter().Warning("Some URLs could not be fetched")
		}
		fmt.Println("Errors:")
		for _, item := range summary.Items {
			if strings.TrimSpace(item.Error) == "" {
				continue
			}
			fmt.Printf("  - %s: %s\n", item.URL, item.Error)
		}
		if summary.Succeeded == 0 {
			return fmt.Errorf("failed to fetch all urls from %s", strings.TrimSpace(kbURLFile))
		}
	}

	return nil
}

func loadKBFetchURLs(path string) ([]string, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read url file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	seen := make(map[string]struct{}, len(lines))
	urls := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		urls = append(urls, line)
	}
	return urls, nil
}

func buildKBFetchPreviewFileName(index int, preview *knowledge.URLFetchPreview) string {
	host := "article"
	if preview != nil {
		candidateURL := strings.TrimSpace(preview.FinalURL)
		if candidateURL == "" {
			candidateURL = strings.TrimSpace(preview.URL)
		}
		if parsed, err := parseKBFetchURLHost(candidateURL); err == nil && parsed != "" {
			host = parsed
		}
	}

	title := "preview"
	if preview != nil && strings.TrimSpace(preview.Title) != "" {
		title = strings.TrimSpace(preview.Title)
	}

	slug := sanitizeKBFetchFilename(host + "-" + title)
	if slug == "" {
		slug = "preview"
	}
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	return fmt.Sprintf("%03d-%s.md", index, slug)
}

func parseKBFetchURLHost(rawURL string) (string, error) {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", err
	}
	return parsed.Hostname(), nil
}

func sanitizeKBFetchFilename(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		default:
			if lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func runKBSearch(cmd *cobra.Command, args []string) error {
	if err := connectDB(); err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	ctx := context.Background()
	results, err := knowledge.SearchWithOptions(ctx, knowledge.SearchOptions{
		Workspace:           kbWorkspace,
		WorkspaceLayers:     kbWorkspaceLayers,
		ScopeLayers:         kbScopeLayers,
		Query:               kbQuery,
		Limit:               kbLimit,
		MinSourceConfidence: kbMinSourceConfidence,
		SampleTypes:         kbSampleTypes,
		ExcludeSampleTypes:  kbExcludeSampleTypes,
	})
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

	indexWorkspace := strings.TrimSpace(summary.StorageWorkspace)
	if indexWorkspace == "" {
		indexWorkspace = strings.TrimSpace(summary.Workspace)
	}
	vectorResult := maybeAutoIndexVectorKnowledge(ctx, cfg, indexWorkspace, "learn")
	applyVectorAutoIndexToLearnSummary(summary, vectorResult)

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
	if strings.TrimSpace(summary.StorageWorkspace) != "" && strings.TrimSpace(summary.StorageWorkspace) != strings.TrimSpace(summary.Workspace) {
		fmt.Printf("Stored In: %s\n", summary.StorageWorkspace)
	}
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
	printVectorAutoIndexResult(vectorResult)

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

func runKBBridgeYakitRAG(cmd *cobra.Command, args []string) error {
	if strings.TrimSpace(kbPath) == "" && strings.TrimSpace(kbYakitKBName) == "" {
		return fmt.Errorf("either --path or --kb-name is required")
	}

	summary, err := knowledge.BridgeYakitRAG(context.Background(), knowledge.YakitRAGBridgeOptions{
		PackagePath:       kbPath,
		YakitDBPath:       kbYakitDB,
		KnowledgeBaseName: kbYakitKBName,
		OutputPath:        kbOutput,
		Format:            kbYakitFormat,
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
	printer.Success("Yakit RAG bridge export completed")
	fmt.Printf("Knowledge Base: %s\n", summary.KnowledgeBaseName)
	fmt.Printf("Yakit DB:       %s\n", summary.YakitDBPath)
	if strings.TrimSpace(summary.PackagePath) != "" {
		fmt.Printf("Package:        %s\n", summary.PackagePath)
	}
	fmt.Printf("Output:         %s\n", summary.OutputPath)
	fmt.Printf("Format:         %s\n", summary.Format)
	fmt.Printf("Entries:        %d\n", summary.Entries)
	if strings.TrimSpace(summary.CollectionUUID) != "" {
		fmt.Printf("Collection:     %s\n", summary.CollectionUUID)
	}
	if strings.TrimSpace(summary.ModelName) != "" {
		fmt.Printf("Model:          %s\n", summary.ModelName)
	}
	if strings.TrimSpace(summary.ResolvedBy) != "" {
		fmt.Printf("Resolved By:    %s\n", summary.ResolvedBy)
	}
	fmt.Println("")
	if summary.Format == "jsonl" {
		fmt.Println("Note: JSONL is best for interchange; prefer --format md when the next step is osmedeus kb ingest.")
	}
	fmt.Printf("Next: osmedeus kb ingest --path %s -w <workspace>\n", summary.OutputPath)
	return nil
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

func applyVectorAutoIndexToPreviewSummary(summary *knowledge.URLPreviewIngestSummary, result *vectorAutoIndexResult) {
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

func printVectorAutoIndexResult(result *vectorAutoIndexResult) {
	if result == nil || !result.Attempted {
		return
	}
	fmt.Println("")
	if result.Indexed {
		terminal.NewPrinter().Success("Vector auto-index completed")
		return
	}
	terminal.NewPrinter().Warning("Vector auto-index failed")
	fmt.Printf("Vector Error: %s\n", strings.TrimSpace(result.Error))
	fmt.Println("Primary KB write completed; keyword search is ready, semantic search needs a later reindex.")
}
