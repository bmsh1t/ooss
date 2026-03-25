package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	writeTestFile(t, filepath.Join(aiDir, "semantic-priority-targets-"+targetSpace+".txt"), "https://api.example.com/graphql\n")
	writeTestFile(t, filepath.Join(aiDir, "best-path-targets-"+targetSpace+".txt"), "https://app.example.com/dashboard\n")
	writeTestFile(t, filepath.Join(aiDir, "confirmed-urls-"+targetSpace+".txt"), "https://app.example.com/login\n")
	writeTestFile(t, filepath.Join(aiDir, "entry-points-"+targetSpace+".txt"), "https://api.example.com/admin\n")
	writeTestFile(t, filepath.Join(aiDir, "rescan-summary-"+targetSpace+".md"), "# rescan summary\n")

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
	assert.ElementsMatch(t,
		[]string{
			"https://app.example.com/recheck",
			"https://app.example.com/login",
			"https://api.example.com/admin",
			"https://portal.example.com",
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
	assert.Equal(t, float64(6), counts["campaign_targets"])
	assert.Equal(t, float64(1), counts["semantic_priority_targets"])
	assert.Equal(t, float64(1), counts["decision_rescan_targets"])

	targetGroups, ok := handoff["targets"].(map[string]interface{})
	require.True(t, ok)
	semanticPriority, ok := targetGroups["semantic_priority"].([]interface{})
	require.True(t, ok)
	assert.Contains(t, semanticPriority, "https://api.example.com/graphql")

	createData, err := os.ReadFile(filepath.Join(aiDir, "campaign-create-"+targetSpace+".json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"created","campaign_id":"camp-123","queued_runs":3}`, string(createData))

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
	assert.ElementsMatch(t,
		[]string{"https://app.example.com/login", "https://api.example.com/admin", "https://cdn.example.com/login"},
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
	assert.Equal(t, float64(3), counts["campaign_targets"])
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

	statusData, err := os.ReadFile(filepath.Join(aiDir, "ai-execution-status.json"))
	require.NoError(t, err)
	var status map[string]interface{}
	require.NoError(t, json.Unmarshal(statusData, &status))
	assert.Equal(t, "completed", status["overall_status"])
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
  "campaign_profile":{"recommended_flow":"web-classic"},
  "counts":{"campaign_targets":2}
}`)
	writeTestFile(t, filepath.Join(aiDir, "campaign-create-"+targetSpace+".json"), `{"status":"created"}`)
	writeTestFile(t, filepath.Join(aiDir, "retest-queue-summary-"+targetSpace+".json"), `{"queued_targets":1}`)
	writeTestFile(t, filepath.Join(aiDir, "rescan-summary-"+targetSpace+".md"), "# AI 定向深扫结果\n- Critical 新发现: 1\n")
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

	summaryData, err := os.ReadFile(filepath.Join(reportDir, "executive-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	summaryText := string(summaryData)
	assert.Contains(t, summaryText, "## AI 实战闭环")
	assert.Contains(t, summaryText, "### 实战优先动作")
	assert.Contains(t, summaryText, "Campaign 目标")

	assetsData, err := os.ReadFile(filepath.Join(reportDir, "assets-summary-"+targetSpace+".md"))
	require.NoError(t, err)
	assert.Contains(t, string(assetsData), "https://a.example.com/admin")
	assert.Contains(t, string(assetsData), "https://a.example.com/dashboard")
	assert.Contains(t, string(assetsData), "https://a.example.com/graphql")

	fullReportData, err := os.ReadFile(filepath.Join(reportDir, "full-report-"+targetSpace+".md"))
	require.NoError(t, err)
	fullReport := string(fullReportData)
	assert.Contains(t, fullReport, "### 🎯 Prioritized Target Pack")
	assert.Contains(t, fullReport, "### ⛓️ Attack Chain 摘要")
	assert.Contains(t, fullReport, "### 👨‍💻 Operator Queue 摘要")
	assert.Contains(t, fullReport, "### 🚀 Campaign Handoff 摘要")
	assert.Contains(t, fullReport, "### 📁 AI Artifacts")
	assert.Contains(t, fullReport, "Semantic priority targets")
}
