package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	osExec "os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/config"
	"github.com/j3ssie/osmedeus/v5/internal/core"
	"github.com/j3ssie/osmedeus/v5/internal/executor"
	"github.com/j3ssie/osmedeus/v5/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getTestdataPath returns the absolute path to the testdata directory
func getTestdataPath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "testdata")
}

// getWorkflowsPath returns the path to the workflows testdata directory
func getWorkflowsPath() string {
	return filepath.Join(getTestdataPath(), "workflows")
}

// getRepoRoot returns the absolute path to the repository root.
func getRepoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..")
}

// getRealWorkflowsPath returns the path to the repository workflows directory.
func getRealWorkflowsPath() string {
	return filepath.Join(getRepoRoot(), "osmedeus-base", "workflows")
}

// testConfig returns a config with isolated temp directories that are
// automatically cleaned up after the test completes.
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	baseDir := t.TempDir()
	return &config.Config{
		BaseFolder:     baseDir,
		WorkspacesPath: filepath.Join(baseDir, "workspaces"),
		WorkflowsPath:  filepath.Join(baseDir, "workflows"),
		BinariesPath:   filepath.Join(baseDir, "binaries"),
		DataPath:       filepath.Join(baseDir, "data"),
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func installStubOsmedeus(t *testing.T) string {
	t.Helper()

	stubDir := t.TempDir()
	callsPath := filepath.Join(stubDir, "osmedeus-calls.log")
	stubPath := filepath.Join(stubDir, "osmedeus")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
if [ "$1" = "--json" ] && [ "$2" = "campaign" ] && [ "$3" = "create" ]; then
  printf '{"status":"created","campaign_id":"camp-123","queued_runs":3}\n'
  exit 0
fi
if [ "$1" = "kb" ] && [ "$2" = "learn" ]; then
  printf '{"status":"learned","documents":4,"chunks":12}\n'
  exit 0
fi
if [ "$1" = "worker" ] && [ "$2" = "queue" ] && [ "$3" = "new" ]; then
  printf 'queued\n'
  exit 0
fi
printf 'unexpected args: %%s\n' "$*" >&2
exit 1
`, callsPath)
	require.NoError(t, os.WriteFile(stubPath, []byte(script), 0755))
	t.Setenv("PATH", stubDir+":"+os.Getenv("PATH"))

	return callsPath
}

func installKBSearchStubOsmedeus(t *testing.T) string {
	t.Helper()

	stubDir := t.TempDir()
	callsPath := filepath.Join(stubDir, "osmedeus-calls.log")
	stubPath := filepath.Join(stubDir, "osmedeus")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
if [ "$1" = "kb" ] && [ "$2" = "export" ]; then
  workspace=""
  output=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -w|--workspace)
        shift
        workspace="$1"
        ;;
      -o|--output)
        shift
        output="$1"
        ;;
    esac
    shift
  done
  if [ -n "$output" ]; then
    mkdir -p "$(dirname "$output")"
    printf '[%%s] exported knowledge chunk\n' "${workspace:-unknown}" > "$output"
  fi
  printf 'export %%s\n' "${workspace:-unknown}" >&2
  exit 0
fi
if [ "$1" = "--json" ] && [ "$2" = "kb" ] && [ "$3" = "vector" ] && [ "$4" = "search" ]; then
  workspace=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -w|--workspace)
        shift
        workspace="$1"
        ;;
    esac
    shift
  done
  printf 'vector %%s\n' "${workspace:-unknown}" >&2
  printf '[{"type":"vector_match","content":"vector knowledge for %%s","relevance_score":0.93,"source":"vector_kb","workspace":"%%s","source_path":"kb://%%s/vector"}]\n' "${workspace:-unknown}" "${workspace:-unknown}" "${workspace:-unknown}"
  exit 0
fi
if [ "$1" = "--json" ] && [ "$2" = "kb" ] && [ "$3" = "search" ]; then
  workspace=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -w|--workspace)
        shift
        workspace="$1"
        ;;
    esac
    shift
  done
  printf 'keyword %%s\n' "${workspace:-unknown}" >&2
  printf '[{"title":"keyword knowledge %%s","content":"keyword knowledge for %%s","score":0.74,"source":"knowledge_keyword","workspace":"%%s","source_path":"kb://%%s/keyword"}]\n' "${workspace:-unknown}" "${workspace:-unknown}" "${workspace:-unknown}" "${workspace:-unknown}"
  exit 0
fi
printf 'unexpected args: %%s\n' "$*" >&2
exit 1
`, callsPath)
	require.NoError(t, os.WriteFile(stubPath, []byte(script), 0755))
	t.Setenv("PATH", stubDir+":"+os.Getenv("PATH"))

	return callsPath
}

// TestLoadAllWorkflows tests that all workflow YAML files can be loaded and parsed
func TestLoadAllWorkflows(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	// Skip files that use experimental features or have validation issues
	skipFiles := map[string]string{
		"test-remote-bash.yaml":           "uses remote-bash step type (requires Docker)",
		"test-remote-bash-ssh.yaml":       "uses remote-bash step type (requires SSH)",
		"test-remote-bash-docker.yaml":    "uses remote-bash step type (requires Docker)",
		"test-docker-file-outputs.yaml":   "uses remote-bash step type (requires Docker)",
		"test-agent-validation-fail.yaml": "intentionally invalid (duplicate agent tools)",
		"test-agent-unknown-preset.yaml":  "intentionally invalid (unknown preset tool)",
	}

	// Get all workflow files (top-level + agent-and-llm subdirectory)
	files, err := filepath.Glob(filepath.Join(workflowsPath, "*.yaml"))
	require.NoError(t, err)
	subFiles, err := filepath.Glob(filepath.Join(workflowsPath, "agent-and-llm", "*.yaml"))
	require.NoError(t, err)
	files = append(files, subFiles...)
	require.Greater(t, len(files), 0, "No workflow files found")

	t.Logf("Found %d workflow files to load", len(files))

	for _, file := range files {
		name := filepath.Base(file)
		if reason, skip := skipFiles[name]; skip {
			t.Run(name, func(t *testing.T) {
				t.Skipf("Skipping: %s", reason)
			})
			continue
		}
		t.Run(name, func(t *testing.T) {
			workflow, err := loader.LoadWorkflowByPath(file)
			require.NoError(t, err, "Failed to load workflow: %s", file)
			assert.NotEmpty(t, workflow.Name, "Workflow name should not be empty")
			assert.NotEmpty(t, workflow.Kind, "Workflow kind should not be empty")
		})
	}
}

// TestValidateAllWorkflows tests that all workflow YAML files pass validation
func TestValidateAllWorkflows(t *testing.T) {
	workflowsPath := getWorkflowsPath()

	// Get all workflow files (top-level + agent-and-llm subdirectory)
	files, err := filepath.Glob(filepath.Join(workflowsPath, "*.yaml"))
	require.NoError(t, err)
	subFiles, err := filepath.Glob(filepath.Join(workflowsPath, "agent-and-llm", "*.yaml"))
	require.NoError(t, err)
	files = append(files, subFiles...)

	// Skip validation test files that are meant to fail or use experimental features
	skipFiles := map[string]string{
		"test-requirements-fail.yaml":     "expected to fail validation",
		"test-remote-bash.yaml":           "uses remote-bash step type",
		"test-remote-bash-ssh.yaml":       "uses remote-bash step type",
		"test-remote-bash-docker.yaml":    "uses remote-bash step type",
		"test-docker-file-outputs.yaml":   "uses remote-bash step type (requires Docker)",
		"test-agent-validation-fail.yaml": "intentionally invalid (duplicate agent tools)",
		"test-agent-unknown-preset.yaml":  "intentionally invalid (unknown preset tool)",
	}

	for _, file := range files {
		name := filepath.Base(file)
		if reason, skip := skipFiles[name]; skip {
			t.Run(name, func(t *testing.T) {
				t.Skipf("Skipping: %s", reason)
			})
			continue
		}

		t.Run(name, func(t *testing.T) {
			p := parser.NewParser()
			workflow, err := p.Parse(file)
			require.NoError(t, err, "Failed to parse workflow: %s", file)

			err = parser.Validate(workflow)
			require.NoError(t, err, "Validation failed for workflow: %s", file)
		})
	}
}

func TestSuperdomainAIWorkflowNamesMatchFileNames(t *testing.T) {
	workflows := []string{
		"superdomain-extensive-ai-stable",
		"superdomain-extensive-ai-hybrid",
		"superdomain-extensive-ai-lite",
		"superdomain-extensive-ai-optimized",
	}

	p := parser.NewParser()
	root := getRealWorkflowsPath()

	for _, workflowName := range workflows {
		t.Run(workflowName, func(t *testing.T) {
			file := filepath.Join(root, workflowName+".yaml")
			workflow, err := p.Parse(file)
			require.NoError(t, err)
			assert.Equal(t, workflowName, workflow.Name)
		})
	}
}

// TestExecuteBashWorkflow tests executing a basic bash workflow
func TestExecuteBashWorkflow(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-bash")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "integration-test",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 1)
	assert.Equal(t, core.StepStatusSuccess, result.Steps[0].Status)
}

// TestExecuteForeachWorkflow tests executing a foreach loop workflow
func TestExecuteForeachWorkflow(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-foreach")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "foreach-test",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	// Should have 4 steps: create-input, process-items (foreach), verify-output, cleanup
	assert.Len(t, result.Steps, 4)

	// All steps should succeed
	for _, step := range result.Steps {
		assert.Equal(t, core.StepStatusSuccess, step.Status, "Step %s failed", step.StepName)
	}
}

// TestExecuteParallelCommandsWorkflow tests executing parallel commands
func TestExecuteParallelCommandsWorkflow(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-parallel-commands")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "parallel-test",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 2)

	// All steps should succeed
	for _, step := range result.Steps {
		assert.Equal(t, core.StepStatusSuccess, step.Status, "Step %s failed", step.StepName)
	}
}

// TestExecuteParallelStepsWorkflow tests executing parallel steps
func TestExecuteParallelStepsWorkflow(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-parallel-steps")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "parallel-steps-test",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
}

// TestExecuteFunctionsWorkflow tests executing utility functions
func TestExecuteFunctionsWorkflow(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-functions")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "functions-test",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	// All 4 steps should complete successfully
	assert.Len(t, result.Steps, 4)
	for _, step := range result.Steps {
		assert.Equal(t, core.StepStatusSuccess, step.Status, "Step %s failed", step.StepName)
	}
}

// TestTimeoutWorkflowSuccess tests workflow with timeout that succeeds
func TestTimeoutWorkflowSuccess(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-timeout")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "timeout-test",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	// Both steps should succeed within timeout
	assert.Len(t, result.Steps, 2)
	for _, step := range result.Steps {
		assert.Equal(t, core.StepStatusSuccess, step.Status, "Step %s failed", step.StepName)
	}
}

// TestTimeoutWorkflowExceeds tests workflow where step exceeds timeout
func TestTimeoutWorkflowExceeds(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-timeout-exceed")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "timeout-exceed-test",
	}, cfg)

	// Execution should fail due to timeout
	require.Error(t, err)
	assert.Equal(t, core.RunStatusFailed, result.Status)
}

// TestRequirementsWorkflowSuccess tests workflow with satisfied dependencies
func TestRequirementsWorkflowSuccess(t *testing.T) {
	workflowsPath := getWorkflowsPath()

	p := parser.NewParser()
	workflow, err := p.Parse(filepath.Join(workflowsPath, "test-requirements.yaml"))
	require.NoError(t, err)

	// Check dependencies
	depChecker := parser.NewDependencyChecker()
	if workflow.Dependencies != nil {
		err = depChecker.CheckCommands(workflow.Dependencies.Commands, "")
		require.NoError(t, err, "Dependency check should pass for common commands like echo, cat")
	}
}

// TestRequirementsWorkflowFail tests workflow with missing dependencies
func TestRequirementsWorkflowFail(t *testing.T) {
	workflowsPath := getWorkflowsPath()

	p := parser.NewParser()
	workflow, err := p.Parse(filepath.Join(workflowsPath, "test-requirements-fail.yaml"))
	require.NoError(t, err)

	// Check dependencies - should fail for nonexistent commands
	depChecker := parser.NewDependencyChecker()
	if workflow.Dependencies != nil {
		err = depChecker.CheckCommands(workflow.Dependencies.Commands, "")
		require.Error(t, err, "Dependency check should fail for nonexistent commands")
	}
}

// TestLoadComplexWorkflows tests loading flow-type workflows
func TestLoadComplexWorkflows(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-flow")
	require.NoError(t, err)

	assert.Equal(t, "test-flow", workflow.Name)
	assert.Equal(t, core.KindFlow, workflow.Kind)
	assert.True(t, workflow.IsFlow())
	assert.Greater(t, len(workflow.Modules), 0, "Flow should have at least one module")
}

// TestListWorkflowsByKind tests listing workflows categorized by kind
func TestListWorkflowsByKind(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	flows, modules, err := loader.ListAllWorkflows()
	require.NoError(t, err)

	t.Logf("Found %d flows and %d modules", len(flows), len(modules))

	assert.Greater(t, len(flows), 0, "Expected at least one flow in workflows directory")
	assert.Greater(t, len(modules), 0, "Expected at least one module")
}

// TestDryRunExecution tests dry-run mode for workflow execution
func TestDryRunExecution(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-bash")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(true)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "dry-run-test",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
	// In dry-run mode, output should indicate dry-run
	assert.Contains(t, result.Steps[0].Output, "DRY-RUN")
}

