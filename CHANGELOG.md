# Changelog

All notable changes to the Axemere Go SDK are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed
- Requests to `minimax` no longer fail — its upstream host and path were incorrect (`api.minimax.chat` + legacy native path → `api.minimax.io` + `/v1/chat/completions`).
- `anthropic` requests no longer silently return empty content and zero metering — `max_tokens` is now sent automatically (Anthropic rejects requests that omit it), and `system`-role messages are moved out of `messages` into Anthropic's separate `system` field. Upstream provider errors (e.g. a rejected request) are now surfaced as a `GatewayError` instead of reading as an empty success.

### Added
- Support for six previously-missing providers: `azure_openai`, `qwen`, `bytedance`, `stepfun`, `bedrock`, `vertex`.
- `ExecuteRequest.TargetHost` overrides the provider's registry host — required for `azure_openai`, which has no default host.

### Changed
- Internal request labeling (`action.type`) is now consistently `"ai.infer"` for all provider requests (was `"http"`).

---

## [0.1.7] - 2026-07-03

### Fixed
- README now links to the Go-specific docs page (`/docs/sdks/go`) instead of the generic docs root.

---

## [0.1.6] - 2026-06-25

### Added
- `gateway.Client` with `Execute()` and `Stream()` methods for the Axemere AI Gateway Action API (`/v1/actions:execute`), supporting OpenAI-compatible, Anthropic, and Gemini providers.
- `gateway.NewConfig()` reads all settings from `AXEMERE_*` environment variables with functional option overrides (`WithGatewayToken()`, `WithDefaultProvider()`, etc.).
- Proxy mode URL builder (`Config.ProxyURL()`) that produces a drop-in base URL for provider SDKs such as the official Go OpenAI or Anthropic clients.
- `Stream()` returns a `<-chan StreamChunk` channel; the final chunk carries the record ID and metering data from the gateway.
- Per-request and config-level attribution fields (`WorkloadID`, `ProjectID`, `AccountID`, `CustomerID`, `Labels`) forwarded to the gateway for cost allocation and reporting.
- Typed errors (`*PolicyDeniedError`, `*QuotaExceededError`, `*GatewayTimeoutError`) implementing `errors.Is`/`errors.As` unwrapping so callers can match on each failure mode.
- Server-sent event streaming with a 1 MB scanner buffer to handle large provider payloads without truncation.
- Support for 20+ providers via proxy mode: openai, anthropic, gemini, cohere, mistral, groq, deepseek, together, fireworks, perplexity, openrouter, xai, nvidia-nim, upstage, moonshot, minimax, zhipu, and more.
- Requires Go 1.24 or later.

### Fixed
- `gateway`: `Execute()` now correctly extracts response content and model from `result.body` (the gateway's standard response shape) instead of a `provider_response` top-level field that the gateway does not emit.

[Unreleased]: https://github.com/Axemere-LLC/axemere-go/compare/v0.1.7...HEAD
[0.1.7]: https://github.com/Axemere-LLC/axemere-go/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/Axemere-LLC/axemere-go/releases/tag/v0.1.6
