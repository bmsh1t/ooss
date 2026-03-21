# AI 工作流改进计划

## 背景

对 `osmedeus-base/workflows/fragments/` 下的 AI 工作流进行实战化改进，提升渗透测试效率。

### 当前问题

1. 缺乏 Root Cause 分析
2. 缺乏 CVE/CVSS 关联
3. 缺乏 POC 生成
4. 缺乏攻击面量化
5. Agent 工具权限过大
6. JSON Schema 过于复杂
7. 缺乏输出验证

---

## 改进计划

### P1 改进（低复杂度，高价值）

#### 1.1 简化 JSON Schema

**问题**：当前 JSON 嵌套过深（4-5层），解析容易出错，Agent 输出格式容易错误

**改进方案**：简化为扁平结构

```yaml
# 改进前（深嵌套）
{
  "summary": {"total_critical": 0, "risk_level": "高"},
  "vulnerability_validation": {
    "critical_findings": [{
      "id": "vuln-1",
      "finding": "...",
      "url": "...",
      ...
    }]
  },
  "attack_chain": {...},
  "attack_path_planning": {...}
}

# 改进后（扁平化）
{
  "risk_level": "高/中/低",
  "risk_score": 8.5,
  "total_critical": 3,
  "total_high": 5,
  "findings": [
    {
      "id": "vuln-1",
      "severity": "critical",
      "type": "sql-injection",
      "cve": "CVE-2024-1234",
      "cvss": 9.8,
      "url": "https://target.com/login",
      "finding": "SQL注入漏洞",
      "status": "confirmed/false_positive/needs_verification",
      "root_cause": "用户输入未过滤直接拼接到SQL",
      "impact": "可导致数据泄露",
      "poc": "python3 poc.py",
      "remediation": "使用参数化查询"
    }
  ],
  "attack_chains": [...],
  "attack_phases": [...]
}
```

**涉及文件**：
- `do-ai-unified-analysis.yaml`
- `do-ai-vuln-validation.yaml`
- `do-ai-attack-path.yaml`

---

#### 1.2 CVE/CVSS 关联（方案2：预查询）

**问题**：当前 LLM 自行判断 CVE，知识截止日期限制+幻觉风险

**选定方案**：方案2 - 预查询模式
- 在 Agent 分析前，先用 bash 预查询相关 CVE
- 查询结果写入 JSON 文件供 Agent 直接读取
- Token 消耗低（~30-50/查询），延迟小（~1-3s/查询）

**Token 消耗分析**：
| 方案 | 额外 Token（10漏洞） | 额外延迟 | 风险 |
|------|---------------------|----------|------|
| LLM 自行判断 | 0 | 0 | 不准确（60-70%） |
| 方案1 Agent联网 | 500-1000 | +30-60s | API 不稳定 |
| **方案2 预查询** | **300-500** | **+3-10s** | **低** |
| 方案3 混合 | 1000-2000 | +30-60s | 复杂 |

**实现方案**：

