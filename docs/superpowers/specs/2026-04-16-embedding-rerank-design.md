# Embedding + Rerank 检索增强设计

## 目标

为 Osmedeus 现有知识库与 AI 工作流补齐一条稳定、通用、可回退的双阶段检索链路：

1. **Embedding / Vector Recall**：负责从 `vector-kb.sqlite` 中快速召回候选；
2. **Rerank API**：负责对少量候选进行二次语义精排；
3. **Workflow / CLI / API 统一复用**：避免只在某一个工作流里做一次性接线。

目标优先级：

- **实战效果**：让 semantic search、attack-chain、path-planning 命中更准；
- **稳定性**：rerank 失败不阻断现有主链；
- **通用性**：未来新顶层 workflow 可直接复用；
- **效率**：只对少量候选 rerank，不做全量重排。

## 当前现状

项目已经具备这些能力：

- 独立向量库：`{{base_folder}}/knowledge/vector-kb.sqlite`
- embedding provider 配置：`knowledge_vector` + `embeddings_config`
- 向量检索入口：
  - CLI：`kb vector search`
  - API：`POST /knowledge/vector/search`
  - workflow：`do-ai-semantic-search.yaml` / `do-ai-semantic-search-hybrid.yaml`
- 当前排序方式：
  - `vectorkb.Search()` 内部使用 `vector_score + keyword_score + layer/scope/metadata boost`
  - workflow 侧再用 `jq/python` 做 hybrid merge / dedupe / score 排序

当前缺口：

- **没有独立的 `rerank_config`**
- **没有通用 rerank client**
- **没有把 rerank 作为平台能力接入 CLI/API/workflow**

## 设计原则

### 1. 双阶段检索，不推翻现有主链

保留当前：

- `kb vector search`
- `kb search`
- workflow 本地 hybrid merge

在此基础上新增：

- `rerank_config`
- `internal/rerank` provider 适配层
- rerank 可选接入点

### 2. Fail-open，不因 rerank 失效拖垮主流程

当出现以下情况时：

- rerank 未启用
- provider 未配置
- API 超时 / 429 / 5xx
- 返回格式异常

系统自动回退到**当前已有的 hybrid 排序结果**，继续执行 AI 主链。

### 3. 只 rerank 候选集，不 rerank 全库

默认策略：

- vector recall：20~40 条
- keyword recall：10~20 条
- merge + dedupe 后候选：最多 30~40 条
- rerank 输出最终 top_n：8~12 条

这样能在精度和时延之间保持稳定平衡。

### 4. rerank 独立于 embeddings 配置

`embeddings_config` 与 `rerank_config` 分离。

原因：

- embedding API 很多能兼容 `/embeddings`
- rerank API 各家请求/响应结构差异更大
- 强行复用 `llm_providers` 会导致配置语义混乱

因此应采用：

- `embeddings_config`：向量化
- `rerank_config`：精排

## 配置设计

新增配置块（按你当前可直接落地的 Tumuer Router 方案）：

```yaml
knowledge_vector:
  enabled: true
  db_path: "{{base_folder}}/knowledge/vector-kb.sqlite"
  default_provider: openai
  default_model: BAAI/bge-m3
  dimension: 1024

embeddings_config:
  enabled: true
  provider: openai
  openai:
    api_url: "https://router.tumuer.me/v1/embeddings"
    model: "BAAI/bge-m3"
    api_key: "${TUMUER_API_KEY}"

rerank_config:
  enabled: false
  provider: openai
  top_n: 10
  max_candidates: 40
  timeout: 15s
  min_score: 0.0

  openai:
    api_url: "https://router.tumuer.me/v1/rerank"
    model: "Pro/BAAI/bge-reranker-v2-m3"
    api_key: "${TUMUER_API_KEY}"
```

### 字段语义

- `enabled`：是否启用 rerank
- `provider`：默认 provider；第一版按 `openai` / OpenAI-compatible router 处理
- `top_n`：最终返回给上层的 rerank 结果数
- `max_candidates`：允许送入 rerank 的最大候选数
- `timeout`：单次 API 请求超时
- `min_score`：可选阈值，过滤低分结果

### Provider 首版支持

第一版优先支持：

- **OpenAI-compatible Router**

原因：

- 你当前环境已经给定：
  - base URL：`https://router.tumuer.me/v1`
  - embedding model：`BAAI/bge-m3`
  - rerank model：`Pro/BAAI/bge-reranker-v2-m3`
- 文档明确提供：
  - `POST /v1/embeddings`
  - `POST /v1/rerank`
- `BAAI/bge-m3` 为 **1024 维**
- 先支持通用 router，比先做 Jina/Cohere 专门适配更实战

## 核心数据流

### A. 平台级向量搜索 + rerank

调用链：

1. 用户调用 `kb vector search` 或 `/knowledge/vector/search`
2. `vectorkb.Search()` 先产出原始候选
3. 如果开启 rerank：
   - 截断到 `max_candidates`
   - 构造轻量 candidates
   - 调用 `internal/rerank` 进行精排
4. 返回：
   - reranked 结果
   - 如果失败则返回原始结果

### B. workflow 语义搜索链

调用链：

1. `do-ai-semantic-search*.yaml` 先做：
   - `kb vector search`
   - `kb search`
   - scan corpus merge
2. 对 merged candidates 做去重
3. 若启用 rerank：
   - 调 API 精排
   - 生成 `rerank-results-*.json`
4. 将最终结果继续喂给：
   - semantic analysis
   - attack chain
   - path planning
   - retest / decision follow-up

## 候选文档构造策略

送入 rerank 的每条候选只包含精简文本：

