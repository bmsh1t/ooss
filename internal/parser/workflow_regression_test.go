package parser

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/j3ssie/osmedeus/v5/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repoRootForWorkflowRegressionTest(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(filename), "..", "..")
}

func TestVulnSuiteSsrfScanIsEnabledByDefaultAndUsesScopedCleanup(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "09-vuln-suite.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var ssrfParam *core.Param
	for i := range workflow.Params {
		if workflow.Params[i].Name == "enableSsrfScan" {
			ssrfParam = &workflow.Params[i]
			break
		}
	}
	require.NotNil(t, ssrfParam)
	assert.True(t, ssrfParam.DefaultBool(), "SSRF scan should stay enabled by default")

	var ssrfStep *core.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "ssrf-scan" {
			ssrfStep = &workflow.Steps[i]
			break
		}
	}

	require.NotNil(t, ssrfStep)
	assert.Equal(t, core.StepTypeBash, ssrfStep.Type)
	assert.Contains(t, ssrfStep.Command, "interactsh.pid")
	assert.Contains(t, ssrfStep.Command, "kill \"$INTERACTSH_PID\"")
	assert.NotContains(t, ssrfStep.Command, "pkill -f interactsh-client")
}

func TestOsintPostleaksNgStaysEnabledAndHandlesRateLimitQuietly(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "01-osint.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var postleaksParam *core.Param
	for i := range workflow.Params {
		if workflow.Params[i].Name == "enablePostleaksNg" {
			postleaksParam = &workflow.Params[i]
			break
		}
	}
	require.NotNil(t, postleaksParam)
	assert.True(t, postleaksParam.DefaultBool(), "postleaksNg should stay enabled by default; rate limits are non-blocking")

	var postleaksStep *core.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "api-leaks-postleaks" {
			postleaksStep = &workflow.Steps[i]
			break
		}
	}

	require.NotNil(t, postleaksStep)
	assert.Equal(t, core.StepTypeBash, postleaksStep.Type)
	assert.Contains(t, postleaksStep.Command, "postleaksNg.log")
	assert.Contains(t, postleaksStep.Command, "Rate Limit exceeded")
	assert.NotContains(t, postleaksStep.Command, "postleaksNg -k {{Target}} --output {{outputDir}}/postleaks_results -t 10 2>/dev/null || true")
}

func TestSubdomainNoerrorTimeoutIncludesPrimaryDuration(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "02-subdomain.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var noerrorStep *core.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "noerror-scan" {
			noerrorStep = &workflow.Steps[i]
			break
		}
	}

	require.NotNil(t, noerrorStep)
	assert.NotContains(t, noerrorStep.Command, "timeout --foreground -k 1m dnsx")
	assert.Contains(t, noerrorStep.Command, "timeout --foreground -k 15s {{dnsExternalTimeout}} dnsx")
}

func TestVulnSuiteNucleiScanAvoidsUnsupportedFlagsAndSanitizesGeneratedConfig(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "09-vuln-suite.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var smartTemplatesStep *core.Step
	var nucleiStep *core.Step
	for i := range workflow.Steps {
		switch workflow.Steps[i].Name {
		case "smart-template-selection":
			smartTemplatesStep = &workflow.Steps[i]
		case "nuclei-scan":
			nucleiStep = &workflow.Steps[i]
		}
	}

	require.NotNil(t, smartTemplatesStep)
	assert.Contains(t, smartTemplatesStep.Command, "http/cves,http/vulnerabilities,http/exposures")
	assert.NotContains(t, smartTemplatesStep.Command, "SELECTED_TEMPLATES=\"cves,vulnerabilities,exposures\"")

	require.NotNil(t, nucleiStep)
	assert.NotContains(t, nucleiStep.Command, "-sjc")
	assert.NotContains(t, nucleiStep.Command, "-sjt")
	assert.NotContains(t, nucleiStep.Command, "-rsh")
	assert.NotContains(t, nucleiStep.Command, "source {{vulnSuiteDir}}/smart-templates.txt")
	assert.NotContains(t, nucleiStep.Command, "source {{vulnSuiteDir}}/waf-adjusted-config.txt")
	assert.Contains(t, nucleiStep.Command, "Ignoring invalid AI threads")
	assert.Contains(t, nucleiStep.Command, "Ignoring invalid AI timeout")
}

func TestScanBackupScannerCheckExpandsHomeToolsDir(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "scan-backup.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var checkScannerStep *core.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "check-scanner-exists" {
			checkScannerStep = &workflow.Steps[i]
			break
		}
	}

	require.NotNil(t, checkScannerStep)
	assert.Contains(t, checkScannerStep.PreCondition, `os_getenv("HOME") + "/Tools/ihoneyBakFileScan_Modify/ihoneyBakFileScan_Modify.py"`)
	assert.NotEqual(t, `!file_exists("{{backupScannerPath}}")`, checkScannerStep.PreCondition)
}