```yaml
# 在 AI 分析前添加 CVE 预查询步骤
- name: pre-query-cve
  type: bash
  pre_condition: "{{enableVulnValidation}}"
  command: |
    mkdir -p {{Output}}/ai-analysis/cve
    
    # 查询关键技术栈的最新 CVE
    TECH_KEYWORDS=$(cat {{fingerprintFile}} 2>/dev/null | jq -r '.tech // [] | .[0:5][]' 2>/dev/null | tr '\n' ',' | sed 's/,$//')
    
    # 预查询 Top 5 技术的 CVE（CVSS > 7.0）
    for tech in $(echo "$TECH_KEYWORDS" | tr ',' '\n' | head -5); do
      curl -s "https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=${tech}&cvssV3Severity=HIGH&resultsPerPage=3" | \
        jq -r '.vulnerabilities[] | "|\(.cve.id)|\(.metrics.cvssMetricV31[0].cvssData.baseScore)|\(.cve.descriptions[0].value | .[0:200])"' >> {{Output}}/ai-analysis/cve/tech-cve.txt 2>/dev/null || true
    done
    
    # 查询通用高危漏洞类型
    for vuln_type in "sql injection" "xss" "rce" "ssrf" "ssti"; do
      curl -s "https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=${vuln_type}&cvssV3Severity=HIGH&resultsPerPage=2" | \
        jq -r '.vulnerabilities[] | "|\(.cve.id)|\(.metrics.cvssMetricV31[0].cvssData.baseScore)|\(.cve.descriptions[0].value | .[0:200])"' >> {{Output}}/ai-analysis/cve/vuln-type-cve.txt 2>/dev/null || true
    done
    
    echo "[CVE] Pre-query completed"

# Agent 分析时引用预查询结果
query: |
  ## 预查询的 CVE 参考数据
  请使用 read_lines 工具读取以下文件获取 CVE 参考：
  - {{Output}}/ai-analysis/cve/tech-cve.txt - 技术栈相关 CVE
  - {{Output}}/ai-analysis/cve/vuln-type-cve.txt - 漏洞类型相关 CVE
  
  ## 数据文件
  请使用 read_lines 工具读取以下文件获取详细信息：
  ...
```

**CVE 数据文件格式**：
```
|CVE-2024-1234|9.8|SQL Injection in login form allows remote code execution
|CVE-2024-5678|8.5|Cross-site scripting vulnerability in comment section
```

**NVD API 限制应对**：
- 免费 API，无 key 要求
- 每秒限速：5 请求
- 建议添加 `sleep 0.2` 控制频率
- 失败时使用缓存降级

**涉及文件**：
- `do-ai-vuln-validation.yaml`
- `do-ai-unified-analysis.yaml`

---

#### 1.4 添加输出验证

**问题**：当前 JSON 验证只是静默失败

**改进方案**：添加强制验证步骤，验证失败时重试或告警

```yaml
- name: validate-json-output
  type: function
  pre_condition: "{{enableUnifiedAnalysis}}"
  functions:
    - 'log_info("Validating AI output format...")'
    - 'assert_json("{{unifiedOutputJson}}", "$.risk_level", "must exist")'
    - 'assert_json("{{unifiedOutputJson}}", "$.findings", "must be array")'

- name: retry-on-invalid-json
  type: agent
  pre_condition: "{{enableUnifiedAnalysis}}"
  # 仅在 JSON 格式错误时触发重试
```

**涉及文件**：
- `do-ai-unified-analysis.yaml`
- `do-ai-vuln-validation.yaml`
- `do-ai-attack-path.yaml`

---

### P2 改进（中复杂度，高价值）

#### 2.1 生成 Python POC

**问题**：当前只输出"手动验证命令"，不是可直接使用的 POC

**改进方案**：要求 Agent 生成可直接运行的 Python POC

```yaml
query: |
  对于每个确认的漏洞，生成 Python POC：

  poc 字段格式：
  ```python
  #!/usr/bin/env python3
  import requests

  target = "https://target.com/login"
  payload = "' OR 1=1 --"

  response = requests.get(f"{target}?q={payload}")
  if "admin" in response.text:
      print("[+] SQL Injection Confirmed!")
  else:
      print("[-] Not vulnerable")
  ```

  poc 字段要求：
  - 独立可运行的完整脚本
  - 包含必要的 import
  - 有清晰的输出判断
  - 避免使用复杂依赖
```

**涉及文件**：
- `do-ai-vuln-validation.yaml`

---

#### 2.2 Root Cause 分析

**问题**：只验证漏洞存在，不分析根本原因

**改进方案**：要求 Agent 输出根因分析

