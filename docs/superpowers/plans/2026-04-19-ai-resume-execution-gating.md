# AI Resume Execution Gating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `resume-context` actively gate downstream follow-up execution so Osmedeus avoids duplicate queueing/campaign creation and more strongly honors manual-first follow-up state.

**Architecture:** Reuse the existing `resume-context > followup-decision > previous_followup_*` contract without introducing a new orchestrator. Add small fail-open gating branches in the three downstream follow-up fragments (`ai-retest-planning`, `ai-operator-queue`, `ai-campaign-handoff`) so they read stable resume state when present, otherwise preserve current behavior exactly.

**Tech Stack:** Osmedeus YAML workflow fragments, bash + jq gating logic, Go integration tests in `test/integration/workflow_test.go`, shell smoke validation in `test/regression/ai-workflow-smoke.sh`.

---

## Stability Rules For This Plan

- Do **not** change the top-level `superdomain-extensive-ai*` workflow ordering.
- Do **not** replace the current `resume-context` contract.
- Do **not** remove existing `followup-decision` / `previous_followup_*` fallback paths.
- Every gating branch must be fail-open: missing or partial `resume-context` means current behavior continues.
- Prefer suppressing duplicate queue/campaign actions over inventing new routing branches.

## File Map

### Modify
- `osmedeus-base/workflows/fragments/do-ai-retest-planning.yaml`
  - Consume `resume-context` for duplicate queue suppression and manual-first target emphasis.
- `osmedeus-base/workflows/fragments/do-ai-operator-queue.yaml`
  - Consume `resume-context` for manual-first operator target shaping and duplicate follow-up suppression.
- `osmedeus-base/workflows/fragments/do-ai-campaign-handoff.yaml`
  - Consume `resume-context` for campaign create de-duplication and next-phase aware handoff shaping.
- `test/integration/workflow_test.go`
  - Add focused gating tests for retest planning, operator queue, and campaign handoff.
- `test/regression/ai-workflow-smoke.sh`
  - Add one small assertion that a stable gating decision actually changes downstream execution behavior.

### Reference
- `docs/superpowers/specs/2026-04-18-superdomain-ai-resume-autopilot-design.md`
- `docs/superpowers/plans/2026-04-19-superdomain-ai-resume-autopilot.md`
- `osmedeus-base/workflows/fragments/do-ai-pre-scan-decision.yaml`
- `osmedeus-base/workflows/fragments/do-ai-pre-scan-decision-acp.yaml`

---

### Task 1: Add failing tests for resume-driven de-duplication and manual-first gating

**Files:**
- Modify: `test/integration/workflow_test.go`

- [ ] **Step 1: Add a failing retest-planning test for queue suppression when resume already says queue follow-up is effective**

Add a focused test near the existing `ai-retest-planning` coverage. Seed a valid `resume-context` carrying `followup_summary.retest_queued_targets > 0` and assert the fragment does not regenerate a duplicate automation queue.

```go
func TestExecuteAIRetestPlanningSkipsDuplicateQueueWhenResumeQueueEffective(t *testing.T) {
	targetSpace := "ai-retest-resume-queue-effective"
	aiDir := filepath.Join(outputDir, "ai-analysis")

	writeTestFile(t, filepath.Join(aiDir, "resume-context-"+targetSpace+".json"), `{
  "followup_decision_source": "followup-decision",
  "scan_profile": "aggressive",
  "severity": "critical,high",
  "next_phase": "manual-exploitation",
  "priority_mode": "manual-first",
  "confidence_level": "high",
  "reuse_sources": ["retest-queue"],
  "signal_scores": {"escalation_score": 17},
  "refined_targets": {"priority_targets": ["https://seed.example.com/admin"], "focus_areas": ["https://seed.example.com/admin"]},
  "seed_targets": {"manual_first_targets": ["https://seed.example.com/admin"], "high_confidence_targets": ["https://seed.example.com/api"]},
  "followup_summary": {
    "operator_tasks": 2,
    "campaign_targets": 1,
    "passive_targets": 0,
    "retest_queued_targets": 3,
    "rescan_critical": 0,
    "rescan_high": 0,
    "campaign_ready": true
  },
  "campaign_create": {"status": "created", "campaign_id": "camp-retest-1", "queued_runs": 3}
}`)

	// seed the minimum vuln / semantic / decision fixtures already used by neighboring retest tests
	// execute fragment
	// assert summary.previous_queue_followup_effective == true
	// assert automation_queue length == 0 or summary.status == "skipped_existing_queue"
}
```

