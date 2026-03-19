# =============================================================================
# Osmedeus Workflow Code Style Guidelines
# =============================================================================
# 本文档定义了工作流 YAML 文件的编码规范
# =============================================================================

## 1. YAML 语法规范

### 1.1 多行命令使用 `|` 而非 `>`
```yaml
# ✅ 正确: 使用 | 保持换行
command: |
  if [ -f "input.txt" ]; then
    cat input.txt | tool -o output.txt
  fi

# ❌ 错误: 使用 > 会合并换行
command: >
  if [ -f "input.txt" ]; then
    cat input.txt | tool -o output.txt
  fi
```

### 1.2 统一缩进
```yaml
# ✅ 正确: 2空格缩进
steps:
  - name: step-name
    type: bash
    command: |
      tool -i input -o output
```

## 2. 文件结构规范

### 2.1 模块文件结构
```yaml
# =============================================================================
# Module Workflow: [模块名称]
# =============================================================================
# 优化说明:
# - [优化点1]
# - [优化点2]
# =============================================================================

kind: module
name: module-name
description: Brief description
tags: tag1, tag2, tag3

# -----------------------------------------------------------------------------
# PARAMS SECTION
# -----------------------------------------------------------------------------
params:
  # 输入文件 (放在前面)
  - name: inputFile
    default: "{{Output}}/path/file.txt"
  
  # 输出文件
  - name: outputFile
    default: "{{Output}}/path/output.txt"
  
  # 配置参数 (放在后面)
  - name: threads
    default: "{{ threads * 2 }}"
  
  # 开关参数 (最后)
  - name: enableFeature
    type: bool
    default: true
    description: "Feature description"

# -----------------------------------------------------------------------------
# DEPENDENCIES SECTION
# -----------------------------------------------------------------------------
dependencies:
  commands:
    - tool1
    - tool2
  variables:
    - name: Target
      type: string
      required: true

# -----------------------------------------------------------------------------
# REPORTS SECTION
# -----------------------------------------------------------------------------
reports:
  - name: report-name
    path: "{{outputFile}}"
    type: text
    description: Report description

# -----------------------------------------------------------------------------
# STEPS SECTION
# -----------------------------------------------------------------------------
steps:
  # ---------------------------------------------------------------------------
  # Setup: Create output folder
  # ---------------------------------------------------------------------------
  - name: create-output-folder
    type: function
    log: "Creating output folder"
    functions:
      - 'create_folder("{{Output}}/folder")'

  # ---------------------------------------------------------------------------
  # Main processing step
  # ---------------------------------------------------------------------------
  - name: main-step
    type: bash
    log: "Running main processing"
    pre_condition: 'file_exists("{{inputFile}}")'
    command: |
      tool -i {{inputFile}} -o {{outputFile}} 2>/dev/null || true
    on_error:
      - action: continue

  # ---------------------------------------------------------------------------
  # Summary
  # ---------------------------------------------------------------------------
  - name: summary
    type: function
    log: "Processing summary"
    functions:
      - 'db_total_assets("{{outputFile}}")'
```

## 3. 错误处理规范

### 3.1 必须添加 on_error 处理
```yaml
# ✅ 正确
- name: risky-step
  type: bash
  command: |
    tool -i input -o output 2>/dev/null || true
  on_error:
    - action: continue
    - 'log_error("Step failed, continuing...")'

# ❌ 错误: 没有错误处理
- name: risky-step
  type: bash
  command: tool -i input -o output
```

### 3.2 使用 `|| true` 防止命令失败
```yaml
command: |
  tool -i input -o output 2>/dev/null || true
```

## 4. 输入验证规范

### 4.1 检查文件存在性
```yaml
- name: process-step
  type: bash
  pre_condition: 'file_exists("{{inputFile}}") && file_length("{{inputFile}}") > 0'
  command: |
    cat {{inputFile}} | tool -o output
```

