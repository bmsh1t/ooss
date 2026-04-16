# Embedding + Rerank 检索增强 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Osmedeus 增加基于 API 的 embedding + rerank 双阶段检索能力，并让 CLI / API / AI semantic workflow 统一复用，同时保证 rerank 失败时自动回退到现有 hybrid 排序。

**Architecture:** 保留现有 `vector-kb.sqlite` 与 `kb vector search` 主链，在配置层新增 `rerank_config`，并优先适配你当前的 Tumuer OpenAI-compatible Router：`https://router.tumuer.me/v1/embeddings` + `BAAI/bge-m3`（1024维）以及 `https://router.tumuer.me/v1/rerank` + `Pro/BAAI/bge-reranker-v2-m3`。所有上层入口都走同一套 rerank client，并统一输出 `ranking_source` / `rerank_score` 等调试字段。

**Tech Stack:** Go, Cobra CLI, Fiber API, SQLite vectorkb, YAML workflows, jq/bash regression harness

---

## File Map

### Create

- `internal/rerank/models.go`
- `internal/rerank/client.go`
- `internal/rerank/openai.go`
- `internal/rerank/client_test.go`
- `internal/vectorkb/search_rerank_test.go`
- `docs/superpowers/specs/2026-04-16-embedding-rerank-design.md`（已存在，仅引用）

### Modify

- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/vectorkb/models.go`
- `internal/vectorkb/search.go`
- `pkg/cli/kb_vector.go`
- `pkg/cli/kb_vector_test.go`
- `pkg/server/handlers/knowledge.go`
- `pkg/server/handlers/knowledge_test.go`
- `osmedeus-base/osm-settings.yaml`
- `public/presets/osm-settings.example.yaml`
- `public/examples/osmedeus-base.example/osm-settings.yaml`
- `osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml`
- `osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml`
- `test/regression/ai-semantic-vector-smoke.sh`
- `README.md`
- `docs/api/knowledge.mdx`
- `docs/api/README.mdx`

---

### Task 1: 补齐 `rerank_config` 配置与解析辅助函数

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `osmedeus-base/osm-settings.yaml`
- Modify: `public/presets/osm-settings.example.yaml`
- Modify: `public/examples/osmedeus-base.example/osm-settings.yaml`

- [ ] **Step 1: 先写配置解析测试**

在 `internal/config/config_test.go` 追加两个测试，先锁定行为：

```go
func TestResolveRerankProviderFromDedicatedConfig(t *testing.T) {
	cfg := &Config{
		Rerank: RerankConfig{
			Enabled: boolPtr(true),
			Provider: "openai",
			TopN: 10,
			MaxCandidates: 40,
			Timeout: "15s",
			OpenAI: RerankProviderConfig{
				APIURL: "https://router.tumuer.me/v1/rerank",
				Model:  "Pro/BAAI/bge-reranker-v2-m3",
				APIKey: "test-key",
			},
		},
	}

	provider, err := cfg.ResolveRerankProvider("")
	require.NoError(t, err)
	require.Equal(t, "openai", provider.Provider)
	require.Equal(t, "https://router.tumuer.me/v1/rerank", provider.BaseURL)
	require.Equal(t, "Pro/BAAI/bge-reranker-v2-m3", provider.Model)
	require.Equal(t, 10, cfg.GetRerankTopN())
	require.Equal(t, 40, cfg.GetRerankMaxCandidates())
	require.Equal(t, 15*time.Second, cfg.GetRerankTimeout())
}

func TestResolveRerankProviderRejectsUnknownProvider(t *testing.T) {
	cfg := &Config{
		Rerank: RerankConfig{
			Enabled:  boolPtr(true),
			Provider: "unknown",
		},
	}

	_, err := cfg.ResolveRerankProvider("")
	require.Error(t, err)
	require.Contains(t, err.Error(), "rerank provider")
}
```

- [ ] **Step 2: 跑配置测试，确认先失败**

Run:

```bash
go test ./internal/config -run 'TestResolveRerankProviderFromDedicatedConfig|TestResolveRerankProviderRejectsUnknownProvider' -v
```

Expected:

- FAIL
- 报 `RerankConfig`、`ResolveRerankProvider`、`GetRerankTopN` 等符号不存在

- [ ] **Step 3: 在配置结构中补类型和 helper**

在 `internal/config/config.go` 增加结构与 helper。核心代码按下面形状实现：

```go
type RerankProviderConfig struct {
	APIURL string `yaml:"api_url"`
	Model  string `yaml:"model"`
	APIKey string `yaml:"api_key"`
}

