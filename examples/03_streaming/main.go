// Package main demonstrates streaming LLM responses via the Axemere Gateway SDK.
// Uses client.Stream() to receive token deltas as they arrive, prints each chunk
// without a newline, then shows context cancellation via context.WithTimeout.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/axemere-llc/axemere-go/gateway"
)

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

	client, err := gateway.NewClient(cfg)
	if err != nil {
		log.Fatalf("client: %v", err)
	}

	// --- Full streaming response ---
	fmt.Println("=== Streaming response (OpenAI gpt-4o-mini) ===")
	demoStream(client, cfg, context.Background(), "Count from 1 to 10, one number per line.")

	// --- Context cancellation demo ---
	fmt.Println("\n=== Streaming with 2s timeout (context cancellation) ===")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	demoStream(client, cfg, ctx, "Write a detailed 500-word essay about the history of computing.")
}

func demoStream(client *gateway.Client, cfg *gateway.Config, ctx context.Context, prompt string) {
	req := gateway.ExecuteRequest{
		Provider:   "openai",
		Model:      "gpt-4o-mini",
		WorkloadID: cfg.WorkloadID,
		ProjectID:  cfg.ProjectID,
		AccountID:  cfg.AccountID,
		CustomerID: cfg.CustomerID,
		Messages: []gateway.Message{
			{Role: "user", Content: prompt},
		},
		Params: map[string]any{"max_tokens": 256},
	}

	ch, err := client.Stream(ctx, req)
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
		fmt.Printf("Stream error: %v\n", err)
		return
	}

	fmt.Print("Response: ")
	for chunk := range ch {
		if chunk.Err != nil {
			if strings.Contains(chunk.Err.Error(), "context deadline exceeded") ||
				strings.Contains(chunk.Err.Error(), "context canceled") {
				fmt.Printf("\n[stream cancelled after timeout: %v]\n", chunk.Err)
			} else {
				fmt.Printf("\n[stream error: %v]\n", chunk.Err)
			}
			return
		}
		if chunk.IsFinal {
			fmt.Println() // newline after the last token
			if chunk.Metering != nil {
				fmt.Printf("Metering:  tokens_in=%d tokens_out=%d cost_usd=%s\n",
					chunk.Metering.TokensIn, chunk.Metering.TokensOut, chunk.Metering.CostUSD)
			}
			if chunk.RecordID != "" {
				fmt.Printf("Record ID: %s\n", chunk.RecordID)
			}
			return
		}
		fmt.Print(chunk.Content)
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
