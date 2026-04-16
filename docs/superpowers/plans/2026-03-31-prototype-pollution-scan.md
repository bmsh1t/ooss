# Prototype Pollution Scan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a lightweight standalone prototype-pollution scan to `vuln-suite` without expanding the AI or workflow orchestration surface.

**Architecture:** Keep the implementation local to the vulnerability suite. Bundle a repo-owned nuclei headless template under the base templates directory, add a narrow workflow step that runs it against a capped list of HTTP targets, and surface the results through a dedicated output file plus the existing nuclei JSONL stream.

**Tech Stack:** YAML workflows, Nuclei headless templates, Go integration tests.

---

### Task 1: Lock the workflow contract with a test

**Files:**
- Modify: `test/integration/workflow_test.go`

- [ ] Add a regression test that parses `osmedeus-base/workflows/common/09-vuln-suite.yaml`.
- [ ] Assert the workflow exposes `enablePrototypePollutionScan`, `prototypePollutionTemplateFile`, and `prototypePollutionOutputFile`.
- [ ] Assert the workflow reports `prototype-pollution-results`.
- [ ] Assert a `prototype-pollution-scan` bash step exists.

### Task 2: Bundle the nuclei template

**Files:**
- Create: `osmedeus-base/templates/nuclei/prototype-pollution-check.yaml`

- [ ] Add a repo-owned headless nuclei template for prototype pollution detection.
- [ ] Keep the template self-contained so the workflow does not depend on upstream template path layout.

### Task 3: Wire the workflow step

**Files:**
- Modify: `osmedeus-base/workflows/common/09-vuln-suite.yaml`

- [ ] Add narrow params for enable flag, template path, output files, and target limit.
- [ ] Add a dedicated report entry.
- [ ] Add a capped `prototype-pollution-scan` step that runs nuclei headless with the bundled template.
- [ ] Append JSONL findings into the main nuclei output so downstream reporting and AI analysis can still see them.

### Task 4: Verify narrowly

**Files:**
- Modify: `test/integration/workflow_test.go`
- Modify: `osmedeus-base/workflows/common/09-vuln-suite.yaml`
- Create: `osmedeus-base/templates/nuclei/prototype-pollution-check.yaml`

- [ ] Run `go test -v -run TestVulnSuiteIncludesPrototypePollutionScan ./test/integration`.
- [ ] Run workflow validation against `osmedeus-base/workflows/superdomain-extensive-ai-optimized.yaml`.