type RerankConfig struct {
	Enabled       *bool                `yaml:"enabled,omitempty"`
	Provider      string               `yaml:"provider"`
	TopN          int                  `yaml:"top_n"`
	MaxCandidates int                  `yaml:"max_candidates"`
	Timeout       string               `yaml:"timeout"`
	MinScore      float64              `yaml:"min_score"`
	OpenAI        RerankProviderConfig `yaml:"openai"`
}

type Config struct {
	// ...
	Rerank RerankConfig `yaml:"rerank_config"`
}

func (c *Config) IsRerankEnabled() bool {
	if c == nil || c.Rerank.Enabled == nil {
		return false
	}
	return *c.Rerank.Enabled
}

func (c *Config) GetRerankTopN() int {
	if c.Rerank.TopN <= 0 {
		return 10
	}
	return c.Rerank.TopN
}

func (c *Config) GetRerankMaxCandidates() int {
	if c.Rerank.MaxCandidates <= 0 {
		return 40
	}
	return c.Rerank.MaxCandidates
}

func (c *Config) GetRerankTimeout() time.Duration {
	if parsed, err := time.ParseDuration(strings.TrimSpace(c.Rerank.Timeout)); err == nil && parsed > 0 {
		return parsed
	}
	return 15 * time.Second
}

func (c *Config) ResolveRerankProvider(providerName string) (*LLMProvider, error) {
	name := strings.TrimSpace(providerName)
	if name == "" {
		name = strings.TrimSpace(c.Rerank.Provider)
	}
	if name == "" {
		return nil, fmt.Errorf("rerank provider is required")
	}

	var provider *RerankProviderConfig
	switch strings.ToLower(name) {
	case "openai":
		provider = &c.Rerank.OpenAI
	default:
		return nil, fmt.Errorf("rerank provider %q is not supported", name)
	}

	if strings.TrimSpace(provider.APIURL) == "" {
		return nil, fmt.Errorf("rerank provider %q is not configured", name)
	}

	return &LLMProvider{
		Provider:  name,
		BaseURL:   strings.TrimSpace(provider.APIURL),
		AuthToken: strings.TrimSpace(provider.APIKey),
		Model:     strings.TrimSpace(provider.Model),
	}, nil
}
```

- [ ] **Step 4: 给配置样例补默认块**

在三个 settings 文件里补这一段：

```yaml
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

- [ ] **Step 5: 再跑配置测试确认通过**

Run:

```bash
go test ./internal/config -run 'TestResolveRerankProviderFromDedicatedConfig|TestResolveRerankProviderRejectsUnknownProvider' -v
```

Expected:

- PASS

- [ ] **Step 6: 提交该任务**

```bash
git add internal/config/config.go internal/config/config_test.go osmedeus-base/osm-settings.yaml public/presets/osm-settings.example.yaml public/examples/osmedeus-base.example/osm-settings.yaml
git commit -m "feat(rerank): 增加重排配置解析"
```

---

### Task 2: 新建 `internal/rerank` 通用 client 与 OpenAI-compatible 适配器

**Files:**
- Create: `internal/rerank/models.go`
- Create: `internal/rerank/client.go`
- Create: `internal/rerank/openai.go`
- Create: `internal/rerank/client_test.go`

- [ ] **Step 1: 先写 provider 级单元测试**

在 `internal/rerank/client_test.go` 先写三个测试：

```go
func TestClientRerankOpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/rerank", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 1, "relevance_score": 0.98},
				{"index": 0, "relevance_score": 0.72},
			},
		})
	}))
	defer server.Close()

	client := NewClient(&config.LLMProvider{
		Provider:  "openai",
		BaseURL:   server.URL + "/v1/rerank",
		AuthToken: "test-key",
		Model:     "Pro/BAAI/bge-reranker-v2-m3",
	}, 10*time.Second)

	results, err := client.Rerank(context.Background(), Request{
		Query: "token confusion admin panel preview route",
		Documents: []Document{
			{ID: "a", Text: "weak keyword hit"},
			{ID: "b", Text: "token confusion admin panel preview route exploit notes"},
		},
		TopN: 2,
	})
	require.NoError(t, err)
	require.Len(t, results.Results, 2)
	require.Equal(t, "b", results.Results[0].ID)
	require.Equal(t, 0.98, results.Results[0].Score)
}

func TestClientRerankRejectsUnsupportedProvider(t *testing.T) {
	client := NewClient(&config.LLMProvider{Provider: "noop"}, 5*time.Second)
	_, err := client.Rerank(context.Background(), Request{
		Query: "x",
		Documents: []Document{{ID: "1", Text: "x"}},
		TopN: 1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported rerank provider")
}
```

