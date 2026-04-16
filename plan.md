# Plan

## Current Handoff (2026-04-16)

### Current Repo State

- Code has been pushed to `origin/main` (`https://github.com/bmsh1t/ooss.git`).
- Default sensitive values were removed from `osmedeus-base/osm-settings.yaml`.
- The main AI workflow / vectorkb / rerank changes are already landed in source.
- Local working tree was clean at handoff time.

### What Is Actually Still Unfinished

#### 1. Real provider-backed runtime validation

- Still not fully closed:
  - a full provider-backed end-to-end run of the main superdomain AI workflow on real scan output
  - a final real-environment verification with:
    - `OSM_LLM_BASE_URL`
    - `OSM_LLM_AUTH_TOKEN`
    - `TUMUER_API_KEY`
- Current situation:
  - controlled smoke paths are already covered
  - chat LLM path was previously verified
  - vectorkb/rerank live verification is blocked until valid provider credentials are supplied in env

#### 2. vectorkb doctor practical hardening

- Closed on current source:
  - `kb vector doctor` now supports an explicit live provider probe via `--probe-provider` / `?probe=true`
  - doctor can now surface `provider_auth_failed` and `provider_probe_failed` without changing the default static/offline-friendly behavior
- Follow-up still optional:
  - extend the same style of runtime probe to rerank-specific health if operator feedback shows it is needed separately from embedding health

#### 3. Plan/doc backfill cleanup

- Code is ahead of some historical plan docs.
- `docs/superpowers/plans/*` still contains unchecked historical task items.
- This is mainly documentation drift, not proof that the implementation is missing.

#### 4. Non-mainline backlog

- There are still older TODOs outside the current mainline:
  - cloud-related TODOs in `internal/cloud/*`
  - cloud CLI TODOs in `pkg/cli/cloud.go`
- These are not blocking the current AI workflow / KB / vectorkb line.

### Recommended Next 3 Tasks

1. Run one real provider-backed superdomain AI workflow verification with valid env credentials.
2. Backfill the most important historical plan docs so documentation state matches shipped code.
3. Revisit whether rerank-specific health probing is worth adding beyond the current embedding-provider probe.

## Current Status

### Completed

- Local knowledge base backend is in place.
  - File ingest for common local document types
  - Public article preview fetch via `kb fetch-url`, including batch preview from `--url-file`, suggested labels/confidence in the preview file, and explicit reviewed ingest via `kb ingest-preview`
  - External KB import framework now exists via `kb import`, with the first `security-sqlite` adapter for `security_kb.sqlite`
  - vectorkb doctor now reports semantic readiness state (`provider_not_configured`, `model_not_bound`, `provider_not_available`, `index_missing`, `ready`) instead of only low-level consistency counters
  - vectorkb doctor now supports an explicit live provider probe that can surface `provider_auth_failed` / `provider_probe_failed` without changing default doctor behavior
  - API ingest/learn now mirror CLI partial-success semantics: primary KB writes stay successful, while vectorkb auto-index failures are returned as warnings plus `vector_indexed` / `vector_error`
  - REST API now exposes vectorkb readiness via `/osm/api/knowledge/vector/doctor`
  - Search and document listing APIs/CLI
  - Workspace knowledge auto-learning from scan outputs
  - Workspace/public layered retrieval defaults, stable learned-document fingerprints, and age-aware learned-knowledge weighting
  - Learned documents now preserve source confidence, sample type, target-type tags, shared-layer storage, and confidence observation timestamps
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
  - current-source stable-flow smoke now verifies ACP fallback closure for vuln validation, attack-chain generation, visualization, path planning, and follow-up packaging
- `09-vuln-suite` now has a deterministic local Nuclei regression guard.
  - fixed the live Nuclei main-scan flag bug (`-rate` -> `-rate-limit`) in `osmedeus-base/workflows/common/09-vuln-suite.yaml`
  - main Nuclei scan now surfaces command failures instead of silently succeeding with empty output
  - `test/regression/vuln-suite-nuclei-smoke.sh` now verifies a real local match plus report generation and is included in `make test-regression-stable-core`
- `scan-content` now has a deterministic local regression guard.
  - `scan-content` and `do-scan-content` now use a more conservative default profile for small target sets
  - ffuf fallback no longer re-runs after a successful deparos result in `scan-content` / `do-scan-content`
  - discovered URLs are now collected before the existing content fingerprint/import stage so both deparos and ffuf paths feed the same downstream closure
  - `test/regression/scan-content-smoke.sh` now verifies both the deparos-success path and ffuf-fallback path and is included in `make test-regression-stable-core`
