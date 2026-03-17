# Osmedeus 开发者指南

本文档为 Osmedeus 项目贡献者提供开发指导。

## 目录

- [开发环境设置](#开发环境设置)
- [项目结构](#项目结构)
- [构建和测试](#构建和测试)
- [添加新功能](#添加新功能)
- [代码规范](#代码规范)

---

## 开发环境设置

### 前置要求

- Go 1.21+
- Git
- Docker (用于测试)
- Make

### 克隆项目

```bash
git clone https://github.com/j3ssie/osmedeus.git
cd osmedeus
```

### 依赖管理

```bash
go mod tidy
```

### 本地构建

```bash
make build
```

构建产物: `bin/osmedeus`

---

## 项目结构

```
osmedeus/
├── cmd/
│   └── osmedeus/          # 主程序入口
├── pkg/
│   ├── cli/               # CLI 命令实现
│   └── server/            # REST API 服务器
├── internal/
│   ├── core/              # 核心类型定义
│   ├── executor/          # 执行引擎
│   ├── runner/            # 运行器 (host/docker/ssh)
│   ├── template/          # 模板引擎
│   ├── functions/         # 函数系统
│   ├── parser/            # YAML 解析器
│   ├── database/          # 数据库层
│   ├── scheduler/         # 调度器
│   ├── distributed/       # 分布式执行
│   ├── cloud/             # 云基础设施
│   ├── snapshot/          # 快照系统
│   └── ...                # 其他工具模块
├── public/                # 静态资源
├── docs/                  # API 文档
└── test/                  # 测试数据
```

### 核心目录说明

| 目录 | 说明 |
|------|------|
| `internal/core` | 工作流、步骤、触发器等核心类型定义 |
| `internal/executor` | 步骤执行器 (bash, function, agent 等) |
| `internal/runner` | 命令执行环境 (本地、Docker、SSH) |
| `internal/template` | 模板渲染引擎 |
| `internal/functions` | JavaScript 函数库 |
| `internal/parser` | YAML 工作流解析 |
| `internal/database` | 数据持久化 |
| `internal/scheduler` | 定时和事件调度 |

---

## 构建和测试

### 构建命令

```bash
# 构建到 bin/osmedeus
make build

# 跨平台构建
make build-all

# 安装到 $GOBIN
make install
```

### 测试命令

```bash
# 单元测试 (无外部依赖)
make test-unit

# 集成测试 (需要 Docker)
make test-integration

# E2E CLI 测试 (需要构建)
make test-e2e

# SSH E2E 测试
make test-e2e-ssh

# API E2E 测试 (需要 Redis + seeded DB)
make test-e2e-api

# 运行特定包测试
go test -v ./internal/functions/...

# 运行单个测试
go test -v -run TestName ./...
```

### 开发命令

```bash
# 代码格式化
make fmt

# Lint 检查
make lint

# 代码生成
make tidy

# 运行 (构建并执行)
make run

# 生成 Swagger 文档
make swagger
```

---

## 添加新功能

### 添加新的步骤类型

1. 在 `internal/core/types.go` 添加常量:

```go
const (
    // 现有类型
    StepTypeBash       StepType = "bash"
    // 添加新类型
    StepTypeNewType    StepType = "new-type"
)
```

2. 创建执行器:

```go
// internal/executor/newtype_executor.go
package executor

type NewTypeExecutor struct {
    templateEngine template.TemplateEngine
}

func NewNewTypeExecutor(engine template.TemplateEngine) *NewTypeExecutor {
    return &NewTypeExecutor{
        templateEngine: engine,
    }
}

func (e *NewTypeExecutor) Execute(ctx context.Context, step *core.Step, execCtx *core.ExecutionContext) (*core.StepResult, error) {
    // 实现执行逻辑
}

func (e *NewTypeExecutor) CanExecute(step *core.Step) bool {
    return step.Type == core.StepTypeNewType
}
```

3. 在 `internal/executor/dispatcher.go` 注册执行器:

```go
d.registry.Register(NewNewTypeExecutor(engine))
```

### 添加新的 Runner

1. 在 `internal/core/types.go` 添加常量:

```go
const (
    RunnerTypeHost   RunnerType = "host"
    // 添加新类型
    RunnerTypeK8s    RunnerType = "k8s"
)
```

2. 实现 Runner 接口:

```go
// internal/runner/k8s_runner.go
package runner

type K8sRunner struct {
    // 配置
}

func (r *K8sRunner) Execute(ctx context.Context, command string) (*CommandResult, error) {
    // 实现
}

func (r *K8sRunner) Setup(ctx context.Context) error {
    // 实现
}

func (r *K8sRunner) Cleanup(ctx context.Context) error {
    // 实现
}

func (r *K8sRunner) Type() core.RunnerType {
    return core.RunnerTypeK8s
}

func (r *K8sRunner) IsRemote() bool {
    return true
}

func (r *K8sRunner) CopyFromRemote(ctx context.Context, remotePath, localPath string) error {
    // 实现
}

func (r *K8sRunner) SetPIDCallbacks(onStart, onEnd PIDCallback) {
    // 实现
}
```

3. 在 `internal/runner/runner.go` 的 `NewRunner` 函数中添加工厂逻辑。

### 添加新的 CLI 命令

1. 创建命令文件:

```go
// pkg/cli/newcommand.go
package cli

import (
    "github.com/spf13/cobra"
)

var newCommandCmd = &cobra.Command{
    Use:   "newcommand",
    Short: "Description of new command",
    RunE: func(cmd *cobra.Command, args []string) error {
        // 实现
        return nil
    },
}

func init() {
    rootCmd.AddCommand(newCommandCmd)
    newCommandCmd.Flags().BoolP("flag", "f", false, "Flag description")
}
```

### 添加新的 API 端点

1. 在 `pkg/server/handlers/` 创建处理器:

```go
// pkg/server/handlers/new_handler.go
package handlers

func NewHandler(c *fiber.Ctx) error {
    // 实现
    return c.JSON(fiber.Map{"status": "ok"})
}
```

2. 在 `pkg/server/server.go` 注册路由:

```go
app.Get("/osm/api/newendpoint", handlers.NewHandler)
```

### 添加新的函数

1. 实现函数:

```go
// internal/functions/new_functions.go
package functions

func myNewFunction(runtime *goja.Runtime, args ...goja.Value) goja.Value {
    // 实现
    return goja.Undefined()
}
```

2. 注册函数:

```go
// internal/functions/goja_runtime.go 或 registry.go
r.Register(Function{
    Name:        "my_new_function",
    Description: "Description",
    fn:          myNewFunction,
})
```

---

## 代码规范

### 命名规范

- **变量/函数**: 驼峰命名 (`myVariable`, `ExecuteCommand`)
- **常量**: 驼峰或全大写下划线 (`StepTypeBash`, `MAX_SIZE`)
- **结构体**: 帕斯卡命名 (`WorkflowRunner`)
- **包**: 简短小写 (`executor`, `runner`)

### 错误处理

- 优先使用 `fmt.Errorf` 和 `%w` 包装错误
- 避免忽略 `error` 返回值
- 在边缘情况下使用 `panic` (仅限严重错误)

### 日志

- 使用 Zap 日志库
- 结构化日志: `log.Info("message", zap.String("key", "value"))`
- 避免使用 `fmt.Print*`

### 测试

- 单元测试使用 `testing` 包
- 测试文件: `*_test.go`
- 使用 Table-Driven Tests 模式

```go
func TestExample(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"case1", "input1", "expected1"},
        {"case2", "input2", "expected2"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := MyFunction(tt.input)
            if result != tt.expected {
                t.Errorf("expected %v, got %v", tt.expected, result)
            }
        })
    }
}
```

### 并发

- 使用 `sync.RWMutex` 保护共享资源
- 使用 `context.Context` 传递取消信号
- 避免 goroutine 泄漏: 确保退出路径

### 导入顺序

```go
import (
    // 标准库
    "fmt"
    "time"
    
    // 第三方库
    "github.com/spf13/cobra"
    "go.uber.org/zap"
    
    // 内部包
    "github.com/j3ssie/osmedeus/v5/internal/core"
)
```

---

## 常见开发任务

### 创建新的工作流模块

工作流存储在 `~/.osmedeus/workflows/` 目录:

```yaml
name: my-module
kind: module
description: My custom security scan module

params:
  - name: target
    required: true
    type: string

steps:
  - name: reconnaissance
    type: bash
    command: |
      echo "Scanning {{target}}"
      # your tool here
    exports:
      result: "{{output}}"
```

### 修改配置

配置文件位于 `~/.osmedeus/osm-settings.yaml`:

```yaml
server:
  address: ":8080"

storage:
  workspace: "~/.osmedeus/workspaces"
```

### 调试执行

```bash
# 启用调试日志
osmedeus run -m <module> -t <target> -v

# Dry run (不实际执行)
osmedeus run -m <module> -t <target> --dry-run
```

---

## 相关文档

- [ARCHITECTURE.md](./ARCHITECTURE.md) - 系统架构
- [INTERFACES.md](./INTERFACES.md) - 接口定义
- [CLAUDE.md](../CLAUDE.md) - AI 助手指南
- [HACKING.md](../HACKING.md) - 高级开发文档