- [ ] **Step 2: 跑新包测试，确认先失败**

Run:

```bash
go test ./internal/rerank -run 'TestClientRerankOpenAICompatible|TestClientRerankRejectsUnsupportedProvider' -v
```

Expected:

- FAIL
- 报 `internal/rerank` 包和 `NewClient` 等不存在

- [ ] **Step 3: 实现统一 request/response 模型**

在 `internal/rerank/models.go` 写：

```go
package rerank

type Document struct {
	ID       string
	Text     string
	Metadata map[string]string
}

type Request struct {
	Query         string
	Documents     []Document
	TopN          int
	MaxCandidates int
	MinScore      float64
	ModelOverride string
}

type Result struct {
	ID       string             `json:"id"`
	Index    int                `json:"index"`
	Score    float64            `json:"score"`
	Text     string             `json:"text"`
	Metadata map[string]string  `json:"metadata,omitempty"`
}

type Response struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Results  []Result `json:"results"`
}
```

- [ ] **Step 4: 实现 client 与 provider adapter**

在 `internal/rerank/client.go` 写统一入口：

```go
type Client struct {
	provider *config.LLMProvider
	http     *http.Client
}

func NewClient(provider *config.LLMProvider, timeout time.Duration) *Client {
	return &Client{
		provider: provider,
		http:     &http.Client{Timeout: timeout},
	}
}

func (c *Client) Rerank(ctx context.Context, req Request) (*Response, error) {
	switch strings.ToLower(strings.TrimSpace(c.provider.Provider)) {
	case "openai":
		return c.rerankOpenAI(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported rerank provider %q", c.provider.Provider)
	}
}
```

在 `internal/rerank/openai.go` 中按 router 文档实现 `/v1/rerank`：

```go
type openaiRerankRequest struct {
	Model    string   `json:"model"`
	Query    string   `json:"query"`
	Documents []string `json:"documents"`
	TopN     int      `json:"top_n,omitempty"`
	ReturnDocuments bool `json:"return_documents,omitempty"`
}

type openaiRerankResponse struct {
	ID string `json:"id"`
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}
```

要求：

- 始终保留原始文档 index
- 对返回结果按 score 降序整理
- 对 `MinScore` 做过滤
- 请求地址使用 `https://router.tumuer.me/v1/rerank`
- 模型默认使用 `Pro/BAAI/bge-reranker-v2-m3`

- [ ] **Step 5: 跑 rerank 单元测试确认通过**

Run:

```bash
go test ./internal/rerank -run 'TestClientRerankOpenAICompatible|TestClientRerankRejectsUnsupportedProvider' -v
```

Expected:

- PASS

- [ ] **Step 6: 提交该任务**

```bash
git add internal/rerank/models.go internal/rerank/client.go internal/rerank/openai.go internal/rerank/client_test.go
git commit -m "feat(rerank): 增加统一重排客户端"
```

---

### Task 3: 把 rerank 接进 `vectorkb.Search()` 与 CLI

**Files:**
- Modify: `internal/vectorkb/models.go`
- Modify: `internal/vectorkb/search.go`
- Create: `internal/vectorkb/search_rerank_test.go`
- Modify: `pkg/cli/kb_vector.go`
- Modify: `pkg/cli/kb_vector_test.go`

- [ ] **Step 1: 先写 vectorkb rerank 测试**

在 `internal/vectorkb/search_rerank_test.go` 先锁定 rerank 后结果顺序与 fallback 行为：

```go
func TestSearchAppliesRerankWhenEnabled(t *testing.T) {
	cfg := setupVectorKBTestConfig(t)
	cfg.Rerank.Enabled = boolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: mockRerankServer(t, []float64{0.21, 0.99}),
		Model:  "Pro/BAAI/bge-reranker-v2-m3",
		APIKey: "test-key",
	}

	results, err := Search(context.Background(), cfg, SearchOptions{
		Workspace:    "acme",
		Limit:        2,
		EnableRerank: true,
	}, "token confusion admin panel preview route")
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "rerank", results[0].RankingSource)
	require.Greater(t, results[0].RerankScore, results[1].RerankScore)
}

func TestSearchFallsBackWhenRerankFails(t *testing.T) {
	cfg := setupVectorKBTestConfig(t)
	cfg.Rerank.Enabled = boolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: "http://127.0.0.1:1/v1/rerank",
		Model:  "Pro/BAAI/bge-reranker-v2-m3",
	}

	results, err := Search(context.Background(), cfg, SearchOptions{
		Workspace:    "acme",
		Limit:        2,
		EnableRerank: true,
	}, "token confusion admin panel preview route")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "fallback_hybrid", results[0].RankingSource)
}
```

