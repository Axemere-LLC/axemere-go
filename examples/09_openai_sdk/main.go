// Package main demonstrates using the official openai-go SDK through the Axemere gateway
// in proxy mode. The gateway URL is set as the OpenAI client's base URL so every request
// is routed, attributed, and governed automatically — identical calling code, zero rework.
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/Axemere-LLC/axemere-go/gateway"
)

func demoSingleCompletion(ctx context.Context, client openai.Client) {
	fmt.Println("\n--- Single chat completion ---")
	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("What is 2 + 2? Answer in one sentence."),
		},
		MaxTokens: openai.Int(128),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Response: %s\n", completion.Choices[0].Message.Content)
	fmt.Printf("Model: %s  Tokens: %d\n", completion.Model, completion.Usage.TotalTokens)
}

func demoMultiTurn(ctx context.Context, client openai.Client) {
	fmt.Println("\n--- Multi-turn conversation ---")
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("Name the three primary colors."),
	}

	first, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     openai.ChatModelGPT4oMini,
		Messages:  messages,
		MaxTokens: openai.Int(64),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	reply := first.Choices[0].Message.Content
	fmt.Printf("Round 1 -- Assistant: %s\n", reply)

	messages = append(messages, openai.AssistantMessage(reply))
	messages = append(messages, openai.UserMessage("Which of those do you get by mixing red and blue?"))

	second, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     openai.ChatModelGPT4oMini,
		Messages:  messages,
		MaxTokens: openai.Int(64),
	})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Round 2 -- Assistant: %s\n", second.Choices[0].Message.Content)
}

func main() {
	loadDotEnv(".env")

	cfg, err := gateway.NewConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Trim the trailing slash so openai-go appends /v1/chat/completions correctly.
	proxyBase := strings.TrimRight(cfg.ProxyURL("openai"), "/")
	fmt.Printf("Gateway: %s\n", cfg.GatewayURL)
	fmt.Printf("Workload: %s  Project: %s\n", cfg.WorkloadID, cfg.ProjectID)
	// The gateway token is embedded in the proxy URL path (/k/<token>); never log
	// the raw URL. Print a redacted form for visibility only.
	fmt.Printf("Proxy base URL: %s\n", redactProxyURL(proxyBase))

	client := openai.NewClient(
		option.WithBaseURL(proxyBase),
		// The real API key is held by the gateway — any non-empty value satisfies the SDK.
		option.WithAPIKey("axemere-gateway"),
	)

	ctx := context.Background()

	// Check connectivity before the demos.
	_, connErr := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:     openai.ChatModelGPT4oMini,
		Messages:  []openai.ChatCompletionMessageParamUnion{openai.UserMessage("ping")},
		MaxTokens: openai.Int(1),
	})
	if connErr != nil {
		msg := strings.ToLower(connErr.Error())
		if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") || strings.Contains(msg, "connect:") {
			fmt.Printf("\nGateway not reachable: %v\n", connErr)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
	}

	demoSingleCompletion(ctx, client)
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
