// Package main demonstrates using the official anthropic-sdk-go SDK through the Axemere
// gateway in proxy mode. Setting the client's base URL to cfg.ProxyURL("anthropic") means
// every request is routed, governed, and attributed — zero changes to calling code.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/Axemere-LLC/axemere-go/gateway"
)

func extractText(blocks []anthropic.ContentBlockUnion) string {
	for _, block := range blocks {
		if tb := block.AsText(); tb.Text != "" {
			return tb.Text
		}
	}
	return "[no text content]"
}

func demoSingleMessage(ctx context.Context, client anthropic.Client) {
	fmt.Println("\n--- Single message ---")
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5_20251001,
		MaxTokens: 128,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("What is the capital of France? Answer in one sentence.")),
		},
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Response: %s\n", extractText(msg.Content))
	fmt.Printf("Model: %s  Stop reason: %s\n", msg.Model, msg.StopReason)
}

func demoMultiTurn(ctx context.Context, client anthropic.Client) {
	fmt.Println("\n--- Multi-turn conversation ---")
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("Name three planets in the solar system.")),
	}

	first, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5_20251001,
		MaxTokens: 64,
		Messages:  messages,
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	reply := extractText(first.Content)
	fmt.Printf("Round 1 -- Assistant: %s\n", reply)

	messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(reply)))
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock("Which of those is closest to the Sun?")))

	second, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5_20251001,
		MaxTokens: 64,
		Messages:  messages,
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Round 2 -- Assistant: %s\n", extractText(second.Content))
}

func main() {
	loadDotEnv(".env")

	cfg, err := gateway.NewConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Trim trailing slash so anthropic-sdk-go appends /v1/messages correctly.
	proxyBase := strings.TrimRight(cfg.ProxyURL("anthropic"), "/")
	fmt.Printf("Gateway: %s\n", cfg.GatewayURL)
	fmt.Printf("Workload: %s  Project: %s\n", cfg.WorkloadID, cfg.ProjectID)
	// The gateway token is embedded in the proxy URL path (/k/<token>); never log
	// the raw URL. Print a redacted form for visibility only.
	fmt.Printf("Proxy base URL: %s\n", redactProxyURL(proxyBase))

	client := anthropic.NewClient(
		option.WithBaseURL(proxyBase),
		// The real API key is held by the gateway — any non-empty value satisfies the SDK.
		option.WithAPIKey("axemere-gateway"),
	)

	ctx := context.Background()

	// Check connectivity before the demos.
	_, connErr := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5_20251001,
		MaxTokens: 1,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("ping")),
		},
	})
	if connErr != nil {
		msg := strings.ToLower(connErr.Error())
		if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") || strings.Contains(msg, "connect:") {
			fmt.Printf("\nGateway not reachable: %v\n", connErr)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
	}

	demoSingleMessage(ctx, client)
	demoMultiTurn(ctx, client)
}

// redactProxyURL masks the token segment (/k/<token>) in a proxy URL so it can
// be logged safely. The gateway token lives in the URL path and must never be
// written to logs, terminals, or error messages in full.
func redactProxyURL(u string) string {
	parts := strings.Split(u, "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "k" {
			parts[i+1] = "***"
		}
	}
	return strings.Join(parts, "/")
}

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
		val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