- [ ] **Step 2: 跑 vectorkb 测试，确认先失败**

Run:

```bash
go test ./internal/vectorkb -run 'TestSearchAppliesRerankWhenEnabled|TestSearchFallsBackWhenRerankFails' -v
```

Expected:

- FAIL
- 报 `EnableRerank`、`RankingSource`、`RerankScore` 等不存在

- [ ] **Step 3: 扩展 SearchOptions 与 SearchHit**

在 `internal/vectorkb/models.go` 补字段：

```go
type SearchOptions struct {
	Workspace           string
	WorkspaceLayers     []string
	ScopeLayers         []string
	Provider            string
	Model               string
	Limit               int
	HybridWeight        float64
	KeywordWeight       float64
	MinSourceConfidence float64
	SampleTypes         []string
	ExcludeSampleTypes  []string
	EnableRerank        bool
	RerankProvider      string
	RerankModel         string
	RerankTopN          int
	RerankMaxCandidates int
}

type SearchHit struct {
	// existing fields...
	BaseRelevanceScore float64 `json:"base_relevance_score,omitempty"`
	RerankScore        float64 `json:"rerank_score,omitempty"`
	RankingSource      string  `json:"ranking_source,omitempty"`
}
```

- [ ] **Step 4: 在 `Search()` 内接入 rerank**

在 `internal/vectorkb/search.go` 中按这个顺序实现：

```go
for i := range results {
	results[i].BaseRelevanceScore = results[i].RelevanceScore
	results[i].RankingSource = "hybrid"
}

if opts.EnableRerank && cfg.IsRerankEnabled() && len(results) > 0 {
	reranked, err := applyRerank(ctx, cfg, opts, query, results)
	if err == nil && len(reranked) > 0 {
		results = reranked
	} else {
		for i := range results {
			results[i].RankingSource = "fallback_hybrid"
		}
	}
}
```

新增私有 helper：

```go
func applyRerank(ctx context.Context, cfg *config.Config, opts SearchOptions, query string, hits []SearchHit) ([]SearchHit, error) {
	providerName := strings.TrimSpace(opts.RerankProvider)
	provider, err := cfg.ResolveRerankProvider(providerName)
	if err != nil {
		return nil, err
	}

	client := rerank.NewClient(provider, cfg.GetRerankTimeout())
	limit := opts.RerankMaxCandidates
	if limit <= 0 {
		limit = cfg.GetRerankMaxCandidates()
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}

	docs := make([]rerank.Document, 0, len(hits))
	for _, hit := range hits {
		docs = append(docs, rerank.Document{
			ID: hit.SourcePath + "#" + strconv.FormatInt(hit.ChunkID, 10),
			Text: strings.TrimSpace(strings.Join([]string{
				"[title]", hit.Title,
				"[section]", hit.Section,
				"[snippet]", hit.Snippet,
				"[meta]", "workspace=" + hit.Workspace + "; doc_type=" + hit.DocType,
			}, "\n")),
		})
	}

	resp, err := client.Rerank(ctx, rerank.Request{
		Query:         query,
		Documents:     docs,
		TopN:          max(1, opts.RerankTopN),
		MaxCandidates: limit,
		ModelOverride: strings.TrimSpace(opts.RerankModel),
		MinScore:      cfg.Rerank.MinScore,
	})
	if err != nil {
		return nil, err
	}

	byID := make(map[string]SearchHit, len(hits))
	for _, hit := range hits {
		key := hit.SourcePath + "#" + strconv.FormatInt(hit.ChunkID, 10)
		byID[key] = hit
	}

	ordered := make([]SearchHit, 0, len(resp.Results))
	for _, item := range resp.Results {
		hit, ok := byID[item.ID]
		if !ok {
			continue
		}
		hit.RerankScore = item.Score
		hit.RankingSource = "rerank"
		hit.RelevanceScore = item.Score
		ordered = append(ordered, hit)
	}
	return ordered, nil
}
```

