# Axemere AI Gateway — Go SDK

Go client for the [Axemere AI Gateway](https://axemere.ai).

## Install

```bash
go get github.com/axemere-llc/axemere-go/gateway
```

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/axemere-llc/axemere-go/gateway"
)

func main() {
    cfg := gateway.ConfigFromEnv() // reads AXEMERE_GATEWAY_URL, AXEMERE_WORKLOAD_TOKEN
    client := gateway.NewClient(cfg)

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

## Examples

See [`examples/`](examples/) for 10 runnable programs.

```bash
cd examples/01_basic_chat
AXEMERE_GATEWAY_URL=http://localhost:7080 AXEMERE_WORKLOAD_TOKEN=your-token go run .
```

## Requirements

- Go 1.21+
- A running Axemere AI Gateway (`AXEMERE_GATEWAY_URL`)
- A workload token (`AXEMERE_WORKLOAD_TOKEN`)

## License

MIT — see [LICENSE](LICENSE).
