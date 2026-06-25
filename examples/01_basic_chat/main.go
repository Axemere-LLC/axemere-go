// Package main demonstrates basic governed LLM chat via the Axemere SDK explicit API.
// Sends one message each to OpenAI and Anthropic using client.Execute(), prints
// content and metering, then shows PolicyDeniedError when project_id is missing.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Axemere-LLC/axemere-go/gateway"
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

	ctx := context.Background()

	// --- OpenAI ---
	fmt.Println("=== OpenAI gpt-4o-mini ===")
	resp, err := client.Execute(ctx, gateway.ExecuteRequest{
		Provider:   "openai",
		Model:      "gpt-4o-mini",
		WorkloadID: cfg.WorkloadID,
		ProjectID:  cfg.ProjectID,
		AccountID:  cfg.AccountID,
		CustomerID: cfg.CustomerID,
		Messages: []gateway.Message{
			{Role: "user", Content: "Say 'hello from Axemere Gateway' in one sentence."},
		},
		Params: map[string]any{"max_tokens": 64},
	})
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
		fmt.Printf("OpenAI error: %v\n", err)
	} else {
		fmt.Printf("Response:  %s\n", resp.Content)
		printMetering(resp.Metering)
		fmt.Printf("Record ID: %s\n\n", resp.RecordID)
	}

	// --- Anthropic ---
	fmt.Println("=== Anthropic claude-haiku-4-5-20251001 ===")
	resp, err = client.Execute(ctx, gateway.ExecuteRequest{
		Provider:   "anthropic",
		Model:      "claude-haiku-4-5-20251001",
		WorkloadID: cfg.WorkloadID,
		ProjectID:  cfg.ProjectID,
		AccountID:  cfg.AccountID,
		CustomerID: cfg.CustomerID,
		Messages: []gateway.Message{
			{Role: "user", Content: "Say 'hello from Axemere Gateway' in one sentence."},
		},
		Params: map[string]any{"max_tokens": 64},
	})
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			os.Exit(0)
		}
		fmt.Printf("Anthropic error: %v\n", err)
	} else {
		fmt.Printf("Response:  %s\n", resp.Content)
		printMetering(resp.Metering)
		fmt.Printf("Record ID: %s\n\n", resp.RecordID)
	}

	// --- Policy denial demo: omit project_id to trigger missing-attribution deny ---
	fmt.Println("=== Policy denial demo: omit project_id ===")
	_, err = client.Execute(ctx, gateway.ExecuteRequest{
		Provider:   "openai",
		Model:      "gpt-4o-mini",
		WorkloadID: cfg.WorkloadID,
		ProjectID:  "", // intentionally empty
		Messages: []gateway.Message{
			{Role: "user", Content: "hello"},
		},
		Params: map[string]any{"max_tokens": 32},
	})
	if err != nil {
		var denied *gateway.PolicyDeniedError
		if errors.As(err, &denied) {
			fmt.Printf("PolicyDeniedError: %s\n", denied.Reason)
			if denied.RecordID != "" {
				fmt.Printf("Record ID: %s\n", denied.RecordID)
			}
		} else if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			os.Exit(0)
		} else {
			fmt.Printf("Error: %v\n", err)
		}
	} else {
		fmt.Println("(no denial — policy may not require project_id for this workload)")
	}
}

func printMetering(m gateway.Metering) {
	if m.CostUSD == "" && m.TokensIn == 0 {
		return
	}
	fmt.Printf("Metering:  tokens_in=%d tokens_out=%d cost_usd=%s\n",
		m.TokensIn, m.TokensOut, m.CostUSD)
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