- `title`
- `section`
- `snippet`
- 必要 metadata（如 source/workspace/doc_type）

不直接发送整块长文。

推荐拼接格式：

```text
[title]
<title>

[section]
<section>

[snippet]
<snippet>

[meta]
workspace=<workspace>; source=<source>; doc_type=<doc_type>
```

这样做的目的：

- 降低 token 和延迟
- 控制 API 成本
- 保持排序语义足够清晰

## 平台改造点

### 1. 配置层

文件：

- `internal/config/config.go`
- `internal/config/config_test.go`
- `osmedeus-base/osm-settings.yaml`
- `public/presets/osm-settings.example.yaml`
- `public/examples/osmedeus-base.example/osm-settings.yaml`

新增内容：

- `RerankProviderConfig`
- `RerankConfig`
- `Config.Rerank`
- `ResolveRerankProvider()`
- `GetRerankTopN()`
- `GetRerankMaxCandidates()`
- `GetRerankTimeout()`

### 2. Rerank 执行层

新增目录：

- `internal/rerank/`

建议文件：

- `internal/rerank/models.go`
- `internal/rerank/client.go`
- `internal/rerank/openai.go`
- `internal/rerank/rerank_test.go`

职责：

- 统一输入输出结构
- OpenAI-compatible `/v1/rerank` 适配
- 超时与错误封装
- 分数标准化

### 3. CLI 层

文件：

- `pkg/cli/kb_vector.go`

新增参数：

- `--rerank`
- `--rerank-provider`
- `--rerank-model`
- `--rerank-top-n`
- `--rerank-max-candidates`

行为：

- 默认关闭，保证兼容
- 用户显式开启，或配置默认开启时才执行

### 4. API 层

文件：

- `pkg/server/handlers/knowledge.go`
- `pkg/server/handlers/knowledge_test.go`
- `docs/api/knowledge.mdx`
- `docs/api/README.mdx`

`KnowledgeVectorSearchRequest` 新增：

- `enable_rerank`
- `rerank_provider`
- `rerank_model`
- `rerank_top_n`
- `rerank_max_candidates`

返回中建议新增：

- `rerank_applied`
- `rerank_provider`
- `rerank_model`
- `ranking_source`（`hybrid` / `rerank` / `fallback_hybrid`）

### 5. Workflow 层

核心文件：

- `osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml`
- `osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml`

建议新增参数：

- `enableRerank`
- `rerankProvider`
- `rerankModel`
- `rerankTopN`
- `rerankMaxCandidates`
- `rerankResultsOutput`

行为：

- 在 merge-results 后插入 rerank step
- 产物写入 `ai-analysis/rerank-results-*.json`
- 下游优先消费 rerank 结果；无结果则消费现有 merge 结果

## 排序策略

### 阶段 1：召回排序

继续沿用现有逻辑：

- vector score
- keyword score
- layer boost
- scope boost
- metadata boost

### 阶段 2：rerank 精排

对召回候选做：

- query vs candidate text 的语义精排
- 返回新的分数与顺序

### 阶段 3：最终输出

最终结果结构保留兼容字段，同时补充：

- `base_relevance_score`
- `rerank_score`
- `ranking_source`

方便调试、报告、回归测试。

## 错误处理

### 回退规则

任一情况触发回退：

- rerank provider 未配置
- top_n / candidates 非法
- API 调用失败
- JSON 解析失败
- 返回为空

回退动作：

- 记录 warning
- 标记 `ranking_source=fallback_hybrid`
- 返回原本 merge/hybrid 排序结果

### 不应发生的行为

- 不因为 rerank 失败让 `kb vector search` 直接报错退出
- 不因为 rerank 异常让 optimized/stable/hybrid 主链中断

## 验证策略

### 单元测试

- `internal/config/config_test.go`
  - provider/model/timeout/top_n/max_candidates 解析
- `internal/rerank/rerank_test.go`
  - Jina/Cohere 请求构造
  - 响应解析
  - 错误回退

### API/CLI 回归

- `kb vector search` 不开 rerank：结果与当前兼容
- `kb vector search --rerank`：能返回 rerank 后顺序
- `/knowledge/vector/search`：验证 request/response 新字段

### workflow smoke

重点验证：

- `superdomain-extensive-ai-optimized`
- `superdomain-extensive-ai-stable`
- `superdomain-extensive-ai-hybrid`

验证点：

- rerank 开启时生成新产物
- rerank 失败时 fallback 正常
- 下游 AI step 能消费 rerank 结果

## 非目标

第一版不做：

- 本地 rerank 模型部署
- 多 provider 自动轮转
- cross-encoder 训练或自定义 fine-tune
- 面板化配置管理
- 对所有 AI fragment 一次性全面硬接

第一版重点只放在：

- `kb vector search`
- `/knowledge/vector/search`
- `do-ai-semantic-search*.yaml`

## 预期收益

实战上会直接改善：

- semantic search 命中排序
- KB 文档与 exploit playbook 的匹配
- attack-chain / path-planning 的上下文相关性
- 大型知识库引入后“召回到了但排不准”的问题

同时保持：

- 现有主链兼容
- 新建顶层 workflow 可直接复用
- 配置层清晰，不和 chat/embedding 混用

## 实施建议

按以下顺序推进最稳：

1. 配置层 + `internal/rerank`
2. `kb vector search` / API 接入 rerank
3. `do-ai-semantic-search.yaml`
4. `do-ai-semantic-search-hybrid.yaml`
5. 再决定是否把 rerank 结果继续下放到更多 AI fragment

这个顺序可以先把平台底座打稳，再让 optimized/stable/hybrid 无痛消费。