func TestFingerprintCmseekCheckExpandsHomeToolsDir(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "05-fingerprint.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var cmseekStep *core.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "cmseek-scan" {
			cmseekStep = &workflow.Steps[i]
			break
		}
	}

	require.NotNil(t, cmseekStep)
	assert.Contains(t, cmseekStep.PreCondition, `os_getenv("HOME") + "/Tools/CMSeeK/cmseek.py"`)
	assert.NotEqual(t, `{{enableCmsScanner}} && file_exists("{{outputDir}}/cms/cms-targets.txt") && file_exists("{{cmseekPath}}")`, cmseekStep.PreCondition)
}

func TestMainAiFlowsEnableLlmReportByDefault(t *testing.T) {
	p := NewParser()
	flowFiles := []string{
		"superdomain-extensive-ai-optimized.yaml",
		"superdomain-extensive-ai-stable.yaml",
		"superdomain-extensive-ai-hybrid.yaml",
	}

	for _, flowFile := range flowFiles {
		t.Run(flowFile, func(t *testing.T) {
			file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", flowFile)
			workflow, err := p.Parse(file)
			require.NoError(t, err)

			var llmReportParam *core.Param
			for i := range workflow.Params {
				if workflow.Params[i].Name == "enableLlmReport" {
					llmReportParam = &workflow.Params[i]
					break
				}
			}

			require.NotNil(t, llmReportParam)
			assert.True(t, llmReportParam.DefaultBool(), "LLM report should be enabled by default in %s", flowFile)
		})
	}
}

func TestReportLlmOutputFocusesOnExploitationAndAttackSurfaceExpansion(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "10-report.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var exploitPlanParam *core.Param
	var exploitPlanStep *core.Step
	var saveExploitPlanStep *core.Step
	var attackSurfaceStep *core.Step
	for i := range workflow.Params {
		if workflow.Params[i].Name == "llmExploitationPlan" {
			exploitPlanParam = &workflow.Params[i]
		}
	}
	for i := range workflow.Steps {
		switch workflow.Steps[i].Name {
		case "llm-exploitation-plan":
			exploitPlanStep = &workflow.Steps[i]
		case "save-llm-exploitation-plan":
			saveExploitPlanStep = &workflow.Steps[i]
		case "llm-attack-surface-analysis":
			attackSurfaceStep = &workflow.Steps[i]
		}
	}

	require.NotNil(t, exploitPlanParam)
	assert.Contains(t, exploitPlanParam.Default, "llm-exploitation-plan-")

	require.NotNil(t, exploitPlanStep)
	assert.Contains(t, exploitPlanStep.Messages[0].Content, "manual exploitation")
	assert.Contains(t, exploitPlanStep.Messages[0].Content, "post-exploitation expansion")
	assert.NotContains(t, exploitPlanStep.Messages[0].Content, "remediation")
	assert.NotContains(t, exploitPlanStep.Messages[1].Content, "remediation")

	require.NotNil(t, saveExploitPlanStep)
	assert.Contains(t, saveExploitPlanStep.Command, "# Exploitation Plan - {{Target}}")
	assert.Contains(t, saveExploitPlanStep.Command, "{{llmExploitationPlan}}")
	assert.NotContains(t, saveExploitPlanStep.Command, "Remediation Recommendations")

	require.NotNil(t, attackSurfaceStep)
	assert.Contains(t, attackSurfaceStep.Messages[0].Content, "attack surface expansion")
	assert.Contains(t, attackSurfaceStep.Messages[0].Content, "next targets")
	assert.NotContains(t, attackSurfaceStep.Messages[0].Content, "security improvement")
}

func TestContentAnalysisGfPatternsUsesProcessedUrlInputAndDedupes(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "07-content-analysis.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	var gfStep *core.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].Name == "gf-patterns" {
			gfStep = &workflow.Steps[i]
			break
		}
	}

	require.NotNil(t, gfStep)
	assert.Contains(t, gfStep.Command, "{{urlExtractFile}}")
	assert.Contains(t, gfStep.Command, "{{urlNoDupesFile}}")
	assert.Contains(t, gfStep.Command, `gf "$pattern" "$gf_input"`)
	assert.Contains(t, gfStep.Command, "sort -u")
	assert.Contains(t, gfStep.Command, "anew -q")
	assert.NotContains(t, gfStep.Command, "cat {{combinedUrlsFile}} | gf $pattern")
}

func TestUrlGfModuleKeepsFullUrlsAndDedupesPatternOutputs(t *testing.T) {
	p := NewParser()
	file := filepath.Join(repoRootForWorkflowRegressionTest(t), "osmedeus-base", "workflows", "common", "url-gf.yaml")

	workflow, err := p.Parse(file)
	require.NoError(t, err)

	for i := range workflow.Steps {
		step := &workflow.Steps[i]
		if step.Type != core.StepTypeBash || len(step.Name) < len("gf-") || step.Name[:len("gf-")] != "gf-" {
			continue
		}
		if step.Name == "gf-all" {
			continue
		}

		assert.NotContains(t, step.Command, `cut -d ":" -f3-5`, "gf step %s should keep complete URLs", step.Name)
		assert.Contains(t, step.Command, "{{urlExtractFile}}", "gf step %s should read the processed URL extract file", step.Name)
		assert.Contains(t, step.Command, "sort -u", "gf step %s should dedupe output", step.Name)
	}
}
