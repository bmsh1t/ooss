---
name: idor-testing
description: IDOR不安全的直接对象引用测试的专业技能和方法论
version: 1.0.0
---

# IDOR不安全的直接对象引用测试

## 概述

IDOR（Insecure Direct Object Reference）是一种访问控制漏洞，当应用程序直接使用用户提供的输入来访问资源，而未验证用户是否有权限访问该资源时发生。

## 漏洞原理

**危险代码示例：**
```php
$file = file_get_contents('/files/' . $_GET['id'] . '.pdf');
```

## 测试方法

### 1. 识别直接对象引用
**常见资源类型：**
- 用户ID、文件ID/文件名、订单ID、文档ID、账户ID、记录ID

**常见位置：**
- URL参数、POST数据、Cookie值、HTTP头、文件路径

### 2. 枚举测试
**顺序ID测试：**
```
/user?id=1
/user?id=2
/user?id=3
```

**UUID测试：**
```
/user?id=550e8400-e29b-41d4-a716-446655440000
```

### 3. 水平权限测试
```
当前用户ID: 100
测试: /user?id=101
/user?id=102
```

### 4. 垂直权限测试
```
/admin/users?id=1
/admin/settings
/admin/logs
```

## 绕过技术

### ID混淆
**Base64编码：** `MTIz` (代表123)

### 参数名混淆
```
/user?id=123
/user?uid=123
/user?user_id=123
```

### HTTP方法绕过
```
GET /user/123
POST /user/123
PUT /user/123
PATCH /user/123
```

## 工具使用

### Burp Suite
1. 拦截请求，发送到Intruder
2. 标记ID参数，使用数字序列
3. 观察响应差异

## 防护措施

1. **访问控制验证** - 验证用户是否有权访问该资源
2. **间接对象引用** - 使用映射表而非直接ID
3. **基于角色的访问控制** - 验证用户角色
4. **资源所有权验证** - 验证资源是否属于当前用户
5. **使用不可预测的标识符** - 使用UUID替代顺序ID