- [ ] **Step 5: 给 CLI 增加 rerank 开关**

在 `pkg/cli/kb_vector.go` 加 flags 和选项透传：

```go
var (
	kbEnableRerank        bool
	kbRerankProvider      string
	kbRerankModel         string
	kbRerankTopN          int
	kbRerankMaxCandidates int
)

kbVectorSearchCmd.Flags().BoolVar(&kbEnableRerank, "rerank", false, "apply rerank to merged vector candidates")
kbVectorSearchCmd.Flags().StringVar(&kbRerankProvider, "rerank-provider", "", "rerank provider override")
kbVectorSearchCmd.Flags().StringVar(&kbRerankModel, "rerank-model", "", "rerank model override")
kbVectorSearchCmd.Flags().IntVar(&kbRerankTopN, "rerank-top-n", 0, "rerank top N override")
kbVectorSearchCmd.Flags().IntVar(&kbRerankMaxCandidates, "rerank-max-candidates", 0, "rerank candidate cap override")
```

并透传到：

```go
results, err := vectorkb.Search(ctx, cfg, vectorkb.SearchOptions{
	// existing fields...
	EnableRerank:        kbEnableRerank,
	RerankProvider:      strings.TrimSpace(kbRerankProvider),
	RerankModel:         strings.TrimSpace(kbRerankModel),
	RerankTopN:          kbRerankTopN,
	RerankMaxCandidates: kbRerankMaxCandidates,
}, kbQuery)
```

- [ ] **Step 6: 给 CLI 增加一个 JSON 输出测试**

在 `pkg/cli/kb_vector_test.go` 追加：

```go
func TestRunKBVectorSearch_JSONIncludesRankingSource(t *testing.T) {
	cfg := setupCampaignTestEnv(t)
	cfg.KnowledgeVector.DefaultProvider = "mock-openai"
	cfg.KnowledgeVector.DefaultModel = "test-embedding-3-small"
	cfg.Rerank.Enabled = boolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: mockRerankURL(t),
		Model:  "Pro/BAAI/bge-reranker-v2-m3",
		APIKey: "test-key",
	}

	oldQuery := kbQuery
	oldJSON := globalJSON
	oldRerank := kbEnableRerank
	t.Cleanup(func() {
		kbQuery = oldQuery
		globalJSON = oldJSON
		kbEnableRerank = oldRerank
	})

	kbQuery = "token confusion admin panel preview route"
	globalJSON = true
	kbEnableRerank = true

	output := captureStdout(t, func() {
		require.NoError(t, runKBVectorSearch(kbVectorSearchCmd, nil))
	})

	var payload []map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &payload))
	require.NotEmpty(t, payload)
	require.Contains(t, payload[0], "ranking_source")
}
```

- [ ] **Step 7: 跑 vectorkb 与 CLI 测试**

Run:

```bash
go test ./internal/vectorkb -run 'TestSearchAppliesRerankWhenEnabled|TestSearchFallsBackWhenRerankFails' -v
go test ./pkg/cli -run 'TestRunKBVectorSearch_JSONIncludesRankingSource|TestRunKBVectorDoctor_JSONReportIncludesSemanticStatus' -v
```

Expected:

- PASS

- [ ] **Step 8: 提交该任务**

```bash
git add internal/vectorkb/models.go internal/vectorkb/search.go internal/vectorkb/search_rerank_test.go pkg/cli/kb_vector.go pkg/cli/kb_vector_test.go
git commit -m "feat(rerank): 接入向量检索与命令行"
```

---

### Task 4: 把 rerank 接入 `/knowledge/vector/search`

**Files:**
- Modify: `pkg/server/handlers/knowledge.go`
- Modify: `pkg/server/handlers/knowledge_test.go`

- [ ] **Step 1: 先补 API request/response 测试**

在 `pkg/server/handlers/knowledge_test.go` 追加一个成功路径测试：

