# Plan

## Current Status

### Completed

- Local knowledge base backend is in place.
  - File ingest for common local document types
  - Search and document listing APIs/CLI
  - Workspace knowledge auto-learning from scan outputs
- Campaign batch-operation backend v1 is in place.
  - Campaign entity and aggregation
  - Failed target rerun
  - High-risk deep-scan queue hook
- Vulnerability lifecycle backend v1 is in place.
  - Lifecycle states: `new`, `triaged`, `verified`, `false_positive`, `retest`, `closed`
  - AI verdict and analyst review fields
  - Retest task creation and queue-state synchronization
  - Workspace vulnerability board API
- Attack chain workbench backend v1 is in place.
  - Attack-chain report persistence
  - Attack-chain import function/API
  - Summary/detail query APIs
  - Workflow fragments now write attack-chain outputs into the report store
- Static checks completed for the current round.
  - `gofmt`
  - `git diff --check`
  - YAML syntax validation for modified attack-chain workflow fragments

## Unfinished Work

### 1. Runtime Validation

- Full runtime validation is not completed yet.
- Not done yet:
  - build from current source
  - server startup validation
  - end-to-end API verification
  - workflow execution verification against current source
  - regression pass across modified queue / vuln / attack-chain paths
- This is the highest-risk remaining gap.

### 2. Knowledge Base

- Current knowledge base is still text/document based, not a real vector knowledge base yet.
- Not done yet:
  - embedding generation
  - vector index / semantic retrieval
  - webpage/article ingestion
  - cross-workspace relevance ranking

### 3. Campaign Batch Operations

- Current campaign layer is backend v1, focused on stable API capability.
- Not done yet:
  - richer campaign risk distribution views
  - campaign-level report/export
  - reusable campaign strategy templates
  - more explicit campaign audit history

### 4. Vulnerability Lifecycle Center

- Current lifecycle center is backend v1.
- Not done yet:
  - automatic retest result interpretation
  - automatic state transition from retest outputs back to `verified` / `false_positive` / `closed`
  - stronger linkage between vulnerabilities, assets, reports, and attack chains
  - richer retest history timeline

### 5. Attack Chain Workbench

- Current attack-chain workbench is backend/API v1.
- Not done yet:
  - frontend or visual workbench page
  - automatic linking from attack chains back to vulnerabilities/assets
  - deeper filtering such as verified-only chain generation from stored vuln state
  - workspace-level attack-chain dashboard

### 6. Documentation

- Root `README.md` has been updated.
- Not done yet:
  - `docs/api/` entries for newly added campaign / vulnerability / attack-chain APIs
  - usage examples for new CLI/API capabilities
  - workflow authoring notes for attack-chain persistence

## Next Plan

### Priority 1: Full Runtime Verification

- Build and run the current source version.
- Verify modified API routes:
  - campaign APIs
  - vulnerability lifecycle APIs
  - attack-chain APIs
  - knowledge-base APIs
- Verify queue runner behavior:
  - normal queued run
  - campaign deep-scan path
  - vulnerability retest path
- Verify workflow persistence path:
  - attack-chain JSON generation
  - attack-chain import into database

### Priority 2: Retest Result Closure

- Add logic to evaluate retest results and write back vulnerability lifecycle state.
- Target outcome:
  - retest run completes
  - result is inspected
  - vulnerability transitions to an explicit post-retest state

### Priority 3: Attack Chain Linking

- Link attack-chain reports to:
  - vulnerabilities
  - related assets
  - report references
  - retest tasks where applicable
- Target outcome:
  - attack-chain report is no longer standalone data
  - users can navigate between chain, vuln, and asset records

### Priority 4: Campaign Productization

- Extend campaign reporting with:
  - target risk distribution
  - deep-scan conversion rate
  - rerun history
  - campaign summary export

### Priority 5: Vector Knowledge Base

- Introduce a real embedding-based retrieval path.
- Keep this behind a stable abstraction so the current text/document KB remains usable.
- Limit first scope to local document corpora only.

### Priority 6: Documentation Completion

- Update `docs/api/`
- Add practical examples for new APIs
- Add operational notes for queue, retest, and attack-chain persistence

## Notes

- Stability is still the primary constraint.
- Prefer backend-first, additive changes over large UI or engine rewrites.
- Do not treat a feature as complete until runtime verification is done against the current source tree.
