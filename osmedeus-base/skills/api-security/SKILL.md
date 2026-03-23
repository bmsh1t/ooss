---
name: api-security-testing
description: API安全测试的专业技能和方法论
version: 1.0.0
---

# API安全测试

## 概述

API安全测试是确保API接口安全性的重要环节。本技能提供API安全测试的方法、工具和最佳实践。

## 测试范围

### 1. 认证和授权
- Token有效性验证
- Token过期处理
- 权限控制
- 角色权限验证

### 2. 输入验证
- 参数类型验证
- 数据长度限制
- 特殊字符处理
- SQL注入防护
- XSS防护

### 3. 业务逻辑
- 工作流验证
- 状态转换
- 并发控制
- 业务规则

### 4. 错误处理
- 错误信息泄露
- 堆栈跟踪
- 敏感信息暴露

## 测试方法

### 1. API发现
```bash
# 使用目录扫描
gobuster dir -u https://target.com -w api-wordlist.txt

# 分析JavaScript文件
# 查找API端点定义
```

### 2. 认证测试
```http
# 测试无效Token
GET /api/user
Authorization: Bearer invalid_token
```

**JWT测试：**
```bash
# 使用jwt_tool
python jwt_tool.py <JWT_TOKEN>

# 测试算法混淆
python jwt_tool.py <JWT_TOKEN> -X a

# 测试密钥暴力破解
python jwt_tool.py <JWT_TOKEN> -C -d wordlist.txt
```

### 3. 授权测试
**水平权限：**
```http
# 用户A访问用户B的资源
GET /api/user/123
```

**垂直权限：**
```http
# 普通用户访问管理员接口
GET /api/admin/users
```

### 4. 输入验证测试
**SQL注入：**
```json
POST /api/search
{
  "query": "test' OR '1'='1"
}
```

**命令注入：**
```json
POST /api/execute
{
  "command": "ping; id"
}
```

## 工具使用

### Postman
1. 导入API文档
2. 设置认证
3. 创建测试用例
4. 运行自动化测试

### Burp Suite
1. 配置API端点
2. 设置认证
3. 运行主动扫描
4. 分析结果

### OWASP ZAP
```bash
zap-cli quick-scan http://target.com/api
```

## 常见漏洞

### 1. 认证绕过
- 弱Token生成
- Token可预测
- Token不验证签名

### 2. 权限提升
- 直接对象引用
- 未验证资源所有权

### 3. 信息泄露
- 详细错误信息
- 堆栈跟踪
- 敏感数据

### 4. 注入漏洞
- SQL注入
- NoSQL注入
- 命令注入
- XXE

## 测试清单

### 认证测试
- [ ] Token有效性验证
- [ ] Token过期处理
- [ ] 弱Token检测
- [ ] Token重放攻击

### 授权测试
- [ ] 水平权限测试
- [ ] 垂直权限测试
- [ ] 角色权限验证
- [ ] 资源访问控制

### 输入验证
- [ ] SQL注入测试
- [ ] XSS测试
- [ ] 命令注入测试
- [ ] XXE测试
- [ ] 参数污染

## 防护措施

1. **认证** - 使用强Token、实现Token刷新、验证Token签名
2. **授权** - 基于角色的访问控制、资源所有权验证，最小权限原则
3. **输入验证** - 参数类型验证、数据长度限制，白名单验证
4. **错误处理** - 统一错误响应、不泄露详细信息、记录错误日志
5. **速率限制** - 实现API限流、防止暴力破解、监控异常请求
