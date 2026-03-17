# Osmedeus 项目文档

本目录包含 Osmedeus 安全自动化工作流引擎的完整项目文档。

## 文档索引

### 核心文档

| 文档 | 说明 |
|------|------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | 系统架构文档，详细介绍各组件的设计和交互 |
| [INTERFACES.md](./INTERFACES.md) | 接口定义文档，包括所有核心接口和 API |
| [DEVELOPER_GUIDE.md](./DEVELOPER_GUIDE.md) | 开发者指南，包含开发环境设置和代码规范 |

## 文档概述

### ARCHITECTURE.md

系统架构文档涵盖：

- 项目概述和设计理念
- 整体架构分层 (CLI/API → Executor → Runner → Core Services)
- 核心概念 (WorkflowKind, StepType, RunnerType, TriggerType)
- 各模块详解 (executor, runner, template, functions, parser, database, scheduler, distributed, cloud)
- 执行流程和数据流
- 决策路由机制
- 性能优化策略
- 技术栈清单

### INTERFACES.md

接口定义文档涵盖：

- 核心类型接口 (Workflow, Step, ExecutionContext)
- Runner 接口定义和实现
- Executor 插件接口和注册机制
- 模板引擎接口 (TemplateEngine, BatchRenderer)
- 函数注册接口 (Registry, VMPool)
- REST API 端点完整列表
- CLI 命令参考
- 配置文件结构

### DEVELOPER_GUIDE.md

开发者指南涵盖：

- 开发环境设置步骤
- 项目目录结构说明
- 构建和测试命令
- 添加新功能教程 (步骤类型、Runner、CLI 命令、API 端点、函数)
- 代码规范和最佳实践
- 常见开发任务示例

## 相关资源

- [官方文档](https://osmedeus.readthedocs.io)
- [GitHub 仓库](https://github.com/j3ssie/osmedeus)
- [CLAUDE.md](../CLAUDE.md) - AI 助手指南
- [HACKING.md](../HACKING.md) - 高级开发文档
