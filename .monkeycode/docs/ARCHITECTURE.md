# Osmedeus 系统架构文档

## 项目概述

Osmedeus 是一个用 Go 语言开发的安全自动化工作流引擎，用于编排和执行安全扫描任务。它支持多种执行环境（本地、Docker、SSH 远程），提供灵活的工作流定义（YAML），并具备模板渲染、函数执行、分布式执行、云基础设施管理等高级功能。

## 架构总览

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CLI / API Layer                                 │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────────┐│
│  │    pkg/cli      │  │   pkg/server    │  │       pkg/server/handlers    ││
│  │  (Cobra CLI)    │  │  (Fiber API)     │  │     (REST API Handlers)      ││
│  └─────────────────┘  └─────────────────┘  └─────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Execution Engine Layer                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                    internal/executor                                      ││
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐      ││
│  │  │  Bash    │ │ Function │ │ Parallel │ │ Foreach  │ │  LLM     │      ││
│  │  │ Executor │ │ Executor │ │ Executor │ │ Executor │ │ Executor │      ││
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘ └──────────┘      ││
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐                   ││
│  │  │ HTTP     │ │  Agent   │ │ Agent-ACP│ │RemoteBash│                   ││
│  │  │ Executor │ │ Executor │ │ Executor │ │ Executor │                   ││
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘                   ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Runner Layer                                       │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────────────┐  │
│  │   HostRunner    │  │  DockerRunner   │  │       SSHRunner             │  │
│  │  (本地执行)      │  │  (容器执行)      │  │      (远程执行)              │  │
│  └─────────────────┘  └─────────────────┘  └─────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Core Services Layer                                  │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐       │
│  │   parser     │ │   template    │ │  functions   │ │  scheduler   │       │
│  │  (YAML解析)   │ │  (模板渲染)   │ │  (函数执行)   │ │   (调度器)    │       │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘       │
│  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐       │
│  │   database   │ │ distributed  │ │    cloud     │ │   snapshot   │       │
│  │  (数据存储)   │ │  (分布式)    │ │   (云设施)   │ │   (快照)      │       │
│  └──────────────┘ └──────────────┘ └──────────────┘ └──────────────┘       │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Core Types Layer                                   │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                      internal/core                                       ││
│  │  Workflow, Step, Trigger, ExecutionContext, RunnerConfig, Agent Types   ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────────┘
```

## 核心概念

### 工作流类型 (WorkflowKind)

| 类型 | 说明 | 用途 |
|------|------|------|
| `module` | 模块工作流 | 单个执行单元，包含多个步骤 |
| `flow` | 流程工作流 | 编排多个模块，定义模块间的依赖关系 |

### 步骤类型 (StepType)

| 类型 | 说明 | 执行器 |
|------|------|--------|
| `bash` | 执行 Shell 命令 | BashExecutor |
| `function` | 执行 JavaScript 函数 | FunctionExecutor |
| `parallel-steps` | 并行执行多个步骤 | ParallelExecutor |
| `foreach` | 遍历输入执行步骤 | ForeachExecutor |
| `remote-bash` | 远程执行命令 | RemoteBashExecutor |
| `http` | HTTP 请求 | HTTPExecutor |
| `llm` | LLM 调用 | LLMExecutor |
| `agent` | Agent 智能体循环 | AgentExecutor |
| `agent-acp` | 外部 Agent (ACP 协议) | ACPExecutor |

### 执行环境类型 (RunnerType)

| 类型 | 说明 | 实现 |
|------|------|------|
| `host` | 本地执行 | HostRunner |
| `docker` | Docker 容器执行 | DockerRunner |
| `ssh` | SSH 远程执行 | SSHRunner |

### 触发器类型 (TriggerType)

| 类型 | 说明 |
|------|------|
| `cron` | Cron 定时触发 |
| `event` | 事件触发 (webhook, asset 新增等) |
| `watch` | 文件监控触发 (fsnotify) |
| `manual` | 手动触发 |

## 模块详解

### 1. CLI 层 (pkg/cli)

CLI 层使用 Cobra 框架实现命令行界面，提供以下命令：

- `run` - 运行工作流
- `server` - 启动 API 服务器
- `workflow` - 工作流管理
- `cloud` - 云基础设施管理
- `install` - 安装二进制文件和工作流
- `agent` - 运行 ACP Agent
- `worker` - 分布式 Worker 管理

### 2. API 层 (pkg/server)

使用 Fiber 框架构建 REST API 服务器，主要功能：

- 工作流执行管理 (创建、查询、取消)
- 资产发现和管理
- 事件接收和处理
- LLM 兼容接口
- ACP Agent 代理接口
- Webhook 触发接口

### 3. 执行引擎层 (internal/executor)

执行引擎是 Osmedeus 的核心，负责调度不同类型的步骤执行。

#### 3.1 StepDispatcher

步骤分发器，使用插件注册模式管理所有执行器：

```go
type StepDispatcher struct {
    registry         *PluginRegistry
    templateEngine   template.TemplateEngine
    functionRegistry *functions.Registry
    // ...
}
```

#### 3.2 执行器插件

每个执行器实现 `StepExecutorPlugin` 接口：

```go
type StepExecutorPlugin interface {
    // Execute 执行步骤
    Execute(ctx context.Context, step *core.Step, execCtx *core.ExecutionContext) (*core.StepResult, error)
    // CanExecute 检查是否可以执行此步骤
    CanExecute(step *core.Step) bool
}
```

##### BashExecutor
- 执行本地 Shell 命令
- 支持命令参数结构化 (speed_args, config_args, input_args, output_args)
- 支持超时、输出文件指定

##### FunctionExecutor
- 通过 Goja JavaScript VM 执行 JavaScript 函数
- 函数注册在 functions.Registry 中
- 支持文件操作、字符串处理、数据库操作等

##### ParallelExecutor
- 并行执行多个子步骤
- 等待所有子步骤完成

##### ForeachExecutor
- 遍历输入列表
- 支持线程数配置 (并发控制)
- 支持变量预处理 (variable_pre_process)

##### RemoteBashExecutor
- 在 Docker 容器或 SSH 远程执行命令
- 支持结果文件回传

##### HTTPExecutor
- HTTP 请求执行
- 支持自定义 Headers、Method、Body
- 支持模板渲染

##### LLMExecutor
- LLM API 调用 (OpenAI 兼容接口)
- 支持 Tools 调用 (Function Calling)
- 支持 Streaming 输出
- 支持多模态 (图像输入)
- 支持 Embeddings
- 支持结构化输出 (JSON Schema)
- 模型回退机制 (多个模型按顺序尝试)

##### AgentExecutor
Agent 智能体是 Osmedeus 的核心 AI 功能，提供 Agentic LLM 执行循环：

**核心特性：**
- **Tool Calling 循环**: 调用 LLM → 执行工具 → 反馈结果 → 循环直到完成
- **多目标支持**: 通过 `queries` 字段顺序执行多个目标
- **规划阶段**: 支持 `plan_prompt` 在主循环前进行任务规划
- **模型偏好**: 通过 `models` 字段指定首选模型列表，支持回退到默认模型
- **结构化输出**: 支持 `output_schema` 在最终迭代强制 JSON 输出
- **停止条件**: 支持 `stop_condition` JS 表达式提前终止

**Memory 管理：**
- **滑动窗口**: `max_messages` 控制保留消息数量
- **自动摘要**: `summarize_on_truncate` 启用 LLM 摘要压缩
- **持久化**: `persist_path` 保存对话历史到 JSON
- **恢复**: `resume_path` 从历史记录恢复对话

**子 Agent 嵌套：**
- 支持通过 `sub_agents` 定义子 Agent
- 通过 `spawn_agent` 工具调用子 Agent
- 最大嵌套深度: 3 层 (可配置)
- 令牌计数自动合并

**工具 Tracing：**
- `on_tool_start`: 工具调用前执行的 JS 表达式
- `on_tool_end`: 工具调用后执行的 JS 表达式

**导出变量：**
- `agent_content`: 最终内容
- `agent_history`: 完整对话历史 (JSON)
- `agent_iterations`: 迭代次数
- `agent_total_tokens` / `prompt_tokens` / `completion_tokens`: 令牌使用统计
- `agent_tool_results`: 工具调用结果 (JSON)
- `agent_plan`: 规划阶段输出
- `agent_goal_results`: 多目标结果 (JSON)

##### ACPExecutor
外部 Agent 执行，通过 Agent Communication Protocol (ACP) 与外部 AI Agent 通信：

**内置 Agent：**
- `claude-code`: Claude Code (npx @zed-industries/claude-code-acp)
- `codex`: Codex (npx @zed-industries/codex-acp)
- `opencode`: OpenCode (opencode acp)
- `gemini`: Gemini CLI (gemini --experimental-acp)

**配置选项：**
- `cwd`: 工作目录
- `allowed_paths`: 允许访问的路径
- `acp_config.env`: 环境变量
- `acp_config.write_enabled`: 允许文件写入

**导出变量：**
- `acp_output`: Agent 输出
- `acp_stderr`: 错误输出
- `acp_agent`: 使用的 Agent 名称

##### Agent 工具预设 (Preset Tools)

AgentExecutor 提供丰富的内置工具预设，位于 `internal/core/agent_tool_presets.go`:

| 工具 | 说明 |
|------|------|
| `bash` | 执行 Shell 命令 |
| `read_file` | 读取文件内容 |
| `read_lines` | 读取文件为行数组 |
| `file_exists` | 检查文件是否存在 |
| `file_length` | 统计文件行数 |
| `append_file` | 追加内容到文件 |
| `save_content` | 写入内容到文件 |
| `glob` | 文件名模式匹配 |
| `grep_string` | 字符串搜索 |
| `grep_regex` | 正则表达式搜索 |
| `http_get` | HTTP GET 请求 |
| `http_request` | HTTP 自定义请求 |
| `jq` | JSON 查询 |
| `exec_python` | 执行 Python 代码 |
| `exec_python_file` | 执行 Python 文件 |
| `exec_ts` | 执行 TypeScript 代码 (via bun) |
| `exec_ts_file` | 执行 TypeScript 文件 |
| `run_module` | 运行 osmedeus 模块 |
| `run_flow` | 运行 osmedeus 流程 |
| `spawn_agent` | (动态生成) 嵌套子 Agent |

**自定义工具：**
支持通过 `agent_tools` 定义自定义工具：
```yaml
agent_tools:
  - name: my_tool
    description: "自定义工具描述"
    parameters:
      type: object
      properties:
        arg1:
          type: string
    handler: "log_info(args.arg1)"