```go
func TestSearchVectorKnowledgeSupportsRerank(t *testing.T) {
	cfg, cleanup := setupKnowledgeHandlerDB(t)
	defer cleanup()

	cfg.Rerank.Enabled = boolPtr(true)
	cfg.Rerank.Provider = "openai"
	cfg.Rerank.OpenAI = config.RerankProviderConfig{
		APIURL: mockRerankURL(t),
		Model:  "Pro/BAAI/bge-reranker-v2-m3",
		APIKey: "test-key",
	}

	app := fiber.New()
	app.Post("/knowledge/vector/search", SearchVectorKnowledge(cfg))

	body, err := json.Marshal(map[string]any{
		"query": "token confusion admin panel preview route",
		"workspace": "acme",
		"enable_rerank": true,
		"rerank_top_n": 5,
	})
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/knowledge/vector/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	defer resp.Body.Close()

	var payload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.Equal(t, true, payload["rerank_applied"])
	require.Equal(t, "rerank", payload["ranking_source"])
}
```

- [ ] **Step 2: 跑 handler 测试，确认先失败**

Run:

```bash
go test ./pkg/server/handlers -run 'TestSearchVectorKnowledgeSupportsRerank|TestSearchVectorKnowledgeValidation' -v
```

Expected:

- FAIL
- 报 request 字段或 response 字段不存在

- [ ] **Step 3: 扩展 request struct 与 handler 透传**

在 `pkg/server/handlers/knowledge.go` 修改：

```go
type KnowledgeVectorSearchRequest struct {
	Query               string   `json:"query"`
	Workspace           string   `json:"workspace,omitempty"`
	WorkspaceLayers     []string `json:"workspace_layers,omitempty"`
	ScopeLayers         []string `json:"scope_layers,omitempty"`
	Provider            string   `json:"provider,omitempty"`
	Model               string   `json:"model,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	MinSourceConfidence float64  `json:"min_source_confidence,omitempty"`
	SampleTypes         []string `json:"sample_types,omitempty"`
	ExcludeSampleTypes  []string `json:"exclude_sample_types,omitempty"`
	EnableRerank        bool     `json:"enable_rerank,omitempty"`
	RerankProvider      string   `json:"rerank_provider,omitempty"`
	RerankModel         string   `json:"rerank_model,omitempty"`
	RerankTopN          int      `json:"rerank_top_n,omitempty"`
	RerankMaxCandidates int      `json:"rerank_max_candidates,omitempty"`
}
```

传入 `vectorkb.SearchOptions`：

```go
EnableRerank:        req.EnableRerank,
RerankProvider:      strings.TrimSpace(req.RerankProvider),
RerankModel:         strings.TrimSpace(req.RerankModel),
RerankTopN:          req.RerankTopN,
RerankMaxCandidates: req.RerankMaxCandidates,
```

返回体改成：

```go
rankingSource := "hybrid"
rerankApplied := false
if len(results) > 0 {
	rankingSource = results[0].RankingSource
	rerankApplied = rankingSource == "rerank"
}

return c.JSON(fiber.Map{
	"query":          req.Query,
	"workspace":      formatKnowledgeWorkspaceLabel(req.Workspace),
	"total":          len(results),
	"rerank_applied": rerankApplied,
	"rerank_provider": strings.TrimSpace(req.RerankProvider),
	"rerank_model":   strings.TrimSpace(req.RerankModel),
	"ranking_source": rankingSource,
	"data":           results,
})
```

- [ ] **Step 4: 跑 handler 测试确认通过**

Run:

```bash
go test ./pkg/server/handlers -run 'TestSearchVectorKnowledgeSupportsRerank|TestSearchVectorKnowledgeValidation|TestVectorKnowledgeDoctor' -v
```

Expected:

- PASS

- [ ] **Step 5: 提交该任务**

```bash
git add pkg/server/handlers/knowledge.go pkg/server/handlers/knowledge_test.go
git commit -m "feat(rerank): 接入知识库搜索 API"
```

---

### Task 5: 给 `do-ai-semantic-search.yaml` 接上 rerank

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml`

- [ ] **Step 1: 给 fragment 增加参数定义**

在参数区新增：

```yaml
  - name: enableRerank
    type: bool
    default: false
    description: "Apply rerank API after hybrid candidate merge"
  - name: rerankProvider
    type: string
    default: ""
  - name: rerankModel
    type: string
    default: ""
  - name: rerankTopN
    type: int
    default: 10
  - name: rerankMaxCandidates
    type: int
    default: 40
  - name: rerankResultsOutput
    default: "{{Output}}/ai-analysis/rerank-results-{{TargetSpace}}.json"
```

- [ ] **Step 2: 在 merge-results 后新增 rerank step**

新增 bash step，直接调用 API，而不是在 workflow 里内嵌 provider 逻辑：

