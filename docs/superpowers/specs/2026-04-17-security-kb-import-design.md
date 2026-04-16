# Security KB SQLite Import Design

**Date:** 2026-04-17

## Goal

为 Osmedeus 增加一个可扩展的知识库导入框架，先落地第一个适配器：把 `security_kb.sqlite` 里的结构化安全知识导入到当前主知识库（`knowledge_documents` / `knowledge_chunks`），然后继续复用现有 `kb vector index` 进入 vectorkb。

## Problem

当前仓库里的 `osmedeus-base/knowledge/knowledge/security_kb.sqlite` 是一份现成的结构化安全知识库，但它的 schema 与当前 Osmedeus 主线不兼容。当前 Osmedeus KB 主线依赖：

- `knowledge_documents`
- `knowledge_chunks`
- `vector_documents`
- `vector_chunks`
- `vector_embeddings`

而 `security_kb.sqlite` 的表是：

- `cwe`
- `capec`
- `attack_technique`
- `agentic_threat`
- `stride_cwe`
- `owasp_top10`
- 若干 `*_fts` 辅助表

所以它不能被当前 `kb docs` / `kb search` / `kb vector search` 直接消费。

## Design

### 1. CLI 入口

新增统一命令：

```bash
osmedeus kb import --type security-sqlite --path /path/to/security_kb.sqlite -w security-kb
```

第一版只支持 `security-sqlite`，但命令和内部接口设计成可扩展。

### 2. 通用导入框架

新增一个小型 importer 框架，职责是：

- 打开外部知识源
- 将源记录转换为标准知识文档/块
- 统一写入 `knowledge_documents` / `knowledge_chunks`
- 复用现有 `database.UpsertKnowledgeDocument(...)`

框架不负责向量索引；向量索引继续交给现有：

```bash
osmedeus kb vector index -w security-kb
```

### 3. Adapter 边界

定义一个 adapter 接口，每种外部知识源只负责：

- 读取自己的 schema
- 将记录映射为标准文档模型

第一版实现：`security-sqlite` adapter。

### 4. 第一版导入范围

优先支持这些核心表：

- `cwe`
- `capec`
- `attack_technique`
- `agentic_threat`
- `stride_cwe`
- `owasp_top10`（如果映射成本低则一起做）

每条记录转成一篇标准知识文档：

- `workspace`: 用户传入的 workspace
- `source_path`: `security-sqlite://<table>/<primary-key>`
- `source_type`: `sqlite-import`
- `doc_type`: `md` 或 `json`
- `title`: 由主标识字段生成
- `content_hash`: 基于规范化内容计算
- `metadata_json`: 保存来源表、主键、标签等

### 5. Chunk 策略

不要复用当前文件 ingest 那条对大 YAML/JSON 不够稳的路径。

Importer 直接把每条结构化记录按字段拼成紧凑文本，再按记录粒度或小段落粒度切 chunk，确保：

- 单个 chunk 不会大到触发 embedding token 限制
- 结构化字段名仍保留，方便关键词检索和语义检索

### 6. 错误处理

采用部分成功语义：

- 某个表/某条记录失败，不回滚整个 import
- 返回导入 summary：成功数、失败数、失败样本
- CLI 默认输出人类可读 summary，`--json` 输出结构化 summary

### 7. 验收标准

满足以下条件即认为第一版成功：

1. `security_kb.sqlite` 能导入到 `security-kb`
2. `osmedeus kb docs -w security-kb` 可见导入文档
3. `osmedeus kb search --query ... -w security-kb` 可搜索
4. `osmedeus kb vector index -w security-kb` 不再依赖那批超长 YAML/JSON 文件
5. `osmedeus kb vector search --query ... -w security-kb` 可返回结果

## Non-Goals

第一版不做：

- 自动 watch/sync
- 任意 sqlite schema 自动推断
- import 后自动触发 vectorkb index
- UI 配置入口
- 通用 YAML/JSON 结构化映射框架

## Rationale

这个方案的优点是：

- 先解决眼前的 `security_kb.sqlite` 可用性问题
- 不再被超长 YAML/JSON 文件卡住
- 保持当前 KB/vectorkb 主线不变
- 为以后别的“现成知识库”保留 adapter 扩展点
