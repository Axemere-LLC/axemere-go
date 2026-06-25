// Package main demonstrates querying multiple LLM providers through the Axemere Gateway.
// Sends the same question to OpenAI and Anthropic, prints a comparison table with
// response content, token counts, and total cost across both calls.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Axemere-LLC/axemere-go/gateway"
)

const question = "In one sentence, what is the capital of France?"

func main() {
	loadDotEnv(".env")

	cfg, err := gateway.NewConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = "http://localhost:7080"
	}

	fmt.Printf("Gateway:  %s\n", cfg.GatewayURL)
	fmt.Printf("Workload: %s  Project: %s\n\n", cfg.WorkloadID, cfg.ProjectID)
	fmt.Printf("Question: %s\n\n", question)

	client, err := gateway.NewClient(cfg)
	if err != nil {
		log.Fatalf("client: %v", err)
	}

	type result struct {
		provider string
		model    string
		content  string
		tokensIn int
		tokensOut int
		costUSD  string
		err      error
	}

	providers := []struct {
		name  string
		model string
	}{
		{"openai", "gpt-4o-mini"},
		{"anthropic", "claude-haiku-4-5-20251001"},
	}

	results := make([]result, 0, len(providers))
	ctx := context.Background()

	for _, p := range providers {
		// SetDefaults updates the config to use this provider/model as default.
		cfg.SetDefaults(
			gateway.WithDefaultProvider(p.name),
			gateway.WithDefaultModel(p.model),
		)

		resp, err := client.Execute(ctx, gateway.ExecuteRequest{
			// Provider and Model are empty: falls back to cfg defaults set above.
			WorkloadID: cfg.WorkloadID,
			ProjectID:  cfg.ProjectID,
			AccountID:  cfg.AccountID,
			CustomerID: cfg.CustomerID,
			Messages: []gateway.Message{
				{Role: "user", Content: question},
			},
			Params: map[string]any{"max_tokens": 64},
		})
		if err != nil {
			if isConnRefused(err) {
				fmt.Printf("Gateway not reachable: %v\n", err)
				fmt.Println("Is the gateway running? Try: docker compose up -d")
				os.Exit(0)
			}
			results = append(results, result{provider: p.name, model: p.model, err: err})
			continue
		}
		results = append(results, result{
			provider:  p.name,
			model:     p.model,
			content:   resp.Content,
			tokensIn:  resp.Metering.TokensIn,
			tokensOut: resp.Metering.TokensOut,
			costUSD:   resp.Metering.CostUSD,
		})
	}

	// Print comparison table.
	fmt.Println("┌─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("│ %-12s %-28s %8s %9s %10s\n", "Provider", "Model", "In", "Out", "Cost USD")
	fmt.Println("├─────────────────────────────────────────────────────────────────────────────")

	var totalIn, totalOut int
	for _, r := range results {
		if r.err != nil {
			fmt.Printf("│ %-12s %-28s  ERROR: %v\n", r.provider, r.model, r.err)
			continue
		}
		totalIn += r.tokensIn
		totalOut += r.tokensOut
		cost := r.costUSD
		if cost == "" {
			cost = "—"
		}
		fmt.Printf("│ %-12s %-28s %8d %9d %10s\n",
			r.provider, r.model, r.tokensIn, r.tokensOut, cost)
	}
	fmt.Println("├─────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("│ %-41s %8d %9d\n", "TOTAL", totalIn, totalOut)
	fmt.Println("└─────────────────────────────────────────────────────────────────────────────")

	fmt.Println()
	for _, r := range results {
		if r.err != nil {
			continue
		}
		fmt.Printf("[%s/%s]\n%s\n\n", r.provider, r.model, r.content)
	}
}

func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") || strings.Contains(s, "no such host")
}

// loadDotEnv reads KEY=VALUE lines from the named file and calls os.Setenv for each.
// Lines starting with # and blank lines are skipped. Missing file is silently ignored.
// Only sets variables that are not already present in the environment.
func loadDotEnv(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
