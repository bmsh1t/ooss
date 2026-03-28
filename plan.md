# Plan

## Current Status

### Completed

- Local knowledge base backend is in place.
  - File ingest for common local document types
  - Search and document listing APIs/CLI
  - Workspace knowledge auto-learning from scan outputs
  - Workspace/public layered retrieval defaults and learned-knowledge metadata weighting
  - Learned documents now preserve source confidence, sample type, target-type tags, and shared-layer storage
- Campaign batch-operation backend v1 is in place.
  - Campaign entity and aggregation
  - Failed target rerun
  - High-risk deep-scan queue hook
  - Report/export analytics with filters, presets, sorting, and pagination
  - Saved campaign report/export profiles in API/CLI
- Vulnerability lifecycle backend v1 is in place.
  - Lifecycle states: `new`, `triaged`, `verified`, `false_positive`, `retest`, `closed`
  - AI verdict and analyst review fields
  - Retest task creation and queue-state synchronization
  - Automatic post-retest closure based on imported retest results
  - Workspace vulnerability board API
  - Vulnerability detail now exposes status timeline and retest timeline
  - Vulnerability list now supports `fingerprint_key` and `source_run_uuid` filters
- Attack chain workbench backend v1 is in place.
  - Attack-chain report persistence
  - Attack-chain import function/API
  - Summary/detail query APIs
  - Workflow fragments now write attack-chain outputs into the report store
  - Detail API now links chains back to matching vulnerabilities and assets
  - Detail API now exposes execution-ready counts, queue recommendations, and recommended deep-scan targets
  - ACP attack-chain input is now pre-curated to prefer verified findings and exclude false-positive nodes
- Superdomain AI workflows are now more cohesive.
  - `stable` and `hybrid` include attack-chain visualization
  - `stable`, `hybrid`, `optimized`, and `lite` include post-run knowledge auto-learning
- Current-source verification completed for this round:
  - `make build`
  - `make test-regression-api-ai`
  - `make test-regression-api-knowledge`
  - `make test-regression-queue-live`
  - `make test-regression-stable-core`
  - targeted `go test ./internal/knowledge`
  - targeted `go test ./internal/vectorkb`
  - targeted `go test ./internal/database`
  - targeted `go test ./pkg/server/handlers`
  - targeted `go test ./pkg/cli`
  - targeted `go test ./internal/linter`
  - workflow validation for `superdomain-extensive-ai-stable`
  - workflow validation for `superdomain-extensive-ai-hybrid`
  - workflow validation for `superdomain-extensive-ai-optimized`
  - workflow validation for `superdomain-extensive-ai-lite`
  - workflow validation for `ai-knowledge-autolearn`
- Targeted tests were added for:
  - vulnerability retest closure
  - source run UUID propagation in vulnerability mapping
  - CLI queued run workspace metadata persistence
- Static checks completed for the current round.
  - `gofmt`
  - YAML structure review for modified workflow fragments
  - builtin workflow variable lint coverage for `OsmedeusBase` / `OsmedeusExec`
  - plain `go test` Make targets for environments where `gotestsum` is flaky

## Unfinished Work

### 1. Runtime Validation

- Runtime validation is mostly closed for the modified AI workflow/backend path.
- Not done yet:
  - full `make test-unit` clean exit across the entire repository in the current environment
  - live vector-knowledge API verification with an explicit embeddings provider
  - workflow execution verification against current source for the full superdomain AI chain
- Verified already:
  - clean current-source build
  - real local server startup via `make test-regression-api-ai`
  - real local server startup via `make test-regression-api-knowledge`
  - real local server + worker startup via `make test-regression-queue-live`
  - live API verification for campaign, vulnerability lifecycle, attack-chain workbench, and non-vector knowledge-base routes
  - live queue-runner verification for normal queued runs, campaign deep-scan queue consumption, and vulnerability retest queue consumption
  - lint/validate pass for all modified superdomain AI workflows and the new knowledge auto-learn fragment
  - targeted package/test coverage for the modified database, handler, and knowledge/URL mapping paths
- Attempted but not yet counted as closed:
  - `make test-unit` printed passing/skipped package output through the suite, but `gotestsum` still did not exit cleanly in this Codex session
