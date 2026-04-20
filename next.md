# Next

## 本轮上下文（2026-04-20）

### 已完成：optimized 的 AI 闭环稳定性收口

本轮重点加固了 `osmedeus-base/workflows/fragments/do-ai-post-followup-coordination.yaml`，目标是让 `superdomain-extensive-ai-optimized` 在 AI 结果缺失、主步骤异常、follow-up 产物不完整时仍然能够继续跑，并把可续跑、可接管的上下文稳定落盘。

### 本轮关键改动

1. `post-followup` 主逻辑失败时，直接补出真实产物，不再只写临时 fallback 再删除
   - 现在会稳定生成：
     - `followup-decision-*.json`
     - `followup-decision-*.md`
     - `resume-context-*.json`
     - `next-actions-*.json`
     - `operator-summary-*.md`

2. `resume-context` 现在会保留并向后续模块下传关键 follow-up 语义
   - `manual_followup_needed`
   - `campaign_followup_recommended`
   - `queue_followup_effective`

3. `queue_followup_effective` 会继承 `resume_suppressed_actions=["retest-queue"]` 的语义
   - 目的：避免下轮继续重复下发 retest queue

4. `resume-context` 补齐了更多可复用字段
   - 顶层保留：`manual_followup_needed / campaign_followup_recommended / queue_followup_effective`
   - `followup_summary` 中也同步补齐上述布尔语义
   - `seed_targets` 中补齐了 `focus_areas / priority_targets`

### 已验证结果

#### validate 通过
- `superdomain-extensive-ai-optimized`
- `superdomain-extensive-ai-stable`
- `superdomain-extensive-ai-hybrid`
- `do-ai-post-followup-coordination`

#### 轻量 smoke 通过
1. 空输入场景下：
   - 成功生成真实 `followup-decision/resume-context/next-actions/operator-summary` 产物

2. `resume_queue_already_effective` 场景下：
   - `followup-decision.seed_focus.queue_followup_effective = true`
   - `followup-decision.execution_feedback.queue_followup_effective = true`
   - `resume-context.queue_followup_effective = true`
   - `resume-context.followup_summary.queue_followup_effective = true`

### 当前结论

这块现在更稳：
- `post-followup` 就算主步骤失效，`knowledge-autolearn / resume / autopilot` 也不容易断链
- `resume-context` 对“queue 已经有效”的记忆不会再丢
- 这部分更适合续跑、人工接管和跨环境恢复

## 当前待继续事项

### 优先级 P1：继续压 `ai-retest-queue + ai-retest-planning`
目标：把“重复下发 / 续跑去重 / 已有效 queue 的抑制”继续做实证和收口。

重点检查：
1. `ai-retest-queue`
   - `resume-context` 存在且 `queue_followup_effective=true` 时，是否始终稳定跳过重复 queue
   - `retestTargetsFile`、`retestPlanOutput`、`previousFollowupDecisionFile`、`previous_followup_*` 参数之间的优先级是否一致
   - fallback target file 是否会导致噪音重复入队

2. `ai-retest-planning`
   - `manual-first / campaign-first / retest-first / knowledge-first` 对 target 分组是否稳定
   - 已经 queue 有效时，是否优先补“新证据”而不是重复下发旧目标
   - `previous_followup_*` 从 `resume-context / followup-decision / queue params` 三种来源进入时，语义是否一致

### 优先级 P2：继续压 operator / campaign / post-followup 之间的一致性
1. `ai-operator-queue`
   - `resume-context manual-first` 注入后，和原 `.tasks` / `.focus_targets` 的去重是否足够稳

2. `ai-campaign-handoff`
   - `campaignTargetsFile` 的来源集合是否过宽
   - 是否会把 `confirmedUrls / semanticPriority / previous_followup / retest / operator` 混得过噪

3. `ai-post-followup-coordination`
   - 三类输出：
     - `followup-decision`
     - `resume-context`
     - `next-actions`
   - 后续若继续增强，要始终保持三者语义一致，不要再次漂移

## 建议的下一步动作

1. 先做 `ai-retest-queue` 的双场景 smoke
   - 空队列场景
   - `resume_queue_already_effective` 场景

2. 再做 `ai-retest-planning` 的来源一致性检查
   - `resume-context`
   - `followup-decision`
   - `previous_followup_*` 参数

3. 最后再决定是否继续补 `operator/campaign` 的 target 收敛逻辑

## 备注
- 本轮 smoke 使用了 `--disable-db`，日志里的 DB warning 可忽略
- 当前实际工作仓库：`/home/shit/Videos/x/ooss`
- 当前分支：`superdomain-ai-resume-autopilot`
- 已推送到：`https://github.com/bmsh1t/ooss`