- Workspace stat summaries were cleaned up where they clearly polluted `total_assets`.
  - removed report/file-path/summary misuse of `db_total_assets(...)` in `10-report`, `09-vuln-suite`, `scan-vuln`, `url-gf`, `01-osint`, `00-incremental-check`, and `iis-shortname`
  - fixed duplicate summary counting in `scan-backup`
  - corrected `06-web-crawl` and `07-content-analysis` to update link/url-oriented stats instead of asset totals
  - removed duplicate end-of-module asset counting from `04-http-probe`
  - current `stable-core` now validates the support modules touched in that cleanup (`incremental-check`, `osint`, `scan-backup`, `scan-vuln`, `url-gf`, `iis-shortname`)
  - default workspace listing now overlays real asset-table counts on top of stored workspace rows, so `/osm/api/workspaces` is less sensitive to stale or previously inflated `workspaces.total_assets`
- Current-source verification completed for this round:
  - `make build`
  - `make test-unit`
  - `go test -short ./...` via plain Go runner with sandbox-safe loopback skips
  - `make test-regression-api-ai`
  - `make test-regression-api-knowledge`
  - `make test-regression-api-vector`
  - `make test-regression-ai-workflow-smoke`
  - `make test-regression-ai-semantic-vector-smoke`
  - `make test-regression-superdomain-lite-smoke`
  - `make test-regression-queue-live`
  - `make test-regression-stable-core`
  - targeted `go test ./internal/knowledge`
  - targeted `go test ./internal/vectorkb`
  - targeted `go test ./internal/database`
  - targeted `go test ./pkg/server/handlers`
  - targeted `go test ./pkg/server/handlers -run TestListWorkspaces_DefaultUsesAssetCountsWhenAvailable -count=1`
  - targeted `go test ./pkg/cli`
  - targeted `go test ./internal/linter`
  - workflow validation for `superdomain-extensive-ai-stable`
  - workflow validation for `superdomain-extensive-ai-hybrid`
  - workflow validation for `superdomain-extensive-ai-optimized`
  - workflow validation for `superdomain-extensive-ai-lite`
  - workflow validation for `scan-content`
  - workflow validation for `incremental-check`
  - workflow validation for `osint`
  - workflow validation for `scan-backup`
  - workflow validation for `scan-vuln`
  - workflow validation for `url-gf`
  - workflow validation for `iis-shortname`
  - workflow validation for `http-probe`
  - workflow validation for `ai-knowledge-autolearn`
  - workflow validation for `do-scan-content`
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
  - provider-backed execution of the full superdomain AI chain on real scan output, beyond the controlled provider-enabled semantic smoke path
- Verified already:
  - clean current-source build
  - full `go test -short ./...` pass with the plain Go runner
  - real local server startup via `make test-regression-api-ai`
  - real local server startup via `make test-regression-api-knowledge`
  - real local server startup via `make test-regression-api-vector`
  - current-source AI follow-up workflow execution via `make test-regression-ai-workflow-smoke`
  - current-source provider-enabled semantic workflow execution via `make test-regression-ai-semantic-vector-smoke`
  - current-source controlled `superdomain-extensive-ai-lite` execution via `make test-regression-superdomain-lite-smoke`
  - real local server + worker startup via `make test-regression-queue-live`
  - live API verification for campaign, vulnerability lifecycle, attack-chain workbench, non-vector knowledge-base routes, and vectorkb routes using a local mock embeddings provider
  - live queue-runner verification for normal queued runs, campaign deep-scan queue consumption, and vulnerability retest queue consumption
  - live workflow verification that `campaign-create`, `retest-queue`, `knowledge-autolearn`, and `kb/queue/campaign` subcommands stay pinned to the active `base-folder` / `workflow-folder`
  - live workflow verification that `ai-semantic-search` and `ai-semantic-search-hybrid` can execute end-to-end with real vectorkb indexing, live workspace/shared/global KB hits, and stable scan-data fallback in a provider-enabled environment
  - live workflow verification that the real current-source `superdomain-extensive-ai-lite` flow can execute end-to-end in a controlled workspace with seeded scan artifacts, semantic hits, decision follow-up, and searchable KB auto-learn output
  - live workflow verification that the real current-source `superdomain-extensive-ai-stable` flow can execute end-to-end in a controlled workspace with ACP fallback vuln-validation, attack-chain generation, visualization, path planning, decision follow-up, and searchable KB auto-learn output
  - live workflow verification that the real current-source `scan-content` module keeps the intended deparos-first / ffuf-fallback behavior and still feeds the downstream fingerprint/import stage in both paths
  - live workflow verification that the real current-source `09-vuln-suite` module can produce a deterministic local Nuclei hit and Markdown report through the normal workflow path
  - back-to-back regression runs no longer depend on fixed embeddings mock ports for the semantic/vector/lite/stable smoke scripts
  - machine-readable CLI verification that `--json` stays clean even with an explicit `--workflow-folder`
  - lint/validate pass for all modified superdomain AI workflows and the new knowledge auto-learn fragment
  - targeted package/test coverage for the modified database, handler, and knowledge/URL mapping paths