- This is the highest-risk remaining gap.

### 2. Knowledge Base

- Current knowledge base is now split into a main relational KB plus a standalone vectorkb retrieval layer.
- Not done yet:
  - webpage/article ingestion
  - richer cross-workspace ranking strategy beyond workspace/public defaults
  - stronger learned-document pruning and confidence aging
  - writeback from more workflow outcomes, not only summary-style learned artifacts

### 3. Campaign Batch Operations

- Current campaign layer is backend v2-ish, with API/CLI target aggregation, attack-chain-aware deep-scan selection, report/export analytics, and saved operator views.
- Not done yet:
  - richer campaign risk distribution views
  - campaign-level trend snapshots across repeated batches
  - reusable campaign strategy templates beyond report/export views
  - more explicit campaign audit history

### 4. Vulnerability Lifecycle Center

- Current lifecycle center is backend v2-ish for storage and queue orchestration.
- Not done yet:
  - less heuristic retest closure rules for edge cases
  - richer evidence diff view on top of the existing evidence timeline
  - workspace-level duplicate cluster review tools

### 5. Attack Chain Workbench

- Current attack-chain workbench is backend/API v2-ish.
- Not done yet:
  - frontend or visual workbench page
  - workspace-level attack-chain dashboard
  - campaign-aware attack-chain queue analytics
  - queue outcome feedback loop into attack-chain success-rate scoring

### 6. Documentation

- Root `README.md` and `plan.md` have been updated.
- Not done yet:
  - usage examples for new CLI/API capabilities
  - workflow authoring notes for attack-chain persistence and knowledge auto-learning
  - deeper operator-facing examples for the new `test-regression-stable-core` target

## Next Plan

### Priority 1: Full Runtime Verification

- Build and run the current source version.
- Keep `test-regression-stable-core` as the serial stable-release smoke path and extend it only when new backend-critical features land.
- Verify modified API routes:
  - vector knowledge APIs with a configured embeddings provider
- Verify workflow persistence path:
  - attack-chain JSON generation
  - attack-chain import into database
  - knowledge auto-learn writeback from a live workflow run
- Investigate and close the `make test-unit` / `gotestsum` non-exit issue so the full unit suite can be counted as green.

### Priority 2: Campaign Productization

- Completed in current branch:
  - `GET /campaigns/:id/report` for target risk distribution, deep-scan conversion, trigger mix, and rerun history
  - `GET /campaigns/:id/export` with CSV export and JSON fallback
  - local CLI wrappers: `osmedeus campaign report` and `osmedeus campaign export`
  - server-side and CLI filters for `risk/status/trigger` target slices
  - operator handoff presets for `high-risk`, `recovered`, and `failed` exports
  - minimal post-filter pagination for report/export with `offset/limit` and page metadata
  - operator ordering overrides for `risk`, `target`, `latest_run`, and `open_high_risk`
  - saved report/export profiles via `GET|PUT|DELETE /campaigns/:id/profiles/:name`
  - CLI profile management via `osmedeus campaign profile list|save|delete`
  - regression coverage for campaign report/export in handler tests and live API verification
- Next hardening steps:
  - add campaign-level trend snapshots for repeated batches on the same asset set
  - add compact batch-history summaries for recurring handoff/report commands
  - add stronger audit/history slices for profile usage and rerun/deep-scan decisions

### Priority 3: Knowledge Productization

- Add webpage/article ingestion.
- Improve layered ranking and source confidence weighting.
- Add maintenance controls for learned-document pruning and KB quality review.

### Priority 4: Documentation Completion

- Update `docs/api/`
- Add practical examples for new APIs
- Add operational notes for queue, retest, attack-chain persistence, and knowledge auto-learning

## Notes

- Stability is still the primary constraint.
- Prefer backend-first, additive changes over large UI or engine rewrites.
- Current-source live verification now covers AI workbench routes, knowledge ingest/search/learn, and real queue-worker consumption.
- Remaining runtime closure is concentrated in vector-provider verification, full superdomain AI workflow execution, and the `make test-unit` clean-exit issue.