// TestMissingRequiredParam tests that execution fails with missing required params
func TestMissingRequiredParam(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflow("test-bash")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	// Execute without required 'target' param
	_, err = exec.ExecuteModule(ctx, workflow, map[string]string{}, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target")
}

// TestWorkflowCaching tests that workflow caching works correctly
func TestWorkflowCaching(t *testing.T) {
	workflowsPath := getWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	// First load
	workflow1, err := loader.LoadWorkflow("test-bash")
	require.NoError(t, err)

	// Second load should return cached version (same pointer)
	workflow2, err := loader.LoadWorkflow("test-bash")
	require.NoError(t, err)

	assert.Same(t, workflow1, workflow2, "Expected same cached instance")

	// Clear cache and reload
	loader.ClearCache()

	workflow3, err := loader.LoadWorkflow("test-bash")
	require.NoError(t, err)

	assert.NotSame(t, workflow1, workflow3, "Expected different instance after cache clear")
}

// TestDecisionWorkflow tests decision/conditional routing
func TestDecisionWorkflow(t *testing.T) {
	workflowsPath := getWorkflowsPath()

	// Check if decision workflow exists
	decisionPath := filepath.Join(workflowsPath, "test-decision.yaml")
	if _, err := os.Stat(decisionPath); os.IsNotExist(err) {
		t.Skip("test-decision.yaml not found")
	}

	loader := parser.NewLoader(workflowsPath)
	workflow, err := loader.LoadWorkflow("test-decision")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target": "continue",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
}

func TestExecuteAICampaignHandoffModule(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-campaign-handoff.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	callsPath := installStubOsmedeus(t)
	targetSpace := "campaign-handoff-test"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "ai-decision-"+targetSpace+".json"), `{
  "focus_areas": ["authentication", "admin-panel"],
  "rescan_targets": ["https://app.example.com/recheck"]
}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "summary": {"total_targets": 2},
  "targets": [{"target": "https://app.example.com/login"}],
  "automation_queue": [{"target": "https://api.example.com/admin"}]
}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{
  "focus_targets": ["https://portal.example.com"],
  "tasks": [
    {"target": "https://api.example.com/admin", "priority": "P1", "title": "Verify admin exposure"}
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://api.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://app.example.com/dashboard\n")
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://app.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://api.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "rescan-summary-"+targetSpace+".md"), "# rescan summary\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload", "https://seed.example.com/graphql"]
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "next_phase": "manual-exploitation",
    "reuse_sources": ["operator-queue", "targeted-rescan"],
    "signal_scores": {"escalation_score": 17}
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation"
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                       "https://app.example.com",
		"space_name":                   targetSpace,
		"enableCampaignHandoff":        "true",
		"enableCampaignCreate":         "true",
		"campaignWorkflow":             "web-classic",
		"campaignWorkflowKind":         "flow",
		"campaignPriority":             "critical",
		"campaignDeepScanWorkflow":     "deep-web",
		"campaignDeepScanWorkflowKind": "module",
		"campaignAutoDeepScan":         "true",
		"campaignHighRiskSeverities":   "critical,high",
		"knowledgeWorkspace":           "shared-kb",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 6)
	for _, step := range result.Steps {
		assert.Equal(t, core.StepStatusSuccess, step.Status, "Step %s failed", step.StepName)
	}

	targetsData, err := os.ReadFile(filepath.Join(aiDir, "campaign-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t,
		[]string{
			"https://app.example.com/recheck",
			"https://app.example.com/login",
			"https://api.example.com/admin",
			"https://portal.example.com",
			"https://seed.example.com/admin",
			"https://seed.example.com/upload",
			"https://seed.example.com/graphql",
			"https://app.example.com/dashboard",
			"https://api.example.com/graphql",
		},
		strings.Split(strings.TrimSpace(string(targetsData)), "\n"),
	)

	handoffData, err := os.ReadFile(filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"))
	require.NoError(t, err)
	var handoff map[string]interface{}
	require.NoError(t, json.Unmarshal(handoffData, &handoff))
	campaignCreation, ok := handoff["campaign_creation"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "created", campaignCreation["status"])
	assert.Equal(t, "camp-123", campaignCreation["campaign_id"])
	assert.Equal(t, float64(3), campaignCreation["queued_runs"])
	counts, ok := handoff["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(9), counts["campaign_targets"])
	assert.Equal(t, float64(1), counts["semantic_priority_targets"])
	assert.Equal(t, float64(1), counts["decision_rescan_targets"])
	assert.Equal(t, float64(3), counts["previous_followup_targets"])

	profile, ok := handoff["campaign_profile"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", profile["previous_priority_mode"])
	assert.Equal(t, "high", profile["previous_confidence_level"])
	assert.Equal(t, "manual-exploitation", profile["previous_next_phase"])
	assert.Equal(t, float64(17), profile["previous_escalation_score"])
	reuseSources, ok := profile["previous_reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "operator-queue")
	assert.Contains(t, reuseSources, "targeted-rescan")

	targetGroups, ok := handoff["targets"].(map[string]interface{})
	require.True(t, ok)
	semanticPriority, ok := targetGroups["semantic_priority"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, semanticPriority, "https://api.example.com/graphql")

	artifacts, ok := handoff["artifacts"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, artifacts["semantic_priority_targets"], "semantic-priority-targets-decision-followup-"+targetSpace+".txt")

	createData, err := os.ReadFile(filepath.Join(aiDir, "campaign-create-"+targetSpace+".json"))
	require.NoError(t, err)
	var create map[string]interface{}
	require.NoError(t, json.Unmarshal(createData, &create))
	assert.Equal(t, "created", create["status"])
	assert.Equal(t, "camp-123", create["campaign_id"])
	assert.Equal(t, float64(3), create["queued_runs"])
	assert.Equal(t, "web-classic", create["workflow"])
	assert.Equal(t, "flow", create["workflow_kind"])
	assert.Equal(t, "critical", create["priority"])
	assert.Equal(t, float64(9), create["target_count"])
	assert.Equal(t, "manual-first", create["campaign_priority_mode"])
	assert.Equal(t, "high", create["campaign_confidence_level"])
	assert.Equal(t, "manual-exploitation", create["campaign_followup_next_phase"])
	assert.Equal(t, float64(3), create["previous_followup_targets"])
	assert.Equal(t, float64(17), create["campaign_escalation_score"])
	assert.Equal(t, true, create["auto_deep_scan"])

	callsData, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	callLine := strings.TrimSpace(string(callsData))
	assert.Contains(t, callLine, "--json campaign create")
	assert.Contains(t, callLine, "--name "+targetSpace+"-ai-handoff")
	assert.Contains(t, callLine, "-f web-classic")
	assert.Contains(t, callLine, "--priority critical")
	assert.Contains(t, callLine, "knowledgeWorkspace=shared-kb")
	assert.Contains(t, callLine, "campaign_source_target=https://app.example.com")
	assert.Contains(t, callLine, "campaign_handoff=")
	assert.Contains(t, callLine, "campaign_priority_mode=manual-first")
	assert.Contains(t, callLine, "campaign_confidence_level=high")
	assert.Contains(t, callLine, "campaign_followup_next_phase=manual-exploitation")
	assert.Contains(t, callLine, "campaign_previous_followup_targets=3")
	assert.Contains(t, callLine, "campaign_escalation_score=17")
	assert.Contains(t, callLine, "campaign_reuse_sources=operator-queue,targeted-rescan")
	assert.Contains(t, callLine, "--deep-scan-workflow deep-web")
	assert.Contains(t, callLine, "--deep-scan-workflow-kind module")
	assert.Contains(t, callLine, "--auto-deep-scan")
	assert.Contains(t, callLine, "--high-risk-severity critical")
	assert.Contains(t, callLine, "--high-risk-severity high")
}

func TestExecuteAICampaignHandoffFallbackTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-campaign-handoff.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "campaign-handoff-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "ai-decision-"+targetSpace+".json"), `{"focus_areas":["auth"]}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{"summary":{"total_targets":0},"targets":[],"automation_queue":[]}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{"summary":{"total_tasks":0},"focus_targets":[],"tasks":[]}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-"+targetSpace+".txt"), "https://cdn.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://app.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://api.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "aggressive",
    "severity": "critical,high,medium",
    "reasoning": "historical-queue-seed"
  },
  "seed_targets": {
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload"],
    "confirmed_targets": ["https://seed.example.com/confirmed"]
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high"
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                "https://app.example.com",
		"space_name":            targetSpace,
		"enableCampaignHandoff": "true",
		"enableCampaignCreate":  "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	targetsData, err := os.ReadFile(filepath.Join(aiDir, "campaign-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t,
		[]string{
			"https://seed.example.com/admin",
			"https://seed.example.com/upload",
			"https://seed.example.com/confirmed",
			"https://app.example.com/login",
			"https://api.example.com/admin",
			"https://cdn.example.com/login",
		},
		strings.Split(strings.TrimSpace(string(targetsData)), "\n"),
	)

	handoffData, err := os.ReadFile(filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"))
	require.NoError(t, err)
	var handoff map[string]interface{}
	require.NoError(t, json.Unmarshal(handoffData, &handoff))
	assert.Equal(t, true, handoff["handoff_ready"])

	counts, ok := handoff["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), counts["confirmed_urls"])
	assert.Equal(t, float64(1), counts["attack_chain_entry_points"])
	assert.Equal(t, float64(1), counts["semantic_priority_targets"])
	assert.Equal(t, float64(3), counts["previous_followup_targets"])
	assert.Equal(t, float64(6), counts["campaign_targets"])

	profile, ok := handoff["campaign_profile"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", profile["previous_priority_mode"])
	assert.Equal(t, "high", profile["previous_confidence_level"])
}

func TestExecuteAICampaignHandoffConsumesQueuedPreviousFollowupTargetLists(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-campaign-handoff.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "campaign-handoff-queued-followup-target-lists"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                  "https://app.example.com",
		"space_name":                              targetSpace,
		"enableCampaignHandoff":                   "true",
		"enableCampaignCreate":                    "false",
		"previous_followup_targets":               "3",
		"previous_followup_priority_mode":         "manual-first",
		"previous_followup_confidence_level":      "high",
		"previous_followup_next_phase":            "manual-exploitation",
		"previous_followup_reuse_sources":         "retest-queue,campaign-create",
		"previous_followup_escalation_score":      "14",
		"previous_followup_combined_targets_list": "https://queued.example.com/admin,https://queued.example.com/graphql,https://queued.example.com/upload",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	targetsData, err := os.ReadFile(filepath.Join(aiDir, "campaign-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{
			"https://queued.example.com/admin",
			"https://queued.example.com/graphql",
			"https://queued.example.com/upload",
		},
		strings.Split(strings.TrimSpace(string(targetsData)), "\n"),
	)

	handoffData, err := os.ReadFile(filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"))
	require.NoError(t, err)
	var handoff map[string]interface{}
	require.NoError(t, json.Unmarshal(handoffData, &handoff))
	assert.Equal(t, true, handoff["handoff_ready"])

	counts, ok := handoff["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(3), counts["previous_followup_targets"])
	assert.Equal(t, float64(3), counts["campaign_targets"])

	profile, ok := handoff["campaign_profile"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", profile["previous_priority_mode"])
	assert.Equal(t, "high", profile["previous_confidence_level"])
	assert.Equal(t, "manual-exploitation", profile["previous_next_phase"])
	assert.Equal(t, float64(14), profile["previous_escalation_score"])

	reuseSources, ok := profile["previous_reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "retest-queue")
	assert.Contains(t, reuseSources, "campaign-create")

	targetGroups, ok := handoff["targets"].(map[string]interface{})
	require.True(t, ok)
	previousFollowupTargets, ok := targetGroups["previous_followup"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, previousFollowupTargets, "https://queued.example.com/admin")
	assert.Contains(t, previousFollowupTargets, "https://queued.example.com/graphql")
	assert.Contains(t, previousFollowupTargets, "https://queued.example.com/upload")
}

func TestExecuteAICampaignHandoffIncludesRetestAndRescanSeedTargetsFromPreviousFollowup(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-campaign-handoff.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "campaign-handoff-followup-seed-retest-rescan"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "retest_targets": ["https://seed.example.com/admin", "https://seed.example.com/graphql"],
    "rescan_critical_targets": ["https://seed.example.com/admin"],
    "rescan_high_targets": ["https://seed.example.com/upload"],
    "confirmed_targets": ["https://seed.example.com/confirmed"]
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "next_phase": "manual-exploitation"
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                "https://app.example.com",
		"space_name":            targetSpace,
		"enableCampaignHandoff": "true",
		"enableCampaignCreate":  "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	targetsData, err := os.ReadFile(filepath.Join(aiDir, "campaign-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t,
		[]string{
			"https://seed.example.com/admin",
			"https://seed.example.com/graphql",
			"https://seed.example.com/upload",
			"https://seed.example.com/confirmed",
		},
		strings.Split(strings.TrimSpace(string(targetsData)), "\n"),
	)

	handoffData, err := os.ReadFile(filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"))
	require.NoError(t, err)
	var handoff map[string]interface{}
	require.NoError(t, json.Unmarshal(handoffData, &handoff))
	assert.Equal(t, true, handoff["handoff_ready"])

	counts, ok := handoff["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(4), counts["previous_followup_targets"])
	assert.Equal(t, float64(4), counts["campaign_targets"])

	targetGroups, ok := handoff["targets"].(map[string]interface{})
	require.True(t, ok)
	previousFollowup, ok := targetGroups["previous_followup"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://seed.example.com/admin",
		"https://seed.example.com/graphql",
		"https://seed.example.com/upload",
		"https://seed.example.com/confirmed",
	}, previousFollowup)
}

func TestExecuteAIOperatorQueueConsumesQueuedPreviousFollowupTargetLists(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-operator-queue.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-operator-queue" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "operator-queue-queued-followup-target-lists"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                  "https://app.example.com",
		"space_name":                              targetSpace,
		"previous_followup_targets":               "3",
		"previous_followup_priority_mode":         "manual-first",
		"previous_followup_confidence_level":      "high",
		"previous_followup_combined_targets_list": "https://queued.example.com/upload,https://queued.example.com/admin,https://queued.example.com/graphql",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "operator-queue-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(3), counts["previous_followup_targets"])

	previousFollowup, ok := payload["previous_followup"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", previousFollowup["priority_mode"])
	assert.Equal(t, "high", previousFollowup["confidence_level"])

	targetList, ok := previousFollowup["targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://queued.example.com/upload",
		"https://queued.example.com/admin",
		"https://queued.example.com/graphql",
	}, targetList)

	queueData, err := os.ReadFile(filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"))
	require.NoError(t, err)
	var queue map[string]interface{}
	require.NoError(t, json.Unmarshal(queueData, &queue))

	focusTargets, ok := queue["focus_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://queued.example.com/upload",
		"https://queued.example.com/admin",
		"https://queued.example.com/graphql",
	}, focusTargets)
}

func TestExecuteAIOperatorQueuePrefersDecisionFollowupSemanticContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-operator-queue.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-operator-queue" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "operator-queue-decision-followup-semantic"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	decisionSemantic := filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json")
	postSemantic := filepath.Join(aiDir, "semantic-search-post-vuln-"+targetSpace+".json")
	earlySemantic := filepath.Join(aiDir, "semantic-search-early-"+targetSpace+".json")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{"validation_summary":{"confirmed_real":1}}`)
	writeTestFile(t, decisionSemantic, `{
  "total_results": 2,
  "results": [
    {"content":"decision followup admin path","relevance_score":0.96},
    {"content":"decision followup graphql path","relevance_score":0.91}
  ]
}`)
	writeTestFile(t, postSemantic, `{
  "total_results": 1,
  "results": [
    {"content":"post vuln semantic context","relevance_score":0.82}
  ]
}`)
	writeTestFile(t, earlySemantic, `{
  "total_results": 1,
  "results": [
    {"content":"early semantic context","relevance_score":0.61}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "operator-queue-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	files, ok := payload["files"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, decisionSemantic, files["semantic_search"])
}

func TestExecuteAIOperatorQueueFallbackPreservesRetestPlanOrder(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-operator-queue.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-operator-queue" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "operator-queue-retest-order"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "summary": {"total_targets": 3},
  "targets": [
    {"target": "https://z.example.com/admin"},
    {"target": "https://a.example.com/login"}
  ],
  "automation_queue": [
    {"target": "https://m.example.com/graphql"},
    {"target": "https://a.example.com/login"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	queueData, err := os.ReadFile(filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"))
	require.NoError(t, err)
	var queue map[string]interface{}
	require.NoError(t, json.Unmarshal(queueData, &queue))

	focusTargets, ok := queue["focus_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://z.example.com/admin",
		"https://a.example.com/login",
		"https://m.example.com/graphql",
	}, focusTargets)
}

func TestExecuteAIOperatorQueueCountsRetestTargetsWithoutSummary(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-operator-queue.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-operator-queue" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "operator-queue-retest-no-summary"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "targets": [
    {"target": "https://b.example.com/admin"}
  ],
  "automation_queue": [
    {"target": "https://a.example.com/graphql"},
    {"target": "https://b.example.com/admin"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	_, err = os.Stat(filepath.Join(aiDir, ".operator-queue-skip"))
	assert.True(t, os.IsNotExist(err))

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "operator-queue-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), counts["retest_targets"])

	queueData, err := os.ReadFile(filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"))
	require.NoError(t, err)
	var queue map[string]interface{}
	require.NoError(t, json.Unmarshal(queueData, &queue))

	focusTargets, ok := queue["focus_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://b.example.com/admin",
		"https://a.example.com/graphql",
	}, focusTargets)
}

func TestExecuteAIOperatorQueueFallsBackToSemanticTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-operator-queue.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-operator-queue" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "operator-queue-semantic-targets"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json"), `{
  "total_results": 2,
  "results": [
    {"target":"https://semantic.example.com/admin","content":"admin surface"},
    {"target":"https://semantic.example.com/graphql","content":"graphql surface"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "operator-queue-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), counts["semantic_results"])
	assert.Equal(t, float64(2), counts["semantic_targets"])

	queueData, err := os.ReadFile(filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"))
	require.NoError(t, err)
	var queue map[string]interface{}
	require.NoError(t, json.Unmarshal(queueData, &queue))

	summary, ok := queue["summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), summary["total_tasks"])

	focusTargets, ok := queue["focus_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://semantic.example.com/admin",
		"https://semantic.example.com/graphql",
	}, focusTargets)
}

func TestExecuteAITargetedRescanCollectsPreviousFollowupTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-targeted-rescan.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		switch workflow.Steps[i].Name {
		case "run-targeted-nuclei", "extract-rescan-findings", "merge-rescan-findings-into-main-results", "refresh-clean-vuln-jsonl", "import-rescan-findings":
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-targeted-rescan-followup-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload"],
    "rescan_targets": ["https://seed.example.com/graphql"],
    "rescan_critical_targets": ["https://seed.example.com/admin"],
    "rescan_high_targets": ["https://seed.example.com/upload"],
    "confirmed_targets": ["https://seed.example.com/confirmed"]
  },
  "seed_focus": {
    "scan_profile": "balanced",
    "severity": "critical,high,medium"
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	targetsData, err := os.ReadFile(filepath.Join(aiDir, "rescan-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{
			"https://seed.example.com/admin",
			"https://seed.example.com/upload",
			"https://seed.example.com/graphql",
			"https://seed.example.com/confirmed",
		},
		strings.Split(strings.TrimSpace(string(targetsData)), "\n"),
	)

	_, err = os.Stat(filepath.Join(aiDir, ".rescan-skip"))
	assert.True(t, os.IsNotExist(err))

	cfgData, err := os.ReadFile(filepath.Join(aiDir, ".rescan-config-"+targetSpace+".sh"))
	require.NoError(t, err)
	cfgText := string(cfgData)
	assert.Contains(t, cfgText, "RESCAN_SEVERITY=critical,high,medium")
	assert.Contains(t, cfgText, "RESCAN_THREADS=12")
	assert.Contains(t, cfgText, "RESCAN_RATE_LIMIT=40")
	assert.Contains(t, cfgText, "RESCAN_TIMEOUT=21600")
}

func TestExecuteAITargetedRescanUsesDecisionFollowupSemanticPriorityTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-targeted-rescan.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		switch workflow.Steps[i].Name {
		case "run-targeted-nuclei", "extract-rescan-findings", "merge-rescan-findings-into-main-results", "refresh-clean-vuln-jsonl", "import-rescan-findings":
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-targeted-rescan-decision-semantic-priority"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://decision.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-post-vuln-"+targetSpace+".txt"), "https://post.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-early-"+targetSpace+".txt"), "https://early.example.com/api\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	targetsData, err := os.ReadFile(filepath.Join(aiDir, "rescan-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t, "https://decision.example.com/graphql\n", string(targetsData))
}

func TestExecuteAITargetedRescanUsesQueuedPreviousFollowupParams(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-targeted-rescan.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		switch workflow.Steps[i].Name {
		case "run-targeted-nuclei", "extract-rescan-findings", "merge-rescan-findings-into-main-results", "refresh-clean-vuln-jsonl", "import-rescan-findings":
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-targeted-rescan-queued-followup"
	aiDir := filepath.Join(cfg.WorkspacesPath, targetSpace, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                         "https://app.example.com",
		"space_name":                                     targetSpace,
		"previous_followup_targets":                      "3",
		"previous_followup_manual_first_targets":         "1",
		"previous_followup_high_confidence_targets":      "2",
		"previous_followup_manual_first_targets_list":    "https://seed.example.com/admin",
		"previous_followup_high_confidence_targets_list": "https://seed.example.com/upload,https://seed.example.com/graphql",
		"previous_followup_scan_profile":                 "balanced",
		"previous_followup_severity":                     "critical,high,medium",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	targetsData, err := os.ReadFile(filepath.Join(aiDir, "rescan-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{
			"https://seed.example.com/admin",
			"https://seed.example.com/upload",
			"https://seed.example.com/graphql",
		},
		strings.Split(strings.TrimSpace(string(targetsData)), "\n"),
	)

	_, err = os.Stat(filepath.Join(aiDir, ".rescan-skip"))
	assert.True(t, os.IsNotExist(err))

	cfgData, err := os.ReadFile(filepath.Join(aiDir, ".rescan-config-"+targetSpace+".sh"))
	require.NoError(t, err)
	cfgText := string(cfgData)
	assert.Contains(t, cfgText, "RESCAN_SEVERITY=critical,high,medium")
	assert.Contains(t, cfgText, "RESCAN_THREADS=12")
	assert.Contains(t, cfgText, "RESCAN_RATE_LIMIT=40")
	assert.Contains(t, cfgText, "RESCAN_TIMEOUT=21600")
}

func TestExecuteAIRetestQueueModule(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-queue.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	callsPath := installStubOsmedeus(t)
	targetSpace := "retest-queue-test"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "retest-targets-"+targetSpace+".txt"), "https://app.example.com/login\nhttps://api.example.com/admin\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":             "https://app.example.com",
		"space_name":         targetSpace,
		"enableRetestQueue":  "true",
		"retestFlow":         "vuln-validation",
		"retestWorkflowKind": "module",
		"retestPriority":     "critical",
		"knowledgeWorkspace": "shared-kb",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 3)
	for _, step := range result.Steps {
		assert.Equal(t, core.StepStatusSuccess, step.Status, "Step %s failed", step.StepName)
	}

	summaryData, err := os.ReadFile(filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"))
	require.NoError(t, err)
	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal(summaryData, &summary))
	assert.Equal(t, "queued", summary["status"])
	assert.Equal(t, "vuln-validation", summary["workflow"])
	assert.Equal(t, "module", summary["workflow_kind"])
	assert.Equal(t, "critical", summary["priority"])
	assert.Equal(t, float64(2), summary["queued_targets"])

	callsData, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	callLine := strings.TrimSpace(string(callsData))
	assert.Contains(t, callLine, "worker queue new -m vuln-validation")
	assert.Contains(t, callLine, "-p knowledgeWorkspace=shared-kb")
	assert.Contains(t, callLine, "-p campaign_stage=retest")
	assert.Contains(t, callLine, "-p campaign_source_target=https://app.example.com")
	assert.Contains(t, callLine, "-p retest_priority=critical")
}

func TestExecuteAIRetestQueueFallsBackToRetestPlanJSONBeforeFollowupSeed(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-queue.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	callsPath := installStubOsmedeus(t)
	targetSpace := "retest-queue-plan-json-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "summary": {"total_targets": 3},
  "targets": [
    {"target": "https://plan.example.com/admin"},
    {"target": "https://plan.example.com/login"}
  ],
  "automation_queue": [
    {"target": "https://plan.example.com/login"},
    {"target": "https://plan.example.com/api"}
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://seed.example.com/only"]
  },
  "seed_focus": {
    "reasoning": "seed-context-preserved"
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":             "https://app.example.com",
		"space_name":         targetSpace,
		"enableRetestQueue":  "true",
		"knowledgeWorkspace": "shared-kb",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"))
	require.NoError(t, err)
	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal(summaryData, &summary))
	assert.Equal(t, "queued", summary["status"])
	assert.Equal(t, "retest-plan-json", summary["target_source"])
	assert.Equal(t, float64(3), summary["queued_targets"])
	assert.Equal(t, "seed-context-preserved", summary["previous_reasoning"])

	fallbackTargetsData, err := os.ReadFile(filepath.Join(aiDir, ".retest-queue-fallback-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://plan.example.com/admin",
		"https://plan.example.com/login",
		"https://plan.example.com/api",
	}, strings.Split(strings.TrimSpace(string(fallbackTargetsData)), "\n"))
	assert.NotContains(t, string(fallbackTargetsData), "seed.example.com/only")

	callsData, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	callLine := strings.TrimSpace(string(callsData))
	assert.Contains(t, callLine, "worker queue new -f web-analysis")
	assert.Contains(t, callLine, ".retest-queue-fallback-targets-"+targetSpace+".txt")
	assert.Contains(t, callLine, "-p previous_followup_reasoning=seed-context-preserved")
}

func TestExecuteAIRetestQueueFallsBackToPreviousFollowupSeed(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-queue.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	callsPath := installStubOsmedeus(t)
	targetSpace := "retest-queue-followup-seed"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload"],
    "priority_targets": ["https://seed.example.com/admin", "https://seed.example.com/review"],
    "confirmed_targets": ["https://seed.example.com/confirmed"],
    "rescan_targets": ["https://seed.example.com/admin", "https://seed.example.com/rescan"]
  },
  "seed_focus": {
    "reasoning": "historical-queue-seed",
    "scan_profile": "aggressive",
    "severity": "critical,high,medium",
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "next_phase": "manual-exploitation",
    "reuse_sources": ["operator-queue", "targeted-rescan", "retest"],
    "signal_scores": {"escalation_score": 17},
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation",
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "followup_summary": {
    "campaign_create_status": "created",
    "campaign_create_id": "camp-retest-7",
    "campaign_create_queued_runs": 3
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":             "https://app.example.com",
		"space_name":         targetSpace,
		"enableRetestQueue":  "true",
		"retestFlow":         "web-analysis",
		"knowledgeWorkspace": "shared-kb",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"))
	require.NoError(t, err)
	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal(summaryData, &summary))
	assert.Equal(t, "queued", summary["status"])
	assert.Equal(t, "previous_followup_seed", summary["target_source"])
	assert.Equal(t, float64(5), summary["queued_targets"])
	assert.Equal(t, float64(5), summary["previous_followup_targets"])
	assert.Equal(t, "manual-first", summary["previous_priority_mode"])
	assert.Equal(t, "high", summary["previous_confidence_level"])
	assert.Equal(t, "manual-exploitation", summary["previous_next_phase"])
	assert.Equal(t, "historical-queue-seed", summary["previous_reasoning"])
	assert.Equal(t, "aggressive", summary["previous_scan_profile"])
	assert.Equal(t, "critical,high,medium", summary["previous_severity"])
	assert.Equal(t, true, summary["previous_manual_followup_needed"])
	assert.Equal(t, true, summary["previous_campaign_followup_recommended"])
	assert.Equal(t, true, summary["previous_queue_followup_effective"])
	assert.Equal(t, float64(17), summary["previous_escalation_score"])
	assert.Equal(t, "created", summary["previous_campaign_create_status"])
	assert.Equal(t, "camp-retest-7", summary["previous_campaign_create_id"])
	assert.Equal(t, float64(3), summary["previous_campaign_create_queued_runs"])
	manualTargetList, ok := summary["previous_manual_first_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"https://seed.example.com/admin"}, manualTargetList)
	highConfidenceTargetList, ok := summary["previous_high_confidence_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"https://seed.example.com/upload"}, highConfidenceTargetList)
	combinedTargetList, ok := summary["previous_combined_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://seed.example.com/admin",
		"https://seed.example.com/upload",
		"https://seed.example.com/review",
		"https://seed.example.com/confirmed",
		"https://seed.example.com/rescan",
	}, combinedTargetList)

	reuseSources, ok := summary["previous_reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "operator-queue")
	assert.Contains(t, reuseSources, "targeted-rescan")
	assert.Contains(t, reuseSources, "retest")

	fallbackTargetsData, err := os.ReadFile(filepath.Join(aiDir, ".retest-queue-fallback-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://seed.example.com/admin",
		"https://seed.example.com/upload",
		"https://seed.example.com/review",
		"https://seed.example.com/confirmed",
		"https://seed.example.com/rescan",
	}, strings.Split(strings.TrimSpace(string(fallbackTargetsData)), "\n"))

	callsData, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	callLine := strings.TrimSpace(string(callsData))
	assert.Contains(t, callLine, "worker queue new -f web-analysis")
	assert.Contains(t, callLine, ".retest-queue-fallback-targets-"+targetSpace+".txt")
	assert.Contains(t, callLine, "-p previous_followup_targets=5")
	assert.Contains(t, callLine, "-p previous_followup_manual_first_targets_list=https://seed.example.com/admin")
	assert.Contains(t, callLine, "-p previous_followup_high_confidence_targets_list=https://seed.example.com/upload")
	assert.Contains(t, callLine, "-p previous_followup_combined_targets_list=https://seed.example.com/admin,https://seed.example.com/upload,https://seed.example.com/review,https://seed.example.com/confirmed,https://seed.example.com/rescan")
	assert.Contains(t, callLine, "-p previous_followup_reasoning=historical-queue-seed")
	assert.Contains(t, callLine, "-p previous_followup_scan_profile=aggressive")
	assert.Contains(t, callLine, "-p previous_followup_severity=critical,high,medium")
	assert.Contains(t, callLine, "-p previous_followup_priority_mode=manual-first")
	assert.Contains(t, callLine, "-p previous_followup_confidence_level=high")
	assert.Contains(t, callLine, "-p previous_followup_next_phase=manual-exploitation")
	assert.Contains(t, callLine, "-p previous_followup_reuse_sources=operator-queue,targeted-rescan,retest")
	assert.Contains(t, callLine, "-p previous_followup_manual_followup_needed=true")
	assert.Contains(t, callLine, "-p previous_followup_campaign_followup_recommended=true")
	assert.Contains(t, callLine, "-p previous_followup_queue_followup_effective=true")
	assert.Contains(t, callLine, "-p previous_followup_escalation_score=17")
	assert.Contains(t, callLine, "-p previous_followup_campaign_create_status=created")
	assert.Contains(t, callLine, "-p previous_followup_campaign_create_id=camp-retest-7")
	assert.Contains(t, callLine, "-p previous_followup_campaign_create_queued_runs=3")
}

func TestExecuteAIRetestQueueFallsBackToQueuedPreviousFollowupParams(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-queue.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	callsPath := installStubOsmedeus(t)
	targetSpace := "retest-queue-queued-followup-params"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                          "https://app.example.com",
		"space_name":                                      targetSpace,
		"enableRetestQueue":                               "true",
		"retestFlow":                                      "web-analysis",
		"knowledgeWorkspace":                              "shared-kb",
		"previous_followup_targets":                       "4",
		"previous_followup_priority_targets":              "3",
		"previous_followup_focus_areas":                   "2",
		"previous_followup_manual_first_targets":          "2",
		"previous_followup_high_confidence_targets":       "1",
		"previous_followup_manual_first_targets_list":     "https://queued.example.com/upload,https://queued.example.com/admin",
		"previous_followup_high_confidence_targets_list":  "https://queued.example.com/graphql",
		"previous_followup_combined_targets_list":         "https://queued.example.com/upload,https://queued.example.com/admin,https://queued.example.com/graphql,https://queued.example.com/review",
		"previous_followup_reasoning":                     "queued-queue-seed",
		"previous_followup_scan_profile":                  "aggressive",
		"previous_followup_severity":                      "critical,high,medium",
		"previous_followup_priority_mode":                 "manual-first",
		"previous_followup_confidence_level":              "high",
		"previous_followup_next_phase":                    "manual-exploitation",
		"previous_followup_reuse_sources":                 "retest-queue,campaign-create",
		"previous_followup_manual_followup_needed":        "true",
		"previous_followup_campaign_followup_recommended": "true",
		"previous_followup_queue_followup_effective":      "true",
		"previous_followup_escalation_score":              "13",
		"previous_followup_campaign_create_status":        "created",
		"previous_followup_campaign_create_id":            "camp-retest-queued-9",
		"previous_followup_campaign_create_queued_runs":   "2",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"))
	require.NoError(t, err)
	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal(summaryData, &summary))
	assert.Equal(t, "queued", summary["status"])
	assert.Equal(t, "queue-params", summary["target_source"])
	assert.Equal(t, float64(4), summary["queued_targets"])
	assert.Equal(t, float64(4), summary["previous_followup_targets"])
	assert.Equal(t, "manual-first", summary["previous_priority_mode"])
	assert.Equal(t, "high", summary["previous_confidence_level"])
	assert.Equal(t, "manual-exploitation", summary["previous_next_phase"])
	assert.Equal(t, "queued-queue-seed", summary["previous_reasoning"])
	assert.Equal(t, "aggressive", summary["previous_scan_profile"])
	assert.Equal(t, "critical,high,medium", summary["previous_severity"])
	assert.Equal(t, true, summary["previous_manual_followup_needed"])
	assert.Equal(t, true, summary["previous_campaign_followup_recommended"])
	assert.Equal(t, true, summary["previous_queue_followup_effective"])
	assert.Equal(t, float64(13), summary["previous_escalation_score"])
	assert.Equal(t, "created", summary["previous_campaign_create_status"])
	assert.Equal(t, "camp-retest-queued-9", summary["previous_campaign_create_id"])
	assert.Equal(t, float64(2), summary["previous_campaign_create_queued_runs"])

	manualTargetList, ok := summary["previous_manual_first_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://queued.example.com/upload",
		"https://queued.example.com/admin",
	}, manualTargetList)

	highConfidenceTargetList, ok := summary["previous_high_confidence_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://queued.example.com/graphql",
	}, highConfidenceTargetList)

	combinedTargetList, ok := summary["previous_combined_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://queued.example.com/upload",
		"https://queued.example.com/admin",
		"https://queued.example.com/graphql",
		"https://queued.example.com/review",
	}, combinedTargetList)

	fallbackTargetsData, err := os.ReadFile(filepath.Join(aiDir, ".retest-queue-fallback-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://queued.example.com/upload",
		"https://queued.example.com/admin",
		"https://queued.example.com/graphql",
		"https://queued.example.com/review",
	}, strings.Split(strings.TrimSpace(string(fallbackTargetsData)), "\n"))

	callsData, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	callLine := strings.TrimSpace(string(callsData))
	assert.Contains(t, callLine, "worker queue new -f web-analysis")
	assert.Contains(t, callLine, ".retest-queue-fallback-targets-"+targetSpace+".txt")
	assert.Contains(t, callLine, "-p previous_followup_targets=4")
	assert.Contains(t, callLine, "-p previous_followup_manual_first_targets=2")
	assert.Contains(t, callLine, "-p previous_followup_high_confidence_targets=1")
	assert.Contains(t, callLine, "-p previous_followup_manual_first_targets_list=https://queued.example.com/upload,https://queued.example.com/admin")
	assert.Contains(t, callLine, "-p previous_followup_high_confidence_targets_list=https://queued.example.com/graphql")
	assert.Contains(t, callLine, "-p previous_followup_combined_targets_list=https://queued.example.com/upload,https://queued.example.com/admin,https://queued.example.com/graphql,https://queued.example.com/review")
	assert.Contains(t, callLine, "-p previous_followup_reasoning=queued-queue-seed")
	assert.Contains(t, callLine, "-p previous_followup_scan_profile=aggressive")
	assert.Contains(t, callLine, "-p previous_followup_severity=critical,high,medium")
	assert.Contains(t, callLine, "-p previous_followup_priority_mode=manual-first")
	assert.Contains(t, callLine, "-p previous_followup_confidence_level=high")
	assert.Contains(t, callLine, "-p previous_followup_next_phase=manual-exploitation")
	assert.Contains(t, callLine, "-p previous_followup_reuse_sources=retest-queue,campaign-create")
	assert.Contains(t, callLine, "-p previous_followup_manual_followup_needed=true")
	assert.Contains(t, callLine, "-p previous_followup_campaign_followup_recommended=true")
	assert.Contains(t, callLine, "-p previous_followup_queue_followup_effective=true")
	assert.Contains(t, callLine, "-p previous_followup_escalation_score=13")
	assert.Contains(t, callLine, "-p previous_followup_campaign_create_status=created")
	assert.Contains(t, callLine, "-p previous_followup_campaign_create_id=camp-retest-queued-9")
	assert.Contains(t, callLine, "-p previous_followup_campaign_create_queued_runs=2")
}

func TestExecuteAIRetestPlanningUsesFallbackSemanticContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-planning.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-retest-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-retest-planning-fallback-context"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	decisionSemantic := filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json")
	postSemantic := filepath.Join(aiDir, "semantic-search-post-vuln-"+targetSpace+".json")
	earlySemantic := filepath.Join(aiDir, "semantic-search-early-"+targetSpace+".json")
	decisionKnowledge := filepath.Join(aiDir, "knowledge-search-results-decision-followup-"+targetSpace+".json")
	postKnowledge := filepath.Join(aiDir, "knowledge-search-results-post-vuln-"+targetSpace+".json")
	earlyKnowledge := filepath.Join(aiDir, "knowledge-search-results-early-"+targetSpace+".json")
	decisionVector := filepath.Join(aiDir, "vector-kb-search-results-decision-followup-"+targetSpace+".json")
	postVector := filepath.Join(aiDir, "vector-kb-search-results-post-vuln-"+targetSpace+".json")
	earlyVector := filepath.Join(aiDir, "vector-kb-search-results-early-"+targetSpace+".json")

	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://app.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://app.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://api.example.com/graphql\n")

	writeTestFile(t, decisionSemantic, `{"total_results":0,"results":[]}`)
	writeTestFile(t, postSemantic, `{
  "total_results": 2,
  "results": [
    {"content":"auth bypass retest path","relevance_score":0.91},
    {"content":"graphql privilege check","relevance_score":0.88}
  ]
}`)
	writeTestFile(t, earlySemantic, `{"total_results":1,"results":[{"content":"early surface","relevance_score":0.75}]}`)

	writeTestFile(t, decisionKnowledge, `[]`)
	writeTestFile(t, postKnowledge, `[{"content":"post vuln auth knowledge","relevance_score":0.8}]`)
	writeTestFile(t, earlyKnowledge, `[{"content":"early knowledge","relevance_score":0.6}]`)

	writeTestFile(t, decisionVector, `[]`)
	writeTestFile(t, postVector, `[{"content":"vector post vuln guidance","relevance_score":0.87}]`)
	writeTestFile(t, earlyVector, `[{"content":"vector early hint","relevance_score":0.55}]`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                  "https://app.example.com",
		"space_name":                              targetSpace,
		"semanticSearchFile":                      decisionSemantic,
		"fallbackSemanticSearchFile":              postSemantic,
		"secondFallbackSemanticSearchFile":        earlySemantic,
		"knowledgeSearchFile":                     decisionKnowledge,
		"fallbackKnowledgeSearchFile":             postKnowledge,
		"secondFallbackKnowledgeSearchFile":       earlyKnowledge,
		"vectorKnowledgeSearchFile":               decisionVector,
		"fallbackVectorKnowledgeSearchFile":       postVector,
		"secondFallbackVectorKnowledgeSearchFile": earlyVector,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "retest-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	files, ok := payload["files"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, postSemantic, files["semantic_search"])
	assert.Equal(t, postKnowledge, files["knowledge_search"])
	assert.Equal(t, postVector, files["vector_knowledge_search"])

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), counts["semantic_results"])
	assert.Equal(t, float64(1), counts["knowledge_hits"])
	assert.Equal(t, float64(1), counts["vector_knowledge_hits"])
}

func TestExecuteAIRetestPlanningPrefersDecisionFollowupContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-planning.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-retest-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-retest-planning-decision-followup-context"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	decisionSemantic := filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json")
	postSemantic := filepath.Join(aiDir, "semantic-search-post-vuln-"+targetSpace+".json")
	decisionKnowledge := filepath.Join(aiDir, "knowledge-search-results-decision-followup-"+targetSpace+".json")
	postKnowledge := filepath.Join(aiDir, "knowledge-search-results-post-vuln-"+targetSpace+".json")
	decisionVector := filepath.Join(aiDir, "vector-kb-search-results-decision-followup-"+targetSpace+".json")
	postVector := filepath.Join(aiDir, "vector-kb-search-results-post-vuln-"+targetSpace+".json")

	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://app.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://app.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://api.example.com/graphql\n")

	writeTestFile(t, decisionSemantic, `{
  "total_results": 2,
  "results": [
    {"content":"decision followup auth retest","relevance_score":0.94},
    {"content":"decision followup graphql retest","relevance_score":0.89}
  ]
}`)
	writeTestFile(t, postSemantic, `{
  "total_results": 1,
  "results": [
    {"content":"post vuln auth retest","relevance_score":0.81}
  ]
}`)
	writeTestFile(t, decisionKnowledge, `[{"content":"decision followup auth knowledge","relevance_score":0.93}]`)
	writeTestFile(t, postKnowledge, `[{"content":"post vuln auth knowledge","relevance_score":0.79}]`)
	writeTestFile(t, decisionVector, `[{"content":"decision vector guidance","relevance_score":0.91}]`)
	writeTestFile(t, postVector, `[{"content":"post vector guidance","relevance_score":0.72}]`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "retest-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	files, ok := payload["files"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, decisionSemantic, files["semantic_search"])
	assert.Equal(t, decisionKnowledge, files["knowledge_search"])
	assert.Equal(t, decisionVector, files["vector_knowledge_search"])

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), counts["semantic_results"])
	assert.Equal(t, float64(1), counts["knowledge_hits"])
	assert.Equal(t, float64(1), counts["vector_knowledge_hits"])
}

func TestExecuteAIRetestPlanningMergesPreviousFollowupAdvisory(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-planning.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-retest-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-retest-planning-followup-advisory"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://confirmed.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://entry.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://best.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload"],
    "priority_targets": ["https://seed.example.com/review"],
    "confirmed_targets": ["https://seed.example.com/confirmed"],
    "rescan_targets": ["https://seed.example.com/admin", "https://seed.example.com/rescan"]
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "next_phase": "manual-exploitation",
    "reuse_sources": ["operator-queue", "targeted-rescan", "retest"],
    "signal_scores": {"escalation_score": 17},
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation",
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "followup_summary": {
    "campaign_create_status": "created",
    "campaign_create_id": "camp-retest-7",
    "campaign_create_queued_runs": 3
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "retest-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5), counts["previous_followup_targets"])
	assert.Equal(t, float64(1), counts["previous_followup_manual_first_targets"])
	assert.Equal(t, float64(1), counts["previous_followup_high_confidence_targets"])
	assert.Equal(t, float64(17), counts["previous_followup_escalation_score"])
	assert.Equal(t, float64(3), counts["previous_followup_campaign_create_queued_runs"])

	previousFollowup, ok := payload["previous_followup"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", previousFollowup["priority_mode"])
	assert.Equal(t, "high", previousFollowup["confidence_level"])
	assert.Equal(t, "manual-exploitation", previousFollowup["next_phase"])
	assert.Equal(t, true, previousFollowup["manual_followup_needed"])
	assert.Equal(t, true, previousFollowup["campaign_followup_recommended"])
	assert.Equal(t, true, previousFollowup["queue_followup_effective"])
	assert.Equal(t, float64(17), previousFollowup["escalation_score"])
	assert.Equal(t, "created", previousFollowup["campaign_create_status"])
	assert.Equal(t, "camp-retest-7", previousFollowup["campaign_create_id"])
	assert.Equal(t, float64(3), previousFollowup["campaign_create_queued_runs"])

	reuseSources, ok := previousFollowup["reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "operator-queue")
	assert.Contains(t, reuseSources, "targeted-rescan")
	assert.Contains(t, reuseSources, "retest")

	planData, err := os.ReadFile(filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	summary, ok := plan["summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", summary["previous_followup_priority_mode"])
	assert.Equal(t, "high", summary["previous_followup_confidence_level"])
	assert.Equal(t, "manual-exploitation", summary["previous_followup_next_phase"])
	assert.Equal(t, float64(17), summary["previous_followup_escalation_score"])
	assert.Equal(t, true, summary["previous_followup_manual_followup_needed"])
	assert.Equal(t, true, summary["previous_followup_campaign_followup_recommended"])
	assert.Equal(t, true, summary["previous_followup_queue_followup_effective"])
	assert.Equal(t, float64(1), summary["previous_followup_manual_first_targets"])
	assert.Equal(t, float64(1), summary["previous_followup_high_confidence_targets"])
	assert.Equal(t, "created", summary["previous_followup_campaign_create_status"])
	assert.Equal(t, "camp-retest-7", summary["previous_followup_campaign_create_id"])
	assert.Equal(t, float64(3), summary["previous_followup_campaign_create_queued_runs"])

	summaryReuseSources, ok := summary["previous_followup_reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, summaryReuseSources, "operator-queue")
	assert.Contains(t, summaryReuseSources, "targeted-rescan")
	assert.Contains(t, summaryReuseSources, "retest")

	findTarget := func(items []interface{}, target string) map[string]interface{} {
		t.Helper()
		for _, item := range items {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if entry["target"] == target {
				return entry
			}
		}
		t.Fatalf("target %s not found", target)
		return nil
	}

	findTargetWithTitle := func(items []interface{}, title, target string) map[string]interface{} {
		t.Helper()
		for _, item := range items {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if entry["title"] == title && entry["target"] == target {
				return entry
			}
		}
		t.Fatalf("target %s with title %s not found", target, title)
		return nil
	}

	targets, ok := plan["targets"].([]interface{})
	require.True(t, ok)
	adminTarget := findTarget(targets, "https://seed.example.com/admin")
	assert.Equal(t, "P1", adminTarget["priority"])
	assert.Contains(t, adminTarget["reason"], "previous followup manual-first")
	assert.Contains(t, adminTarget["reason"], "manual follow-up advised")

	uploadTarget := findTarget(targets, "https://seed.example.com/upload")
	assert.Equal(t, "P1", uploadTarget["priority"])
	assert.Contains(t, uploadTarget["reason"], "previous followup high-confidence")
	assert.Contains(t, uploadTarget["reason"], "queue follow-up already effective")

	manualChecks, ok := plan["manual_checks"].([]interface{})
	require.True(t, ok)
	manualEntry := findTargetWithTitle(manualChecks, "Previous followup manual-first", "https://seed.example.com/admin")
	assert.Equal(t, "Previous followup manual-first", manualEntry["title"])
	assert.Contains(t, manualEntry["reason"], "Inherited from previous followup seed")

	automationQueue, ok := plan["automation_queue"].([]interface{})
	require.True(t, ok)
	queueAdmin := findTarget(automationQueue, "https://seed.example.com/admin")
	assert.Equal(t, "high", queueAdmin["priority"])
	assert.Contains(t, queueAdmin["reason"], "previous followup manual-first")

	queueUpload := findTarget(automationQueue, "https://seed.example.com/upload")
	assert.Equal(t, "high", queueUpload["priority"])
	assert.Contains(t, queueUpload["reason"], "previous followup high-confidence")

	retestTargetsData, err := os.ReadFile(filepath.Join(aiDir, "retest-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	retestTargets := string(retestTargetsData)
	assert.Contains(t, retestTargets, "https://seed.example.com/admin")
	assert.Contains(t, retestTargets, "https://seed.example.com/upload")
	assert.Contains(t, retestTargets, "https://seed.example.com/review")

	markdownData, err := os.ReadFile(filepath.Join(aiDir, "retest-plan-"+targetSpace+".md"))
	require.NoError(t, err)
	markdown := string(markdownData)
	assert.Contains(t, markdown, "## Previous Follow-up Advisory")
	assert.Contains(t, markdown, "manual-first")
	assert.Contains(t, markdown, "Queue follow-up effective: true")
	assert.Contains(t, markdown, "## Automation Queue")
}

func TestExecuteAIRetestPlanningConsumesQueuedPreviousFollowupTargetLists(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-planning.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-retest-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-retest-planning-queued-followup-target-lists"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                          "https://app.example.com",
		"space_name":                                      targetSpace,
		"previous_followup_targets":                       "4",
		"previous_followup_priority_mode":                 "manual-first",
		"previous_followup_confidence_level":              "high",
		"previous_followup_next_phase":                    "manual-exploitation",
		"previous_followup_reuse_sources":                 "retest-queue,campaign-create",
		"previous_followup_manual_followup_needed":        "true",
		"previous_followup_campaign_followup_recommended": "true",
		"previous_followup_queue_followup_effective":      "true",
		"previous_followup_escalation_score":              "13",
		"previous_followup_campaign_create_status":        "created",
		"previous_followup_campaign_create_id":            "camp-plan-queued-9",
		"previous_followup_campaign_create_queued_runs":   "2",
		"previous_followup_manual_first_targets_list":     "https://queued.example.com/admin,https://queued.example.com/graphql",
		"previous_followup_high_confidence_targets_list":  "https://queued.example.com/upload",
		"previous_followup_combined_targets_list":         "https://queued.example.com/admin,https://queued.example.com/graphql,https://queued.example.com/upload,https://queued.example.com/review",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "retest-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(4), counts["previous_followup_targets"])
	assert.Equal(t, float64(2), counts["previous_followup_manual_first_targets"])
	assert.Equal(t, float64(1), counts["previous_followup_high_confidence_targets"])
	assert.Equal(t, float64(13), counts["previous_followup_escalation_score"])
	assert.Equal(t, float64(2), counts["previous_followup_campaign_create_queued_runs"])

	previousFollowup, ok := payload["previous_followup"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", previousFollowup["priority_mode"])
	assert.Equal(t, "high", previousFollowup["confidence_level"])
	assert.Equal(t, "manual-exploitation", previousFollowup["next_phase"])
	assert.Equal(t, true, previousFollowup["manual_followup_needed"])
	assert.Equal(t, true, previousFollowup["campaign_followup_recommended"])
	assert.Equal(t, true, previousFollowup["queue_followup_effective"])
	assert.Equal(t, float64(13), previousFollowup["escalation_score"])
	assert.Equal(t, "created", previousFollowup["campaign_create_status"])
	assert.Equal(t, "camp-plan-queued-9", previousFollowup["campaign_create_id"])
	assert.Equal(t, float64(2), previousFollowup["campaign_create_queued_runs"])

	manualTargetList, ok := previousFollowup["manual_first_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, manualTargetList, "https://queued.example.com/admin")
	assert.Contains(t, manualTargetList, "https://queued.example.com/graphql")

	highConfidenceTargetList, ok := previousFollowup["high_confidence_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, highConfidenceTargetList, "https://queued.example.com/upload")

	combinedTargetList, ok := previousFollowup["combined_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, combinedTargetList, "https://queued.example.com/admin")
	assert.Contains(t, combinedTargetList, "https://queued.example.com/graphql")
	assert.Contains(t, combinedTargetList, "https://queued.example.com/upload")
	assert.Contains(t, combinedTargetList, "https://queued.example.com/review")

	planData, err := os.ReadFile(filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	summary, ok := plan["summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", summary["previous_followup_priority_mode"])
	assert.Equal(t, "high", summary["previous_followup_confidence_level"])
	assert.Equal(t, "manual-exploitation", summary["previous_followup_next_phase"])
	assert.Equal(t, float64(13), summary["previous_followup_escalation_score"])
	assert.Equal(t, true, summary["previous_followup_manual_followup_needed"])
	assert.Equal(t, true, summary["previous_followup_campaign_followup_recommended"])
	assert.Equal(t, true, summary["previous_followup_queue_followup_effective"])
	assert.Equal(t, float64(2), summary["previous_followup_manual_first_targets"])
	assert.Equal(t, float64(1), summary["previous_followup_high_confidence_targets"])
	assert.Equal(t, "created", summary["previous_followup_campaign_create_status"])
	assert.Equal(t, "camp-plan-queued-9", summary["previous_followup_campaign_create_id"])
	assert.Equal(t, float64(2), summary["previous_followup_campaign_create_queued_runs"])

	retestTargetsData, err := os.ReadFile(filepath.Join(aiDir, "retest-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	retestTargets := string(retestTargetsData)
	assert.Contains(t, retestTargets, "https://queued.example.com/admin")
	assert.Contains(t, retestTargets, "https://queued.example.com/graphql")
	assert.Contains(t, retestTargets, "https://queued.example.com/upload")
	assert.Contains(t, retestTargets, "https://queued.example.com/review")
}

func TestExecuteAIRetestPlanningFallsBackToSemanticTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-retest-planning.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-retest-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-retest-planning-semantic-targets"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json"), `{
  "total_results": 2,
  "results": [
    {"target":"https://semantic.example.com/admin","content":"admin surface retest"},
    {"target":"https://semantic.example.com/graphql","content":"graphql retest"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, ".input", "retest-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	counts, ok := payload["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), counts["semantic_results"])
	assert.Equal(t, float64(2), counts["semantic_targets"])

	planData, err := os.ReadFile(filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	summary, ok := plan["summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), summary["total_targets"])

	targets, ok := plan["targets"].([]interface{})
	require.True(t, ok)
	require.Len(t, targets, 2)

	retestTargetsData, err := os.ReadFile(filepath.Join(aiDir, "retest-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://semantic.example.com/admin",
		"https://semantic.example.com/graphql",
	}, strings.Split(strings.TrimSpace(string(retestTargetsData)), "\n"))
}

func TestExecuteAIIntelligentAnalysisModule(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-intelligent-analysis.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-intelligent-analysis-test"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\nb.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\nhttps://b.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "a.example.com\nb.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","wordpress"]}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-critical-"+targetSpace+".txt"), "[critical] https://a.example.com - admin\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-high-"+targetSpace+".txt"), "[high] https://b.example.com - auth\n")
	writeTestFile(t, filepath.Join(outputDir, "waf", "waf-"+targetSpace+".txt"), "a.example.com,cloudflare\n")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{
  "confirmed_real": 3,
  "false_positives": 1,
  "risk_level": "高"
}`)
	writeTestFile(t, filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"), `{
  "attack_chain_summary": {"total_chains": 2, "critical_chains": 1}
}`)
	writeTestFile(t, filepath.Join(aiDir, "path-planning-"+targetSpace+".json"), `{
  "plan_summary": {"total_phases": 3},
  "phases": [{"name": "phase-1", "objective": "reach admin panel"}]
}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-early-"+targetSpace+".json"), `{
  "total_results": 4,
  "highlights": {"critical_findings": ["admin panel exposure"]}
}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-results-"+targetSpace+".json"), `{
  "total_results": 5,
  "highlights": {"critical_findings": ["auth bypass path"]}
}`)
	writeTestFile(t, filepath.Join(aiDir, "priority-targets-"+targetSpace+".txt"), "https://a.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://a.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://api.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://a.example.com/dashboard\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-"+targetSpace+".txt"), "https://b.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {"reasoning":"previous operator evidence"},
  "refined_targets": {
    "focus_areas": ["https://legacy.example.com/portal"],
    "priority_targets": ["https://legacy.example.com/graphql"]
  },
  "seed_focus": {
    "next_phase": "campaign-followup",
    "priority_mode": "campaign-first",
    "confidence_level": "medium",
    "reuse_sources": ["campaign-create", "retest-queue"],
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "execution_feedback": {"next_phase":"campaign-followup"},
  "followup_summary": {
    "campaign_create_status": "created",
    "campaign_create_id": "camp-intel-42",
    "campaign_create_queued_runs": 3
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 8)
	for _, step := range result.Steps {
		assert.Equal(t, core.StepStatusSuccess, step.Status, "Step %s failed", step.StepName)
	}

	decisionData, err := os.ReadFile(filepath.Join(aiDir, "ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var decision map[string]interface{}
	require.NoError(t, json.Unmarshal(decisionData, &decision))

	focusAreas, ok := decision["focus_areas"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, focusAreas, "https://a.example.com/admin")
	assert.Contains(t, focusAreas, "https://api.example.com/admin")

	rescanTargets, ok := decision["rescan_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, rescanTargets, "https://a.example.com/login")
	assert.Contains(t, rescanTargets, "https://a.example.com/dashboard")
	assert.Contains(t, rescanTargets, "https://b.example.com/graphql")
	assert.Contains(t, rescanTargets, "https://legacy.example.com/graphql")

	decisionInputs, ok := decision["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), decisionInputs["previous_followup_focus_count"])
	assert.Equal(t, float64(1), decisionInputs["previous_followup_priority_count"])
	assert.Equal(t, "campaign-followup", decisionInputs["previous_followup_next_phase"])
	assert.Equal(t, "created", decisionInputs["previous_followup_campaign_create_status"])
	assert.Equal(t, "camp-intel-42", decisionInputs["previous_followup_campaign_create_id"])
	assert.Equal(t, float64(3), decisionInputs["previous_followup_campaign_create_queued_runs"])
	assert.Equal(t, true, decisionInputs["previous_followup_manual_followup_needed"])
	assert.Equal(t, true, decisionInputs["previous_followup_campaign_followup_recommended"])
	assert.Equal(t, true, decisionInputs["previous_followup_queue_followup_effective"])
	reuseSources, ok := decisionInputs["previous_followup_reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "campaign-create")
	assert.Contains(t, reuseSources, "retest-queue")

	statusData, err := os.ReadFile(filepath.Join(aiDir, "ai-execution-status.json"))
	require.NoError(t, err)
	var status map[string]interface{}
	require.NoError(t, json.Unmarshal(statusData, &status))
	assert.Equal(t, "completed", status["overall_status"])
}

func TestExecuteAIIntelligentAnalysisFallsBackToDecisionFollowupSemanticPriorityTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-intelligent-analysis.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-intelligent-analysis-semantic-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-critical-"+targetSpace+".txt"), "[critical] https://a.example.com - admin\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-results-"+targetSpace+".json"), `{"total_results":1}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://fallback.example.com/graphql\nhttps://fallback.example.com/admin\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	decisionData, err := os.ReadFile(filepath.Join(aiDir, "ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var decision map[string]interface{}
	require.NoError(t, json.Unmarshal(decisionData, &decision))

	focusAreas, ok := decision["focus_areas"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, focusAreas, "https://fallback.example.com/graphql")
	assert.Contains(t, focusAreas, "https://fallback.example.com/admin")

	rescanTargets, ok := decision["rescan_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, rescanTargets, "https://fallback.example.com/graphql")
	assert.Contains(t, rescanTargets, "https://fallback.example.com/admin")
}

func TestExecuteAIApplyDecisionMergesPreviousFollowup(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-apply-decision.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-apply-decision-merge"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "ai-decision-"+targetSpace+".json"), `{
  "nuclei_severity": "critical,high",
  "suggested_threads": 12,
  "suggested_rate_limit": 40,
  "recommended_timeout": "6h",
  "focus_areas": ["https://curr.example.com/admin"],
  "priority_targets": ["https://curr.example.com/api"],
  "rescan_targets": ["https://curr.example.com/login"],
  "decision_inputs": {"knowledge_context_hits": 4},
  "reasoning": "current run decision"
}`)
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "balanced",
    "severity": "critical,high",
    "reasoning": "historical operator evidence"
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "reuse_sources": ["operator-queue", "campaign-create"],
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "refined_targets": {
    "focus_areas": ["https://hist.example.com/auth"],
    "priority_targets": ["https://hist.example.com/graphql"]
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation",
    "manual_followup_needed": true
  },
  "followup_summary": {
    "campaign_create_status": "created",
    "campaign_create_id": "camp-apply-7",
    "campaign_create_queued_runs": 2
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	appliedData, err := os.ReadFile(filepath.Join(aiDir, "applied-ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var applied map[string]interface{}
	require.NoError(t, json.Unmarshal(appliedData, &applied))

	source, ok := applied["source"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ai-decision", source["kind"])
	assert.Equal(t, true, source["followup_used"])

	targets, ok := applied["targets"].(map[string]interface{})
	require.True(t, ok)
	focusAreas, ok := targets["focus_areas"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, focusAreas, "https://curr.example.com/admin")
	assert.Contains(t, focusAreas, "https://hist.example.com/auth")

	priorityTargets, ok := targets["priority_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, priorityTargets, "https://curr.example.com/api")
	assert.Contains(t, priorityTargets, "https://hist.example.com/graphql")
	assert.Contains(t, priorityTargets, "https://hist.example.com/auth")

	rescanTargets, ok := targets["rescan_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, rescanTargets, "https://curr.example.com/login")
	assert.Contains(t, rescanTargets, "https://hist.example.com/graphql")

	decisionInputs, ok := applied["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, decisionInputs["followup_available"])
	assert.Equal(t, float64(1), decisionInputs["followup_focus_count"])
	assert.Equal(t, float64(1), decisionInputs["followup_priority_count"])
	assert.Equal(t, "manual-exploitation", decisionInputs["followup_next_phase"])
	assert.Equal(t, "created", decisionInputs["followup_campaign_create_status"])
	assert.Equal(t, "camp-apply-7", decisionInputs["followup_campaign_create_id"])
	assert.Equal(t, float64(2), decisionInputs["followup_campaign_create_queued_runs"])
	assert.Equal(t, true, decisionInputs["followup_manual_followup_needed"])
	assert.Equal(t, true, decisionInputs["followup_campaign_followup_recommended"])
	assert.Equal(t, true, decisionInputs["followup_queue_followup_effective"])
	applyReuseSources, ok := decisionInputs["followup_reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, applyReuseSources, "operator-queue")
	assert.Contains(t, applyReuseSources, "campaign-create")
}

func TestExecuteAIApplyDecisionFallbackToPreviousFollowup(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-apply-decision.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-apply-decision-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "balanced",
    "severity": "critical,high",
    "reasoning": "replay last successful exploitation path"
  },
  "refined_targets": {
    "focus_areas": ["https://legacy.example.com/admin"],
    "priority_targets": ["https://legacy.example.com/graphql"]
  },
  "execution_feedback": {
    "next_phase": "targeted-retest",
    "manual_followup_needed": true
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	appliedData, err := os.ReadFile(filepath.Join(aiDir, "applied-ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var applied map[string]interface{}
	require.NoError(t, json.Unmarshal(appliedData, &applied))

	source, ok := applied["source"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "previous-followup", source["kind"])
	assert.Equal(t, true, source["followup_used"])

	scan, ok := applied["scan"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "critical,high", scan["severity"])
	assert.Equal(t, "balanced", scan["profile"])
	assert.Equal(t, float64(12), scan["threads"])
	assert.Equal(t, float64(40), scan["rate_limit"])

	targets, ok := applied["targets"].(map[string]interface{})
	require.True(t, ok)
	priorityTargets, ok := targets["priority_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, priorityTargets, "https://legacy.example.com/graphql")
	assert.Contains(t, priorityTargets, "https://legacy.example.com/admin")
}

func TestExecuteAIApplyDecisionFallbackToSeedOnlyFollowup(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-apply-decision.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-apply-decision-seed-only-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "focused",
    "severity": "critical,high",
    "reasoning": "seed-only replay"
  },
  "seed_targets": {
    "focus_areas": ["authentication"],
    "priority_targets": ["https://seed.example.com/api"],
    "rescan_targets": ["https://seed.example.com/graphql"],
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload"],
    "rescan_critical_targets": ["https://seed.example.com/admin"],
    "rescan_high_targets": ["https://seed.example.com/upload"],
    "confirmed_targets": ["https://seed.example.com/confirmed"],
    "semantic_targets": ["https://seed.example.com/semantic"]
  },
  "seed_focus": {
    "scan_profile": "focused",
    "severity": "critical,high",
    "reasoning": "seed-only manual escalation",
    "next_phase": "manual-exploitation",
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "signal_scores": {
      "escalation_score": 18
    },
    "manual_followup_needed": true
  },
  "execution_feedback": {}
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	appliedData, err := os.ReadFile(filepath.Join(aiDir, "applied-ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var applied map[string]interface{}
	require.NoError(t, json.Unmarshal(appliedData, &applied))

	source, ok := applied["source"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "previous-followup", source["kind"])
	assert.Equal(t, true, source["followup_used"])

	scan, ok := applied["scan"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "critical,high", scan["severity"])
	assert.Equal(t, "aggressive", scan["profile"])
	assert.Equal(t, float64(15), scan["threads"])
	assert.Equal(t, float64(50), scan["rate_limit"])

	targets, ok := applied["targets"].(map[string]interface{})
	require.True(t, ok)
	focusAreas, ok := targets["focus_areas"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, focusAreas, "authentication")
	assert.Contains(t, focusAreas, "https://seed.example.com/admin")
	assert.Contains(t, focusAreas, "https://seed.example.com/upload")

	priorityTargets, ok := targets["priority_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, priorityTargets, "https://seed.example.com/api")
	assert.Contains(t, priorityTargets, "https://seed.example.com/admin")
	assert.Contains(t, priorityTargets, "https://seed.example.com/upload")
	assert.Contains(t, priorityTargets, "https://seed.example.com/confirmed")

	rescanTargets, ok := targets["rescan_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, rescanTargets, "https://seed.example.com/graphql")
	assert.Contains(t, rescanTargets, "https://seed.example.com/admin")
	assert.Contains(t, rescanTargets, "https://seed.example.com/upload")
	assert.Contains(t, rescanTargets, "https://seed.example.com/confirmed")

	decisionInputs, ok := applied["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, decisionInputs["followup_available"])
	assert.Equal(t, "manual-first", decisionInputs["followup_priority_mode"])
	assert.Equal(t, "high", decisionInputs["followup_confidence_level"])
	assert.Equal(t, float64(18), decisionInputs["followup_escalation_score"])
	assert.Equal(t, float64(1), decisionInputs["followup_manual_count"])
	assert.Equal(t, float64(2), decisionInputs["followup_high_confidence_count"])
}

func TestExecuteAIApplyDecisionFallbackToRetestAndSemanticSeedTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-apply-decision.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-apply-decision-retest-semantic-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "balanced",
    "severity": "critical,high",
    "reasoning": "retest and semantic seed replay"
  },
  "seed_targets": {
    "retest_targets": ["https://seed.example.com/admin", "https://seed.example.com/login"],
    "semantic_targets": ["https://seed.example.com/graphql"]
  },
  "seed_focus": {
    "priority_mode": "retest-first",
    "confidence_level": "medium",
    "next_phase": "targeted-retest"
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	appliedData, err := os.ReadFile(filepath.Join(aiDir, "applied-ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var applied map[string]interface{}
	require.NoError(t, json.Unmarshal(appliedData, &applied))

	source, ok := applied["source"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "previous-followup", source["kind"])
	assert.Equal(t, true, source["followup_used"])

	targets, ok := applied["targets"].(map[string]interface{})
	require.True(t, ok)

	focusAreas, ok := targets["focus_areas"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, focusAreas, "https://seed.example.com/admin")
	assert.Contains(t, focusAreas, "https://seed.example.com/login")
	assert.Contains(t, focusAreas, "https://seed.example.com/graphql")

	priorityTargets, ok := targets["priority_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, priorityTargets, "https://seed.example.com/admin")
	assert.Contains(t, priorityTargets, "https://seed.example.com/login")
	assert.Contains(t, priorityTargets, "https://seed.example.com/graphql")

	rescanTargets, ok := targets["rescan_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, rescanTargets, "https://seed.example.com/admin")
	assert.Contains(t, rescanTargets, "https://seed.example.com/login")
	assert.Contains(t, rescanTargets, "https://seed.example.com/graphql")

	decisionInputs, ok := applied["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(3), decisionInputs["followup_target_count"])
	assert.Equal(t, "retest-first", decisionInputs["followup_priority_mode"])
	assert.Equal(t, "medium", decisionInputs["followup_confidence_level"])
	assert.Equal(t, "targeted-retest", decisionInputs["followup_next_phase"])
}

func TestExecuteAIPreScanDecisionFallbackToPreviousFollowup(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-pre-scan-decision.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-pre-scan-analysis" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-pre-scan-followup-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "balanced",
    "severity": "critical,high",
    "reasoning": "previous manual validation"
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "reuse_sources": ["campaign-create", "operator-queue"],
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "refined_targets": {
    "focus_areas": ["authentication", "graphql"],
    "priority_targets": ["admin.example.com", "api.example.com"]
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation"
  },
  "followup_summary": {
    "campaign_create_status": "created",
    "campaign_create_id": "camp-pre-55",
    "campaign_create_queued_runs": 4
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":        "example.com",
		"space_name":    targetSpace,
		"pre_scan_json": "invalid-json",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	priorityTargetsData, err := os.ReadFile(filepath.Join(aiDir, "priority-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	priorityTargets := strings.Split(strings.TrimSpace(string(priorityTargetsData)), "\n")
	assert.ElementsMatch(t, []string{"admin.example.com", "api.example.com"}, priorityTargets)

	focusAreasData, err := os.ReadFile(filepath.Join(aiDir, "focus-areas-pre.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(focusAreasData), "authentication")
	assert.Contains(t, string(focusAreasData), "graphql")

	preDecisionData, err := os.ReadFile(filepath.Join(aiDir, "pre-ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var preDecision map[string]interface{}
	require.NoError(t, json.Unmarshal(preDecisionData, &preDecision))

	preDecisionPriority, ok := preDecision["priority_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, preDecisionPriority, "admin.example.com")
	assert.Contains(t, preDecisionPriority, "api.example.com")

	decisionInputs, ok := preDecision["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), decisionInputs["previous_followup_priority_targets"])
	assert.Equal(t, float64(2), decisionInputs["previous_followup_focus_areas"])
	assert.Equal(t, "manual-exploitation", decisionInputs["previous_followup_next_phase"])
	assert.Equal(t, "created", decisionInputs["previous_followup_campaign_create_status"])
	assert.Equal(t, "camp-pre-55", decisionInputs["previous_followup_campaign_create_id"])
	assert.Equal(t, float64(4), decisionInputs["previous_followup_campaign_create_queued_runs"])
	assert.Equal(t, true, decisionInputs["previous_followup_manual_followup_needed"])
	assert.Equal(t, true, decisionInputs["previous_followup_campaign_followup_recommended"])
	assert.Equal(t, true, decisionInputs["previous_followup_queue_followup_effective"])
	preReuseSources, ok := decisionInputs["previous_followup_reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, preReuseSources, "campaign-create")
	assert.Contains(t, preReuseSources, "operator-queue")
}

func TestExecuteAIPreScanDecisionACPBuildsPreviousFollowupContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-pre-scan-decision-acp.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-pre-scan" || workflow.Steps[i].Name == "save-pre-scan-results" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-pre-scan-acp-followup-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "osint", "emails-"+targetSpace+".txt"), "ops@example.com\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "balanced",
    "severity": "critical,high",
    "reasoning": "previous campaign-driven validation"
  },
  "seed_focus": {
    "reuse_sources": ["campaign-create", "operator-queue"],
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "refined_targets": {
    "focus_areas": ["authentication", "graphql"],
    "priority_targets": ["admin.example.com", "api.example.com"]
  },
  "execution_feedback": {
    "next_phase": "campaign-followup"
  },
  "followup_summary": {
    "campaign_create_status": "created",
    "campaign_create_id": "camp-acp-88",
    "campaign_create_queued_runs": 5
  }
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                "example.com",
		"space_name":            targetSpace,
		"enablePreScanDecision": "true",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	priorityTargetsData, err := os.ReadFile(filepath.Join(aiDir, ".input", "previous-followup-priority-targets.txt"))
	require.NoError(t, err)
	priorityTargets := strings.Split(strings.TrimSpace(string(priorityTargetsData)), "\n")
	assert.ElementsMatch(t, []string{"admin.example.com", "api.example.com"}, priorityTargets)

	focusAreasData, err := os.ReadFile(filepath.Join(aiDir, ".input", "previous-followup-focus-areas.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(focusAreasData), "authentication")
	assert.Contains(t, string(focusAreasData), "graphql")

	summaryData, err := os.ReadFile(filepath.Join(aiDir, ".input", "previous-followup-summary.json"))
	require.NoError(t, err)
	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal(summaryData, &summary))

	campaignCreate, ok := summary["campaign_create"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "created", campaignCreate["status"])
	assert.Equal(t, "camp-acp-88", campaignCreate["campaign_id"])
	assert.Equal(t, float64(5), campaignCreate["queued_runs"])
	reuseSourcesACP, ok := summary["reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSourcesACP, "campaign-create")
	assert.Contains(t, reuseSourcesACP, "operator-queue")

	counts, ok := summary["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), counts["priority_targets"])
	assert.Equal(t, float64(2), counts["focus_areas"])

	assert.Equal(t, "campaign-followup", summary["next_phase"])
	assert.Equal(t, true, summary["manual_followup_needed"])
	assert.Equal(t, true, summary["campaign_followup_recommended"])
	assert.Equal(t, true, summary["queue_followup_effective"])
}

func TestExecuteAIPreScanDecisionFallbackToQueuedPreviousFollowupParams(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-pre-scan-decision.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-pre-scan-analysis" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-pre-scan-queued-followup-params"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                          "example.com",
		"space_name":                                      targetSpace,
		"pre_scan_json":                                   "invalid-json",
		"previous_followup_targets":                       "6",
		"previous_followup_priority_targets":              "4",
		"previous_followup_focus_areas":                   "3",
		"previous_followup_manual_first_targets":          "2",
		"previous_followup_high_confidence_targets":       "1",
		"previous_followup_reasoning":                     "queued-manual-followup",
		"previous_followup_scan_profile":                  "aggressive",
		"previous_followup_severity":                      "critical,high,medium",
		"previous_followup_priority_mode":                 "manual-first",
		"previous_followup_confidence_level":              "high",
		"previous_followup_next_phase":                    "manual-exploitation",
		"previous_followup_reuse_sources":                 "retest-queue,campaign-create",
		"previous_followup_manual_followup_needed":        "true",
		"previous_followup_campaign_followup_recommended": "true",
		"previous_followup_queue_followup_effective":      "true",
		"previous_followup_escalation_score":              "14",
		"previous_followup_campaign_create_status":        "created",
		"previous_followup_campaign_create_id":            "camp-queued-12",
		"previous_followup_campaign_create_queued_runs":   "3",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(aiDir, ".input", "previous-followup-summary.json"))
	require.NoError(t, err)
	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal(summaryData, &summary))

	assert.Equal(t, true, summary["available"])
	assert.Equal(t, "queue-params", summary["source_kind"])
	assert.Equal(t, "aggressive", summary["base_profile"])
	assert.Equal(t, "critical,high,medium", summary["base_severity"])
	assert.Equal(t, "manual-exploitation", summary["next_phase"])
	assert.Equal(t, "queued-manual-followup", summary["reasoning"])
	reuseSources, ok := summary["reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "retest-queue")
	assert.Contains(t, reuseSources, "campaign-create")

	counts, ok := summary["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(6), counts["targets"])
	assert.Equal(t, float64(4), counts["priority_targets"])
	assert.Equal(t, float64(3), counts["focus_areas"])
	assert.Equal(t, float64(2), counts["manual_first_targets"])
	assert.Equal(t, float64(1), counts["high_confidence_targets"])

	preDecisionData, err := os.ReadFile(filepath.Join(aiDir, "pre-ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var preDecision map[string]interface{}
	require.NoError(t, json.Unmarshal(preDecisionData, &preDecision))
	assert.Equal(t, "critical,high,medium", preDecision["nuclei_severity"])
	assert.Equal(t, float64(15), preDecision["suggested_threads"])
	assert.Equal(t, float64(50), preDecision["suggested_rate_limit"])
	assert.Equal(t, "8h", preDecision["recommended_timeout"])

	decisionInputs, ok := preDecision["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "queue-params", decisionInputs["previous_followup_source_kind"])
	assert.Equal(t, float64(6), decisionInputs["previous_followup_targets"])
	assert.Equal(t, float64(4), decisionInputs["previous_followup_priority_targets"])
	assert.Equal(t, float64(3), decisionInputs["previous_followup_focus_areas"])
	assert.Equal(t, "queued-manual-followup", decisionInputs["previous_followup_reasoning"])
	assert.Equal(t, "aggressive", decisionInputs["previous_followup_scan_profile"])
	assert.Equal(t, "critical,high,medium", decisionInputs["previous_followup_severity"])
	assert.Equal(t, "manual-exploitation", decisionInputs["previous_followup_next_phase"])
}

func TestExecuteAIPreScanDecisionACPBuildsQueuedPreviousFollowupContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-pre-scan-decision-acp.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-pre-scan" || workflow.Steps[i].Name == "save-pre-scan-results" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-pre-scan-acp-queued-followup-params"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "www.example.com\nadmin.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "osint", "emails-"+targetSpace+".txt"), "ops@example.com\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                          "example.com",
		"space_name":                                      targetSpace,
		"enablePreScanDecision":                           "true",
		"previous_followup_targets":                       "5",
		"previous_followup_priority_targets":              "3",
		"previous_followup_focus_areas":                   "2",
		"previous_followup_manual_first_targets":          "1",
		"previous_followup_high_confidence_targets":       "1",
		"previous_followup_reasoning":                     "queued-acp-followup",
		"previous_followup_scan_profile":                  "balanced",
		"previous_followup_severity":                      "critical,high",
		"previous_followup_priority_mode":                 "campaign-first",
		"previous_followup_confidence_level":              "medium",
		"previous_followup_next_phase":                    "campaign-followup",
		"previous_followup_reuse_sources":                 "campaign-create,retest-queue",
		"previous_followup_manual_followup_needed":        "true",
		"previous_followup_campaign_followup_recommended": "true",
		"previous_followup_queue_followup_effective":      "true",
		"previous_followup_escalation_score":              "9",
		"previous_followup_campaign_create_status":        "created",
		"previous_followup_campaign_create_id":            "camp-acp-queued-3",
		"previous_followup_campaign_create_queued_runs":   "4",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(aiDir, ".input", "previous-followup-summary.json"))
	require.NoError(t, err)
	var summary map[string]interface{}
	require.NoError(t, json.Unmarshal(summaryData, &summary))

	assert.Equal(t, true, summary["available"])
	assert.Equal(t, "queue-params", summary["source_kind"])
	assert.Equal(t, "balanced", summary["base_profile"])
	assert.Equal(t, "critical,high", summary["base_severity"])
	assert.Equal(t, "campaign-followup", summary["next_phase"])
	assert.Equal(t, "queued-acp-followup", summary["reasoning"])
	campaignCreate, ok := summary["campaign_create"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "created", campaignCreate["status"])
	assert.Equal(t, "camp-acp-queued-3", campaignCreate["campaign_id"])
	assert.Equal(t, float64(4), campaignCreate["queued_runs"])
	counts, ok := summary["counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5), counts["targets"])
	assert.Equal(t, float64(3), counts["priority_targets"])
	assert.Equal(t, float64(2), counts["focus_areas"])
}

func TestExecuteAIApplyDecisionFallbackToPreviousFollowupParams(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-apply-decision.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-apply-decision-queued-followup-params"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                          "https://app.example.com",
		"space_name":                                      targetSpace,
		"previous_followup_targets":                       "7",
		"previous_followup_priority_targets":              "4",
		"previous_followup_focus_areas":                   "2",
		"previous_followup_manual_first_targets":          "2",
		"previous_followup_high_confidence_targets":       "1",
		"previous_followup_reasoning":                     "queued-apply-followup",
		"previous_followup_scan_profile":                  "aggressive",
		"previous_followup_severity":                      "critical,high,medium",
		"previous_followup_priority_mode":                 "manual-first",
		"previous_followup_confidence_level":              "high",
		"previous_followup_next_phase":                    "manual-exploitation",
		"previous_followup_reuse_sources":                 "retest-queue,campaign-create",
		"previous_followup_manual_followup_needed":        "true",
		"previous_followup_campaign_followup_recommended": "true",
		"previous_followup_queue_followup_effective":      "true",
		"previous_followup_escalation_score":              "13",
		"previous_followup_campaign_create_status":        "created",
		"previous_followup_campaign_create_id":            "camp-apply-queued-9",
		"previous_followup_campaign_create_queued_runs":   "2",
		"previous_followup_manual_first_targets_list":     "https://queued.example.com/admin,https://queued.example.com/graphql",
		"previous_followup_high_confidence_targets_list":  "https://queued.example.com/upload",
		"previous_followup_combined_targets_list":         "https://queued.example.com/admin,https://queued.example.com/graphql,https://queued.example.com/upload,https://queued.example.com/review",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	appliedData, err := os.ReadFile(filepath.Join(outputDir, "ai-analysis", "applied-ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var applied map[string]interface{}
	require.NoError(t, json.Unmarshal(appliedData, &applied))

	source, ok := applied["source"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "previous-followup-params", source["kind"])
	assert.Equal(t, "queue-params", source["followup_source_kind"])
	assert.Equal(t, true, source["followup_used"])

	scan, ok := applied["scan"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "critical,high,medium", scan["severity"])
	assert.Equal(t, "aggressive", scan["profile"])
	assert.Equal(t, float64(15), scan["threads"])
	assert.Equal(t, float64(50), scan["rate_limit"])

	decisionInputs, ok := applied["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, decisionInputs["followup_available"])
	assert.Equal(t, "queue-params", decisionInputs["followup_source_kind"])
	assert.Equal(t, float64(7), decisionInputs["followup_target_count"])
	assert.Equal(t, float64(4), decisionInputs["followup_priority_count"])
	assert.Equal(t, float64(2), decisionInputs["followup_focus_count"])
	assert.Equal(t, "queued-apply-followup", decisionInputs["followup_reasoning"])
	assert.Equal(t, "aggressive", decisionInputs["followup_scan_profile"])
	assert.Equal(t, "critical,high,medium", decisionInputs["followup_severity"])
	assert.Equal(t, "manual-exploitation", decisionInputs["followup_next_phase"])

	targets, ok := applied["targets"].(map[string]interface{})
	require.True(t, ok)
	expectedCombined := []interface{}{
		"https://queued.example.com/admin",
		"https://queued.example.com/graphql",
		"https://queued.example.com/upload",
		"https://queued.example.com/review",
	}
	focusAreas, ok := targets["focus_areas"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, expectedCombined, focusAreas)

	priorityTargets, ok := targets["priority_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, expectedCombined, priorityTargets)

	rescanTargets, ok := targets["rescan_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, expectedCombined, rescanTargets)

	manualTargetList, ok := decisionInputs["followup_manual_first_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://queued.example.com/admin",
		"https://queued.example.com/graphql",
	}, manualTargetList)

	highConfidenceTargetList, ok := decisionInputs["followup_high_confidence_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://queued.example.com/upload",
	}, highConfidenceTargetList)

	combinedTargetList, ok := decisionInputs["followup_combined_targets_list"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, expectedCombined, combinedTargetList)
}

func TestExecuteAIIntelligentAnalysisConsumesQueuedPreviousFollowupParams(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-intelligent-analysis.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-intelligent-analysis-queued-followup-params"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\nb.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\nhttps://b.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "a.example.com\nb.example.com\n")
	writeTestFile(t, filepath.Join(aiDir, "priority-targets-"+targetSpace+".txt"), "https://a.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://a.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://api.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://a.example.com/dashboard\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                          "https://app.example.com",
		"space_name":                                      targetSpace,
		"previous_followup_targets":                       "6",
		"previous_followup_priority_targets":              "3",
		"previous_followup_focus_areas":                   "2",
		"previous_followup_manual_first_targets":          "1",
		"previous_followup_high_confidence_targets":       "1",
		"previous_followup_reasoning":                     "queued-intel-followup",
		"previous_followup_scan_profile":                  "aggressive",
		"previous_followup_severity":                      "critical,high,medium",
		"previous_followup_priority_mode":                 "manual-first",
		"previous_followup_confidence_level":              "high",
		"previous_followup_next_phase":                    "manual-exploitation",
		"previous_followup_reuse_sources":                 "retest-queue,campaign-create",
		"previous_followup_manual_followup_needed":        "true",
		"previous_followup_campaign_followup_recommended": "true",
		"previous_followup_queue_followup_effective":      "true",
		"previous_followup_escalation_score":              "13",
		"previous_followup_campaign_create_status":        "created",
		"previous_followup_campaign_create_id":            "camp-intel-queued-7",
		"previous_followup_campaign_create_queued_runs":   "2",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	decisionData, err := os.ReadFile(filepath.Join(aiDir, "ai-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var decision map[string]interface{}
	require.NoError(t, json.Unmarshal(decisionData, &decision))

	assert.Equal(t, "critical,high,medium", decision["nuclei_severity"])
	assert.Equal(t, float64(15), decision["suggested_threads"])
	assert.Equal(t, float64(50), decision["suggested_rate_limit"])
	assert.Equal(t, "8h", decision["recommended_timeout"])

	decisionInputs, ok := decision["decision_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "queue-params", decisionInputs["previous_followup_source_kind"])
	assert.Equal(t, float64(6), decisionInputs["previous_followup_targets"])
	assert.Equal(t, float64(2), decisionInputs["previous_followup_focus_count"])
	assert.Equal(t, float64(3), decisionInputs["previous_followup_priority_count"])
	assert.Equal(t, "queued-intel-followup", decisionInputs["previous_followup_reasoning"])
	assert.Equal(t, "aggressive", decisionInputs["previous_followup_scan_profile"])
	assert.Equal(t, "critical,high,medium", decisionInputs["previous_followup_severity"])
	assert.Equal(t, "manual-exploitation", decisionInputs["previous_followup_next_phase"])
}

func TestExecuteAIPostFollowupCoordinationBuildsEnrichedSeedSignals(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-post-followup-coordination.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-post-followup-enriched-seed"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "ai-decision-"+targetSpace+".json"), `{
  "scan": {
    "profile": "focused",
    "severity": "critical,high"
  },
  "targets": {
    "focus_areas": ["authentication"],
    "rescan_targets": ["https://seed.example.com/graphql"]
  },
  "reasoning": "initial ai decision"
}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "summary": {"total_targets": 1},
  "targets": [{"target": "https://retest.example.com/admin"}],
  "automation_queue": [{"target": "https://queue.example.com/graphql"}]
}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{
  "summary": {"total_tasks": 2},
  "focus_targets": ["https://operator.example.com/admin"],
  "tasks": [
    {"target": "https://operator.example.com/admin"},
    {"target": "https://operator.example.com/upload"}
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"), `{
  "handoff_ready": true,
  "counts": {"campaign_targets": 1}
}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-create-"+targetSpace+".json"), `{
  "status": "created",
  "campaign_id": "camp-post-123",
  "queued_runs": 2
}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"), `{
  "queued_targets": 1,
  "status": "queued"
}`)
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-rescan-"+targetSpace+".jsonl"), strings.Join([]string{
		`{"info":{"severity":"critical"},"matched-at":"https://critical.example.com/admin"}`,
		`{"info":{"severity":"high"},"matched-at":"https://high.example.com/upload"}`,
	}, "\n")+"\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://semantic.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://confirmed.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-targets-"+targetSpace+".txt"), "https://operator.example.com/manual\n")
	writeTestFile(t, filepath.Join(aiDir, "retest-targets-"+targetSpace+".txt"), "https://retest.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "campaign-targets-"+targetSpace+".txt"), "https://campaign.example.com/api\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	followupData, err := os.ReadFile(filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var followup map[string]interface{}
	require.NoError(t, json.Unmarshal(followupData, &followup))

	seedFocus, ok := followup["seed_focus"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", seedFocus["priority_mode"])
	assert.Equal(t, "high", seedFocus["confidence_level"])
	assert.Equal(t, "manual-exploitation", seedFocus["next_phase"])
	assert.Equal(t, "aggressive", seedFocus["scan_profile"])

	reuseSources, ok := seedFocus["reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "retest")
	assert.Contains(t, reuseSources, "operator-queue")
	assert.Contains(t, reuseSources, "campaign-handoff")
	assert.Contains(t, reuseSources, "semantic-priority")
	assert.Contains(t, reuseSources, "confirmed-urls")
	assert.Contains(t, reuseSources, "targeted-rescan")

	signalScores, ok := seedFocus["signal_scores"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(24), signalScores["escalation_score"])
	assert.Equal(t, float64(1), signalScores["confirmed_urls"])
	assert.Equal(t, float64(2), signalScores["operator_tasks"])
	assert.Contains(t, reuseSources, "campaign-create")

	followupSummary, ok := followup["followup_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "created", followupSummary["campaign_create_status"])
	assert.Equal(t, "camp-post-123", followupSummary["campaign_create_id"])
	assert.Equal(t, float64(2), followupSummary["campaign_create_queued_runs"])

	nextActions, ok := followup["next_actions"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, nextActions, "Track campaign camp-post-123 and monitor 2 queued runs for follow-up evidence")

	seedTargets, ok := followup["seed_targets"].(map[string]interface{})
	require.True(t, ok)
	manualFirst, ok := seedTargets["manual_first_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://operator.example.com/admin",
		"https://operator.example.com/upload",
		"https://confirmed.example.com/login",
		"https://critical.example.com/admin",
		"https://high.example.com/upload",
		"https://retest.example.com/admin",
		"https://queue.example.com/graphql",
	}, manualFirst)

	highConfidence, ok := seedTargets["high_confidence_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://confirmed.example.com/login",
		"https://operator.example.com/admin",
		"https://operator.example.com/upload",
		"https://critical.example.com/admin",
		"https://high.example.com/upload",
		"https://retest.example.com/admin",
		"https://queue.example.com/graphql",
		"https://campaign.example.com/api",
		"https://semantic.example.com/login",
	}, highConfidence)
	assert.Contains(t, highConfidence, "https://high.example.com/upload")

	semanticTargets, ok := seedTargets["semantic_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, semanticTargets, "https://semantic.example.com/login")

	rescanTargets, ok := seedTargets["rescan_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, rescanTargets, "https://critical.example.com/admin")
	assert.Contains(t, rescanTargets, "https://high.example.com/upload")
}

func TestExecuteAIPostFollowupCoordinationCountsRetestTargetsWithoutSummary(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-post-followup-coordination.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-post-followup-retest-no-summary"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "targets": [
    {"target": "https://retest.example.com/admin"}
  ],
  "automation_queue": [
    {"target": "https://queue.example.com/graphql"},
    {"target": "https://retest.example.com/admin"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	followupData, err := os.ReadFile(filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"))
	require.NoError(t, err)
	var followup map[string]interface{}
	require.NoError(t, json.Unmarshal(followupData, &followup))

	followupSummary, ok := followup["followup_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), followupSummary["retest_targets"])

	seedFocus, ok := followup["seed_focus"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "retest-first", seedFocus["priority_mode"])
	assert.Equal(t, "medium", seedFocus["confidence_level"])
	assert.Equal(t, "targeted-retest", seedFocus["next_phase"])
	assert.Equal(t, "balanced", seedFocus["scan_profile"])

	reuseSources, ok := seedFocus["reuse_sources"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, reuseSources, "retest")

	signalScores, ok := seedFocus["signal_scores"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), signalScores["retest_targets"])
	assert.Equal(t, float64(4), signalScores["escalation_score"])

	seedTargets, ok := followup["seed_targets"].(map[string]interface{})
	require.True(t, ok)
	retestTargets, ok := seedTargets["retest_targets"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{
		"https://retest.example.com/admin",
		"https://queue.example.com/graphql",
	}, retestTargets)
}

func TestExecuteAISemanticSearchUsesSeedFollowupContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-semantic-search.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "semantic-search-agent" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-semantic-search-seed-followup"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "seed.example.com\napi.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://seed.example.com/admin\nhttps://api.example.com/graphql\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql","go"]}`+"\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "base_decision": {
    "profile": "focused",
    "severity": "critical,high",
    "reasoning": "old decision"
  },
  "refined_targets": {
    "focus_areas": [],
    "priority_targets": []
  },
  "seed_targets": {
    "focus_areas": ["graphql-auth"],
    "priority_targets": ["https://seed.example.com/admin"],
    "rescan_targets": ["https://seed.example.com/admin"],
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload"],
    "semantic_targets": ["https://seed.example.com/semantic"]
  },
  "seed_focus": {
    "scan_profile": "aggressive",
    "severity": "critical,high,medium",
    "reasoning": "seeded operator followup",
    "next_phase": "manual-exploitation",
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "reuse_sources": ["operator-queue", "targeted-rescan"],
    "signal_scores": {"escalation_score": 17},
    "manual_followup_needed": true,
    "queue_followup_effective": true
  },
  "execution_feedback": {}
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":               "https://app.example.com",
		"space_name":           targetSpace,
		"searchStage":          "decision-followup",
		"useVectorSearch":      "false",
		"includeKnowledgeBase": "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, "semantic-index", "resolved-search-query-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	assert.Equal(t, "decision-followup", payload["stage"])

	focusTargets, ok := payload["focus_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, focusTargets, "graphql-auth")
	assert.Contains(t, focusTargets, "https://seed.example.com/admin")

	candidateTargets, ok := payload["candidate_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, candidateTargets, "https://seed.example.com/admin")
	assert.Contains(t, candidateTargets, "https://seed.example.com/upload")

	decisionHints, ok := payload["decision_hints"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, decisionHints, "aggressive")
	assert.Contains(t, decisionHints, "critical,high,medium")
	assert.Contains(t, decisionHints, "manual-exploitation")
	assert.Contains(t, decisionHints, "manual-first")
	assert.Contains(t, decisionHints, "high")
	assert.Contains(t, decisionHints, "reuse-operator-queue")
	assert.Contains(t, decisionHints, "reuse-targeted-rescan")
	assert.Contains(t, decisionHints, "escalation-17")
	assert.Contains(t, decisionHints, "manual-followup")
	assert.Contains(t, decisionHints, "queue-followup")

	resolvedQueryData, err := os.ReadFile(filepath.Join(aiDir, "semantic-index", "resolved-search-query.txt"))
	require.NoError(t, err)
	resolvedQuery := string(resolvedQueryData)
	assert.Contains(t, resolvedQuery, "manual-exploitation")
	assert.Contains(t, resolvedQuery, "manual-first")
	assert.Contains(t, resolvedQuery, "graphql-auth")
	assert.Contains(t, resolvedQuery, "https://seed.example.com/admin")
}

func TestExecuteAISemanticSearchConsumesQueuedPreviousFollowupParams(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-semantic-search.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "semantic-search-agent" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-semantic-search-queued-followup-params"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "queued.example.com\napi.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://queued.example.com/admin\nhttps://api.example.com/graphql\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql","go"]}`+"\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                                          "https://app.example.com",
		"space_name":                                      targetSpace,
		"searchStage":                                     "decision-followup",
		"useVectorSearch":                                 "false",
		"includeKnowledgeBase":                            "false",
		"previous_followup_targets":                       "6",
		"previous_followup_priority_targets":              "4",
		"previous_followup_focus_areas":                   "3",
		"previous_followup_manual_first_targets":          "2",
		"previous_followup_high_confidence_targets":       "1",
		"previous_followup_reasoning":                     "queued-manual-followup",
		"previous_followup_scan_profile":                  "aggressive",
		"previous_followup_severity":                      "critical,high,medium",
		"previous_followup_priority_mode":                 "manual-first",
		"previous_followup_confidence_level":              "high",
		"previous_followup_next_phase":                    "manual-exploitation",
		"previous_followup_reuse_sources":                 "retest-queue,campaign-create",
		"previous_followup_manual_followup_needed":        "true",
		"previous_followup_campaign_followup_recommended": "true",
		"previous_followup_queue_followup_effective":      "true",
		"previous_followup_escalation_score":              "14",
		"previous_followup_campaign_create_status":        "created",
		"previous_followup_campaign_create_id":            "camp-semantic-queued-12",
		"previous_followup_campaign_create_queued_runs":   "3",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, "semantic-index", "resolved-search-query-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	assert.Equal(t, "decision-followup", payload["stage"])
	assert.Equal(t, true, payload["previous_followup_available"])
	assert.Equal(t, "queue-params", payload["previous_followup_source_kind"])
	assert.Equal(t, float64(6), payload["previous_followup_targets"])
	assert.Equal(t, float64(4), payload["previous_followup_priority_targets"])
	assert.Equal(t, float64(3), payload["previous_followup_focus_areas"])
	assert.Equal(t, "queued-manual-followup", payload["previous_followup_reasoning"])
	assert.Equal(t, "manual-exploitation", payload["previous_followup_next_phase"])
	assert.Equal(t, "created", payload["previous_followup_campaign_create_status"])

	candidateTargets, ok := payload["candidate_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, candidateTargets, "https://queued.example.com/admin")

	focusTargets, ok := payload["focus_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, focusTargets, "manual-exploitation")
	assert.Contains(t, focusTargets, "manual-first")
	assert.Contains(t, focusTargets, "queued-manual-followup")

	decisionHints, ok := payload["decision_hints"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, decisionHints, "aggressive")
	assert.Contains(t, decisionHints, "critical,high,medium")
	assert.Contains(t, decisionHints, "manual-exploitation")
	assert.Contains(t, decisionHints, "manual-first")
	assert.Contains(t, decisionHints, "high")
	assert.Contains(t, decisionHints, "previous-followup-queue-params")
	assert.Contains(t, decisionHints, "followup-targets-6")
	assert.Contains(t, decisionHints, "followup-priority-4")
	assert.Contains(t, decisionHints, "followup-focus-3")
	assert.Contains(t, decisionHints, "manual-first-targets-2")
	assert.Contains(t, decisionHints, "high-confidence-targets-1")
	assert.Contains(t, decisionHints, "reuse-retest-queue")
	assert.Contains(t, decisionHints, "reuse-campaign-create")
	assert.Contains(t, decisionHints, "escalation-14")
	assert.Contains(t, decisionHints, "manual-followup")
	assert.Contains(t, decisionHints, "campaign-followup")
	assert.Contains(t, decisionHints, "queue-followup")
	assert.Contains(t, decisionHints, "campaign-create-created")
	assert.Contains(t, decisionHints, "campaign-queued-runs-3")

	resolvedQueryData, err := os.ReadFile(filepath.Join(aiDir, "semantic-index", "resolved-search-query.txt"))
	require.NoError(t, err)
	resolvedQuery := string(resolvedQueryData)
	assert.Contains(t, resolvedQuery, "manual-exploitation")
	assert.Contains(t, resolvedQuery, "manual-first")
	assert.Contains(t, resolvedQuery, "queued-manual-followup")
	assert.Contains(t, resolvedQuery, "https://queued.example.com/admin")
}

func TestExecuteAISemanticSearchFallsBackToDecisionFollowupSemanticPriorityTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-semantic-search.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "semantic-search-agent" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-semantic-search-semantic-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "fallback.example.com\napi.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://fallback.example.com/login\nhttps://api.example.com/graphql\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql"]}`+"\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://fallback.example.com/graphql\nhttps://fallback.example.com/admin\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":               "https://app.example.com",
		"space_name":           targetSpace,
		"searchStage":          "decision-followup",
		"useVectorSearch":      "false",
		"includeKnowledgeBase": "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, "semantic-index", "resolved-search-query-context.json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &payload))

	candidateTargets, ok := payload["candidate_targets"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, candidateTargets, "https://fallback.example.com/graphql")
	assert.Contains(t, candidateTargets, "https://fallback.example.com/admin")

	resolvedQueryData, err := os.ReadFile(filepath.Join(aiDir, "semantic-index", "resolved-search-query.txt"))
	require.NoError(t, err)
	resolvedQuery := string(resolvedQueryData)
	assert.Contains(t, resolvedQuery, "https://fallback.example.com/graphql")
	assert.Contains(t, resolvedQuery, "https://fallback.example.com/admin")
}

func TestExecuteAIAttackChainFallbackGeneration(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-attack-chain.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-analyze-attack-chains" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-attack-chain-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{
  "findings": [
    {"status":"confirmed","type":"SQL Injection","url":"https://app.example.com/api?id=1","severity":"critical"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":            "https://app.example.com",
		"space_name":        targetSpace,
		"attack_chain_json": "analysis text without valid json",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	attackChainData, err := os.ReadFile(filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"))
	require.NoError(t, err)
	var attackChain map[string]interface{}
	require.NoError(t, json.Unmarshal(attackChainData, &attackChain))

	summary, ok := attackChain["attack_chain_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), summary["total_chains"])

	entryPoints, ok := summary["most_likely_entry_points"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, entryPoints, "https://app.example.com/api?id=1")

	bestTargetsData, err := os.ReadFile(filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(bestTargetsData), "https://app.example.com/api?id=1")
}

func TestExecuteAIAttackChainFallsBackToDecisionFollowupContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-attack-chain.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-analyze-attack-chains" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-attack-chain-followup-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{
  "validation_summary": {"confirmed_real": 1},
  "findings": [
    {
      "status": "confirmed",
      "type": "Auth Bypass",
      "url": "https://portal.example.com/admin",
      "severity": "critical",
      "confidence": 0.96
    }
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://portal.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://portal.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "knowledge-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "source":"knowledge-base",
    "title":"Admin auth chain",
    "section":"authentication",
    "relevance_score":0.95,
    "snippet":"When admin and GraphQL surfaces coexist, verify auth context reuse and privilege escalation first."
  }
]`)
	writeTestFile(t, filepath.Join(aiDir, "vector-kb-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "title":"GraphQL pivot",
    "section":"api",
    "score":0.91,
    "snippet":"GraphQL often becomes a pivot after admin auth boundary weaknesses."
  }
]`)
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql","go"]}`+"\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":            "https://portal.example.com",
		"space_name":        targetSpace,
		"attack_chain_json": "",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	preparedData, err := os.ReadFile(filepath.Join(aiDir, "attack-chain-input-"+targetSpace+".json"))
	require.NoError(t, err)
	var prepared map[string]interface{}
	require.NoError(t, json.Unmarshal(preparedData, &prepared))

	activeInputs, ok := prepared["active_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, activeInputs["semantic_priority_targets"], "semantic-priority-targets-decision-followup-"+targetSpace+".txt")
	assert.Contains(t, activeInputs["knowledge_search"], "knowledge-search-results-decision-followup-"+targetSpace+".json")
	assert.Contains(t, activeInputs["vector_knowledge_search"], "vector-kb-search-results-decision-followup-"+targetSpace+".json")

	preparedContext, ok := prepared["prepared_context"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), preparedContext["confirmed_vuln_count"])
	assert.Equal(t, float64(1), preparedContext["semantic_context_count"])
	assert.Equal(t, float64(2), preparedContext["knowledge_context_count"])

	attackChainData, err := os.ReadFile(filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"))
	require.NoError(t, err)
	var attackChain map[string]interface{}
	require.NoError(t, json.Unmarshal(attackChainData, &attackChain))

	summary, ok := attackChain["attack_chain_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), summary["total_chains"])

	entryPoints, ok := summary["most_likely_entry_points"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, entryPoints, "https://portal.example.com/admin")

	bestTargetsData, err := os.ReadFile(filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(bestTargetsData), "https://portal.example.com/admin")
}

func TestExecuteAIAttackChainACPFallsBackToDecisionFollowupContext(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-attack-chain-acp.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-analyze-attack-chains" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-attack-chain-acp-followup-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{
  "validation_summary": {"confirmed_real": 1},
  "findings": [
    {
      "status": "confirmed",
      "type": "Auth Bypass",
      "url": "https://portal.example.com/admin",
      "severity": "critical",
      "confidence": 0.96
    }
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://portal.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://portal.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "knowledge-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "source":"knowledge-base",
    "title":"Admin auth chain",
    "section":"authentication",
    "relevance_score":0.95,
    "snippet":"When admin and GraphQL surfaces coexist, verify auth context reuse and privilege escalation first."
  }
]`)
	writeTestFile(t, filepath.Join(aiDir, "vector-kb-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "title":"GraphQL pivot",
    "section":"api",
    "score":0.91,
    "snippet":"GraphQL often becomes a pivot after admin auth boundary weaknesses."
  }
]`)
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql","go"]}`+"\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":            "https://portal.example.com",
		"space_name":        targetSpace,
		"attack_chain_json": "",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	preparedData, err := os.ReadFile(filepath.Join(aiDir, "attack-chain-input-"+targetSpace+".json"))
	require.NoError(t, err)
	var prepared map[string]interface{}
	require.NoError(t, json.Unmarshal(preparedData, &prepared))

	activeInputs, ok := prepared["active_inputs"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, activeInputs["semantic_priority_targets"], "semantic-priority-targets-decision-followup-"+targetSpace+".txt")
	assert.Contains(t, activeInputs["knowledge_search"], "knowledge-search-results-decision-followup-"+targetSpace+".json")
	assert.Contains(t, activeInputs["vector_knowledge_search"], "vector-kb-search-results-decision-followup-"+targetSpace+".json")

	preparedContext, ok := prepared["prepared_context"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), preparedContext["confirmed_vuln_count"])
	assert.Equal(t, float64(1), preparedContext["semantic_context_count"])
	assert.Equal(t, float64(2), preparedContext["knowledge_context_count"])

	attackChainData, err := os.ReadFile(filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"))
	require.NoError(t, err)
	var attackChain map[string]interface{}
	require.NoError(t, json.Unmarshal(attackChainData, &attackChain))

	summary, ok := attackChain["attack_chain_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), summary["total_chains"])

	entryPoints, ok := summary["most_likely_entry_points"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, entryPoints, "https://portal.example.com/admin")

	bestTargetsData, err := os.ReadFile(filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(bestTargetsData), "https://portal.example.com/admin")
}

func TestExecuteAIAttackChainPreservesNoDataSkipOutput(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-attack-chain.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-attack-chain-no-data"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://empty.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	_, err = os.Stat(filepath.Join(aiDir, ".attack-chain-skip"))
	require.NoError(t, err)

	attackChainData, err := os.ReadFile(filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"))
	require.NoError(t, err)
	var attackChain map[string]interface{}
	require.NoError(t, json.Unmarshal(attackChainData, &attackChain))

	assert.Equal(t, "no_data", attackChain["error"])
	summary, ok := attackChain["attack_chain_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(0), summary["total_chains"])

	chains, ok := attackChain["attack_chains"].([]interface{})
	require.True(t, ok)
	assert.Len(t, chains, 0)
}

func TestExecuteAIAttackChainACPPreservesNoDataSkipOutput(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-attack-chain-acp.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-attack-chain-acp-no-data"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://empty.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	_, err = os.Stat(filepath.Join(aiDir, ".attack-chain-skip"))
	require.NoError(t, err)

	attackChainData, err := os.ReadFile(filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"))
	require.NoError(t, err)
	var attackChain map[string]interface{}
	require.NoError(t, json.Unmarshal(attackChainData, &attackChain))

	assert.Equal(t, "no_data", attackChain["error"])
	summary, ok := attackChain["attack_chain_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(0), summary["total_chains"])

	chains, ok := attackChain["attack_chains"].([]interface{})
	require.True(t, ok)
	assert.Len(t, chains, 0)
}

func TestExecuteAIVulnValidationFallbackGeneration(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-vuln-validation.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-validate-vulns" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-vuln-validation-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-jsonl-"+targetSpace+".txt"), `{"template-id":"critical-admin","info":{"severity":"critical"},"matched-at":"https://app.example.com/admin"}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-critical-"+targetSpace+".txt"), "[critical] https://app.example.com/admin - admin exposure\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-high-"+targetSpace+".txt"), "[high] https://app.example.com/login - auth weakness\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":          "https://app.example.com",
		"space_name":      targetSpace,
		"validation_json": "analysis text without valid json",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	validationData, err := os.ReadFile(filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"))
	require.NoError(t, err)
	var validation map[string]interface{}
	require.NoError(t, json.Unmarshal(validationData, &validation))

	assert.Equal(t, "parse_failed_fallback", validation["error"])
	assert.Equal(t, float64(0), validation["confirmed_real"])
	assert.GreaterOrEqual(t, validation["needs_manual_verification"].(float64), 1.0)

	findings, ok := validation["findings"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, findings)

	firstFinding, ok := findings[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "needs_manual_verification", firstFinding["status"])
	assert.NotEmpty(t, firstFinding["url"])

	manualCommands, err := os.ReadFile(filepath.Join(aiDir, "validation", "manual-commands.sh"))
	require.NoError(t, err)
	assert.Contains(t, string(manualCommands), "curl -skI")

	validatedList, err := os.ReadFile(filepath.Join(aiDir, "validated-vulns-"+targetSpace+".json"))
	require.NoError(t, err)
	assert.Contains(t, string(validatedList), "needs_manual_verification")
}

func TestExecuteAIVulnValidationACPFallbackGeneration(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-vuln-validation-acp.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-validate-vulns" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-vuln-validation-acp-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-jsonl-"+targetSpace+".txt"), `{"template-id":"critical-admin","info":{"severity":"critical"},"matched-at":"https://app.example.com/admin"}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-critical-"+targetSpace+".txt"), "[critical] https://app.example.com/admin - admin exposure\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-high-"+targetSpace+".txt"), "[high] https://app.example.com/login - auth weakness\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","wordpress"]}`+"\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":          "https://app.example.com",
		"space_name":      targetSpace,
		"validation_json": "analysis text without valid json",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	validationData, err := os.ReadFile(filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"))
	require.NoError(t, err)
	var validation map[string]interface{}
	require.NoError(t, json.Unmarshal(validationData, &validation))

	assert.Equal(t, "parse_failed_fallback", validation["error"])
	assert.Equal(t, float64(0), validation["confirmed_real"])
	assert.GreaterOrEqual(t, validation["needs_manual_verification"].(float64), 1.0)

	findings, ok := validation["findings"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, findings)

	firstFinding, ok := findings[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "needs_manual_verification", firstFinding["status"])
	assert.NotEmpty(t, firstFinding["url"])

	manualCommands, err := os.ReadFile(filepath.Join(aiDir, "validation", "manual-commands.sh"))
	require.NoError(t, err)
	assert.Contains(t, string(manualCommands), "curl -skI")

	validatedList, err := os.ReadFile(filepath.Join(aiDir, "validated-vulns-"+targetSpace+".json"))
	require.NoError(t, err)
	assert.Contains(t, string(validatedList), "needs_manual_verification")
}

func TestExecuteAISemanticSearchFallbackNormalization(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-semantic-search.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "semantic-search-agent" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-semantic-search-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "app.example.com\napi.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://app.example.com/admin\nhttps://api.example.com/graphql\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql"]}`+"\n")
	writeTestFile(t, filepath.Join(aiDir, "vector-search-results-"+targetSpace+".json"), `{
  "status":"success",
  "total_results":2,
  "results":[
    {"type":"vector_match","content":"admin surface at https://app.example.com/admin requires auth review","relevance_score":0.93,"source":"vector_search"},
    {"type":"vector_match","content":"graphql endpoint exposed at https://api.example.com/graphql","relevance_score":0.89,"source":"vector_search"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":               "https://app.example.com",
		"space_name":           targetSpace,
		"useVectorSearch":      "false",
		"includeKnowledgeBase": "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	searchData, err := os.ReadFile(filepath.Join(aiDir, "semantic-search-results-"+targetSpace+".json"))
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(searchData, &payload))
	assert.Equal(t, "fallback_completed", payload["status"])

	results, ok := payload["results"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, results)

	first, ok := results[0].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, first["target"])
	assert.NotEmpty(t, first["severity_hint"])
	assert.NotEmpty(t, first["action_type"])

	priorityTargets, err := os.ReadFile(filepath.Join(aiDir, "semantic-priority-targets-"+targetSpace+".txt"))
	require.NoError(t, err)
	assert.Contains(t, string(priorityTargets), "https://app.example.com/admin")
	assert.Contains(t, string(priorityTargets), "https://api.example.com/graphql")

	priorityVulns, err := os.ReadFile(filepath.Join(aiDir, "semantic-priority-vulns-"+targetSpace+".json"))
	require.NoError(t, err)
	assert.Contains(t, string(priorityVulns), "admin_surface")
	assert.Contains(t, string(priorityVulns), "api_surface")
}

func TestExecuteAISemanticSearchUsesWorkspaceLocalKBLogs(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-semantic-search.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "semantic-search-agent" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	installKBSearchStubOsmedeus(t)

	targetSpace := "ai-semantic-search-local-kb-logs"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "app.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://app.example.com/admin\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql"]}`+"\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                   "https://app.example.com",
		"space_name":               targetSpace,
		"knowledgeWorkspace":       "primary-kb",
		"sharedKnowledgeWorkspace": "shared-kb",
		"globalKnowledgeWorkspace": "global",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	knowledgeIndexData, err := os.ReadFile(filepath.Join(aiDir, "semantic-index", "knowledge-index.txt"))
	require.NoError(t, err)
	knowledgeIndex := string(knowledgeIndexData)
	assert.Contains(t, knowledgeIndex, "[primary-kb] exported knowledge chunk")
	assert.Contains(t, knowledgeIndex, "[shared-kb] exported knowledge chunk")
	assert.Contains(t, knowledgeIndex, "[global] exported knowledge chunk")

	vectorData, err := os.ReadFile(filepath.Join(aiDir, "vector-search-results-"+targetSpace+".json"))
	require.NoError(t, err)
	var vectorPayload map[string]interface{}
	require.NoError(t, json.Unmarshal(vectorData, &vectorPayload))
	assert.Equal(t, "config-default", vectorPayload["provider"])
	assert.Equal(t, float64(3), vectorPayload["total_results"])

	logDir := filepath.Join(aiDir, "semantic-index", "logs")
	primaryExportLog, err := os.ReadFile(filepath.Join(logDir, "semantic-kb-export-primary.log"))
	require.NoError(t, err)
	assert.Contains(t, string(primaryExportLog), "export primary-kb")

	sharedExportLog, err := os.ReadFile(filepath.Join(logDir, "semantic-kb-export-shared.log"))
	require.NoError(t, err)
	assert.Contains(t, string(sharedExportLog), "export shared-kb")

	globalExportLog, err := os.ReadFile(filepath.Join(logDir, "semantic-kb-export-global.log"))
	require.NoError(t, err)
	assert.Contains(t, string(globalExportLog), "export global")

	primaryVectorLog, err := os.ReadFile(filepath.Join(logDir, "semantic-kb-vector-search-primary.log"))
	require.NoError(t, err)
	assert.Contains(t, string(primaryVectorLog), "vector primary-kb")

	primaryKeywordLog, err := os.ReadFile(filepath.Join(logDir, "semantic-kb-search-primary.log"))
	require.NoError(t, err)
	assert.Contains(t, string(primaryKeywordLog), "keyword primary-kb")
}

func TestExecuteAIHybridSemanticSearchUsesWorkspaceLocalKBLogs(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-semantic-search-hybrid.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	installKBSearchStubOsmedeus(t)

	targetSpace := "ai-hybrid-semantic-search-local-kb-logs"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "app.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://app.example.com/admin\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","graphql"]}`+"\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                   "https://app.example.com",
		"space_name":               targetSpace,
		"knowledgeWorkspace":       "primary-kb",
		"sharedKnowledgeWorkspace": "shared-kb",
		"globalKnowledgeWorkspace": "global",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	vectorData, err := os.ReadFile(filepath.Join(aiDir, "hybrid-vector-search-results-"+targetSpace+".json"))
	require.NoError(t, err)
	var vectorPayload map[string]interface{}
	require.NoError(t, json.Unmarshal(vectorData, &vectorPayload))
	assert.Equal(t, "config-default", vectorPayload["provider"])
	assert.Equal(t, float64(3), vectorPayload["total_results"])

	logDir := filepath.Join(aiDir, "semantic-index", "logs")
	primaryExportLog, err := os.ReadFile(filepath.Join(logDir, "hybrid-kb-export-primary.log"))
	require.NoError(t, err)
	assert.Contains(t, string(primaryExportLog), "export primary-kb")

	sharedExportLog, err := os.ReadFile(filepath.Join(logDir, "hybrid-kb-export-shared.log"))
	require.NoError(t, err)
	assert.Contains(t, string(sharedExportLog), "export shared-kb")

	globalExportLog, err := os.ReadFile(filepath.Join(logDir, "hybrid-kb-export-global.log"))
	require.NoError(t, err)
	assert.Contains(t, string(globalExportLog), "export global")

	primaryVectorLog, err := os.ReadFile(filepath.Join(logDir, "hybrid-kb-vector-search-primary.log"))
	require.NoError(t, err)
	assert.Contains(t, string(primaryVectorLog), "vector primary-kb")

	primaryKeywordLog, err := os.ReadFile(filepath.Join(logDir, "hybrid-kb-search-primary.log"))
	require.NoError(t, err)
	assert.Contains(t, string(primaryKeywordLog), "keyword primary-kb")
}

func TestExecuteAIPathPlanningFallbackGeneration(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-path-planning.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-attack-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-path-planning-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"), `{
  "attack_chain_summary": {
    "total_chains": 1,
    "most_likely_entry_points": ["https://app.example.com/admin"]
  },
  "attack_chains": [
    {
      "chain_id": "chain-1",
      "entry_point": {"url":"https://app.example.com/admin","vulnerability":"Auth Bypass","severity":"high"},
      "final_objective": "Admin access"
    }
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://app.example.com/admin\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":             "https://app.example.com",
		"space_name":         targetSpace,
		"path_planning_json": "analysis text without valid json",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	planData, err := os.ReadFile(filepath.Join(aiDir, "path-planning-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	summary, ok := plan["plan_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), summary["total_phases"])

	checklist, ok := plan["verification_checklist"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, checklist)

	attackPlanData, err := os.ReadFile(filepath.Join(aiDir, "attack-plan-"+targetSpace+".md"))
	require.NoError(t, err)
	assert.Contains(t, string(attackPlanData), "https://app.example.com/admin")
}

func TestExecuteAIPathPlanningFallsBackToDecisionFollowupInputs(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-path-planning.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-attack-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-path-planning-followup-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{
  "confirmed_real": 2,
  "risk_level": "高",
  "findings": [
    {"url":"https://app.example.com/admin","status":"confirmed","type":"auth-bypass"},
    {"url":"https://api.example.com/graphql","status":"confirmed","type":"graphql-introspection"}
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://app.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://app.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://app.example.com/dashboard\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://api.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "knowledge-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "source":"knowledge-base",
    "title":"Auth bypass validation",
    "section":"authentication",
    "relevance_score":0.94,
    "snippet":"Admin and GraphQL surfaces often need auth boundary replay and token abuse checks."
  }
]`)
	writeTestFile(t, filepath.Join(aiDir, "vector-kb-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "title":"GraphQL exploitation",
    "section":"api",
    "score":0.91,
    "snippet":"Prioritize introspection, auth context reuse, and mutation abuse."
  }
]`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":             "https://app.example.com",
		"space_name":         targetSpace,
		"path_planning_json": "",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	planData, err := os.ReadFile(filepath.Join(aiDir, "path-planning-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	summary, ok := plan["plan_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), summary["total_phases"])

	phases, ok := plan["execution_phases"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, phases)

	attackPlanData, err := os.ReadFile(filepath.Join(aiDir, "attack-plan-"+targetSpace+".md"))
	require.NoError(t, err)
	assert.Contains(t, string(attackPlanData), "https://api.example.com/graphql")
	assert.Contains(t, string(attackPlanData), "https://app.example.com/admin")
}

func TestExecuteAIPathPlanningPreservesNoDataSkipOutput(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-path-planning.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-path-planning-no-data"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://empty.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	_, err = os.Stat(filepath.Join(aiDir, ".path-planning-skip"))
	require.NoError(t, err)

	planData, err := os.ReadFile(filepath.Join(aiDir, "path-planning-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	assert.Equal(t, "no_data", plan["error"])
	summary, ok := plan["plan_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(0), summary["total_phases"])

	phases, ok := plan["execution_phases"].([]interface{})
	require.True(t, ok)
	assert.Len(t, phases, 0)
}

func TestExecuteAIPathPlanningACPFallbackUsesPreparedPriorityTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-path-planning-acp.yaml"))
	require.NoError(t, err)

	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ai-generate-attack-plan" {
			workflow.Steps[i].PreCondition = "false"
		}
	}

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-path-planning-acp-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{
  "confirmed_real": 1,
  "risk_level": "中",
  "findings": [
    {"url":"https://portal.example.com/admin","status":"confirmed","type":"admin-panel"}
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://portal.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://portal.example.com/api\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://portal.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "knowledge-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "source":"knowledge-base",
    "title":"Admin panel playbook",
    "section":"admin",
    "relevance_score":0.93,
    "snippet":"Start with low-noise admin auth checks, then verify GraphQL and API pivots."
  }
]`)
	writeTestFile(t, filepath.Join(aiDir, "vector-kb-search-results-decision-followup-"+targetSpace+".json"), `[
  {
    "title":"API pivot guidance",
    "section":"api",
    "score":0.89,
    "snippet":"Re-test admin session reuse against adjacent API and GraphQL endpoints."
  }
]`)
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"url":"https://portal.example.com","tech":["nginx","graphql"]}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "waf", "waf-"+targetSpace+".txt"), "portal.example.com,cloudflare\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":             "https://portal.example.com",
		"space_name":         targetSpace,
		"path_planning_json": "",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	planData, err := os.ReadFile(filepath.Join(aiDir, "path-planning-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	summary, ok := plan["plan_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), summary["total_phases"])

	attackPlanData, err := os.ReadFile(filepath.Join(aiDir, "attack-plan-"+targetSpace+".md"))
	require.NoError(t, err)
	assert.Contains(t, string(attackPlanData), "https://portal.example.com/admin")
	assert.Contains(t, string(attackPlanData), "https://portal.example.com/graphql")
}

func TestExecuteAIPathPlanningACPPreservesNoDataSkipOutput(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-path-planning-acp.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "ai-path-planning-acp-no-data"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://empty.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	_, err = os.Stat(filepath.Join(aiDir, ".path-planning-skip"))
	require.NoError(t, err)

	planData, err := os.ReadFile(filepath.Join(aiDir, "path-planning-"+targetSpace+".json"))
	require.NoError(t, err)
	var plan map[string]interface{}
	require.NoError(t, json.Unmarshal(planData, &plan))

	assert.Equal(t, "no_data", plan["error"])
	summary, ok := plan["plan_summary"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(0), summary["total_phases"])

	phases, ok := plan["execution_phases"].([]interface{})
	require.True(t, ok)
	assert.Len(t, phases, 0)
}

func TestExecuteFinalReportWithAIArtifacts(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "common", "10-report.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "report-ai-artifacts"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")
	reportDir := filepath.Join(outputDir, "report")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\nb.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\nhttps://b.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "a.example.com\nb.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "fingerprint", "http-fingerprint-"+targetSpace+".jsonl"), `{"tech":["nginx","wordpress"]}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "content-analysis", "graphql-endpoints-"+targetSpace+".txt"), "https://a.example.com/graphql\n")
	writeTestFile(t, filepath.Join(outputDir, "vuln-scan-suite", "secrets-"+targetSpace+".txt"), "AWS_KEY=redacted\n")
	writeTestFile(t, filepath.Join(outputDir, "vuln-scan-suite", "takeover-"+targetSpace+".txt"), "orphan.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-jsonl-"+targetSpace+".txt"), `{"template-id":"test-critical","info":{"name":"Critical issue","severity":"critical"},"matched-at":"https://a.example.com/admin"}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-critical-"+targetSpace+".txt"), `{"template-id":"test-critical","info":{"name":"Critical issue","severity":"critical"},"matched-at":"https://a.example.com/admin"}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-high-"+targetSpace+".txt"), `{"template-id":"test-high","info":{"name":"High issue","severity":"high"},"matched-at":"https://b.example.com/login"}`+"\n")

	writeTestFile(t, filepath.Join(aiDir, "vuln-validation-"+targetSpace+".json"), `{"confirmed_real":2,"false_positives":1,"risk_level":"高"}`)
	writeTestFile(t, filepath.Join(aiDir, "attack-chain-"+targetSpace+".json"), `{
  "attack_chain_summary":{"total_chains":1,"most_likely_entry_points":["https://a.example.com/admin"]},
  "attack_chains":[{"chain_name":"Auth Bypass -> Admin","entry_point":{"url":"https://a.example.com/admin"},"impact":"admin access","success_probability":0.8}]
}`)
	writeTestFile(t, filepath.Join(aiDir, "path-planning-"+targetSpace+".json"), `{
  "plan_summary":{"total_phases":2,"estimated_total_time":"45分钟","success_probability":0.65,"risk_level":"中"},
  "execution_phases":[{"phase":1,"phase_name":"Validate admin","success_criteria":"confirm auth boundary"}],
  "verification_checklist":[{"step":1,"verification_point":"check admin","command":"curl -sk https://a.example.com/admin","expected":"reachable"}]
}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-post-vuln-"+targetSpace+".json"), `{"total_results":4}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "summary":{"total_targets":1},
  "targets":[{"target":"https://a.example.com/admin","priority":"P1","reason":"confirmed auth surface"}]
}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{
  "summary":{"total_tasks":1},
  "tasks":[{"priority":"P1","title":"Manual auth review","target":"https://a.example.com/admin","reason":"privilege boundary"}]
}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"), `{
  "handoff_ready":true,
  "campaign_profile":{"recommended_flow":"web-classic","previous_priority_mode":"manual-first","previous_confidence_level":"high"},
  "counts":{"campaign_targets":2}
}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-create-"+targetSpace+".json"), `{"status":"created","campaign_id":"camp-report-42","queued_runs":3}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"), `{"queued_targets":1,"status":"queued"}`)
	writeTestFile(t, filepath.Join(aiDir, "rescan-summary-"+targetSpace+".md"), "# AI 定向深扫结果\n- Critical 新发现: 1\n")
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://a.example.com/admin"],
    "high_confidence_targets": ["https://a.example.com/graphql", "https://a.example.com/api"]
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "reuse_sources": ["operator-queue", "targeted-rescan", "retest"],
    "signal_scores": {"escalation_score": 17},
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation",
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "next_actions": [
    "Validate admin auth boundary",
    "Replay queued retest results"
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://a.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://a.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://a.example.com/dashboard\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-"+targetSpace+".txt"), "https://a.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "retest-targets-"+targetSpace+".txt"), "https://a.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-targets-"+targetSpace+".txt"), "https://a.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "campaign-targets-"+targetSpace+".txt"), "https://a.example.com/admin\nhttps://a.example.com/dashboard\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                 "https://app.example.com",
		"space_name":             targetSpace,
		"enableLlmReport":        "false",
		"enableLlmAttackSurface": "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	statsData, err := os.ReadFile(filepath.Join(reportDir, "statistics-"+targetSpace+".json"))
	require.NoError(t, err)
	var stats map[string]interface{}
	require.NoError(t, json.Unmarshal(statsData, &stats))
	statistics := stats["statistics"].(map[string]interface{})
	aiStats := statistics["ai"].(map[string]interface{})
	assert.Equal(t, float64(2), aiStats["confirmed_findings"])
	assert.Equal(t, float64(1), aiStats["attack_chains"])
	assert.Equal(t, float64(1), aiStats["semantic_priority_targets"])
	assert.Equal(t, float64(1), aiStats["operator_tasks"])
	assert.Equal(t, true, aiStats["campaign_ready"])
	assert.Equal(t, "manual-first", aiStats["priority_mode"])
	assert.Equal(t, "high", aiStats["confidence_level"])
	assert.Equal(t, float64(1), aiStats["manual_first_targets"])
	assert.Equal(t, float64(2), aiStats["high_confidence_targets"])
	assert.Equal(t, float64(17), aiStats["escalation_score"])
	assert.Equal(t, true, aiStats["queue_followup_effective"])
	assert.Equal(t, "created", aiStats["campaign_create_status"])
	assert.Equal(t, "camp-report-42", aiStats["campaign_create_id"])
	assert.Equal(t, float64(3), aiStats["campaign_create_queued_runs"])

	summaryData, err := os.ReadFile(filepath.Join(reportDir, "executive-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	summaryText := string(summaryData)
	assert.Contains(t, summaryText, "## AI 实战闭环")
	assert.Contains(t, summaryText, "### 实战优先动作")
	assert.Contains(t, summaryText, "Campaign 目标")
	assert.Contains(t, summaryText, "Seed 优先模式")
	assert.Contains(t, summaryText, "manual-first")
	assert.Contains(t, summaryText, "Escalation Score")
	assert.Contains(t, summaryText, "Reuse Sources")
	assert.Contains(t, summaryText, "Campaign Create")
	assert.Contains(t, summaryText, "camp-report-42")

	assetsData, err := os.ReadFile(filepath.Join(reportDir, "assets-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	assert.Contains(t, string(assetsData), "https://a.example.com/admin")
	assert.Contains(t, string(assetsData), "https://a.example.com/dashboard")
	assert.Contains(t, string(assetsData), "https://a.example.com/graphql")
	assert.Contains(t, string(assetsData), "## Manual-First Targets")
	assert.Contains(t, string(assetsData), "## High-Confidence Targets")

	fullReportData, err := os.ReadFile(filepath.Join(reportDir, "full-report-"+targetSpace+".md"))
	require.NoError(t, err)
	fullReport := string(fullReportData)
	assert.Contains(t, fullReport, "### 🎯 Prioritized Target Pack")
	assert.Contains(t, fullReport, "### ⛓️ Attack Chain 摘要")
	assert.Contains(t, fullReport, "### 👨‍💻 Operator Queue 摘要")
	assert.Contains(t, fullReport, "### 🚀 Campaign Handoff 摘要")
	assert.Contains(t, fullReport, "### 🚀 Campaign Create Result")
	assert.Contains(t, fullReport, "### 📁 AI Artifacts")
	assert.Contains(t, fullReport, "Semantic priority targets")
	assert.Contains(t, fullReport, "Priority mode: manual-first")
	assert.Contains(t, fullReport, "Previous priority mode: manual-first")
	assert.Contains(t, fullReport, "next-action: Validate admin auth boundary")
	assert.Contains(t, fullReport, "Campaign ID: camp-report-42")
}

func TestExecuteFinalReportFallsBackToDecisionFollowupSemanticPriorityTargets(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "common", "10-report.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "report-semantic-priority-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")
	reportDir := filepath.Join(outputDir, "report")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-post-vuln-"+targetSpace+".json"), `{"total_results":2}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json"), `{"total_results":7}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://a.example.com/graphql\nhttps://a.example.com/admin\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                 "https://app.example.com",
		"space_name":             targetSpace,
		"enableLlmReport":        "false",
		"enableLlmAttackSurface": "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	statsData, err := os.ReadFile(filepath.Join(reportDir, "statistics-"+targetSpace+".json"))
	require.NoError(t, err)
	var stats map[string]interface{}
	require.NoError(t, json.Unmarshal(statsData, &stats))
	statistics := stats["statistics"].(map[string]interface{})
	aiStats := statistics["ai"].(map[string]interface{})

	assert.Equal(t, float64(2), aiStats["semantic_priority_targets"])
	assert.Equal(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), aiStats["semantic_priority_targets_file"])
	assert.Equal(t, float64(7), aiStats["semantic_results"])
	assert.Equal(t, filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json"), aiStats["semantic_results_file"])

	assetsData, err := os.ReadFile(filepath.Join(reportDir, "assets-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	assetsText := string(assetsData)
	assert.Contains(t, assetsText, "https://a.example.com/graphql")
	assert.Contains(t, assetsText, "https://a.example.com/admin")

	fullReportData, err := os.ReadFile(filepath.Join(reportDir, "full-report-"+targetSpace+".md"))
	require.NoError(t, err)
	fullReport := string(fullReportData)
	assert.Contains(t, fullReport, "semantic-priority-targets-decision-followup-"+targetSpace+".txt")
}

func TestExecuteFinalReportRendersPlaintextSeverityFiles(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "common", "10-report.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "report-plaintext-severity-files"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	reportDir := filepath.Join(outputDir, "report")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "resolved-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-jsonl-"+targetSpace+".txt"),
		`{"template-id":"critical-admin","info":{"name":"Admin Exposure","severity":"critical"},"matched-at":"https://a.example.com/admin"}`+"\n"+
			`{"template-id":"high-auth","info":{"name":"Auth Weakness","severity":"high"},"matched-at":"https://a.example.com/login"}`+"\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-critical-"+targetSpace+".txt"), "[critical] https://a.example.com/admin - admin exposure\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-high-"+targetSpace+".txt"), "[high] https://a.example.com/login - auth weakness\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                 "https://a.example.com",
		"space_name":             targetSpace,
		"enableLlmReport":        "false",
		"enableLlmAttackSurface": "false",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	fullReportData, err := os.ReadFile(filepath.Join(reportDir, "full-report-"+targetSpace+".md"))
	require.NoError(t, err)
	fullReport := string(fullReportData)
	assert.Contains(t, fullReport, "### 🔴 关键漏洞")
	assert.Contains(t, fullReport, "[critical] https://a.example.com/admin - admin exposure")
	assert.Contains(t, fullReport, "### 🟠 高危漏洞")
	assert.Contains(t, fullReport, "[high] https://a.example.com/login - auth weakness")
}

func TestExecuteEnhancedFinalReportIncludesFollowupOperationalContext(t *testing.T) {
	if _, err := osExec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}

	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "common", "10-report-enhanced.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "report-enhanced-followup"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")
	reportDir := filepath.Join(outputDir, "report")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\nb.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-jsonl-"+targetSpace+".txt"),
		`{"info":{"cve_id":"CVE-2025-0001","title":"Critical issue","severity":"critical"},"matched_at":"https://a.example.com/admin"}`+"\n"+
			`{"info":{"cve_id":"CVE-2025-0002","title":"High issue","severity":"high"},"matched_at":"https://a.example.com/login"}`+"\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-post-vuln-"+targetSpace+".json"), `{"total_results":6}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json"), `{"total_results":9}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{"summary":{"total_targets":2}}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{"summary":{"total_tasks":3}}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"), `{"handoff_ready":true,"counts":{"campaign_targets":4}}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-create-"+targetSpace+".json"), `{"status":"created","campaign_id":"camp-enhanced-7","queued_runs":5}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"), `{"status":"queued","queued_targets":2}`)
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://a.example.com/admin"],
    "high_confidence_targets": ["https://a.example.com/graphql", "https://a.example.com/api"]
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "reuse_sources": ["operator-queue", "campaign-create", "retest"],
    "signal_scores": {"escalation_score": 19},
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation",
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "next_actions": [
    "Validate admin boundary",
    "Replay queued retest results"
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-decision-followup-"+targetSpace+".txt"), "https://a.example.com/graphql\nhttps://a.example.com/admin\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://app.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(reportDir, "enhanced-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	summaryText := string(summaryData)
	assert.Contains(t, summaryText, "Semantic search results: 9")
	assert.Contains(t, summaryText, "semantic-search-decision-followup-"+targetSpace+".json")
	assert.Contains(t, summaryText, "Semantic priority targets: 2")
	assert.Contains(t, summaryText, "semantic-priority-targets-decision-followup-"+targetSpace+".txt")
	assert.Contains(t, summaryText, "Next follow-up phase: manual-exploitation")
	assert.Contains(t, summaryText, "Priority mode: manual-first")
	assert.Contains(t, summaryText, "Confidence level: high")
	assert.Contains(t, summaryText, "Escalation score: 19")
	assert.Contains(t, summaryText, "Manual follow-up needed: true")
	assert.Contains(t, summaryText, "Campaign follow-up recommended: true")
	assert.Contains(t, summaryText, "Retest queue effective: true")
	assert.Contains(t, summaryText, "Operator tasks: 3")
	assert.Contains(t, summaryText, "Campaign ready: true")
	assert.Contains(t, summaryText, "Campaign targets: 4")
	assert.Contains(t, summaryText, "Campaign create: created")
	assert.Contains(t, summaryText, "Campaign ID: camp-enhanced-7")
	assert.Contains(t, summaryText, "Campaign queued runs: 5")
	assert.Contains(t, summaryText, "Retest queue: queued")
	assert.Contains(t, summaryText, "Retest queued targets: 2")
	assert.Contains(t, summaryText, "Validate admin boundary")
	assert.Contains(t, summaryText, "Replay queued retest results")

	topologyData, err := os.ReadFile(filepath.Join(reportDir, "topology.mmd"))
	require.NoError(t, err)
	topologyText := string(topologyData)
	assert.Contains(t, topologyText, "Subdomains: 2")
	assert.Contains(t, topologyText, "HTTP: 1")
}

func TestExecuteEnhancedFinalReportUsesFirstSemanticArtifactWithHits(t *testing.T) {
	if _, err := osExec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}

	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "common", "10-report-enhanced.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "report-enhanced-semantic-fallback"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")
	reportDir := filepath.Join(outputDir, "report")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-jsonl-"+targetSpace+".txt"),
		`{"info":{"cve_id":"CVE-2025-0099","title":"Critical issue","severity":"critical"},"matched_at":"https://a.example.com/admin"}`+"\n")
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json"), `{"total_results":0,"results":[]}`)
	writeTestFile(t, filepath.Join(aiDir, "semantic-search-post-vuln-"+targetSpace+".json"), `{
  "results": [
    {"target":"https://a.example.com/admin"},
    {"target":"https://a.example.com/graphql"},
    {"target":"https://a.example.com/api"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://a.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(reportDir, "enhanced-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	summaryText := string(summaryData)
	assert.Contains(t, summaryText, "Semantic search results: 3")
	assert.Contains(t, summaryText, "semantic-search-post-vuln-"+targetSpace+".json")
	assert.NotContains(t, summaryText, "Semantic search file: "+filepath.Join(aiDir, "semantic-search-decision-followup-"+targetSpace+".json"))
}

func TestExecuteEnhancedFinalReportCountsRealOperationalArtifacts(t *testing.T) {
	if _, err := osExec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}

	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "common", "10-report-enhanced.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	targetSpace := "report-enhanced-real-operational-counts"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")
	reportDir := filepath.Join(outputDir, "report")

	writeTestFile(t, filepath.Join(outputDir, "subdomain", "subdomain-"+targetSpace+".txt"), "a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "probing", "http-"+targetSpace+".txt"), "https://a.example.com\n")
	writeTestFile(t, filepath.Join(outputDir, "vulnscan", "nuclei-jsonl-"+targetSpace+".txt"),
		`{"info":{"cve_id":"CVE-2025-0100","title":"Critical issue","severity":"critical"},"matched_at":"https://a.example.com/admin"}`+"\n")
	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "summary": {"total_targets": 1},
  "targets": [
    {"target": "https://a.example.com/admin"}
  ],
  "automation_queue": [
    {"target": "https://a.example.com/graphql"},
    {"target": "https://a.example.com/admin"}
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{
  "summary": {"total_tasks": 1},
  "tasks": [
    {"target": "https://a.example.com/admin"},
    {"target": "https://a.example.com/upload"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":     "https://a.example.com",
		"space_name": targetSpace,
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	summaryData, err := os.ReadFile(filepath.Join(reportDir, "enhanced-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	summaryText := string(summaryData)
	assert.Contains(t, summaryText, "Retest planned targets: 2")
	assert.Contains(t, summaryText, "Operator tasks: 2")
}

func TestExecuteAIKnowledgeAutolearnBuildsFollowupContextArtifact(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-knowledge-autolearn.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	callsPath := installStubOsmedeus(t)
	targetSpace := "knowledge-autolearn-followup"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "applied-ai-decision-"+targetSpace+".json"), `{
  "scan": {"profile": "balanced", "severity": "critical,high"}
}`)
	writeTestFile(t, filepath.Join(aiDir, "followup-decision-"+targetSpace+".json"), `{
  "seed_targets": {
    "manual_first_targets": ["https://seed.example.com/admin"],
    "high_confidence_targets": ["https://seed.example.com/upload", "https://seed.example.com/graphql"]
  },
  "seed_focus": {
    "priority_mode": "manual-first",
    "confidence_level": "high",
    "reuse_sources": ["operator-queue", "targeted-rescan"],
    "signal_scores": {"escalation_score": 19},
    "manual_followup_needed": true,
    "campaign_followup_recommended": true,
    "queue_followup_effective": true
  },
  "execution_feedback": {
    "next_phase": "manual-exploitation"
  },
  "next_actions": [
    "Revalidate admin boundary",
    "Fold retest output back into knowledge"
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{"summary":{"total_targets":1}}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{"summary":{"total_tasks":2}}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-handoff-"+targetSpace+".json"), `{"handoff_ready":true,"counts":{"campaign_targets":3}}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-create-"+targetSpace+".json"), `{"status":"created","campaign_id":"camp-knowledge-9","queued_runs":4}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"), `{"queued_targets":1}`)
	writeTestFile(t, filepath.Join(aiDir, "rescan-summary-"+targetSpace+".md"), "# Rescan\n\n- New critical hit\n")

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                  "https://app.example.com",
		"space_name":              targetSpace,
		"knowledgeWorkspace":      "shared-kb",
		"knowledgeScope":          "workspace",
		"enableKnowledgeLearning": "true",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)
	assert.Len(t, result.Steps, 4)

	contextData, err := os.ReadFile(filepath.Join(aiDir, "unified-analysis-knowledge-"+targetSpace+".json"))
	require.NoError(t, err)
	var learnContext map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &learnContext))

	followupSeed, ok := learnContext["followup_seed_focus"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "manual-first", followupSeed["priority_mode"])
	assert.Equal(t, "high", followupSeed["confidence_level"])
	assert.Equal(t, "manual-exploitation", followupSeed["next_phase"])
	assert.Equal(t, float64(19), followupSeed["escalation_score"])
	assert.Equal(t, float64(1), followupSeed["manual_first_targets"])
	assert.Equal(t, float64(2), followupSeed["high_confidence_targets"])

	operationalCounts, ok := learnContext["operational_counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(1), operationalCounts["retest_targets"])
	assert.Equal(t, float64(2), operationalCounts["operator_tasks"])
	assert.Equal(t, float64(3), operationalCounts["campaign_targets"])
	assert.Equal(t, float64(4), operationalCounts["campaign_create_queued_runs"])
	assert.Equal(t, float64(1), operationalCounts["retest_queued_targets"])

	campaignCreation, ok := learnContext["campaign_creation"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "created", campaignCreation["status"])
	assert.Equal(t, "camp-knowledge-9", campaignCreation["campaign_id"])
	assert.Equal(t, float64(4), campaignCreation["queued_runs"])

	summaryLog, err := os.ReadFile(filepath.Join(aiDir, "knowledge-learning-"+targetSpace+".log"))
	require.NoError(t, err)
	summaryText := string(summaryLog)
	assert.Contains(t, summaryText, "Context artifact")
	assert.Contains(t, summaryText, "Priority mode: manual-first")
	assert.Contains(t, summaryText, "Confidence level: high")
	assert.Contains(t, summaryText, "Campaign create: created")
	assert.Contains(t, summaryText, "Campaign queued runs: 4")

	callsData, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	callLine := strings.TrimSpace(string(callsData))
	assert.Contains(t, callLine, "kb learn -w shared-kb --scope workspace --include-ai")
}

func TestExecuteAIKnowledgeAutolearnCountsRealOperationalArtifacts(t *testing.T) {
	workflowsPath := getRealWorkflowsPath()
	loader := parser.NewLoader(workflowsPath)

	workflow, err := loader.LoadWorkflowByPath(filepath.Join(workflowsPath, "fragments", "do-ai-knowledge-autolearn.yaml"))
	require.NoError(t, err)

	ctx := context.Background()
	cfg := testConfig(t)
	cfg.WorkflowsPath = workflowsPath

	callsPath := installStubOsmedeus(t)
	targetSpace := "knowledge-autolearn-real-operational-counts"
	outputDir := filepath.Join(cfg.WorkspacesPath, targetSpace)
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "retest-plan-"+targetSpace+".json"), `{
  "summary": {"total_targets": 1},
  "targets": [
    {"target": "https://seed.example.com/admin"}
  ],
  "automation_queue": [
    {"target": "https://seed.example.com/graphql"},
    {"target": "https://seed.example.com/admin"}
  ]
}`)
	writeTestFile(t, filepath.Join(aiDir, "operator-queue-"+targetSpace+".json"), `{
  "summary": {"total_tasks": 1},
  "tasks": [
    {"target": "https://seed.example.com/admin"},
    {"target": "https://seed.example.com/upload"}
  ]
}`)

	exec := executor.NewExecutor()
	exec.SetDryRun(false)
	exec.SetSpinner(false)

	result, err := exec.ExecuteModule(ctx, workflow, map[string]string{
		"target":                  "https://app.example.com",
		"space_name":              targetSpace,
		"knowledgeWorkspace":      "shared-kb",
		"knowledgeScope":          "workspace",
		"enableKnowledgeLearning": "true",
	}, cfg)

	require.NoError(t, err)
	assert.Equal(t, core.RunStatusCompleted, result.Status)

	contextData, err := os.ReadFile(filepath.Join(aiDir, "unified-analysis-knowledge-"+targetSpace+".json"))
	require.NoError(t, err)
	var learnContext map[string]interface{}
	require.NoError(t, json.Unmarshal(contextData, &learnContext))

	operationalCounts, ok := learnContext["operational_counts"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(2), operationalCounts["retest_targets"])
	assert.Equal(t, float64(2), operationalCounts["operator_tasks"])

	callsData, err := os.ReadFile(callsPath)
	require.NoError(t, err)
	callLine := strings.TrimSpace(string(callsData))
	assert.Contains(t, callLine, "kb learn -w shared-kb --scope workspace --include-ai")
}
