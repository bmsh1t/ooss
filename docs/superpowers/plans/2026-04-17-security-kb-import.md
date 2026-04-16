# Security KB Import Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a reusable KB import framework and the first `security-sqlite` adapter so `security_kb.sqlite` can be imported into Osmedeus knowledge documents/chunks and then indexed by the existing vectorkb pipeline.

**Architecture:** Add a small importer framework in `internal/knowledge`, expose it as `osmedeus kb import`, and implement a `security-sqlite` adapter that reads the source tables, converts rows into normalized knowledge documents/chunks, and writes them through the existing knowledge database upsert path. Keep vectorkb indexing separate.

**Tech Stack:** Go, Cobra CLI, Bun/SQLite, existing `internal/database` knowledge models, testify tests.

---

### Task 1: Add CLI surface for KB import

**Files:**
- Modify: `pkg/cli/kb.go`
- Test: `pkg/cli/kb_import_test.go`

- [ ] Add a new `kb import` subcommand with flags:
  - `--type`
  - `--path`
  - `--workspace` / `-w`
- [ ] Validate required flags and reject unsupported importer types.
- [ ] Call into a new `knowledge.Import(...)` entrypoint.
- [ ] Return human output by default and JSON output under `--json`.
- [ ] Add CLI tests for validation and successful summary output.

### Task 2: Add importer framework in `internal/knowledge`

**Files:**
- Create: `internal/knowledge/importer.go`
- Create: `internal/knowledge/importer_test.go`

- [ ] Define a small `ImportOptions` struct with:
  - `Type`
  - `Path`
  - `Workspace`
- [ ] Define an `ImportSummary` struct with at least:
  - imported documents
  - imported chunks
  - failed rows/documents
  - error samples
- [ ] Add a dispatcher like `Import(ctx, cfg, opts)` that selects an adapter by type.
- [ ] Add tests for unsupported type and empty path/workspace validation.

### Task 3: Implement `security-sqlite` adapter

**Files:**
- Create: `internal/knowledge/import_security_sqlite.go`
- Create: `internal/knowledge/import_security_sqlite_test.go`

- [ ] Open the sqlite database at `opts.Path` read-only.
- [ ] Enumerate/import the first supported tables:
  - `cwe`
  - `capec`
  - `attack_technique`
  - `agentic_threat`
  - `stride_cwe`
  - `owasp_top10` (if present and cheap to map)
- [ ] For each row, build a normalized knowledge document:
  - `source_path` like `security-sqlite://<table>/<id>`
  - stable title
  - metadata containing source table + row id
- [ ] Convert each record into bounded text chunks instead of one giant blob.
- [ ] Upsert via `database.UpsertKnowledgeDocument(...)`.
- [ ] Add tests using a temporary sqlite fixture with minimal versions of the supported tables.

### Task 4: Define record-to-document mapping rules

**Files:**
- Modify: `internal/knowledge/import_security_sqlite.go`
- Test: `internal/knowledge/import_security_sqlite_test.go`

- [ ] For `cwe`, map code/name/description/mitigation/hierarchy into a compact markdown-style document.
- [ ] For `capec`, map id/name/description/execution flow/prerequisites/mitigations where present.
- [ ] For `attack_technique`, map ATT&CK id/name/tactic/platform/description/mitigation links where present.
- [ ] For `agentic_threat`, map threat id/name/description/controls/related techniques.
- [ ] For `stride_cwe`, map stride category to related CWE entries in a compact lookup-style document.
- [ ] Ensure each generated chunk stays reasonably small and deterministic.

### Task 5: Add end-to-end import coverage

**Files:**
- Create: `test/testdata/security-kb-import/` (or reuse temp sqlite fixtures in tests)
- Modify: `pkg/cli/kb_import_test.go`
- Modify: `internal/knowledge/import_security_sqlite_test.go`

- [ ] Add a small fixture sqlite DB representing the supported source schema.
- [ ] Verify import creates `knowledge_documents` and `knowledge_chunks` rows.
- [ ] Verify imported documents are visible through existing list/search code paths.
- [ ] Verify repeated import is idempotent through stable `source_path` + content hash.

### Task 6: Documentation

**Files:**
- Modify: `README.md`
- Modify: `plan.md`

- [ ] Document the new `kb import --type security-sqlite` workflow.
- [ ] Explain that this path is preferred over raw YAML/JSON ingest for `security_kb.sqlite` derived content.
- [ ] Update the running plan/handoff so the repository reflects the new importer path once merged.

### Task 7: Verification

**Files:**
- Modify: none

- [ ] Run targeted `internal/knowledge` tests.
- [ ] Run targeted `pkg/cli` tests for the new command.
- [ ] Run a focused manual import against `security_kb.sqlite` into a temporary workspace.
- [ ] Verify `kb docs`, `kb search`, and optionally `kb vector index` on the imported temporary workspace.