```yaml
  - name: rerank-merged-results
    type: bash
    pre_condition: "{{enableSemanticSearch}} && {{enableRerank}}"
    command: |
      INPUT="{{semanticResultsOutput}}"
      OUTPUT="{{rerankResultsOutput}}"
      if [ ! -f "$INPUT" ] || [ ! -s "$INPUT" ]; then
        echo '[]' > "$OUTPUT"
        exit 0
      fi

      jq 'map({
        document_id,
        chunk_id,
        workspace,
        title,
        source_path,
        doc_type,
        section,
        snippet,
        relevance_score,
        source,
        type
      })' "$INPUT" > "{{semanticIndexDir}}/rerank-candidates.json"

      set +e
      "$OSM_BIN" --silent $OSM_CLI_BASE_ARGS --json kb vector search \
        --workspace "{{knowledgeWorkspace}}" \
        --query "$(cat "{{resolvedSearchQueryOutput}}")" \
        --limit "{{maxSearchResults}}" \
        --rerank \
        --rerank-provider "{{rerankProvider}}" \
        --rerank-model "{{rerankModel}}" \
        --rerank-top-n "{{rerankTopN}}" \
        --rerank-max-candidates "{{rerankMaxCandidates}}" > "$OUTPUT"
      status=$?
      set -e

      if [ $status -ne 0 ] || [ ! -s "$OUTPUT" ]; then
        cp "$INPUT" "$OUTPUT"
      fi
```

- [ ] **Step 3: 下游优先消费 rerank 产物**

把后续汇总/高亮输入改成“优先 rerank，否则用原结果”的模式：

```yaml
      FINAL_RESULTS="{{rerankResultsOutput}}"
      if [ ! -f "$FINAL_RESULTS" ] || [ ! -s "$FINAL_RESULTS" ]; then
        FINAL_RESULTS="{{semanticResultsOutput}}"
      fi
```

要求下游至少这些节点改为读取 `FINAL_RESULTS`：

- semantic highlights 生成
- AI semantic agent 输入摘要
- report 中的 semantic section

- [ ] **Step 4: 先验证 workflow 能过 schema**

Run:

```bash
GOCACHE=/tmp/go-build go run ./cmd/osmedeus workflow validate ./osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml
```

Expected:

- PASS

- [ ] **Step 5: 提交该任务**

```bash
git add osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml
git commit -m "feat(rerank): 接入语义搜索主链"
```

---

### Task 6: 给 `do-ai-semantic-search-hybrid.yaml` 接上 rerank

**Files:**
- Modify: `osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml`

- [ ] **Step 1: 复用同一组参数**

在 hybrid fragment 中增加与 Task 5 完全一致的参数：

```yaml
  - name: enableRerank
    type: bool
    default: false
  - name: rerankProvider
    type: string
    default: ""
  - name: rerankModel
    type: string
    default: ""
  - name: rerankTopN
    type: int
    default: 10
  - name: rerankMaxCandidates
    type: int
    default: 40
  - name: rerankResultsOutput
    default: "{{Output}}/ai-analysis/hybrid-rerank-results-{{TargetSpace}}.json"
```

- [ ] **Step 2: 在 `merge-results` 后增加 rerank step**

插入的逻辑与 Task 5 同构，但输入输出改为 hybrid 文件：

```yaml
      INPUT="{{hybridSearchResultsOutput}}"
      OUTPUT="{{rerankResultsOutput}}"
```

要求：

- rerank 成功时保留 `ranking_source=rerank`
- 失败时直接复制 `{{hybridSearchResultsOutput}}`

- [ ] **Step 3: 调整后续消费者**

把这些输入改成优先用 `{{rerankResultsOutput}}`：

- `hybridSearchHighlightsOutput`
- AI semantic summary 输入
- report / markdown evidence 摘要

- [ ] **Step 4: 跑 workflow validate**

Run:

```bash
GOCACHE=/tmp/go-build go run ./cmd/osmedeus workflow validate ./osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml
```

Expected:

- PASS

- [ ] **Step 5: 提交该任务**

```bash
git add osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml
git commit -m "feat(rerank): 接入混合语义搜索链"
```

---

### Task 7: 补 smoke/regression 与文档

**Files:**
- Modify: `test/regression/ai-semantic-vector-smoke.sh`
- Modify: `README.md`
- Modify: `docs/api/knowledge.mdx`
- Modify: `docs/api/README.mdx`

- [ ] **Step 1: 扩展 smoke 脚本增加 mock rerank server**

