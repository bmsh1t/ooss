# Superdomain AI Balanced Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `superdomain-extensive-ai-optimized` the stable default operator workflow while keeping semantic/KB/rerank/follow-up abilities useful, resumable, and reusable by `stable`, `hybrid`, and future derived top-level workflows.

**Architecture:** Tighten the shared AI fragment contract instead of adding workflow-specific hacks. First harden `optimized` around semantic retrieval, decision application, follow-up feedback, and operator resume artifacts; then back-port only mature parts into `stable` and `hybrid`; finally verify derived top-level workflows stay compatible through defaults and fail-open conditions.

**Tech Stack:** YAML workflows, Osmedeus workflow fragments, existing `agent` / `agent-acp` steps, vectorkb, rerank, jq/bash glue, regression smoke scripts.

---

### Task 1: Freeze the shared AI contract surface

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-pre-scan-decision.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-intelligent-analysis.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-apply-decision.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-post-followup-coordination.yaml`
- Test: `test/regression/ai-workflow-smoke.sh`
- Test: `test/regression/ai-semantic-vector-smoke.sh`

- [ ] Define a stable output contract for all five fragments:
  - machine JSON
  - operator summary
  - next-actions / resume hints
  - status / degraded / reason fields
- [ ] Normalize file names under `{{Output}}/ai-analysis/` so later stages can consume the same names across `optimized`, `stable`, `hybrid`, and future derived workflows.
- [ ] Ensure each fragment still succeeds when optional upstream files are missing by emitting an empty-but-valid artifact instead of hard failing.
- [ ] Re-run:
  - `go run ./cmd/osmedeus workflow validate ./osmedeus-base/workflows/superdomain-extensive-ai-optimized.yaml`
  - `go run ./cmd/osmedeus workflow validate ./osmedeus-base/workflows/superdomain-extensive-ai-stable.yaml`
  - `go run ./cmd/osmedeus workflow validate ./osmedeus-base/workflows/superdomain-extensive-ai-hybrid.yaml`

### Task 2: Make semantic / rerank / KB retrieval a first-class decision input

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-intelligent-analysis.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-apply-decision.yaml`
- Test: `test/regression/ai-semantic-vector-smoke.sh`

- [ ] Make semantic-search output always expose:
  - layered workspace/shared/global hit counts
  - rerank_applied flag
  - fallback mode flag
  - normalized highlights / priority targets
- [ ] Ensure `do-ai-intelligent-analysis.yaml` consumes semantic highlights and source-layer evidence explicitly instead of treating them as optional garnish.
- [ ] Ensure `do-ai-apply-decision.yaml` preserves retrieval provenance in the final applied decision JSON so follow-up modules know whether they are acting on scan evidence, KB evidence, or both.
- [ ] Verify `kb vector search --rerank` absence or failure only downgrades ranking quality and never blocks decision generation.

### Task 3: Strengthen optimized as the default mainline

**Files:**
- Modify: `osmedeus-base/workflows/superdomain-extensive-ai-optimized.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-pre-scan-decision.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-apply-decision.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-post-followup-coordination.yaml`
- Test: `test/regression/ai-workflow-smoke.sh`

- [ ] Review every AI stage in `superdomain-extensive-ai-optimized.yaml` and remove brittle assumptions that require every optional toggle to be declared in the top-level workflow.
- [ ] Keep `enableIntelligentAnalysis`-style optional toggles default-on when omitted so derived workflows do not silently lose AI stages.
- [ ] Make the decision-followup chain deterministic:
  - `ai-apply-decision`
  - `ai-decision-semantic-search`
  - `ai-retest-planning`
  - `ai-operator-queue`
  - `ai-campaign-handoff`
  - `ai-targeted-rescan`
  - `ai-post-followup-coordination`
- [ ] Ensure `optimized` always leaves behind a compact operator handoff set even when a follow-up stage is skipped or degraded.

### Task 4: Add operator-first resume artifacts

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-intelligent-analysis.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-apply-decision.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-post-followup-coordination.yaml`
- Create: `osmedeus-base/workflows/fragments/do-ai-operator-summary.yaml` (only if a new fragment is cleaner than extending existing ones)
- Test: `test/regression/ai-workflow-smoke.sh`

- [ ] Standardize three operator-facing artifacts:
  - `operator-summary-{{TargetSpace}}.md`
  - `next-actions-{{TargetSpace}}.json`
  - `resume-context-{{TargetSpace}}.json`
- [ ] Make them point to:
  - current severity / scan profile
  - top priority targets
  - unresolved focus areas
  - recommended next modules or follow-up actions
  - latest degraded stages and reasons
- [ ] Keep the content short enough to be read quickly during real ops.
- [ ] Verify the files are still written when `campaign`, `retest`, or `operator` branches are disabled.

### Task 5: Close the follow-up feedback loop

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-pre-scan-decision.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-post-followup-coordination.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-retest-planning.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-operator-queue.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-campaign-handoff.yaml`
- Test: `test/regression/ai-workflow-smoke.sh`