- [ ] **Step 2: Run the new retest-planning test and confirm it fails**

Run:

```bash
mkdir -p /tmp/go-build-cache /tmp/go-mod-cache && \
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
go test ./test/integration -run TestExecuteAIRetestPlanningSkipsDuplicateQueueWhenResumeQueueEffective -count=1
```

Expected: fail because current retest planning does not yet honor `resume-context` queue state.

- [ ] **Step 3: Add a failing operator-queue test for manual-first target shaping from resume-context**

Add a focused test that seeds `resume-context.priority_mode = manual-first` plus manual/high-confidence targets and asserts operator queue output favors those targets in `focus_targets` / task ordering.

```go
func TestExecuteAIOperatorQueuePrefersResumeManualFirstTargets(t *testing.T) {
	targetSpace := "ai-operator-resume-manual-first"
	// seed resume-context with manual_first_targets=[admin], high_confidence_targets=[upload]
	// seed minimal decision + semantic fixtures
	// execute do-ai-operator-queue fragment
	// assert payload.summary.previous_priority_mode == "manual-first"
	// assert first high-priority target/task references https://seed.example.com/admin
}
```

- [ ] **Step 4: Run the operator-queue test and confirm it fails**

Run:

```bash
mkdir -p /tmp/go-build-cache /tmp/go-mod-cache && \
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
go test ./test/integration -run TestExecuteAIOperatorQueuePrefersResumeManualFirstTargets -count=1
```

Expected: fail because current operator queue only reads legacy previous-followup inputs.

- [ ] **Step 5: Add a failing campaign-handoff test for campaign-create de-duplication**

Add a test that seeds `resume-context.campaign_create.status = "created"` and asserts the handoff module skips or downgrades duplicate campaign creation instead of creating another campaign package blindly.

```go
func TestExecuteAICampaignHandoffSkipsDuplicateCampaignCreateWhenResumeAlreadyCreated(t *testing.T) {
	targetSpace := "ai-campaign-resume-created"
	// seed resume-context with campaign_create.status=created, campaign_id, queued_runs
	// seed retest/operator queue artifacts
	// execute do-ai-campaign-handoff fragment
	// assert campaign-create-*.json keeps status "created" or marks duplicate skip
	// assert no second create command is issued in the command/log artifact
}
```

- [ ] **Step 6: Run the campaign-handoff test and confirm it fails**

Run:

```bash
mkdir -p /tmp/go-build-cache /tmp/go-mod-cache && \
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
go test ./test/integration -run TestExecuteAICampaignHandoffSkipsDuplicateCampaignCreateWhenResumeAlreadyCreated -count=1
```

Expected: fail because current handoff only uses old previous-followup inputs.

- [ ] **Step 7: Commit the tests-only checkpoint**

```bash
git add test/integration/workflow_test.go
git commit -m "test: lock resume execution gating behavior"
```

---

### Task 2: Teach retest planning to honor resume-context queue and manual-first state

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-retest-planning.yaml`
- Test: `test/integration/workflow_test.go`

- [ ] **Step 1: Add `resumeContextFile` param next to `previousFollowupDecisionFile`**

```yaml
  - name: resumeContextFile
    default: "{{Output}}/ai-analysis/resume-context-{{TargetSpace}}.json"
```

- [ ] **Step 2: Read resume-context before legacy follow-up state in `build-retest-context`**

Use the same validity guard already proven in pre-scan:

```bash
if [ -f "{{resumeContextFile}}" ] \
  && jq -e '(.scan_profile? != null) and (.severity? != null) and ((.followup_decision_source? != null) or (.refined_targets? != null) or (.seed_targets? != null))' "{{resumeContextFile}}" >/dev/null 2>&1; then
  PREV_FOLLOWUP_SOURCE_KIND="resume-context"
  PREV_FOLLOWUP_TARGETS=$(jq -r '[((.refined_targets.priority_targets // []) + (.seed_targets.manual_first_targets // []) + (.seed_targets.high_confidence_targets // []) + (.seed_targets.retest_targets // []) + (.seed_targets.rescan_targets // []))[]?] | reduce .[] as $item ([]; if ($item != null and $item != "" and (index($item) == null)) then . + [$item] else . end) | length' "{{resumeContextFile}}" 2>/dev/null || echo 0)
  PREV_PRIORITY_MODE=$(jq -r '.priority_mode // ""' "{{resumeContextFile}}" 2>/dev/null || echo "")
  PREV_CONFIDENCE_LEVEL=$(jq -r '.confidence_level // ""' "{{resumeContextFile}}" 2>/dev/null || echo "")
  PREV_NEXT_PHASE=$(jq -r '.next_phase // ""' "{{resumeContextFile}}" 2>/dev/null || echo "")
  PREV_REUSE_SOURCES_JSON=$(jq -c '.reuse_sources // []' "{{resumeContextFile}}" 2>/dev/null || echo '[]')
  PREV_QUEUE_EFFECTIVE=$(jq -r 'if .queue_followup_effective != null then .queue_followup_effective else ((.followup_summary.retest_queued_targets // 0) > 0) end' "{{resumeContextFile}}" 2>/dev/null || echo false)
