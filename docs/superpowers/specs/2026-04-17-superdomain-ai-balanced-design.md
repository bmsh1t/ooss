# Superdomain AI 平衡增强设计

**Date:** 2026-04-17

## Goal

为 `superdomain-extensive-ai` 系列工作流制定第一版“可实战、可续跑、可降级”的统一增强方案，优先把 `optimized` 压成默认主跑版本，同时保证：

- AI 增强主链命中率，而不是增加操作负担；
- `security-kb` 这类基座知识库能稳定进入 semantic / rerank / decision 主链；
- 新顶层 workflow 可以复用同一套增强，不把逻辑写死在单一入口；
- provider、vectorkb、ACP 任意一层失效时，流程都能继续跑。

## 验收维度

本轮设计以 4 个维度为主：

1. **主链稳定**：`optimized` 默认主跑不脆，关键 AI 模块都有明确前置、降级和输出。
2. **结果有用**：AI 输出可以直接驱动 `rescan / retest / operator / campaign`。
3. **接管顺手**：中断后知道从哪继续，人工知道先看什么。
4. **知识可复用**：`security-kb` 等基座库长期复用，不污染目标库。

## Current State

当前仓库已经有这些基础：

- 顶层流：
  - `osmedeus-base/workflows/superdomain-extensive-ai-optimized.yaml`
  - `osmedeus-base/workflows/superdomain-extensive-ai-stable.yaml`
  - `osmedeus-base/workflows/superdomain-extensive-ai-hybrid.yaml`
- 关键 AI fragment 已存在：
  - `do-ai-pre-scan-decision.yaml`
  - `do-ai-semantic-search.yaml`
  - `do-ai-intelligent-analysis.yaml`
  - `do-ai-apply-decision.yaml`
  - `do-ai-post-followup-coordination.yaml`
  - `do-ai-retest-planning.yaml`
  - `do-ai-operator-queue.yaml`
  - `do-ai-campaign-handoff.yaml`
  - `do-ai-knowledge-autolearn.yaml`
- 独立 vectorkb、embedding、rerank、KB import 主线已接入平台。

当前主要问题不是“没有功能”，而是“功能已经很多，但默认主跑链还需要收口”：

- `optimized` 功能强，但主链状态、人工接管点、降级语义还不够统一；
- semantic / rerank / learned knowledge / base KB 虽已接线，但还缺“主链消费优先级”和“失败时的稳定退路”；
- follow-up 结果虽然能生成，但反灌到下一轮 pre-scan / decision 的操盘体验还不够稳定；
- 顶层 workflow 之间存在复制式接线，未来新建顶层流时容易漏增强点。

## Design Principles

### 1. `optimized` 是默认主力版，但不能变成脆皮版

`optimized` 保留最完整闭环：

- pre-scan
- semantic / rerank
- vuln validation
- attack-chain / path-planning
- intelligent-analysis
- apply-decision
- retest / operator / campaign
- post-followup coordination
- knowledge autolearn

但它必须满足：

- 关键前置失败时自动降级；
- 输出文件统一、稳定、可追踪；
- 新增能力默认进入主链，而不是挂在旁路上“看起来高级”。

### 2. 自动推进更强，但人工接管必须顺手

默认行为：

- 尽量自动推进；
- 尽量直接生成可执行 follow-up；
- 尽量少让操作员自己拼上下文。

同时增加统一接管点：

- 每个关键阶段输出固定 summary / decision / next-actions 文件；
- 中断恢复优先读取这些固定文件，而不是要求人重新读全量日志；
- follow-up 结果反灌下一轮 pre-scan，使“断点续跑”有连续性。

### 3. 知识库必须进入主链消费，而不是只停留在导入层

知识分层保持：

- `knowledgeWorkspace`: 目标知识层
- `sharedKnowledgeWorkspace`: 共享经验层
- `globalKnowledgeWorkspace`: 基座知识层（如 `security-kb`）

主链消费顺序定为：

1. 当前目标层
2. 共享经验层
3. 基座层
4. 本次扫描原始 corpus

AI 语义检索要优先消费这一分层结果，再进入 rerank / decision / follow-up，而不是把基座 KB 当成“有就查一下”的旁支能力。

### 4. 顶层兼容优先，增强写在 fragment / param contract，不写死在 workflow 名字里

增强原则：

- 参数 contract 尽量在 fragment 层收敛；
- 顶层 workflow 只负责：
  - 开关
  - 依赖顺序
  - 输出路径
  - 级别差异

这样像 `domain-superdomain-extensive-ai.yaml` 这类新的顶层流，只要沿用同一套 fragment contract，就能复用增强能力。

### 5. 全链 fail-open

降级优先级：

- ACP 不可用 → 回退已有 agent / 非 ACP 分支
- rerank 不可用 → 保留 semantic + keyword hybrid 结果
- vectorkb 未准备好 → 保留 keyword / scan corpus / learned summary
- KB 未准备好 → 仍能跑扫描和基础 AI 决策
- intelligent-analysis 不可用 → `apply-decision` 继续消费已有结构化输入

