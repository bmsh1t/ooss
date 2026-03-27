# Osmedeus

<p align="center">
  <a href="https://www.osmedeus.org"><img alt="Osmedeus" src="https://raw.githubusercontent.com/osmedeus/assets/main/osm-logo-with-white-border.png" height="140" /></a>
  <br />
  <strong>Osmedeus - A Modern Orchestration Engine for Security</strong>

  <p align="center">
  <a href="https://docs.osmedeus.org/"><img src="https://img.shields.io/badge/Documentation-0078D4?style=for-the-badge&logo=GitBook&logoColor=39ff14&labelColor=black&color=black"></a>
  <a href="https://docs.osmedeus.org/donation/"><img src="https://img.shields.io/badge/Sponsors-0078D4?style=for-the-badge&logo=GitHub-Sponsors&logoColor=39ff14&labelColor=black&color=black"></a>
  <a href="https://twitter.com/OsmedeusEngine"><img src="https://img.shields.io/badge/%40OsmedeusEngine-0078D4?style=for-the-badge&logo=Twitter&logoColor=39ff14&labelColor=black&color=black"></a>
  <a href="https://discord.gg/gy4SWhpaPU"><img src="https://img.shields.io/badge/Discord%20Server-0078D4?style=for-the-badge&logo=Discord&logoColor=39ff14&labelColor=black&color=black"></a>
  <a href="https://github.com/j3ssie/osmedeus/releases"><img src="https://img.shields.io/github/release/j3ssie/osmedeus?style=for-the-badge&labelColor=black&color=2fc414&logo=Github"></a>
  </p>
</p>

## What is Osmedeus?