```

### 4. 运行器层 (internal/runner)

Runner 接口定义了命令执行环境：

```go
type Runner interface {
    Execute(ctx context.Context, command string) (*CommandResult, error)
    Setup(ctx context.Context) error
    Cleanup(ctx context.Context) error
    Type() core.RunnerType
    IsRemote() bool
    CopyFromRemote(ctx context.Context, remotePath, localPath string) error
    SetPIDCallbacks(onStart, onEnd PIDCallback)
}
```

#### 4.1 HostRunner
- 本地命令执行
- 使用 os/exec 执行命令
- 支持进程 PID 跟踪和终止

#### 4.2 DockerRunner
- Docker 容器内执行
- 支持容器复用 (persistent mode)
- 支持卷挂载、网络配置
- 支持环境变量注入

#### 4.3 SSHRunner
- SSH 远程执行
- 支持连接池
- 支持密钥和密码认证
- 使用 rsync 进行文件同步

### 5. 模板引擎 (internal/template)

#### 5.1 模板引擎接口

```go
type TemplateEngine interface {
    Render(template string, ctx map[string]any) (string, error)
    RenderMap(m map[string]string, ctx map[string]any) (map[string]string, error)
    RenderSlice(s []string, ctx map[string]any) ([]string, error)
    RenderSecondary(template string, ctx map[string]any) (string, error)
    ExtractVariablesSet(template string) map[string]struct{}
}
```

#### 5.2 变量语法

- `{{Variable}}` - 标准模板变量
- `[[variable]]` - Foreach 循环变量 (避免冲突)

#### 5.3 分片引擎 (ShardedEngine)

为了提升高并发下的模板渲染性能，实现了分片引擎：

- 16 个分片 (可配置)
- 每个分片独立缓存
- 支持连接池
- 支持批量渲染 (BatchRenderer 接口)

### 6. 函数系统 (internal/functions)

函数系统通过 Goja JavaScript VM 提供运行时函数。

#### 6.1 函数分类

| 分类 | 说明 | 示例 |
|------|------|------|
| 文件函数 | 文件操作 | file_exists, file_length, extract_to |
| 字符串函数 | 字符串处理 | trim, split, replace |
| URL 函数 | URL 处理 | parse_url, build_url |
| 数据库函数 | 数据库操作 | db_query, db_import_sarif |
| LLM 函数 | LLM 调用 | llm_request |
| Tmux 函数 | Tmux 会话管理 | tmux_run, tmux_capture |
| SSH 函数 | SSH 远程操作 | ssh_exec, ssh_rsync |
| Nmap 函数 | Nmap 结果处理 | nmap_to_jsonl, run_nmap |
| SARIF 函数 | SARIF 解析 | db_import_sarif, convert_sarif_to_markdown |

#### 6.2 函数注册

```go
type Registry struct {
    functions map[string]Function
    vmPool    *VMPool
}
```

#### 6.3 VM 池

使用 VM 池复用 JavaScript 虚拟机实例，减少创建开销：

```go
type VMPool struct {
    pool chan *goja.Runtime
    // ...
}
```

### 7. 工作流解析器 (internal/parser)

#### 7.1 Loader

工作流加载器，负责：

- 从文件系统加载 YAML 工作流
- 缓存管理 (基于 mtime 验证)
- 工作流继承 (extends)

#### 7.2 Parser

YAML 解析器，验证和解析工作流定义。

#### 7.3 Validator

工作流验证器，检查：

- 必填字段
- 步骤依赖 (DAG)
- 变量引用

### 8. 调度器 (internal/scheduler)

Scheduler 管理工作流的触发器。

#### 8.1 支持的触发器

- **Cron 触发器**: 使用 gocron 库
- **事件触发器**: 接收外部事件 (webhook, asset 新增等)
- **文件监控触发器**: 使用 fsnotify 库监控文件变化
- **手动触发器**: 通过 API 手动触发

#### 8.2 事件队列

- 可配置的队列大小 (默认 1000)
- 背压超时处理
- 去重缓存

### 9. 数据库层 (internal/database)

#### 9.1 数据库支持

- SQLite (默认)
- PostgreSQL (通过 Bun ORM)

#### 9.2 核心功能

- 工作流索引 (Workflow Index)
- 资产存储 (Assets)
- 运行历史 (Runs)
- Agent 会话 (Agent Sessions)
- 批量写入协调器 (Write Coordinator) - 减少约 70% I/O

### 10. 分布式 (internal/distributed)

#### 10.1 Master-Worker 架构

- **Master**: 任务分发和协调
- **Worker**: 实际任务执行
- 支持水平扩展

#### 10.2 通信

- HTTP API 通信
- 事件同步

### 11. 云基础设施 (internal/cloud)

支持多云提供商：

- DigitalOcean
- AWS
- GCP
- Azure
- Linode

使用 Pulumi 进行基础设施即代码 (IaC) 管理。

### 12. 快照系统 (internal/snapshot)

工作空间导出/导入为压缩 ZIP 归档。

## 执行流程

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ 1. CLI/API 接收请求                                                           │
│    └── osmedeus run -m <module> -t <target>                                  │
└──────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 2. Parser 加载和验证工作流                                                     │
│    ├── Loader.LoadWorkflow(name)                                             │
│    ├── Validator.Validate(workflow)                                          │
│    └── 继承处理 (extends)                                                     │
└──────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 3. Executor 初始化执行上下文                                                   │
│    ├── 创建 ExecutionContext                                                  │
│    ├── 注入内置变量 (Target, Output, threads 等)                              │
│    └── 初始化 Runner                                                          │
└──────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 4. StepDispatcher 调度步骤执行                                                 │
│    ├── 模板渲染 (templateEngine.Render)                                       │
│    ├── 查找合适的 Executor (registry.Find)                                    │
│    └── 执行步骤 (executor.Execute)                                            │
└──────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 5. Runner 执行命令                                                            │
│    ├── HostRunner.Execute (本地)                                              │
│    ├── DockerRunner.Execute (容器)                                            │
│    └── SSHRunner.Execute (远程)                                               │
└──────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│ 6. 结果处理和导出                                                              │
│    ├── StepResult 处理                                                        │
│    ├── Exports 注入上下文                                                      │
│    └── 决策路由 (decision)                                                    │
└──────────────────────────────────────────────────────────────────────────────┘
```

