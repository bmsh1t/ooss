# LLM Responses Endpoint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a minimal OpenAI-compatible `POST /osm/api/llm/v1/responses` endpoint without weakening the existing chat completions path.

**Architecture:** Keep this as a thin compatibility layer in the HTTP handler. Accept a focused subset of Responses API fields, normalize them into the existing chat-completions request model, reuse the current provider execution path, then wrap the result back into a Responses-style JSON payload.

**Tech Stack:** Go, Fiber, existing Osmedeus LLM handler and HTTP tests with `httptest`.

---

### Task 1: Lock the compatibility contract with narrow handler tests

**Files:**
- Create: `pkg/server/handlers/llm_test.go`

- [ ] Add a handler test that posts `input` as a string and verifies the upstream payload is converted into chat messages.
- [ ] Add a handler test that posts `input` as a message array and verifies `developer`/`instructions` normalization still succeeds.
- [ ] Add a validation test that posts an unsupported `input` type and expects HTTP 400.

### Task 2: Implement the compatibility layer

**Files:**
- Modify: `pkg/server/handlers/llm.go`
- Modify: `pkg/server/server.go`

- [ ] Add request/response structs for the new `/responses` endpoint.
- [ ] Normalize `input`, `instructions`, `tools`, and token settings into the existing `LLMChatRequest`.
- [ ] Reuse the existing provider validation and `executeLLMChatRequest` flow.
- [ ] Wrap the result into a Responses-style payload with `output`, `output_text`, and `usage`.

### Task 3: Document and verify narrowly

**Files:**
- Modify: `docs/api/llm.mdx`
- Modify: `pkg/server/handlers/llm.go`
- Modify: `pkg/server/server.go`
- Create: `pkg/server/handlers/llm_test.go`

- [ ] Document the new endpoint and call out that this first version focuses on text + function tools compatibility.
- [ ] Run `go test ./pkg/server/handlers -run 'TestLLMResponses'`.
- [ ] Run `go test ./pkg/server/handlers`.
