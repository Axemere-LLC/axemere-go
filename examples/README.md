# Axemere AI Gateway — Go Examples

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](../LICENSE)

Eight standalone programs covering the gateway's core capabilities. Each compiles and runs independently against a local or managed gateway.

## Setup

```bash
cp .env.example .env
# Edit .env to match your gateway
go run ./examples/01_basic_chat/
```

Or run all examples and see a pass/fail summary:

```bash
bash examples/run_all.sh
# Skip specific examples:  --skip 5,6
# Run only specific ones:  --only 1,2,3
```

## Examples

| # | Program | What it demonstrates |
|---|---------|----------------------|
| 01 | `01_basic_chat` | `client.Execute()` against OpenAI and Anthropic; `*PolicyDeniedError` when `project_id` is omitted |
| 02 | `02_multi_provider` | Same question to both providers; `cfg.SetDefaults()` to switch provider mid-loop; cost comparison table |
| 03 | `03_streaming` | `client.Stream()` channel drain; token-by-token output; `context.WithTimeout` cancellation |
| 04 | `04_proxy_mode` | `cfg.ProxyURL("openai")` URL construction; raw `*http.Client` POST to proxy path; response parsing |
| 05 | `05_delegation` | Ed25519 key generation via `crypto/ed25519`; `mvgc.delegation.v2` wire token; scope enforcement; budget cap |
| 06 | `06_governance_outcomes` | HTTP 202 approval-required (+ admin-API polling); 403 policy deny; 429 rate-limit with Retry-After |
| 07 | `07_path_attribution` | Workload, project, account, customer, and label attribution through `ExecuteRequest`; proxy-URL path encoding |
| 08 | `08_routing_modes` | Path-prefix routing via `ProxyURL`; explicit `X-MVGC-Target-Host` header override; Azure graceful skip |
| 09 | `09_openai_sdk` | Official `openai-go` SDK pointed at `cfg.ProxyURL("openai")`; single completion; multi-turn history |
| 10 | `10_anthropic_sdk` | Official `anthropic-sdk-go` pointed at `cfg.ProxyURL("anthropic")`; single message; multi-turn history |

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `AXEMERE_GATEWAY_URL` | Yes | Gateway base URL (e.g. `http://localhost:7080`) |
| `AXEMERE_WORKLOAD_ID` | Yes | Workload identifier for attribution |
| `AXEMERE_PROJECT_ID` | No | Project identifier for attribution |
| `AXEMERE_ACCOUNT_ID` | No | Account identifier for attribution |
| `AXEMERE_CUSTOMER_ID` | No | Customer identifier for attribution |
| `AXEMERE_GATEWAY_TOKEN` | Managed only | Bearer token for managed gateway auth |
| `AXEMERE_AZURE_ENDPOINT` | Example 08 | Azure OpenAI hostname (e.g. `myresource.openai.azure.com`) |
| `MVGC_ADMIN_TOKEN` | Example 06 | Gateway admin token for approval polling |

## What's not here (and why)

These Python examples have no Go equivalent in this set:

- **LangChain integration** — LangChain is Python/TypeScript only; no official Go port exists.
- **LlamaIndex integration** — Python/TypeScript only.
- **Gemini SDK** — `google.golang.org/genai` exists but the gateway's explicit API (`/v1/actions:execute`) and proxy mode cover Gemini without a provider-specific wrapper; see `01_basic_chat` and `04_proxy_mode`.
- **Cohere SDK** — Same rationale; use `01_basic_chat` with `provider: "cohere"`.
- **Azure OpenAI SDK** — Azure routing is shown in `08_routing_modes` via `X-MVGC-Target-Host`; no separate SDK wrapper is needed in Go.
- **Instructor / structured output** — No official Go port of Instructor; use `encoding/json` with the response content directly.
- **Async patterns** — Go's goroutines and channels are the native async model; `03_streaming` demonstrates this via the channel-based `client.Stream()`.
- **Chain/RAG pipelines** — Framework-level patterns (LangChain chains, LlamaIndex pipelines) have no idiomatic Go equivalent; implement directly with the `gateway.Client` and stdlib.