在 `test/regression/ai-semantic-vector-smoke.sh` 新增 rerank mock 变量与启动逻辑，保持与 embedding mock 分离：

```bash
RERANK_PORT="${RERANK_PORT:-8912}"
MOCK_RERANK_MODEL="${MOCK_RERANK_MODEL:-Pro/BAAI/bge-reranker-v2-m3}"
MOCK_RERANK_PROVIDER="${MOCK_RERANK_PROVIDER:-openai}"

RERANK_PORT="$(find_free_tcp_port "$RERANK_PORT")"

cat >>"$BASE_DIR/osm-settings.yaml" <<EOF
rerank_config:
  enabled: true
  provider: "openai"
  top_n: 5
  max_candidates: 20
  timeout: 5s
  openai:
    api_url: "http://127.0.0.1:$RERANK_PORT/v1/rerank"
    model: "Pro/BAAI/bge-reranker-v2-m3"
    api_key: ""
EOF
```

启动一个本地 mock rerank server，并在脚本末尾增加断言：

```bash
RERANK_FILE="$AI_DIR/rerank-results-$WORKSPACE.json"
assert_ge "$(jq 'length' "$RERANK_FILE")" 1 "rerank results"
assert_contains "$(jq -r '.[0].ranking_source' "$RERANK_FILE")" "rerank" "ranking source"
```

- [ ] **Step 2: 先跑 smoke，确认失败点**

Run:

```bash
test/regression/ai-semantic-vector-smoke.sh
```

Expected:

- 初次 FAIL，原因是 rerank 结果文件或字段尚未生成

- [ ] **Step 3: 更新 README 与 API 文档**

在 `README.md` 增加：

```md
### Rerank configuration

Osmedeus now supports API-based rerank after vectorkb recall. The recommended setup is:

- `embeddings_config` for vectorization
- `rerank_config` for semantic re-ordering
- 推荐默认：
  - embeddings：`https://router.tumuer.me/v1/embeddings` + `BAAI/bge-m3`
  - rerank：`https://router.tumuer.me/v1/rerank` + `Pro/BAAI/bge-reranker-v2-m3`

If rerank fails, workflows automatically fall back to the existing hybrid ranking path.
```

在 `docs/api/knowledge.mdx` 增加 `/knowledge/vector/search` 的新请求参数与响应字段示例：

```json
{
  "query": "token confusion admin panel preview route",
  "workspace": "example.com",
  "enable_rerank": true,
		"rerank_provider": "openai",
		"rerank_top_n": 8
}
```

返回示例：

```json
{
  "query": "token confusion admin panel preview route",
  "total": 8,
  "rerank_applied": true,
  "ranking_source": "rerank",
  "data": [
    {
      "title": "Workspace Auth Playbook",
      "base_relevance_score": 1.27,
      "rerank_score": 0.98,
      "ranking_source": "rerank"
    }
  ]
}
```

- [ ] **Step 4: 跑最终验证**

Run:

```bash
go test ./internal/config ./internal/rerank ./internal/vectorkb ./pkg/cli ./pkg/server/handlers -v
GOCACHE=/tmp/go-build go run ./cmd/osmedeus workflow validate ./osmedeus-base/workflows/fragments/do-ai-semantic-search.yaml
GOCACHE=/tmp/go-build go run ./cmd/osmedeus workflow validate ./osmedeus-base/workflows/fragments/do-ai-semantic-search-hybrid.yaml
test/regression/ai-semantic-vector-smoke.sh
```

Expected:

- 所有 Go 测试 PASS
- 两个 workflow validate PASS
- semantic smoke PASS，且能看到 rerank 产物

- [ ] **Step 5: 提交该任务**

```bash
git add test/regression/ai-semantic-vector-smoke.sh README.md docs/api/knowledge.mdx docs/api/README.mdx
git commit -m "docs(rerank): 补齐回归与使用说明"
```

---

## Self-Review Checklist

- 配置层是否完整覆盖：`enabled/provider/top_n/max_candidates/timeout/min_score`
- `internal/rerank` 是否只做 provider adapter，不夹杂 workflow 逻辑
- `vectorkb.Search()` 是否严格 fail-open
- CLI / API / workflow 是否都透传了同一套 rerank 参数
- 输出字段是否统一使用：
  - `base_relevance_score`
  - `rerank_score`
  - `ranking_source`
- smoke 是否同时覆盖：
  - rerank 成功
  - rerank 失败 fallback

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-16-embedding-rerank-implementation.md`.

Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
