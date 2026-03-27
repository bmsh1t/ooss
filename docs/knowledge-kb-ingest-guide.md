# Knowledge KB Ingest Guide

Recommended import flow:

1. Import global knowledge:

```bash
osmedeus kb ingest --path /data/kb/global --workspace global --recursive
```

2. Import shared team knowledge:

```bash
osmedeus kb ingest --path /data/kb/shared-web --workspace shared-web --recursive
```

3. Import target-specific knowledge:

```bash
osmedeus kb ingest --path /data/kb/targets/example.com --workspace example.com --recursive
```

4. Verify storage and vector health:

```bash
osmedeus kb docs -w example.com
osmedeus kb vector doctor -w example.com
osmedeus kb vector search --query "auth bypass admin api" -w example.com
```

5. Run the optimized workflow with layered knowledge:

```bash
osmedeus run -f superdomain-extensive-ai-optimized -t example.com \
  -p knowledgeWorkspace=example.com \
  -p sharedKnowledgeWorkspace=shared-web \
  -p globalKnowledgeWorkspace=global
```

6. If vector state drifts:

```bash
osmedeus kb vector sync -w example.com
osmedeus kb vector doctor -w example.com
```

7. If a workspace becomes dirty:

```bash
osmedeus kb vector purge -w example.com
osmedeus kb vector rebuild -w example.com
```

Recommended ongoing practice:

- keep target-specific findings in `knowledgeWorkspace`
- move reusable lessons into `sharedKnowledgeWorkspace`
- keep universal exploitation material in `globalKnowledgeWorkspace`
- after real operations, ingest or summarize the useful parts back into the proper layer
