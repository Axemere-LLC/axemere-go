# Axemere AI Gateway — Go SDK

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Add governance, cost controls, and audit trails to every AI API call — without changing your application code.

**[Get started at axemere.ai →](https://axemere.ai)**

---

## What is Axemere?

The [Axemere AI Gateway](https://axemere.ai) sits between your application and AI providers (OpenAI, Anthropic, Gemini, and more). It enforces spend budgets, policy rules, and delegation controls, and records every request to an append-only audit ledger.

- **Explicit API** — typed request/response structs, no magic
- **Cost controls** — per-workload and per-project spend limits enforced at the gateway
- **Policy engine** — allow/deny rules based on model, provider, context, and labels
- **Audit ledger** — tamper-evident record of every request, token count, and cost
- **Delegation** — issue scoped tokens to users or agents with budget and model limits

## Install

```bash
go get github.com/Axemere-LLC/axemere-go/gateway
```

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/Axemere-LLC/axemere-go/gateway"
)

func main() {
    cfg, err := gateway.NewConfig() // reads AXEMERE_GATEWAY_URL, AXEMERE_GATEWAY_TOKEN
    if err != nil {
        log.Fatal(err)
    }
    client, err := gateway.NewClient(cfg)
    if err != nil {
        log.Fatal(err)
    }

    result, err := client.Execute(context.Background(), gateway.ExecuteRequest{
        Provider: "openai",
        Model:    "gpt-4o-mini",
        Messages: []gateway.Message{
            {Role: "user", Content: "Hello"},
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(result.Content)
    fmt.Printf("Cost: $%s\n", result.Metering.CostUSD)
}
```

## Get a gateway

- **Self-Hosted** — team features, policy engine, full audit ledger. [Get started →](https://axemere.ai/docs/get-started/cp-connected)
- **Managed** — fully hosted, multi-tenant, SOC 2 ready. [Get started →](https://axemere.ai/docs/get-started/managed-gateway)

## Examples

See [`examples/`](examples/) for 10 runnable programs covering all major providers, streaming, delegation, governance, and proxy mode.

```bash
cd examples/01_basic_chat
AXEMERE_GATEWAY_URL=http://localhost:7080 AXEMERE_GATEWAY_TOKEN=your-token go run .
```

## Requirements

- Go 1.24+
- A running Axemere AI Gateway (`AXEMERE_GATEWAY_URL`)
- A gateway token (`AXEMERE_GATEWAY_TOKEN`)

## Links

- Website: [axemere.ai](https://axemere.ai)
- Docs: [axemere.ai/docs](https://axemere.ai/docs/sdks/go)
- Issues: [github.com/Axemere-LLC/axemere-go/issues](https://github.com/Axemere-LLC/axemere-go/issues)

## License

MIT — see [LICENSE](LICENSE).
