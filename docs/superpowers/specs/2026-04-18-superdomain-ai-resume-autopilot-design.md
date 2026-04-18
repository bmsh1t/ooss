# Superdomain AI Resume / Autopilot State Design

**Date:** 2026-04-18

## Goal

在不重写当前 `superdomain-extensive-ai*` 主链、不新增单体 agent runtime 的前提下，为现有 AI workflow 增加一层统一的 **resume / autopilot state contract**，让流程更适合：

- 断点续跑
- 人工接管
- follow-up 结果反灌下一轮 pre-scan
- 在 `optimized` / `stable` / `hybrid` / `lite` 之间保持一致但分层消费

本设计只收口 **调度状态层**，不改变当前 workflow-first 架构。

## Current State

当前四条主线已经具备完整 AI 动作链：

- `superdomain-extensive-ai-optimized`
- `superdomain-extensive-ai-stable`
- `superdomain-extensive-ai-hybrid`
- `superdomain-extensive-ai-lite`

其中前三条基本都包含：

1. `ai-pre-scan-decision`
2. `ai-semantic-search`
3. `ai-vuln-validation`
4. `ai-attack-chain`
5. `ai-path-planning`
6. `ai-post-vuln-semantic-search`
7. `ai-intelligent-analysis`
8. `ai-apply-decision`
9. `ai-decision-semantic-search`
10. `ai-retest-planning`
11. `ai-operator-queue`
12. `ai-campaign-handoff`
13. `ai-targeted-rescan`
14. `ai-retest-queue`
15. `ai-post-followup-coordination`
16. `ai-knowledge-autolearn`

`lite` 保留了较轻的闭环：

- semantic search
- vuln validation
- intelligent analysis
- apply decision
- retest / operator / campaign / rescan
- post-followup coordination
- knowledge autolearn

当前问题不是缺少 AI step，而是缺少一个显式统一的 **调度状态视图**：

- 上一轮 follow-up 的有效结果仍主要散落在 `previous_followup_*` 参数里
- `followup-decision` 已存在，但更偏结果包，不是标准 resume state
- operator handoff 还没有稳定的统一 contract
- degraded / fallback / blocked 分支还没有被系统性压成下一轮输入

## Design Principles

### 1. 只补状态层，不重排主链

保持当前 workflow 编排和 fragment 角色不变：

- 不新增单体 orchestrator
- 不把主控从 workflow 挪给 agent
- 不把 `optimized/stable/hybrid` 重新拆成新的顶层链路

### 2. 统一 contract，分层消费

四条 workflow 共用同一套状态产物命名和字段语义，但消费深度不同：

- `optimized`：消费完整状态层
- `stable`：消费完整状态层，但执行 backend 更保守
- `hybrid`：消费完整状态层，后续最适合扩 policy
- `lite`：只消费瘦身字段

### 3. 优先读统一状态，再回退历史参数

当前已有的 `previous_followup_*` 参数先保留，兼容已有链路。

新规则：

1. 优先读取 `resume-context-{{TargetSpace}}.json`
2. 若不存在，再回退到 `previous_followup_*`

### 4. 状态层同时服务 AI 和人

统一状态层既要给下游 fragment/AI 用，也要给人工接管用。

因此设计为三类产物：

1. 机器可消费状态：`resume-context-*.json`
2. 机器/人工都可直接执行：`next-actions-*.json`
3. 人类快速阅读：`operator-summary-*.md`

## Shared Artifacts

### A. `resume-context-{{TargetSpace}}.json`

这是第一优先级的统一状态产物。

**主要产出者**

- `do-ai-post-followup-coordination.yaml`

**主要消费者**

- `do-ai-pre-scan-decision.yaml`
- `do-ai-pre-scan-decision-acp.yaml`

**建议字段**

```json
{
  "target": "example.com",
  "next_action": "continue_followup|resume_manual|refresh_semantic|refresh_scan",
  "recommended_targets": [],
  "focus_areas": [],
  "manual_first_targets": [],
  "high_confidence_targets": [],
  "combined_targets": [],
  "latest_scan_profile": "",
  "latest_severity": "",
  "reusable_sources": [],
  "degraded_stages": [],
  "fallback_modes": [],
  "blocked_followups": [],
  "safe_pivot": "",
  "campaign_status": {},
  "retest_status": {},
  "operator_queue_status": {},
  "summary_hint": ""
}
```

