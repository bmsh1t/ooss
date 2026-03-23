# API Security Testing Skill

## Overview
APIs (REST, GraphQL, SOAP) are common attack vectors. This skill covers API-specific testing techniques.

## Testing Methodology

### 1. REST API Testing

#### Common Endpoints
```
/api/v1/users
/api/v1/users/{id}
/api/login
/api/admin
/api/config
/api/docs
/swagger.json
/openapi.json
/api/v1/search?q=
```

#### Testing Techniques
```bash
# Directory discovery
ffuf -w wordlist.txt -u https://target.com/api/FUZZ

# Parameter enumeration
ffuf -w params.txt -u https://target.com/api/users?FUZZ=test

# HTTP methods
curl -X OPTIONS https://target.com/api/users -I

# Headers testing
curl -X GET https://target.com/api/users -H "X-Forwarded-For: 127.0.0.1"
```

### 2. Authentication Testing

#### JWT Testing
```bash
# Decode JWT
echo "eyJ..." | cut -d. -f1,2 | base64 -d

# JWT brute force
jwt_tool https://target.com/api token -C -w wordlist.txt

# Modify and resign
jwt_tool https://target.com/api token -T -HS256:"secret"
```

#### OAuth Testing
```bash
# Discover OAuth endpoints
/.well-known/openid-configuration
/oauth/.well-known/jwks.json

# Test redirect_uri
/test?redirect_uri=https://evil.com
```

### 3. GraphQL Testing

```bash
# Introspection query
curl -X POST https://target.com/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{__schema{queryType{fields{name}}}}"}'

# Field discovery
{"query":"{__type(name:\"User\"){fields{name}}}"}'

# Mutation testing
{"query":"mutation{createUser(input:{name:\"test\"}){id}}"}
```

### 4. Mass Assignment
```bash
# Test for hidden parameters
{"role": "admin"}
{"is_admin": true}
{"user_id": 999}
```

### 5. Rate Limiting Bypass
```bash
# IP rotation
for i in {1..100}; do
  curl -H "X-Forwarded-For: 10.0.$i.1" https://target.com/api/login
done

# Header manipulation
X-Originating-IP: 127.0.0.1
X-Forwarded-For: 127.0.0.1
X-Real-IP: 127.0.0.1
```

### 6. Tools
```bash
# API testing framework
amass enum -api                        # API discovery
ffuf -w wordlist.txt -u https://target.com/api/FUZZ

# Specialized scanners
nuclei -t api/                          # Nuclei API templates
```

## Testing Checklist
- [ ] Enumerate API endpoints
- [ ] Test authentication mechanisms
- [ ] Test authorization (IDOR)
- [ ] Test rate limiting
- [ ] Test input validation
- [ ] Test for mass assignment
- [ ] Test GraphQL/REST specific issues
- [ ] Document API documentation gaps