fi
```

- [ ] **Step 3: Add minimal gating rule to suppress duplicate automation queue**

Inside the target selection / queue assembly logic, short-circuit duplicate queue creation when resume says the queue already worked:

```bash
if [ "$PREV_QUEUE_EFFECTIVE" = "true" ] && [ "$PREV_FOLLOWUP_TARGETS" -gt 0 ] 2>/dev/null; then
  RETEST_TARGET_SOURCE="previous_followup_resume"
  RETEST_AUTOMATION_QUEUE_JSON='[]'
  RETEST_NOTES="resume queue already effective; suppress duplicate queue creation"
fi
```

- [ ] **Step 4: Bias manual-first targets ahead of generic rescan targets**

When `PREV_PRIORITY_MODE=manual-first`, build the candidate target list in this order:

```bash
RETEST_RESUME_TARGETS_JSON=$(jq -nc \
  --slurpfile resume "{{resumeContextFile}}" \
  '($resume[0] // {}) as $doc
   | ((($doc.seed_targets.manual_first_targets // [])
      + ($doc.seed_targets.high_confidence_targets // [])
      + ($doc.refined_targets.priority_targets // [])
      + ($doc.seed_targets.retest_targets // [])
      + ($doc.seed_targets.rescan_targets // []))
      | reduce .[] as $item ([]; if ($item != null and $item != "" and (index($item) == null)) then . + [$item] else . end))')
```

- [ ] **Step 5: Re-run focused retest tests and confirm they pass**

Run:

```bash
mkdir -p /tmp/go-build-cache /tmp/go-mod-cache && \
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
go test ./test/integration -run 'TestExecuteAIRetestPlanning(SkipsDuplicateQueueWhenResumeQueueEffective|BuildsCampaignHandoffFromPreviousFollowup|BuildsRetestPlanFromQueuedPreviousFollowupParams)' -count=1
```

Expected: `ok .../test/integration`

- [ ] **Step 6: Commit the retest gating change**

```bash
git add osmedeus-base/workflows/fragments/do-ai-retest-planning.yaml test/integration/workflow_test.go
git commit -m "feat: gate retest planning with resume context"
```

---

### Task 3: Teach operator queue to honor manual-first and queue-effective resume state

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-operator-queue.yaml`
- Test: `test/integration/workflow_test.go`

- [ ] **Step 1: Add `resumeContextFile` param**

```yaml
  - name: resumeContextFile
    default: "{{Output}}/ai-analysis/resume-context-{{TargetSpace}}.json"
```

- [ ] **Step 2: Read resume-context before `previousFollowupDecisionFile`**

Mirror the retest/pre-scan guard and populate these operator-specific fields:

```bash
PREV_FOLLOWUP_SOURCE_KIND="resume-context"
PREV_PRIORITY_MODE=$(jq -r '.priority_mode // ""' "{{resumeContextFile}}")
PREV_CONFIDENCE_LEVEL=$(jq -r '.confidence_level // ""' "{{resumeContextFile}}")
PREV_COMBINED_TARGETS_JSON=$(jq -c '((.seed_targets.manual_first_targets // []) + (.seed_targets.high_confidence_targets // []) + (.refined_targets.priority_targets // []) + (.seed_targets.operator_targets // []) + (.seed_targets.confirmed_targets // [])) | reduce .[] as $item ([]; if ($item != null and $item != "" and (index($item) == null)) then . + [$item] else . end)' "{{resumeContextFile}}" 2>/dev/null || echo '[]')
PREV_QUEUE_EFFECTIVE=$(jq -r 'if .queue_followup_effective != null then .queue_followup_effective else ((.followup_summary.retest_queued_targets // 0) > 0) end' "{{resumeContextFile}}" 2>/dev/null || echo false)
```

- [ ] **Step 3: Bias `manual_first_targets` to the top of operator queue generation**

Use the resume target groups before decision/semantic fallback:

```bash
if [ "$PREV_PRIORITY_MODE" = "manual-first" ]; then
  OPERATOR_MANUAL_TARGETS_JSON=$(jq -c '.seed_targets.manual_first_targets // []' "{{resumeContextFile}}" 2>/dev/null || echo '[]')
  OPERATOR_HIGH_CONFIDENCE_JSON=$(jq -c '.seed_targets.high_confidence_targets // []' "{{resumeContextFile}}" 2>/dev/null || echo '[]')
fi
```

Then ensure P1 tasks are built from `OPERATOR_MANUAL_TARGETS_JSON` first.

- [ ] **Step 4: Avoid duplicating queue-derived tasks when resume says queue follow-up already worked**

```bash
if [ "$PREV_QUEUE_EFFECTIVE" = "true" ]; then
  OPERATOR_QUEUE_DUPLICATE_SUPPRESSED=true
  # keep manual validation tasks, but do not re-import retest queue targets as fresh operator tasks
fi
```

- [ ] **Step 5: Re-run focused operator queue tests and confirm they pass**

Run:

```bash
mkdir -p /tmp/go-build-cache /tmp/go-mod-cache && \
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
go test ./test/integration -run 'TestExecuteAIOperatorQueue(PrefersResumeManualFirstTargets|BuildsQueueFromPreviousFollowupSeed|BuildsQueueFromQueuedPreviousFollowupParams)' -count=1
```

Expected: `ok .../test/integration`

- [ ] **Step 6: Commit the operator queue gating change**

```bash
git add osmedeus-base/workflows/fragments/do-ai-operator-queue.yaml test/integration/workflow_test.go
git commit -m "feat: gate operator queue with resume context"
```

---

### Task 4: Teach campaign handoff to skip duplicate campaign creation and honor next-phase state

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-campaign-handoff.yaml`
- Test: `test/integration/workflow_test.go`

- [ ] **Step 1: Add `resumeContextFile` param**

```yaml
  - name: resumeContextFile
    default: "{{Output}}/ai-analysis/resume-context-{{TargetSpace}}.json"
```

- [ ] **Step 2: Read resume-context before legacy follow-up state**

Populate the previously existing campaign fields from resume first:

```bash
PREVIOUS_CAMPAIGN_CREATE_STATUS=$(jq -r '.campaign_create.status // .followup_summary.campaign_create_status // ""' "{{resumeContextFile}}" 2>/dev/null || echo "")
PREVIOUS_CAMPAIGN_CREATE_ID=$(jq -r '.campaign_create.campaign_id // .followup_summary.campaign_create_id // ""' "{{resumeContextFile}}" 2>/dev/null || echo "")
PREVIOUS_CAMPAIGN_CREATE_QUEUED_RUNS=$(jq -r '.campaign_create.queued_runs // .followup_summary.campaign_create_queued_runs // 0' "{{resumeContextFile}}" 2>/dev/null || echo 0)
PREVIOUS_NEXT_PHASE=$(jq -r '.next_phase // ""' "{{resumeContextFile}}" 2>/dev/null || echo "")
PREVIOUS_PRIORITY_MODE=$(jq -r '.priority_mode // ""' "{{resumeContextFile}}" 2>/dev/null || echo "")
```

- [ ] **Step 3: Add duplicate-campaign gate before `create-campaign-from-handoff` logic**

```bash
if [ "$PREVIOUS_CAMPAIGN_CREATE_STATUS" = "created" ] && [ -n "$PREVIOUS_CAMPAIGN_CREATE_ID" ]; then
  jq -n \
    --arg status "created" \
    --arg campaign_id "$PREVIOUS_CAMPAIGN_CREATE_ID" \
    --arg reason "resume_context_existing_campaign" \
    --arg workflow "{{campaignWorkflow}}" \
    --arg workflow_kind "{{campaignWorkflowKind}}" \
    --argjson queued_runs "$PREVIOUS_CAMPAIGN_CREATE_QUEUED_RUNS" \
    '{status:$status, campaign_id:$campaign_id, queued_runs:$queued_runs, reason:$reason, workflow:$workflow, workflow_kind:$workflow_kind}' \
    > "{{campaignCreateOutput}}"
  exit 0
fi
```

- [ ] **Step 4: Make handoff summary expose when resume gating drove the decision**

```bash
CAMPAIGN_HANDOFF_SOURCE_KIND="fresh-handoff"
if [ "$PREVIOUS_CAMPAIGN_CREATE_STATUS" = "created" ] && [ -n "$PREVIOUS_CAMPAIGN_CREATE_ID" ]; then
  CAMPAIGN_HANDOFF_SOURCE_KIND="resume-existing-campaign"
fi
```

Include it in the handoff JSON summary so smoke/tests can assert the gate.

- [ ] **Step 5: Re-run focused campaign tests and confirm they pass**

Run:

```bash
mkdir -p /tmp/go-build-cache /tmp/go-mod-cache && \
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
go test ./test/integration -run 'TestExecuteAICampaignHandoff(SkipsDuplicateCampaignCreateWhenResumeAlreadyCreated|BuildsCampaignFromPreviousFollowupSeed|BuildsCampaignFromQueuedPreviousFollowupParams)' -count=1
```

Expected: `ok .../test/integration`

- [ ] **Step 6: Commit the campaign gating change**

```bash
git add osmedeus-base/workflows/fragments/do-ai-campaign-handoff.yaml test/integration/workflow_test.go
git commit -m "feat: gate campaign handoff with resume context"
```

---

### Task 5: Lock one smoke assertion proving resume state changes downstream execution

**Files:**
- Modify: `test/regression/ai-workflow-smoke.sh`

- [ ] **Step 1: Add one gating assertion after existing artifact checks**

Use a concrete, stable downstream effect. Recommended: verify duplicate campaign creation is not reissued once campaign status is already created in resume flow.

```bash
RESUME_CONTEXT="$AI_DIR/resume-context-$WORKSPACE.json"
CAMPAIGN_CREATE="$AI_DIR/campaign-create-$WORKSPACE.json"

resume_campaign_status=$(jq -er '.campaign_create.status' "$RESUME_CONTEXT")
campaign_create_reason=$(jq -er '.reason // .status' "$CAMPAIGN_CREATE")

assert_eq "$resume_campaign_status" "created" "resume campaign status"
assert_contains "$campaign_create_reason" "created" "campaign create gating result"
```

If implementation chooses a dedicated reason like `resume_context_existing_campaign`, assert that exact value instead.

- [ ] **Step 2: Run focused verification**

Run:

```bash
mkdir -p /tmp/go-build-cache /tmp/go-mod-cache && \
GOCACHE=/tmp/go-build-cache GOMODCACHE=/tmp/go-mod-cache \
go test ./test/integration -run 'TestExecuteAIRetestPlanningSkipsDuplicateQueueWhenResumeQueueEffective|TestExecuteAIOperatorQueuePrefersResumeManualFirstTargets|TestExecuteAICampaignHandoffSkipsDuplicateCampaignCreateWhenResumeAlreadyCreated' -count=1

OSMEDEUS_BIN=/home/himan/Videos/ooss/build/bin/osmedeus \
bash test/regression/ai-workflow-smoke.sh
```

Expected:

```text
ok   .../test/integration  0.xxxs
ok ai workflow smoke regression: intelligent-analysis, follow-up packaging, queueing, and knowledge autolearn
```

- [ ] **Step 3: Re-run top-level workflow validation**

Run:

```bash
for flow in superdomain-extensive-ai-optimized.yaml superdomain-extensive-ai-stable.yaml superdomain-extensive-ai-hybrid.yaml superdomain-extensive-ai-lite.yaml; do
  /home/himan/Videos/ooss/build/bin/osmedeus \
    --base-folder /home/himan/Videos/ooss/.worktrees/superdomain-ai-resume-autopilot/osmedeus-base \
    workflow validate \
    "/home/himan/Videos/ooss/.worktrees/superdomain-ai-resume-autopilot/osmedeus-base/workflows/$flow"
done
```

Expected: all four flows pass lint/validation.

- [ ] **Step 4: Commit the smoke/verification pass**

```bash
git add test/regression/ai-workflow-smoke.sh test/integration/workflow_test.go
git commit -m "test: verify resume execution gating stays stable"
```

---

## Self-Review

### Spec coverage
- Resume state actively affects downstream execution: covered in Tasks 2-4
- No new orchestrator / keep fail-open: covered in Tasks 2-4
- Duplicate queue/campaign suppression: covered in Tasks 2 and 4
- Manual-first target shaping: covered in Tasks 2 and 3
- Stable regression protection: covered in Tasks 1 and 5

### Placeholder scan
- No `TODO` / `TBD`
- All modified files are explicit
- Validation commands are concrete

### Type consistency
- Shared state file name is consistent: `resume-context-{{TargetSpace}}.json`
- Shared param name is consistent: `resumeContextFile`
- Gating fields stay consistent with current contract: `next_phase`, `priority_mode`, `confidence_level`, `campaign_create.*`, `followup_summary.*`