**字段语义**

- `next_action`：下一轮 pre-scan 最先考虑的动作
- `recommended_targets`：优先目标总表
- `focus_areas`：仍值得继续的高价值方向
- `manual_first_targets`：优先人工确认/利用的目标
- `high_confidence_targets`：自动 follow-up 高置信目标
- `combined_targets`：用于回退兼容的合并列表
- `degraded_stages`：本轮降级发生在哪些阶段
- `fallback_modes`：实际走过哪些 fail-open 路径
- `blocked_followups`：未执行或执行失败的 follow-up 分支
- `safe_pivot`：当前最适合转向的安全/稳定方向
- `summary_hint`：给人和 AI 都能快速消费的一句摘要

### B. `next-actions-{{TargetSpace}}.json`

比 `followup-decision` 更轻，比 `resume-context` 更直接。

**主要产出者**

- `do-ai-post-followup-coordination.yaml`

**定位**

- 给 operator
- 给二次自动化
- 给后续轻量恢复逻辑

**建议内容**

- top actions
- each action target list
- required inputs
- blocker/degraded note

### C. `operator-summary-{{TargetSpace}}.md`

给人工接管的最短摘要。

**主要产出者**

- `do-ai-post-followup-coordination.yaml`

**建议内容**

- 当前最重要结论
- 最优先目标/方向
- 哪些 follow-up 已完成
- 哪些阶段 degraded
- 下一步建议先做什么

## Workflow Tier Mapping

### `superdomain-extensive-ai-optimized`

这是完整状态层的主消费方。

#### 保持不变

- 现有主链顺序不变
- semantic / validation / attack-chain / path-planning / follow-up 闭环不变

#### 需要增加的契约行为

- `ai-post-followup-coordination` 统一产出：
  - `followup-decision`
  - `resume-context`
  - `next-actions`
  - `operator-summary`
- `ai-pre-scan-decision` 启动时优先读取 `resume-context`

#### 作用

让 `optimized` 真正具备类似 `ccc` 的：

- autopilot bootstrap
- resume handoff
- follow-up feedback loop

### `superdomain-extensive-ai-stable`

与 `optimized` 使用同一套状态 contract。

#### 保持不变

- 继续使用 ACP 版本的 pre-scan / validation / attack-chain / path-planning

#### 需要增加的契约行为

- `do-ai-pre-scan-decision-acp.yaml` 同样优先读取 `resume-context`
- `do-ai-post-followup-coordination.yaml` 的输出 contract 与 `optimized` 保持一致

#### 作用

`stable` 的差异应继续体现在执行 backend，而不是重新发明一套 resume 机制。

### `superdomain-extensive-ai-hybrid`

`hybrid` 当前已经部分体现“调度策略”和“执行后端”分离。

#### 保持不变

- 继续使用混合 backend 组合

#### 第一版只增加

- 与 `optimized/stable` 同一套状态产物
- 不单独引入新的 policy engine

#### 作用

第一版先 contract 对齐；后续如果要从 `ccc` 借更多 checkpoint/policy 语义，`hybrid` 是最合适的承载点。

### `superdomain-extensive-ai-lite`

`lite` 只消费瘦身版状态层，不应被重新做重。

#### 只建议消费的字段

- `next_action`
- `recommended_targets`
- `focus_areas`
- `summary_hint`

#### 不建议第一版强塞

- 完整 `degraded_stages` 细表
- 复杂 campaign / retest 状态结构
- 复杂 safe pivot / blocked follow-up 细节

#### 作用

保留 `lite` 的轻闭环定位，同时具备基本 resume 能力。

## Fragment-Level Changes

### 1. `do-ai-post-followup-coordination.yaml`

这是第一版的主收口点。

#### 新职责

在已有 `followup-decision` 之外，再统一生成：

