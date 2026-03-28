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
  - `fragments/do-ai-knowledge-autolearn.yaml`
- live AI API regression:
  - campaign
  - vulnerabilities
  - attack-chains
- live knowledge regression:
  - ingest
  - documents
  - keyword search
  - workspace/public learning path
- live queue regression:
  - CLI queue consumption
  - vulnerability retest queue consumption
  - campaign deep-scan queue consumption

Temporary artifact roots:

- `/tmp/osm-stable-core-ai`
- `/tmp/osm-stable-core-knowledge`
- `/tmp/osm-stable-core-queue`
- `/tmp/osm-stable-core-validate`

## 4. Optional Provider-Dependent Checks

Run these only when an embeddings provider is configured on the host:

- live vector knowledge indexing/search verification
- provider-bound semantic search workflow verification

## 5. Optional Full Workflow Run

Run only when the local toolchain and target setup are ready:

- execute the current-source `superdomain-extensive-ai-*` workflow against a controlled target
- confirm attack-chain persistence and knowledge auto-learn artifacts are written back as expected

## Release Bar

The branch is in the current "stable core" band when all of the following are true:

- build succeeds
- unit tests have a trustworthy pass signal
- `make test-regression-stable-core` passes end-to-end
- no new workflow lint warnings were introduced in modified AI fragments