主链可以降级，但不能直接断。

## Target Architecture

### A. 主链骨架

统一主链：

1. `ai-pre-scan-decision`
2. `ai-semantic-search`
3. `ai-vuln-validation`
4. `ai-attack-chain`
5. `ai-path-planning`
6. `ai-intelligent-analysis`
7. `ai-apply-decision`
8. `ai-decision-semantic-search`
9. `ai-retest-planning` / `ai-operator-queue` / `ai-campaign-handoff`
10. `ai-targeted-rescan`
11. `ai-post-followup-coordination`
12. `ai-knowledge-autolearn`

### B. 三层输出

每个关键阶段统一产出三类文件：

1. **machine output**
   - JSON 决策结果
   - 给下游模块消费
2. **operator summary**
   - markdown / txt 摘要
   - 给人工快速看
3. **resume hints**
   - next-actions / focus-targets / resume pointers
   - 给断点续跑和下一轮 pre-scan 使用

### C. 三层知识输入

`do-ai-semantic-search*.yaml` 统一收敛：

- layered keyword hits
- layered vector hits
- optional rerank result
- scan-data fallback
- semantic-highlights / priority-target extraction

其最终产物不是“检索结果文件而已”，而是后续：

- intelligent-analysis
- apply-decision
- retest planning
- campaign handoff
- post-followup coordination

共同消费的标准输入。

## Workflow Tiering

### optimized

定位：

- 默认主跑
- 实战优先
- 功能最全，但必须稳

要求：

- 默认启用 semantic / decision / follow-up / knowledge learn
- 降级清晰
- summary / resume 文件最完整

### stable

定位：

- 更保守的 ACP/AI 路线
- 保留成熟闭环

要求：

- 只回灌低风险、验证过的能力
- 保留与 optimized 同一套输出 contract，避免分叉

### hybrid

定位：

- 平衡深度、成本、稳定性

要求：

- 保留较强分析能力
- 不新增专属 contract
- 继续复用 optimized/stable 同一套 fragment 语义

## Planned Enhancements

### 1. 统一 AI stage contract

为以下 fragment 收敛统一输入/输出约定：

- `do-ai-pre-scan-decision.yaml`
- `do-ai-semantic-search.yaml`
- `do-ai-intelligent-analysis.yaml`
- `do-ai-apply-decision.yaml`
- `do-ai-post-followup-coordination.yaml`

重点统一：

- summary 文件命名
- next-actions 文件命名
- priority targets / focus areas 文件命名
- “是否成功、是否降级、降级原因”的状态字段

### 2. 把 semantic / rerank / KB 变成决策主输入

不是单纯“产出检索文件”，而是要求：

- intelligent-analysis 明确消费 semantic highlights
- apply-decision 明确保留 semantic / KB / follow-up 来源字段
- retest / operator / campaign 默认使用 decision-followup 语义检索结果

### 3. 增加 post-followup 协调层的反灌价值

`do-ai-post-followup-coordination.yaml` 负责沉淀：

- 哪些 follow-up 有效果
- 哪些 priority target 仍值得继续
- 哪些区域建议下轮 pre-scan 提前关注
- 哪些 campaign / retest 可以直接继承

并把这些结果标准化给下一轮 `do-ai-pre-scan-decision.yaml` 读取。

### 4. 增加 operator-first resume view

统一生成：

- `operator-summary-*.md`
- `next-actions-*.json`
- `resume-context-*.json`

让人工接手时先看固定 2~3 个文件，而不是翻工作目录。

### 5. 对新顶层 workflow 提供低耦合复用方式

不增加“只能给 optimized 用”的隐藏逻辑。

复用方式应基于：

- fragment 级默认参数
- 顶层 workflow 明确注入开关
- 缺省值对未声明参数保持兼容

## Rollout Strategy

### Phase 1

先压实 `optimized`：

- 主链 contract
- semantic / decision / follow-up 闭环
- summary / resume 输出
- fail-open 语义

### Phase 2

把成熟 contract 回灌到 `stable`：

- 只带入验证过的能力
- 不把高脆弱度逻辑强行塞进去

### Phase 3

同步 `hybrid`：

- 保持分析深度
- 继续复用同一 contract

### Phase 4

验证派生顶层 workflow：

- 至少保证新的顶层流不会因缺少某个细粒度参数而掉主链

## Non-Goals

本轮不做：

- 大规模重写 agent 内核
- 引入新的图编排框架
- 新建重面板 UI
- 让所有 AI 步骤都强制依赖 ACP
- 为单一顶层 workflow 写专属私有逻辑

## Success Criteria

满足以下条件即认为第一版设计成功：

1. `optimized` 能作为默认主跑版本稳定工作；
2. semantic / rerank / KB 进入 decision 主链，而不是旁路；
3. `retest / operator / campaign / post-followup` 能形成实用闭环；
4. 人工接手时有固定 summary / resume 文件可看；
5. 新顶层 workflow 复用同一套 fragment contract 时，不需要额外补很多私有逻辑；
6. provider / KB / ACP 任一层异常时，工作流仍能以降级模式继续。