[Osmedeus](https://www.osmedeus.org) is a security focused declarative orchestration engine that simplifies complex workflow automation into auditable YAML definitions, complete with encrypted data handling, secure credential management, and sandboxed execution.

Built for both beginners and experts, it delivers powerful, composable automation without sacrificing the integrity and safety of your infrastructure.

## Key Features

- **Declarative YAML Workflows** - Define pipelines with hooks, decision routing, module exclusion, and conditional branching across multiple runners (host, Docker, SSH)
- **Distributed Execution** - Redis-based master-worker pattern with queue system, webhook triggers, and file sync across workers
- **Rich Function Library** - 80+ utility functions including nmap integration, tmux sessions, SSH execution, TypeScript/Python scripting, SARIF parsing, and CDN/WAF classification
- **Local Knowledge Base** - Ingest local documents (`pdf`, `txt`, `md`, `json`, `jsonl`, `html`, `epub`, `docx`, and more), search them from CLI/API, and synthesize scan findings back into reusable workspace/public knowledge layers
- **Independent Vector Knowledge DB** - Store reusable semantic knowledge in a standalone `vector-kb.sqlite`, auto-index on `kb ingest` / `kb learn`, and let workflows query it directly
- **Campaign Batch Operations** - Create grouped queued runs with shared strategy metadata, aggregated target status, failed-target rerun, and optional high-risk deep-scan escalation
- **Vulnerability Lifecycle Center** - Manage vulnerabilities through `new`, `triaged`, `verified`, `false_positive`, `retest`, and `closed` states with AI verdicts, analyst notes, retest tasks, workspace risk boards, and evidence/status timelines
- **Attack Chain Workbench API** - Persist AI attack-chain outputs as queryable reports, expose summary/detail APIs, generate execution checklists, and keep visualization artifacts linked to the same report with execution-ready recommendations
- **Superdomain AI Workflow Family** - `superdomain-extensive-ai-optimized`, `superdomain-extensive-ai-stable`, `superdomain-extensive-ai-hybrid`, and `superdomain-extensive-ai-lite` now share a cleaner AI closure: validated findings, attack-chain visualization where enabled, targeted rescan, and post-run knowledge auto-learning
- **Event-Driven Scheduling** - Cron, file-watch, and event triggers with filtering, deduplication, and delayed task queues
- **Agentic LLM Steps** - Tool-calling agent loops with sub-agent orchestration, memory management, and structured output; plus ACP subprocess agents (Claude Code, Codex, OpenCode, Gemini)
- **Cloud Infrastructure** - Provision and run scans across DigitalOcean, AWS, GCP, Linode, and Azure with cost controls and automatic cleanup
- **Rich CLI Interface** - Interactive database queries, bulk function evaluation, workflow linting, progress bars, and comprehensive usage examples
- **REST API & Web UI** - Full API server with webhook triggers, database queries, and embedded dashboard for visualization

See [Documentation Page](https://docs.osmedeus.org/) for more details.

## Installation

```bash
curl -sSL http://www.osmedeus.org/install.sh | bash
```

See [Quickstart](https://docs.osmedeus.org/quickstart/) for quick setup and [Installation](https://docs.osmedeus.org/installation/) for advanced configurations.

| CLI Usage | Web UI Assets | Workflow Visualization |
|-----------|--------------|-----------------|
| ![CLI Usage](https://raw.githubusercontent.com/osmedeus/assets/refs/heads/main/demo-images/cli-run-with-verbose-output.png) | ![Web UI Assets](https://raw.githubusercontent.com/osmedeus/assets/refs/heads/main/demo-images/web-ui-assets.png) | ![Workflow Visualization](https://raw.githubusercontent.com/osmedeus/assets/refs/heads/main/demo-images/web-ui-workflow.png) |

## Quick Start

```bash
# Run a module workflow
osmedeus run -m recon -t example.com

# Run a flow workflow
osmedeus run -f general -t example.com

# Multiple targets with concurrency
osmedeus run -m recon -T targets.txt -c 5

# Dry-run mode (preview)
osmedeus run -f general -t example.com --dry-run

# Start API server
osmedeus serve

# List available workflows
osmedeus workflow list

# Query discovered assets
osmedeus assets -w example.com                          # List assets for workspace
osmedeus assets --stats                                 # Show unique technologies, sources, types
osmedeus assets --source httpx --type web --json        # Filter and output as JSON

# Query database tables
osmedeus db list --table runs
osmedeus db list --table event_logs --search "nuclei"

# Evaluate utility functions
osmedeus func eval 'log_info("hello")'
osmedeus func eval -e 'http_get("https://example.com")' -T targets.txt -c 10

# Platform variables available in eval
osmedeus func eval 'log_info("OS: " + PlatformOS + ", Arch: " + PlatformArch)'

# Install from preset repositories
osmedeus install base --preset
osmedeus install base --preset --keep-setting   # preserve existing osm-settings.yaml
osmedeus install workflow --preset

# Exclude modules from flow execution
osmedeus run -f general -t example.com -x portscan
osmedeus run -f general -t example.com -X vuln    # Fuzzy exclude by substring

# Worker queue system
osmedeus worker queue new -f general -t example.com   # Queue for later
osmedeus worker queue run --concurrency 5              # Process queue

# Local knowledge base
osmedeus kb ingest --path ./notes -w example.com --recursive
osmedeus kb search --query "jwt bypass" -w example.com
osmedeus kb docs -w example.com
osmedeus kb learn -w example.com
osmedeus kb export -w example.com --output ./knowledge-index.txt
osmedeus kb vector index -w example.com
osmedeus kb vector search --query "jwt bypass" -w example.com
osmedeus kb vector stats
osmedeus kb vector doctor -w example.com
osmedeus kb vector rebuild -w example.com
osmedeus kb vector purge -w example.com
osmedeus kb vector sync -w example.com

# AI-heavy recon workflows
osmedeus run -f superdomain-extensive-ai-stable -t example.com
osmedeus run -f superdomain-extensive-ai-hybrid -t example.com
osmedeus run -f superdomain-extensive-ai-optimized -t example.com
osmedeus run -f superdomain-extensive-ai-lite -t example.com

# Worker management
osmedeus worker status                          # Show workers
osmedeus worker eval -e 'ssh_exec("host", "whoami")'  # Eval with distributed hooks

# Run an ACP agent interactively
osmedeus agent "analyze this codebase"
osmedeus agent --agent codex "explain main.go"
osmedeus agent --list

# Show all usage examples
osmedeus --usage-example
```

## Knowledge Base and Vector Workflow Usage

You can now extend the local knowledge base with your own documents and have `superdomain-extensive-ai-optimized`, `superdomain-extensive-ai-stable`, `superdomain-extensive-ai-hybrid`, and `superdomain-extensive-ai-lite` consume that knowledge during semantic search.

The practical storage layout is now split into two layers:

- Main DB: `knowledge_documents` / `knowledge_chunks` stay in the primary Osmedeus database as the source of truth
- Independent vector DB: semantic embeddings are stored in a separate SQLite file, defaulting to `{{base_folder}}/knowledge/vector-kb.sqlite`

### Supported document types

- `txt`, `md`, `markdown`, `log`
- `yaml`, `yml`, `csv`
- `json`, `jsonl`
- `html`, `htm`
- `epub`
- `doc`, `docx`
- `pdf`
- `pptx`, `xlsx`

### Local dependencies

- `docling`
  - Required for `pdf`, `docx`, `pptx`, and `xlsx` ingestion
- `antiword`
  - Required for legacy `.doc` ingestion

### Optional dependencies

- at least one configured `llm.llm_providers` entry with an OpenAI-compatible `/embeddings` endpoint
  - Enables vectorkb indexing/search and AI workflow semantic retrieval
- a matching `knowledge_vector.default_provider` / `knowledge_vector.default_model`
  - Keeps vectorkb index/search bound to the same provider/model pair unless you override them explicitly

### Vector knowledge config

Add or verify this section in `~/osmedeus-base/osm-settings.yaml`:

```yaml
knowledge_vector:
  enabled: true
  db_path: "{{base_folder}}/knowledge/vector-kb.sqlite"
  default_provider: openai
  default_model: text-embedding-3-small
  auto_index_on_ingest: true
  auto_index_on_learn: true
  top_k: 20
  hybrid_weight: 0.7
  keyword_weight: 0.3
  batch_size: 32
  max_indexing_chunks: 5000
```

Reference files:

- `public/presets/superdomain-ai-kb.example.yaml`
- `docs/knowledge-kb-layout.md`
- `docs/knowledge-kb-ingest-guide.md`

### CLI workflow

1. Ingest your documents into a workspace-scoped knowledge base:

```bash
osmedeus kb ingest --path /data/kb/books --workspace example.com --recursive
osmedeus kb ingest --path /data/kb/playbook.pdf --workspace example.com
```

2. Verify the content is searchable:

```bash
osmedeus kb docs -w example.com
osmedeus kb search --query "authentication bypass" -w example.com
osmedeus kb vector search --query "authentication bypass" -w example.com
osmedeus kb vector doctor -w example.com
```

3. Optionally synthesize scan findings back into the same workspace knowledge:

```bash
osmedeus kb learn -w example.com --include-ai
```

When `knowledge_vector.auto_index_on_ingest=true` or `knowledge_vector.auto_index_on_learn=true`, Osmedeus will refresh `vector-kb.sqlite` automatically after these commands succeed.

If the vector DB drifts over time, use:

```bash
osmedeus kb vector doctor -w example.com
osmedeus kb vector sync -w example.com
osmedeus kb vector purge -w example.com
osmedeus kb vector rebuild -w example.com
```

4. Run an AI workflow that will automatically use the same knowledge workspace during semantic search:

```bash
osmedeus run -f superdomain-extensive-ai-hybrid -t example.com
```

### Using a custom knowledge workspace

By default, the AI semantic-search modules use `knowledgeWorkspace={{TargetSpace}}`. If you want to reuse a shared document corpus across different targets, pass a params file:

```yaml
# params.yaml
knowledgeWorkspace: shared-websec
includeKnowledgeBase: true
maxKnowledgeChunks: 400
```

```bash
osmedeus run -f superdomain-extensive-ai-hybrid -t example.com -P params.yaml
```

### What happens inside the workflow

- `kb export` turns stored knowledge chunks into a line-oriented corpus for retrieval
- `ai-semantic-search` now:
  - performs direct `kb vector search` hits against the standalone `vector-kb.sqlite`
  - merges those results with direct `kb search` keyword hits as fallback/supplement
  - supports layered retrieval with primary/shared/global knowledge workspaces
  - keeps vectorkb bound to the selected provider/model pair
  - feeds both direct knowledge hits and vector recall candidates into the downstream semantic-search agent
- `ai-semantic-search-hybrid` now:
  - uses vectorkb vector recall plus `kb search` keyword recall
  - avoids Chroma/Python runtime-install behavior
  - fuses vector hits, keyword hits, and local scan corpus hits through jq-based ranking
- `ai-apply-decision` normalizes the AI output into `applied-ai-decision-{{TargetSpace}}.json`, `dynamic-config.yaml`, and `scan-env.sh`, so downstream modules consume one stable decision layer
- `targeted-rescan` now feeds verified follow-up hits back into the main nuclei result set instead of leaving them isolated in a side artifact
- `ai-post-followup-coordination` aggregates retest, operator queue, campaign handoff/create, and rescan outputs into `followup-decision-{{TargetSpace}}.json` and `.md`
- `ai-retest-queue` now forwards normalized `previous_followup_*` state into queued reruns, and the pre-scan / apply-decision / intelligent-analysis stages can recover that context even when only queue params remain
- the default follow-up workflow for retest/campaign execution is `web-analysis`
- `ai-knowledge-autolearn` now generates structured learned knowledge documents such as workspace summary, verified findings, false-positive samples, and AI insights

### API usage

```bash
curl -X POST http://localhost:8002/osm/api/knowledge/vector/index \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"workspace":"example.com"}'

curl -X POST http://localhost:8002/osm/api/knowledge/vector/search \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"workspace":"example.com","query":"authentication bypass","limit":10}'

curl http://localhost:8002/osm/api/knowledge/vector/stats \
  -H "Authorization: Bearer $TOKEN"
```

## Recent Backend Additions

- **Knowledge Base APIs and CLI**
  - Ingest local files into a searchable workspace-scoped knowledge store
  - Search/list stored documents from CLI and REST API
  - Auto-learn scan results back into the knowledge base for later reuse
  - Maintain a standalone `vector-kb.sqlite` with direct CLI/API semantic search
  - Knowledge search now defaults to `workspace + public` layered recall when a workspace is provided
  - Learned knowledge now preserves source confidence, sample type, target-type tags, and shared `public` storage
- **Campaign APIs**
  - `GET /osm/api/campaigns`
  - `POST /osm/api/campaigns`
  - `GET /osm/api/campaigns/:id`
  - `GET /osm/api/campaigns/:id/report`
  - `GET /osm/api/campaigns/:id/export`
  - `GET /osm/api/campaigns/:id/profiles`
  - `PUT /osm/api/campaigns/:id/profiles/:name`
  - `DELETE /osm/api/campaigns/:id/profiles/:name`
  - `POST /osm/api/campaigns/:id/rerun-failed`
  - `POST /osm/api/campaigns/:id/deep-scan`
  - CLI now includes `osmedeus campaign report <id>`, `osmedeus campaign export <id> --format csv|json`, and `osmedeus campaign profile list|save|delete`
  - report/export now support `risk/status/trigger` target-row filters and `high-risk`, `recovered`, `failed` export presets
  - report/export now support post-filter `offset/limit` pagination with explicit page metadata
  - report/export now support operator-friendly ordering overrides such as `target`, `latest_run`, and `open_high_risk`
  - report/export now support reusable saved profiles with `--profile` or `?profile=...`, plus saved default export format
  - campaign target status now includes `attack_chain_summary` beside `vuln_summary`
  - high-risk deep-scan escalation can now be triggered by operational critical/high-impact attack-chain signals, not only raw vulnerability severities
- **Vulnerability Lifecycle APIs**
  - `GET /osm/api/vulnerabilities/board`
  - `PATCH /osm/api/vulnerabilities/:id`
  - `POST /osm/api/vulnerabilities/:id/retest`
  - vulnerability creation now supports merge-on-create with fingerprint dedup and evidence history
  - vulnerability list now supports `fingerprint_key` and `source_run_uuid` filters
  - vulnerability detail now resolves evidence timeline, status timeline, retest timeline, related runs, related asset rows, and related attack-chain matches
- **Attack Chain Workbench APIs**
  - `GET /osm/api/attack-chains`
  - `GET /osm/api/attack-chains/:id`
  - `POST /osm/api/attack-chains/import`
  - `POST /osm/api/attack-chains/:id/queue-retest`
  - `POST /osm/api/attack-chains/:id/queue-deep-scan`
  - attack-chain import now backfills matching vulnerability rows with reverse `attack_chain_ref`, merged `report_refs`, and merged `related_assets`
  - attack-chain retest queue now persists the selected chain linkage onto queued vulnerabilities
  - attack-chain detail now returns execution-ready counts, queue recommendations, and recommended deep-scan targets
- **Superdomain AI workflow closure**
  - `stable` and `hybrid` now generate persisted attack-chain visualization artifacts in addition to the attack-chain report
  - `stable`, `hybrid`, `optimized`, and `lite` now run knowledge auto-learning at the end of the workflow when `enableKnowledgeLearning=true`
  - All four workflows now emit a normalized `applied-ai-decision` artifact and a post-execution `followup-decision` artifact for downstream reuse and reporting
  - Retest, operator queue, campaign handoff, and targeted rescan are now folded back into the same decision chain instead of remaining isolated outputs
  - Queued reruns now preserve manual-first, high-confidence, campaign-create, and retest follow-up signals through normalized `previous_followup_*` params when the previous `followup-decision` file is unavailable
  - Retest lifecycle now propagates source run UUIDs so post-retest state can converge back to `verified`, `closed`, or `triaged`
  - Knowledge auto-learning now writes higher-signal learned knowledge back into the KB for future retrieval
  - Attack-chain ACP input is now pre-curated to prefer verified findings and exclude false-positive nodes from chain generation
- **Verification snapshot**
  - Current source builds successfully with `make build`
  - focused handler tests now cover vulnerability evidence/timeline enrichment, attack-chain reverse linkage, and campaign attack-chain-aware deep-scan selection
  - focused workflow tests now cover queued `previous_followup_*` recovery across `ai-pre-scan-decision`, `ai-pre-scan-decision-acp`, `ai-apply-decision`, `ai-intelligent-analysis`, and `ai-retest-queue`
  - Local real-API regression passed for campaign, vulnerability, and attack-chain flows on a clean no-auth server instance
  - `superdomain-extensive-ai-stable`, `superdomain-extensive-ai-hybrid`, `superdomain-extensive-ai-optimized`, `superdomain-extensive-ai-lite`, and `ai-knowledge-autolearn` all pass workflow validation
  - Remaining full-suite test blockers are environment-dependent: local socket listeners, usable `tmux`, and local `uv` execution support

## Docker

```bash
# Show help
docker run --rm j3ssie/osmedeus:latest --help

# Run a scan
docker run --rm -v $(pwd)/output:/root/workspaces-osmedeus \
    j3ssie/osmedeus:latest run -f general -t example.com
```

For more CLI usage and example commands, refer to the [CLI Reference](https://docs.osmedeus.org/getting-started/cli).

## High-Level Architecture

```plaintext
┌───────────────────────────────────────────────────────────────────────────┐
│                   Osmedeus Orchestration Engine                           │
├───────────────────────────────────────────────────────────────────────────┤
│  ENTRY POINTS                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────────┐                │
│  │   CLI    │  │ REST API │  │Scheduler │  │ Distributed │                │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────┬───────┘                │
│       └─────────────┴─────────────┴──────────────┘                        │
│                              │                                            │
│                              ▼                                            │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │ CONFIG ──▶ PARSER ──▶ EXECUTOR ──▶ STEP DISPATCHER ──▶ RUNNER       │  │
│  │                          │                                          │  │
│  │  Step Executors: bash | function | parallel | foreach | remote-bash │  │
│  │                  http | llm | agent | agent-acp | SARIF/SAST       │  │
│  │  Hooks: pre_scan_steps → [main steps] → post_scan_steps             │  │
│  │                          │                                          │  │
│  │  Runners: HostRunner | DockerRunner | SSHRunner                     │  │
│  │  Queue: DB + Redis polling → dedup → concurrent execution           │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────────────────┘
```

For more information about the architecture, refer to the [Architecture Documentation](https://docs.osmedeus.org/architecture).

## Roadmap and Status

The high-level ambitious plan for the project, in order:

|  #  | Step                                                                        |  Status |
| :-: | --------------------------------------------------------------------------- |  :----: |
|  1  | Osmedeus Engine reforged with a next-generation architecture                |    ✅   |
|  2  | Flexible workflows and step types                                           |    ✅   |
|  3  | Event-driven architectural model and the different trigger event categories |    ✅   |
|  4  | Beautiful UI for visualize results and workflow diagram                     |    ✅   |
|  5  | Rewriting the workflow to adapt to new architecture and syntax              |    ✅   |
|  6  | Testing more utility functions like notifications                           |    ✅   |
|  7  | SAST integration with SARIF parsing (Semgrep, Trivy, etc.)                  |    ✅   |
|  8  | Cloud integration, which supports running the scan on the cloud provider.   |    🚧   |
|  9  | Generate diff reports showing new/removed/unchanged assets between runs.    |    ❌   |
|  10 | Adding step type from cloud provider that can be run via serverless         |    ❌   |
|  N  | Fancy features (to be discussed later)                                      |    ❌   |
## Documentation

| Topic                | Link                                                                                                     |
|----------------------|----------------------------------------------------------------------------------------------------------|
| Getting Started      | [docs.osmedeus.org/getting-started](https://docs.osmedeus.org/getting-started) |
| CLI Usage & Examples | [docs.osmedeus.org/getting-started/cli](https://docs.osmedeus.org/getting-started/cli) |
| Writing Workflows    | [docs.osmedeus.org/workflows/overview](https://docs.osmedeus.org/workflows/overview) |
| Event-Driven Triggers| [docs.osmedeus.org/advanced/event-driven](https://docs.osmedeus.org/advanced/event-driven) |
| Deployment           | [docs.osmedeus.org/deployment](https://docs.osmedeus.org/deployment) |
| Architecture         | [docs.osmedeus.org/concepts/architecture](https://docs.osmedeus.org/concepts/architecture) |
| Development          | [docs.osmedeus.org/development](https://docs.osmedeus.org/development) and [HACKING.md](HACKING.md) |
| Extending Osmedeus   | [docs.osmedeus.org/development/extending-osmedeus](https://docs.osmedeus.org/development/extending-osmedeus)   |
| Full Documentation   | [docs.osmedeus.org](https://docs.osmedeus.org) |

## Disclaimer

**Osmedeus** is designed to execute arbitrary code and commands from user supplied input via CLI, API, and workflow definitions. This flexibility is intentional and central to how the engine operates.

Please refer to the [⚠️ Security Warning](https://docs.osmedeus.org/others/security-warning) page for more information on how to stay safe.

**Think twice before you:**
- Run workflows downloaded from untrusted sources
- Execute commands or scans against targets you don't own or have permission to test
- Use workflows without reviewing their contents first

You are responsible for what you run. Always review workflow YAML files before execution, especially those obtained from third parties.

## License

Osmedeus is made with ♥ by [@j3ssie](https://twitter.com/j3ssie) and it is released under the MIT license.