### 4.2 检查文件大小限制
```yaml
- name: check-limit
  type: function
  pre_condition: 'file_length("{{inputFile}}") > {{maxLimit}}'
  functions:
    - 'log_warn("Input exceeds limit, skipping...")'
    - 'skip()'
```

## 5. 日志规范

### 5.1 步骤必须有 log 字段
```yaml
- name: step-name
  type: bash
  log: "Running [tool name] for [purpose]"
  command: |
    tool -i input -o output
```

### 5.2 Summary 步骤格式
```yaml
- name: summary
  type: function
  log: "[Module name] summary"
  functions:
    - 'db_total_assets("{{outputFile}}")'
```

## 6. 并行执行规范

### 6.1 使用 parallel-steps
```yaml
- name: parallel-scans
  type: parallel-steps
  pre_condition: 'file_exists("{{inputFile}}")'
  parallel_steps:
    - name: scan-1
      type: bash
      command: tool1 -i {{inputFile}}
    
    - name: scan-2
      type: bash
      command: tool2 -i {{inputFile}}
```

## 7. 命名规范

### 7.1 步骤命名
```yaml
# ✅ 正确: kebab-case，描述性
- name: resolve-dns
- name: http-fingerprint
- name: nuclei-critical-scan

# ❌ 错误: 不够描述
- name: step1
- name: run
- name: scan
```

### 7.2 参数命名
```yaml
# ✅ 正确: camelCase
- name: httpThreads
- name: enableDeepScan
- name: maxSubdomains

# ❌ 错误
- name: http_threads
- name: ENABLE_DEEP_SCAN
```

### 7.3 文件路径命名
```yaml
# ✅ 正确: kebab-case
- name: outputFile
  default: "{{Output}}/vulnscan/nuclei-critical-{{TargetSpace}}.txt"

# ❌ 错误
- name: outputFile
  default: "{{Output}}/vulnscan/nuclei_critical_{{TargetSpace}}.txt"
```

## 8. 安全规范

### 8.1 敏感数据处理
```yaml
# ✅ 正确: 使用环境变量
- name: apiKey
  default: "{{env.API_KEY}}"

# ❌ 错误: 硬编码
- name: apiKey
  default: "sk-xxxxx"
```

### 8.2 命令注入防护
```yaml
# ✅ 正确: 变量在引号内
command: |
  tool -i "{{inputFile}}" -o "{{outputFile}}"

# ❌ 错误: 变量未引用
command: |
  tool -i {{inputFile}} -o {{outputFile}}
```

## 9. 性能优化规范

### 9.1 添加速率限制
```yaml
params:
  - name: rateLimit
    default: 150

command: |
  tool -rate-limit {{rateLimit}} -i input -o output
```

### 9.2 添加超时限制
```yaml
params:
  - name: scanTimeout
    default: "30m"

command: |
  timeout -k 1m {{scanTimeout}} tool -i input -o output
```

### 9.3 添加输入大小限制
```yaml
params:
  - name: inputLimit
    default: 10000

- name: check-limit
  type: function
  pre_condition: 'file_length("{{inputFile}}") > {{inputLimit}}'
  functions:
    - 'log_warn("Input too large, truncating...")'
    - 'exec_cmd("head -n {{inputLimit}} {{inputFile}} > {{inputFile}}.limited")'
```

## 10. 检查清单

在提交工作流前，请确认：

- [ ] 所有步骤都有 `log` 字段
- [ ] 所有步骤都有 `on_error` 处理
- [ ] 多行命令使用 `|` 而非 `>`
- [ ] 命令末尾添加 `|| true` 或 `2>/dev/null || true`
- [ ] 输入文件有 `pre_condition` 检查
- [ ] 添加了速率限制和超时参数
- [ ] 命名遵循 kebab-case (步骤) 和 camelCase (参数)
- [ ] 有 Summary 步骤总结结果
