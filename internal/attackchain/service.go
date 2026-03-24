package attackchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/j3ssie/osmedeus/v5/internal/database"
)

type importPayload struct {
	AttackChainSummary struct {
		TotalChains           int      `json:"total_chains"`
		CriticalChains        int      `json:"critical_chains"`
		HighImpactChains      int      `json:"high_impact_chains"`
		MostLikelyEntryPoints []string `json:"most_likely_entry_points"`
	} `json:"attack_chain_summary"`
	AttackChains           json.RawMessage `json:"attack_chains"`
	CriticalPaths          json.RawMessage `json:"critical_paths"`
	DefenseRecommendations []string        `json:"defense_recommendations"`
}

// ImportSummary reports the result of attack-chain report ingestion.
type ImportSummary struct {
	ID                    int64    `json:"id"`
	Workspace             string   `json:"workspace"`
	Target                string   `json:"target,omitempty"`
	SourcePath            string   `json:"source_path"`
	TotalChains           int      `json:"total_chains"`
	CriticalChains        int      `json:"critical_chains"`
	HighImpactChains      int      `json:"high_impact_chains"`
	MostLikelyEntryPoints []string `json:"most_likely_entry_points,omitempty"`
	MermaidPath           string   `json:"mermaid_path,omitempty"`
	TextPath              string   `json:"text_path,omitempty"`
}

// ImportFile normalizes an attack-chain JSON file and stores it in the local report table.
func ImportFile(ctx context.Context, workspace, sourcePath, target, runUUID, mermaidPath, textPath string) (*ImportSummary, error) {
	workspace = strings.TrimSpace(workspace)
	sourcePath = strings.TrimSpace(sourcePath)
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	if sourcePath == "" {
		return nil, fmt.Errorf("source path is required")
	}

	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source path: %w", err)
	}

	content, err := os.ReadFile(absSource)
	if err != nil {
		return nil, fmt.Errorf("failed to read attack chain file: %w", err)
	}

	var payload importPayload
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, fmt.Errorf("invalid attack chain json: %w", err)
	}

	report := &database.AttackChainReport{
		Workspace:              workspace,
		Target:                 strings.TrimSpace(target),
		RunUUID:                strings.TrimSpace(runUUID),
		SourcePath:             absSource,
		SourceHash:             hashContent(content),
		Status:                 "ready",
		TotalChains:            payload.AttackChainSummary.TotalChains,
		CriticalChains:         payload.AttackChainSummary.CriticalChains,
		HighImpactChains:       payload.AttackChainSummary.HighImpactChains,
		MostLikelyEntryPoints:  payload.AttackChainSummary.MostLikelyEntryPoints,
		AttackChainsJSON:       normalizeRawJSON(payload.AttackChains, "[]"),
		CriticalPathsJSON:      normalizeRawJSON(payload.CriticalPaths, "[]"),
		DefenseRecommendations: payload.DefenseRecommendations,
		MermaidPath:            strings.TrimSpace(mermaidPath),
		TextPath:               strings.TrimSpace(textPath),
	}
	if report.Target == "" {
		report.Target = workspace
	}

	if err := database.UpsertAttackChainReport(ctx, report); err != nil {
		return nil, err
	}

	return &ImportSummary{
		ID:                    report.ID,
		Workspace:             report.Workspace,
		Target:                report.Target,
		SourcePath:            report.SourcePath,
		TotalChains:           report.TotalChains,
		CriticalChains:        report.CriticalChains,
		HighImpactChains:      report.HighImpactChains,
		MostLikelyEntryPoints: report.MostLikelyEntryPoints,
		MermaidPath:           report.MermaidPath,
		TextPath:              report.TextPath,
	}, nil
}

func normalizeRawJSON(data json.RawMessage, fallback string) string {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return fallback
	}

	var decoded interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fallback
	}

	normalized, err := json.Marshal(decoded)
	if err != nil {
		return fallback
	}
	return string(normalized)
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
