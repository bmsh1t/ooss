# KB Vector Doctor Provider Probe Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit opt-in provider probe to `kb vector doctor` so operators can tell whether the configured embedding provider actually works without changing default doctor behavior.

**Architecture:** Keep the current static doctor flow unchanged by default. Add a `ProbeProvider` option that is passed from CLI/API into `vectorkb.Doctor`, then reuse the existing embedding request path to issue a tiny live probe only when explicitly requested. Map probe failures into two new semantic statuses while preserving the existing index/consistency logic.

**Tech Stack:** Go, Cobra CLI, Fiber handlers, existing `internal/llm` embedding client, testify-based unit tests.

---

### Task 1: Thread the new probe option through doctor inputs

**Files:**
- Modify: `internal/vectorkb/models.go`
- Modify: `pkg/cli/kb_vector.go`
- Modify: `pkg/server/handlers/knowledge.go`
- Test: `pkg/cli/kb_vector_test.go`
- Test: `pkg/server/handlers/knowledge_test.go`

- [ ] Add `ProbeProvider bool` to `vectorkb.DoctorOptions`.
- [ ] Add `--probe-provider` to `osmedeus kb vector doctor` and pass it into `vectorkb.Doctor(...)`.
- [ ] Read `probe=true` from `/knowledge/vector/doctor` and pass it into `vectorkb.Doctor(...)`.
- [ ] Add/adjust lightweight CLI and handler tests so the new opt-in surface is covered.

### Task 2: Implement the opt-in provider probe inside doctor

**Files:**
- Modify: `internal/vectorkb/maintenance.go`
- Test: `internal/vectorkb/vectorkb_test.go`

- [ ] Add a small helper that reuses `llm.GenerateEmbeddingsWithProvider(...)` with a minimal input like `[]string{"doctor probe"}`.
- [ ] Only run that helper when `ProbeProvider` is true and static provider/model checks have already passed.
- [ ] If the probe error clearly indicates auth/permission failure, set `semantic_status=provider_auth_failed`.
- [ ] For other probe failures, set `semantic_status=provider_probe_failed`.
- [ ] If the probe succeeds, keep the existing `finalizeDoctorReport(...)` behavior so `index_missing`, `consistency_issues`, and `ready` still work as before.

### Task 3: Lock behavior with focused tests

**Files:**
- Modify: `internal/vectorkb/vectorkb_test.go`
- Modify: `pkg/server/handlers/knowledge_test.go`
- Modify: `pkg/cli/kb_vector_test.go`

- [ ] Add a vectorkb test where the probe server returns 401 and assert `provider_auth_failed`.
- [ ] Add a vectorkb test where the probe server returns a non-auth failure (for example 502) and assert `provider_probe_failed`.
- [ ] Add a vectorkb test showing default doctor behavior is unchanged when probe is not requested.
- [ ] Keep API/CLI tests narrow: verify flag/query plumbing and the resulting reported status.

### Task 4: Update operator-facing docs

**Files:**
- Modify: `README.md`
- Modify: `plan.md`

- [ ] Mention the new opt-in probe in the KB/vector doctor usage docs.
- [ ] Update the current handoff note so it reflects that doctor can now explicitly probe provider auth/runtime availability when requested.

### Task 5: Verification

**Files:**
- Modify: none
- Test: existing targeted suites

- [ ] Run targeted vectorkb tests.
- [ ] Run targeted handler tests.
- [ ] Run targeted CLI tests.
- [ ] If needed, run a focused regression that depends on doctor output semantics and confirm default behavior remains unchanged.