- The highest-risk remaining gap is now a provider-backed full superdomain AI workflow run on current source with real scan tooling and verified live semantic hits, not the controlled semantic, stable-flow, lite-flow, or follow-up closure paths themselves.
- The next backend-focused audit target after that is the report/summary metric path, especially places that still rely on generic `db_total_assets(...)` counters over filtered content-style files.

### 2. Knowledge Base

- Current knowledge base is now split into a main relational KB plus a standalone vectorkb retrieval layer.
- `ai-knowledge-autolearn` no longer reuses `knowledgeWorkspace` as the source workspace for `kb learn`; retrieval and learning are split so shared KB retrieval does not poison source selection during writeback.
- Not done yet:
  - direct webpage/article ingestion into the KB without a manual review hop
  - richer cross-workspace ranking strategy beyond workspace/public defaults
  - stronger learned-document pruning and KB quality review controls beyond the current preview-first confirm step
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
  - deeper operator-facing examples for the new `test-regression-stable-core` and `test-regression-ai-workflow-smoke` targets

## Next Plan

### Priority 1: Full Runtime Verification

- Build and run the current source version.
- Keep `test-regression-stable-core` as the serial stable-release smoke path and extend it only when new backend-critical features land.
- Keep `test-regression-ai-workflow-smoke` as the current-source workflow closure guard for the superdomain AI follow-up path.
- Keep `test-regression-ai-semantic-vector-smoke` as the provider-enabled semantic workflow guard for the vectorkb workflow path.
- Keep `test-regression-scan-content-smoke` as the deterministic guard for the content-discovery module's deparos/ffuf/fingerprint closure.
- Keep `test-regression-superdomain-lite-smoke` as the current-source guard for the controlled `superdomain-extensive-ai-lite` closure path.
- Keep `make test-unit-plain` / `go test -short ./...` as the trustworthy unit-suite signal in sandbox-limited environments.
- Verify workflow persistence path:
  - attack-chain JSON generation
  - attack-chain import into database
  - provider-backed semantic-search execution with verified live KB hits on a real scan run, not just the controlled smoke path
- Investigate `gotestsum` wrapper behavior only as a secondary polish task now that the underlying short suite is green.

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

- Add direct webpage/article ingestion beyond the current preview-first `kb fetch-url` + `kb ingest-preview` path.
- Improve layered ranking beyond workspace/public defaults.
- Add maintenance controls for learned-document pruning and KB quality review.

### Priority 4: Documentation Completion

- Update `docs/api/`
- Add practical examples for new APIs
- Add operational notes for queue, retest, attack-chain persistence, and knowledge auto-learning

## Notes

- Stability is still the primary constraint.
- Prefer backend-first, additive changes over large UI or engine rewrites.
- Current-source live verification now covers AI workbench routes, knowledge ingest/search/learn, vectorkb with a local mock embeddings provider, real queue-worker consumption, a controlled superdomain-style AI follow-up workflow closure path, provider-enabled semantic fragment execution through the current source semantic workflows with verified live KB hits, and a controlled real-flow `superdomain-extensive-ai-lite` execution with KB auto-learn writeback.
- Current-source stable-core verification now also covers the real `scan-content` module closure through deterministic deparos-success and ffuf-fallback local smokes.
- Remaining runtime closure is concentrated in provider-backed full superdomain AI workflow execution on real scan output with verified live semantic hits; the underlying short unit suite and stable-core regression path are now green on current source.
