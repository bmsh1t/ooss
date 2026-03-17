# Osmedeus 接口文档

本文档描述 Osmedeus 的核心接口定义，包括内部包接口和 REST API 接口。

## 目录

- [核心类型接口](#核心类型接口)
- [Runner 接口](#runner-接口)
- [Executor 接口](#executor-接口)
- [模板引擎接口](#模板引擎接口)
- [函数注册接口](#函数注册接口)
- [REST API 接口](#rest-api-接口)
- [CLI 命令接口](#cli-命令接口)

---

## 核心类型接口

### Workflow

工作流定义结构体，位于 `internal/core/workflow.go`:

```go
type Workflow struct {
    Kind         WorkflowKind       // module 或 flow
    Name         string             // 工作流名称
    Description  string             // 描述
    Tags         TagList           // 标签列表
    Hidden       bool               // 是否隐藏
    Params       []Param           // 参数定义
    Triggers     []Trigger         // 触发器列表
    Dependencies *Dependencies      // 依赖
    Reports      []Report          // 报告配置
    Preferences  *Preferences      // 执行偏好
    Hooks        *WorkflowHooks    // 钩子函数
    Runner       RunnerType         // 执行器类型
    RunnerConfig *RunnerConfig     // 执行器配置
    Steps        []Step            // 步骤列表 (module 类型)
    Modules      []ModuleRef       // 模块引用列表 (flow 类型)
    Extends      string            // 继承的父工作流
    Override    *WorkflowOverride  // 覆盖配置
}
```

### Step

步骤定义结构体，位于 `internal/core/step.go`:

```go
type Step struct {
    Name         string            // 步骤名称
    Type         StepType          // 步骤类型
    DependsOn    []string          // 依赖步骤
    StepRunner   RunnerType        // 步骤级执行器
    PreCondition string            // 前置条件
    Log          string            // 日志文件
    Timeout      StepTimeout       // 超时时间
    
    // Bash 步骤字段
    Command          string
    Commands         []string
    ParallelCommands []string
    StdFile          string
    
    // Function 步骤字段
    Function          string
    Functions         []string
    ParallelFunctions []string
    
    // Parallel 步骤字段
    ParallelSteps []Step
    
    // Foreach 步骤字段
    Input              string
    Variable           string
    VariablePreProcess string
    Threads            StepThreads
    Step               *Step
    
    // HTTP 步骤字段
    URL         string
    Method      string
    Headers     map[string]string
    RequestBody string
    
    // LLM 步骤字段
    Messages       []LLMMessage
    Tools          []LLMTool
    LLMConfig      *LLMStepConfig
    
    // Agent 步骤字段
    Query             string
    SystemPrompt      string
    AgentTools        []AgentToolDef
    MaxIterations     int
    Memory            *AgentMemoryConfig
    
    // Agent-ACP 步骤字段
    Agent        string
    Cwd          string
    AllowedPaths []string
    ACPConfig    *ACPStepConfig
    
    // 通用字段
    Exports         map[string]string
    OnSuccess       []Action
    OnError         []Action
    Decision        *DecisionConfig
}
```

### ExecutionContext

执行上下文，包含运行时变量和环境:

```go
type ExecutionContext struct {
    WorkflowName  string                 // 工作流名称
    WorkflowKind  WorkflowKind           // 工作流类型
    RunUUID       string                 // 运行 UUID
    Target        string                 // 目标
    Output        string                 // 输出目录
    Params        map[string]string      // 参数
    Vars          map[string]interface{} // 运行时变量
    Config        *config.Config         // 全局配置
    Runner        runner.Runner          // 执行器
    // ... 其他字段
}
```

### StepResult

步骤执行结果:

```go
type StepResult struct {
    StepName      string
    Status        StepStatus
    Output        string
    Error         error
    StartTime     time.Time
    EndTime       time.Time
    Duration      time.Duration
    Exports       map[string]interface{}
    NextStep      string
    LogFile       string
    InlineResults []*StepResult
}
```

---

## Runner 接口

### Runner

命令执行器接口，位于 `internal/runner/runner.go`:

```go
type Runner interface {
    // Execute 执行命令并返回结果
    Execute(ctx context.Context, command string) (*CommandResult, error)
    
    // Setup 准备执行环境
    Setup(ctx context.Context) error
    
    // Cleanup 清理资源
    Cleanup(ctx context.Context) error
    
    // Type 返回执行器类型
    Type() core.RunnerType
    
    // IsRemote 是否远程执行
    IsRemote() bool
    
    // CopyFromRemote 从远程复制文件
    CopyFromRemote(ctx context.Context, remotePath, localPath string) error
    
    // SetPIDCallbacks 设置进程回调
    SetPIDCallbacks(onStart, onEnd PIDCallback)
}
```

### CommandResult

命令执行结果:

```go
type CommandResult struct {
    Output   string // 合并的 stdout 和 stderr
    ExitCode int    // 退出码
    Error    error  // 错误
}
```

### 创建 Runner

```go
func NewRunner(workflow *core.Workflow, binaryPath string) (Runner, error)
```

---

## Executor 接口

### StepExecutorPlugin

步骤执行器插件接口，位于 `internal/executor/plugin.go`:

```go
type StepExecutorPlugin interface {
    // Execute 执行步骤
    Execute(ctx context.Context, step *core.Step, execCtx *core.ExecutionContext) (*core.StepResult, error)
    
    // CanExecute 检查是否可以执行此类型步骤
    CanExecute(step *core.Step) bool
}
```

### PluginRegistry

执行器注册表:

```go
type PluginRegistry struct {
    executors map[core.StepType]StepExecutorPlugin
    mu        sync.RWMutex
}

// 注册执行器
func (r *PluginRegistry) Register(plugin StepExecutorPlugin)

// 查找执行器
func (r *PluginRegistry) Find(stepType core.StepType) (StepExecutorPlugin, error)
```

### StepDispatcher

步骤分发器:

```go
type StepDispatcher struct {
    registry         *PluginRegistry
    templateEngine   template.TemplateEngine
    functionRegistry *functions.Registry
    // ...
}

// 创建分发器
func NewStepDispatcher() *StepDispatcher

// 分发步骤执行
func (d *StepDispatcher) Dispatch(ctx context.Context, step *core.Step, execCtx *core.ExecutionContext) (*core.StepResult, error)
```

---

## 模板引擎接口

### TemplateEngine

模板渲染引擎接口，位于 `internal/template/interface.go`:

```go
type TemplateEngine interface {
    // Render 渲染模板字符串
    Render(template string, ctx map[string]any) (string, error)
    
    // RenderMap 渲染 map 中的所有模板
    RenderMap(m map[string]string, ctx map[string]any) (map[string]string, error)
    
    // RenderSlice 渲染 slice 中的所有模板
    RenderSlice(s []string, ctx map[string]any) ([]string, error)
    
    // RenderSecondary 使用 [[ ]] 分隔符渲染
    RenderSecondary(template string, ctx map[string]any) (string, error)
    
    // HasSecondaryVariable 检查是否包含二级变量
    HasSecondaryVariable(template string) bool
    
    // ExecuteGenerator 执行生成器函数
    ExecuteGenerator(expr string) (string, error)
    
    // RegisterGenerator 注册自定义生成器
    RegisterGenerator(name string, fn GeneratorFunc)
    
    // ExtractVariablesSet 提取变量集合
    ExtractVariablesSet(template string) map[string]struct{}
    
    // RenderLazy 懒加载渲染
    RenderLazy(template string, fullCtx map[string]any) (string, error)
}
```

### BatchRenderer

批量渲染接口:

```go
type BatchRenderer interface {
    TemplateEngine
    
    // RenderBatch 批量渲染
    RenderBatch(requests []RenderRequest, ctx map[string]any) (map[string]string, error)
}
```

### RenderRequest

批量渲染请求:

```go
type RenderRequest struct {
    Key      string // 标识符
    Template string // 模板字符串
}
```

---

## 函数注册接口

### Function

函数定义:

```go
type Function struct {
    Name        string      // 函数名
    Description string      // 描述
    Parameters  []Parameter // 参数
    fn          interface{} // 实际函数
}
```

### Registry

函数注册表:

```go
type Registry struct {
    functions map[string]Function
    vmPool    *VMPool
}

// 创建注册表
func NewRegistry() *Registry

// 注册函数
func (r *Registry) Register(fn Function)

// 调用函数
func (r *Registry) Call(name string, args ...interface{}) (interface{}, error)

// 获取函数列表
func (r *Registry) GetAll() map[string]Function
```

### VMPool

JavaScript VM 池:

```go
type VMPool struct {
    pool chan *goja.Runtime
    // ...
}

// 获取 VM
func (p *VMPool) Get() *goja.Runtime

// 归还 VM
func (p *VMPool) Put(vm *goja.Runtime)
```

---

## REST API 接口

### 基础信息

- 基础路径: `/osm/api/`
- 认证: API Key (通过 Header: `Authorization: Bearer <API_KEY>`)

### 工作流接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/osm/api/workflows` | 列出所有工作流 |
| GET | `/osm/api/workflows/:name` | 获取工作流详情 |
| POST | `/osm/api/workflows/:name/run` | 执行工作流 |

### 运行接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/osm/api/runs` | 列出所有运行 |
| GET | `/osm/api/runs/:uuid` | 获取运行详情 |
| POST | `/osm/api/runs/:uuid/cancel` | 取消运行 |
| GET | `/osm/api/runs/:uuid/steps` | 获取运行步骤 |

### 资产接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/osm/api/assets` | 列出资产 |
| GET | `/osm/api/assets/:id` | 获取资产详情 |

### 调度接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/osm/api/schedules` | 列出调度 |
| POST | `/osm/api/schedules` | 创建调度 |
| PUT | `/osm/api/schedules/:id` | 更新调度 |
| DELETE | `/osm/api/schedules/:id` | 删除调度 |
| POST | `/osm/api/schedules/:id/trigger` | 触发调度 |

### LLM 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/osm/api/llm/chat/completions` | Chat Completions |
| POST | `/osm/api/llm/embeddings` | Embeddings |

### Agent ACP 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/osm/api/agent/chat/completions` | Agent Chat Completions |

### Webhook 接口

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/osm/api/webhook-runs` | 列出 webhook 触发的工作流 |
| GET | `/osm/api/webhook-runs/:uuid/trigger` | 触发工作流 |
| POST | `/osm/api/webhook-runs/:uuid/trigger` | 触发工作流 (POST) |

---

## CLI 命令接口

### 核心命令

```bash
# 运行工作流
osmedeus run -f <flow> -t <target>
osmedeus run -m <module> -t <target>
osmedeus run -m <module> -t <target> --timeout 2h
osmedeus run -m <module> -t <target> --repeat

# 工作流管理
osmedeus workflow list
osmedeus workflow show <name>
osmedeus workflow validate <name>

# 服务器
osmedeus server
osmedeus server --master

# 云管理
osmedeus cloud create --instances N
osmedeus cloud list
osmedeus cloud destroy <id>
osmedeus cloud run -f <flow> -t <target> --instances N

# 安装
osmedeus install base --preset
osmedeus install workflow --preset
osmedeus install binary --name <name>

# Agent
osmedeus agent "your prompt"
osmedeus agent --agent codex "prompt"

# 分布式 Worker
osmedeus worker join
osmedeus worker status
osmedeus worker queue list
osmedeus worker queue run --concurrency 5

# 快照
osmedeus snapshot export <workspace>
osmedeus snapshot import <source>
osmedeus snapshot list

# 资产
osmedeus assets
osmedeus assets -w <workspace>
osmedeus assets --source httpx --type web

# 函数
osmedeus func list
osmedeus func e 'log_info("{{target}}")'
```

---

## 配置文件结构

### osm-settings.yaml

```yaml
server:
  address: ":8080"
  license: ""

storage:
  data: "~/.osmedeus/storages"
  workspace: "~/.osmedeus/workspaces"
  logs: "~/.osmedeus/logs"

workflows:
  path: "~/.osmedeus/workflows"
  exclude: []

binaries:
  path: "~/.osmedeus/bin"

env:
  HTTP_PROXY: ""
  HTTPS_PROXY: ""

cloud:
  provider: ""
  token: ""
```

### 工作流 YAML

```yaml
name: example-module
kind: module
description: Example module

params:
  - name: target
    required: true

steps:
  - name: scan
    type: bash
    command: echo {{target}}
```
