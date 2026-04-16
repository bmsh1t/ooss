# Stable Core Checklist

Use this checklist before calling the branch "stable enough" for local backend release work.

## 1. Build Current Source

```bash
make build
```

Expected result:

- `build/bin/osmedeus` exists
- the build completes without touching unrelated state

## 2. Run Unit Tests

Preferred:

```bash
make test-unit
```

Fallback when `gotestsum` formatting/wrapping is flaky in the current environment:

```bash
make test-unit-plain
```

Expected result:

- unit packages pass
- if only the formatter wrapper is unstable, `test-unit-plain` should still provide a clean Go-native signal
- listener-based tests now skip cleanly when the environment forbids loopback binds, instead of failing unrelated unit paths

## 3. Run Stable Core Smoke Regression

```bash
make test-regression-stable-core
```

What it covers:

- workflow validation for:
  - `superdomain-extensive-ai-stable`
  - `superdomain-extensive-ai-hybrid`
  - `superdomain-extensive-ai-optimized`
  - `superdomain-extensive-ai-lite`
  - `incremental-check`
  - `osint`
  - `scan-content`
  - `scan-backup`
  - `scan-vuln`
  - `url-gf`
  - `iis-shortname`
  - `04-http-probe`
  - `05-fingerprint`
  - `06-web-crawl`
  - `07-content-analysis`
  - `09-vuln-suite`
  - `10-report`
  - `fragments/do-ai-knowledge-autolearn.yaml`
  - `fragments/do-scan-content.yaml`
- current-source AI workflow smoke regression:
  - `intelligent-analysis -> apply-decision -> retest/operator fallback -> campaign create -> retest queue -> post-followup -> knowledge autolearn`
  - verifies workflow-generated campaign and queued retest artifacts without external targets
  - verifies fragment CLI subcalls keep the active `base-folder` / `workflow-folder`
- current-source provider-enabled semantic workflow smoke:
  - real `kb ingest` into workspace/shared/global layers
  - real mock-provider vectorkb indexing and semantic fragment execution through `do-ai-semantic-search` and `do-ai-semantic-search-hybrid`
  - deterministic semantic smoke via `enableSemanticAgent=false`
  - verifies live workspace/shared/global KB hits through both direct CLI JSON output and workflow-generated semantic artifacts
  - keeps scan-data fallback intact even when KB retrieval is not the only contributing source
  - auto-selects the next free local embeddings mock port when the default regression port is already occupied
- current-source `superdomain-extensive-ai-lite` controlled flow smoke:
  - runs the real current-source lite workflow from `osmedeus-base/workflows`
  - seeds realistic subdomain, probing, fingerprint, content-analysis, and vuln artifacts into a temporary workspace
  - verifies provider-backed semantic retrieval, decision application, follow-up coordination, and knowledge auto-learning in one closure path
  - verifies learned KB artifacts are searchable after the workflow completes
  - auto-selects the next free local embeddings mock port when the default regression port is already occupied
- current-source `superdomain-extensive-ai-stable` controlled flow smoke:
  - runs the real current-source stable workflow from `osmedeus-base/workflows`
  - seeds realistic subdomain, probing, fingerprint, vuln, and knowledge-layer artifacts into a temporary workspace
  - verifies ACP-timeout fallback closure for vulnerability validation, attack-chain generation, attack-chain visualization, path planning, follow-up packaging, and knowledge auto-learning
  - verifies the stable-flow artifacts are persisted and the learned KB metadata stays searchable after the workflow completes
  - auto-selects the next free local embeddings mock port when the default regression port is already occupied
- current-source `superdomain-extensive-ai-optimized` controlled flow smoke:
  - runs the real current-source optimized workflow from `osmedeus-base/workflows`
  - seeds realistic subdomain, probing, fingerprint, vuln, and knowledge-layer artifacts into a temporary workspace
  - verifies the optimized AI closure persists semantic / decision / retest / operator / campaign / follow-up artifacts and keeps KB writeback searchable after the workflow completes
  - auto-selects the next free local embeddings mock port when the default regression port is already occupied
- current-source `vuln-suite` Nuclei smoke:
  - starts a loopback-only local HTTP server with a deterministic marker page
  - runs the real `09-vuln-suite` workflow path against a custom local Nuclei template
  - verifies the workflow-produced JSONL and Markdown outputs contain a real match, catching command-line regressions in the main Nuclei scan path
- current-source `scan-content` smoke:
  - runs the real `scan-content` module against deterministic local mock `deparos`, `ffuf`, and `httpx` binaries
  - verifies the deparos-success path skips ffuf instead of double-running fallback bruteforce
  - verifies the ffuf fallback path still produces discovered URLs that reach the downstream fingerprint/import steps
- live AI API regression:
  - campaign
  - vulnerabilities
  - attack-chains
- live knowledge regression:
  - ingest
  - documents
  - keyword search
  - workspace/public learning path
- live vectorkb regression:
  - local mock embeddings provider startup
  - ingest auto-index
  - explicit vector reindex skip path
  - vector stats
  - workspace/public layered vector search
  - learn auto-index into the public layer
- live queue regression:
  - CLI queue consumption
  - vulnerability retest queue consumption
  - campaign deep-scan queue consumption

Temporary artifact roots:

- `/tmp/osm-stable-core-ai-workflow`
- `/tmp/osm-stable-core-ai-semantic`
- `/tmp/osm-stable-core-superdomain-lite`
- `/tmp/osm-stable-core-superdomain-stable`
- `/tmp/osm-stable-core-superdomain-optimized`
- `/tmp/osm-stable-core-scan-content`
- `/tmp/osm-stable-core-vuln-suite`
- `/tmp/osm-stable-core-ai`
- `/tmp/osm-stable-core-knowledge`
- `/tmp/osm-stable-core-vector`
- `/tmp/osm-stable-core-queue`
- `/tmp/osm-stable-core-validate`

## 4. Optional Full Workflow Run

Run only when the local toolchain and target setup are ready:

- provider-bound semantic search workflow verification
- execute the current-source `superdomain-extensive-ai-*` workflow against a controlled target with real scan tooling and provider-backed semantic search enabled
- confirm attack-chain persistence and knowledge auto-learn artifacts are written back as expected

## Release Bar

The branch is in the current "stable core" band when all of the following are true:

- build succeeds
- unit tests have a trustworthy pass signal
- `make test-regression-stable-core` passes end-to-end
- no new workflow lint warnings were introduced in modified AI fragments
- controlled current-source `superdomain-extensive-ai-stable`, `superdomain-extensive-ai-optimized`, and `superdomain-extensive-ai-lite` closure smokes stay green inside the stable-core regression
- the deterministic local `scan-content` smoke stays green inside the stable-core regression
- the deterministic local `vuln-suite` Nuclei smoke stays green inside the stable-core regression