- `resume-context-{{TargetSpace}}.json`
- `next-actions-{{TargetSpace}}.json`
- `operator-summary-{{TargetSpace}}.md`

#### 输入来源

- `ai-apply-decision`
- `ai-decision-semantic-search`
- `ai-retest-planning`
- `ai-operator-queue`
- `ai-campaign-handoff`
- `ai-targeted-rescan`
- `ai-retest-queue`
- 现有 semantic priority target artifacts

#### 输出要求

- 缺少部分 follow-up 产物时仍产出空但有效的状态
- degraded / fallback 信息要明确落字段
- 不要求所有 branch 都执行成功才写 handoff

### 2. `do-ai-pre-scan-decision.yaml`

#### 新职责

启动时优先读取：

- `resume-context-{{TargetSpace}}.json`

若不存在，再回退：

- `previous_followup_*`

#### 新行为

优先使用统一字段：

- `recommended_targets`
- `focus_areas`
- `manual_first_targets`
- `high_confidence_targets`
- `next_action`
- `safe_pivot`

### 3. `do-ai-pre-scan-decision-acp.yaml`

与非 ACP 版本行为对齐。

第一版不要求它理解所有复杂字段，但至少需要优先消费：

- `next_action`
- `recommended_targets`
- `focus_areas`
- `summary_hint`

### 4. `do-ai-apply-decision.yaml`

不改变其主职责，但建议继续保证：

- 最终 applied decision 对 follow-up 来源可追踪
- 能为 `post-followup-coordination` 提供稳定来源字段

### 5. `do-ai-intelligent-analysis.yaml`

不新增主职责，但建议继续保证：

- semantic / knowledge / vector 来源字段不丢失
- 为后续 `resume-context` 汇总提供稳定输入

## Compatibility Strategy

### 保持兼容的部分

- 所有现有 `followupDecisionOutput` 路径
- 所有 `previous_followup_*` 参数
- 当前主链 module 顺序
- 当前 `optimized/stable/hybrid/lite` 的角色定位

### 新旧优先级

1. `resume-context`
2. `followup-decision`
3. `previous_followup_*`

这样可以逐步迁移，不需要一次性推翻历史参数接口。

## Relation to `ccc`

本设计借的是 `ccc` 的调度思想，而不是运行时形态：

### 借用的点

- bootstrap context
- resume summary
- autopilot state 汇总视图
- session summary / handoff

### 不借用的点

- 单体 `agent.py` runtime
- file-first memory 架构
- agent-first orchestration

原因是当前 Osmedeus 的 `superdomain-extensive-ai*` 已经是成熟的 workflow-first 架构，最适合补 contract，而不是换主控方式。

## Verification

第一版验证只需要覆盖 contract，不做重扫描验证。

### 静态/轻量验证

- workflow validate:
  - `superdomain-extensive-ai-optimized`
  - `superdomain-extensive-ai-stable`
  - `superdomain-extensive-ai-hybrid`
  - `superdomain-extensive-ai-lite`
- focused regression:
  - `ai-workflow-smoke`
  - 需要新增/调整的轻量 artifact checks

### 应重点验证的行为

1. `post-followup-coordination` 在部分 branch 缺失时仍产出有效 `resume-context`
2. `pre-scan-decision*` 优先消费 `resume-context`
3. `resume-context` 缺失时仍能回退到 `previous_followup_*`
4. `lite` 只消费瘦身字段，不被迫依赖完整状态结构

## Non-Goals

- 不新增新的顶层 workflow
- 不引入新的单体 orchestrator / autopilot runtime
- 不把当前 workflow-first 改成 agent-first
- 不在第一版实现复杂 checkpoint mode
- 不在第一版重做 memory 存储体系

## Expected Outcome

完成后，当前 `superdomain-extensive-ai*` 将获得一层统一的调度状态收口：

- `optimized/stable/hybrid`：具备完整的 resume / autopilot handoff 能力
- `lite`：具备最小 resume handoff 能力
- follow-up 结果能更稳定地反灌下一轮 pre-scan
- operator 接管点更清晰
- 不破坏当前主链与现有运行时架构
