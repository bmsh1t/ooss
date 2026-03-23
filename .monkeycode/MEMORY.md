# 用户指令记忆

本文件记录了用户的指令、偏好和教导，用于在未来的交互中提供参考。

## 格式

### 用户指令条目
用户指令条目应遵循以下格式：

[用户指令摘要]
- Date: [YYYY-MM-DD]
- Context: [提及的场景或时间]
- Instructions:
  - [用户教导或指示的内容，逐行描述]

### 项目知识条目
Agent 在任务执行过程中发现的条目应遵循以下格式：

[项目知识摘要]
- Date: [YYYY-MM-DD]
- Context: Agent 在执行 [具体任务描述] 时发现
- Category: [代码结构|代码模式|代码生成|构建方法|测试方法|依赖关系|环境配置]
- Instructions:
  - [具体的知识点，逐行描述]

## 去重策略
- 添加新条目前，检查是否存在相似或相同的指令
- 若发现重复，跳过新条目或与已有条目合并
- 合并时，更新上下文或日期信息
- 这有助于避免冗余条目，保持记忆文件整洁

## 条目

[AI 工作流执行顺序问题修复]
- Date: 2026-03-23
- Context: Agent 在优化 superdomain-extensive-ai-optimized.yaml 工作流时发现
- Category: 代码模式
- Instructions:
  - AI 模块（ai-vuln-validation, ai-attack-chain, ai-path-planning）与 vuln-suite 并行执行，导致 AI 分析时数据尚未生成
  - 解决方案：调整执行顺序，将 Phase 8 vuln-suite 移到 Phase 7，使 AI 模块在漏洞扫描完成后执行
  - 各 AI 模块增加数据有效性检查（check-xxx-data 步骤），无数据时生成空结果而非调用 LLM
  - max_iterations 从 8-10 优化到 4-6，减少无效 token 消耗

[AI 模块重大优化 - 2026-03-23]
- Date: 2026-03-23
- Context: Agent 根据实战反馈对 AI 工作流进行系统性优化
- Category: 代码模式
- Instructions:
  - P0 JSON 输出稳定性：改用 Markdown + JSON 代码块格式，JSON 解析从 grep 提取改为 sed 精确提取
  - P0 Token 优化：max_iterations 从 6-8 降到 3，输入数据精简到单个合并文件
  - P1 执行验证能力：agent 增加 bash 工具，可以真正执行验证命令而不是只生成命令
  - P1 模块并行化：ai-attack-chain 和 ai-path-planning 改为依赖 vuln-suite，实现三个 AI 模块并行执行
  - P1 Prompt 优化：添加 few-shot 示例，system_prompt 包含完整输出格式示例
  - 修改文件：
    - do-ai-vuln-validation.yaml（完整重写）
    - do-ai-attack-chain.yaml（完整重写）
    - do-ai-path-planning.yaml（完整重写）
    - superdomain-extensive-ai-stable.yaml（依赖关系调整）
    - superdomain-extensive-ai-hybrid.yaml（依赖关系调整）
  - 预期效果：
    - JSON 解析成功率从 50-70% 提升到 90%+
    - Token 消耗减少 30-40%
    - AI 分析并行执行，总耗时减少 40-50%