- [ ] Ensure post-followup coordination emits normalized carry-over fields for the next run:
  - previous priority targets
  - previous focus areas
  - effective follow-up types
  - escalation / downgrade hints
- [ ] Make `do-ai-pre-scan-decision.yaml` prefer these carry-over inputs when present, but degrade cleanly when they are absent.
- [ ] Preserve the distinction between:
  - “worth repeating”
  - “needs manual escalation”
  - “not worth continuing”
- [ ] Confirm the next-run feedback loop never pollutes the persistent global KB directly.

### Task 6: Keep knowledge layers reusable and clean

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml`
- Modify: `osmedeus-base/workflows/fragments/do-ai-knowledge-autolearn.yaml`
- Modify: `README.md`
- Test: `test/regression/ai-semantic-vector-smoke.sh`

- [ ] Keep retrieval layered:
  - target workspace
  - shared workspace
  - global/base workspace such as `security-kb`
- [ ] Keep `kb learn` writeback pointed at the intended learning workspace instead of writing reusable target noise into the base KB.
- [ ] Document the recommended operator pattern:
  - `security-kb` as long-lived base
  - shared reusable lessons in shared workspace
  - target-only findings in target workspace
- [ ] Verify semantic search still returns useful results when only the global/base workspace is populated.

### Task 7: Back-port mature pieces into stable and hybrid

**Files:**
- Modify: `osmedeus-base/workflows/superdomain-extensive-ai-stable.yaml`
- Modify: `osmedeus-base/workflows/superdomain-extensive-ai-hybrid.yaml`
- Test: `test/regression/superdomain-lite-smoke.sh` (only if shared defaults are touched broadly)
- Test: `test/regression/ai-workflow-smoke.sh`

- [ ] Mirror the shared contract paths and summary/resume artifacts into `stable` and `hybrid`.
- [ ] Keep `stable` conservative: only adopt hardened defaults and low-risk follow-up artifacts.
- [ ] Keep `hybrid` feature-rich but contract-compatible with `optimized`.
- [ ] Avoid adding workflow-specific private file names or one-off params that future derived top-level flows would have to rediscover manually.

### Task 8: Protect future derived top-level workflows

**Files:**
- Modify: `osmedeus-base/workflows/superdomain-extensive-ai-optimized.yaml`
- Modify: `osmedeus-base/workflows/superdomain-extensive-ai-stable.yaml`
- Modify: `osmedeus-base/workflows/superdomain-extensive-ai-hybrid.yaml`
- Modify: `README.md`
- Test: workflow validation for a representative derived top-level flow if present in-repo

- [ ] Audit every fine-grained AI param used by top-level flows and keep omission-safe defaults where possible.
- [ ] Prefer fragment-local defaults over requiring every new top-level flow to copy a large param block.
- [ ] Add a short workflow-authoring note documenting which params a derived workflow should override and which ones can be left implicit.
- [ ] Verify a derived workflow path can still keep the AI chain alive without redefining every optional toggle.

### Task 9: Add regression coverage for fail-open behavior

**Files:**
- Modify: `test/regression/ai-workflow-smoke.sh`
- Modify: `test/regression/ai-semantic-vector-smoke.sh`
- Modify: `test/regression/stable-core.sh` (only if the coverage belongs there)

- [ ] Add at least one smoke path where rerank is unavailable but semantic search still succeeds.
- [ ] Add at least one smoke path where vectorkb is absent but keyword/search-corpus fallback still yields valid AI artifacts.
- [ ] Add at least one smoke path where a follow-up branch is disabled but operator-summary / next-actions / resume-context are still produced.
- [ ] Keep the regression suite serial and bounded so it does not become another machine-killer.

### Task 10: Final verification and docs sync

**Files:**
- Modify: `plan.md`
- Modify: `README.md`
- Modify: `docs/api/knowledge.mdx` (if operator-facing KB layering guidance belongs there)

- [ ] Update `plan.md` to reflect the new “balanced mainline” objective and its rollout order.
- [ ] Update `README.md` with the practical operator story:
  - which workflow to run by default
  - how KB layers are meant to be used
  - where to look when a run is interrupted
- [ ] Run final verification:
  - workflow validate for `optimized`, `stable`, `hybrid`
  - `make test-regression-ai-workflow-smoke`
  - `make test-regression-ai-semantic-vector-smoke`
- [ ] Record any remaining runtime-only gaps separately instead of silently mixing them into shipped defaults.
