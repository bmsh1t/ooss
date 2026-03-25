# Knowledge KB Layout

Recommended three-layer layout for practical use:

```text
/data/kb/
├── global/
│   ├── web-basics/
│   ├── auth/
│   ├── api/
│   ├── business-logic/
│   ├── file-upload/
│   ├── ssrf-ssti-rce/
│   ├── waf-bypass/
│   ├── cloud/
│   └── ebooks/
├── shared-web/
│   ├── products/
│   ├── bypass-cases/
│   ├── operator-playbooks/
│   ├── bugchain-patterns/
│   └── review-checklists/
└── targets/
    ├── example.com/
    │   ├── notes/
    │   ├── reports/
    │   ├── screenshots/
    │   ├── exported-docs/
    │   └── retest-history/
    └── another-target/
```

Layer guidance:

- `global`
  - universal playbooks, ebooks, product-agnostic exploit notes, auth/api/business-logic/file handling guidance
- `shared-web`
  - team-validated patterns, product weaknesses, bypass cases, checklists, bug-chain templates
- `targets/<target>`
  - target-specific notes, historical reports, retest findings, screenshots, exported docs

Naming guidance:

- good: `vendor-product-auth-bypass.md`
- good: `graphql-introspection-abuse-playbook.md`
- good: `example.com-retest-2026-03.md`
- avoid: `1.md`
- avoid: `new notes.md`
- avoid: `temp.pdf`

Recommended document structure:

```md
# Title

## Scenario
## Applicable Targets
## Fingerprints / Preconditions
## Attack Surface
## Test Steps
## Common Bypasses
## Success Signals
## Failure Signals
## Retest Advice
## Risks
```

Operational guidance:

- keep `global` curated, not noisy
- put team-proven experience into `shared-web`
- allow `targets/<target>` to be messier because it serves current operations
- store failed attempts separately if you want them preserved without polluting main retrieval