## 决策路由

步骤支持条件分支：

```yaml
decision:
  switch: "{{variable}}"
  cases:
    "value1": { goto: step-a }
    "value2": { goto: step-b }
  default: { goto: fallback }
```

使用 `goto: _end` 终止工作流。

## 平台变量

内置环境检测变量：

- `{{PlatformOS}}` - 操作系统 (linux, darwin, windows)
- `{{PlatformArch}}` - CPU 架构 (amd64, arm64)
- `{{PlatformInDocker}}` - 是否在 Docker 中
- `{{PlatformInKubernetes}}` - 是否在 Kubernetes 中
- `{{PlatformCloudProvider}}` - 云提供商

## 性能优化

1. **Goja VM 池**: 复用 JavaScript 虚拟机实例
2. **模板分片渲染**: 16 分片并发渲染
3. **内存映射 I/O**: 大文件行计数使用 mmap
4. **批量数据库写入**: Write Coordinator 减少 I/O
5. **预编译模板缓存**: 循环条件编译一次并缓存

## 技术栈

| 组件 | 技术 |
|------|------|
| Web 框架 | Fiber |
| ORM | Bun |
| 脚本引擎 | Goja (JavaScript) |
| 定时任务 | gocron |
| 文件监控 | fsnotify |
| 日志 | Zap |
| CLI | Cobra |
| 云原生 | Pulumi |