```yaml
query: |
  对于每个漏洞，提供根因分析：

  root_cause 字段：
  - vulnerability_type: 漏洞类型
  - root_cause: 根本原因描述
  - affected_components: 受影响组件
  - exploitation_requirements: 利用条件（需要什么才能利用）
  - impact: 潜在影响
  - business_impact: 业务影响

  示例：
  {
    "root_cause": "用户输入 'username' 字段未经过滤直接拼接到 SQL 查询语句",
    "affected_components": "/app/routes/auth.py:45",
    "exploitation_requirements": "无需认证即可利用",
    "impact": "可导致数据库敏感信息泄露",
    "business_impact": "违反合规要求，可能面临处罚"
  }
```

**涉及文件**：
- `do-ai-vuln-validation.yaml`
- `do-ai-unified-analysis.yaml`

---

#### 2.3 攻击面量化评分

**问题**：当前只有定性描述（高/中/低）

**改进方案**：输出量化评分

```yaml
query: |
  提供量化攻击面评分：

  attack_surface 字段：
  {
    "overall_score": 7.5,
    "max_score": 10,
    "entry_points": 5,
    "exploitable_vulnerabilities": 8,
    "defense_bypass": ["WAF", "MFA"],
    "impact_potential": 9,
    "ease_of_exploitation": 6,
    "breakdown": {
      "web_attack_surface": {"score": 8, "entry_points": 3},
      "api_attack_surface": {"score": 7, "entry_points": 2},
      "network_attack_surface": {"score": 5, "entry_points": 1}
    },
    "risk_factors": [
      "未授权访问风险",
      "数据泄露风险"
    ]
  }
```

**涉及文件**：
- `do-ai-unified-analysis.yaml`
- `do-ai-attack-path.yaml`

---

### P3 改进（高复杂度，中价值）

#### 3.1 限制 Agent 工具权限

**问题**：`preset: bash` 权限过大，不适合自动化

**改进方案**：使用自定义工具替代 bash

```yaml
# 改进前
agent_tools:
  - preset: bash  # 危险：可以执行任意命令

# 改进后
agent_tools:
  - preset: read_file
  - preset: read_lines
  - preset: file_exists
  - preset: http_get
  - preset: http_request
  # 移除 preset: bash
  # 添加自定义安全工具
  - name: safe_nmap
    description: Run nmap scan on target
    parameters:
      type: object
      properties:
        target:
          type: string
        ports:
          type: string
    handler: |
      safe_exec("nmap -p " + args.ports + " " + args.target)
```

**涉及文件**：
- 所有 AI 工作流

---

## 实施顺序

1. **第一阶段**：~~简化 JSON Schema~~ ✅ 已完成
2. **第二阶段**：CVE 预查询实现
3. **第三阶段**：添加输出验证
4. **第四阶段**：POC 生成 + Root Cause 分析
5. **第五阶段**：攻击面量化
6. **第六阶段**：限制 Agent 权限（可选）

---

## 风险评估

| 改进项 | 风险 | 缓解措施 |
|--------|------|----------|
| 简化 JSON | 低：可能影响解析逻辑 | 充分测试 |
| CVE 预查询 | 中：NVD API 限速/不可用 | 添加缓存降级 + sleep 控制频率 |
| Agent POC 生成 | 中：POC 可能不稳定 | 沙箱环境执行 |
| 限制 Agent 权限 | 中：功能受限 | 提供白名单机制 |

---

## 预期收益

| 指标 | 当前 | 改进后 |
|------|------|--------|
| JSON 解析成功率 | ~70% | >95% |
| CVE 关联准确率 | N/A | >90% |
| CVE 关联覆盖率 | 0% | >80% |
| POC 可用率 | ~30% | >70% |
| 攻击面量化 | 无 | 完整量化 |

---

## 待确认事项

1. ~~是否需要支持本地 CVE 数据库？~~ - **已选定方案2：NVD API 预查询**
2. ~~POC 生成是否需要沙箱环境？~~ - 可后续根据需求决定
3. ~~JSON Schema 简化后的具体字段需求？~~ - ✅ 已完成
4. CVE 预查询是否需要添加本地缓存？（减少 API 调用）
