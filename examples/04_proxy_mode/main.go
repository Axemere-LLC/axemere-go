// Package main demonstrates transparent proxy mode via the Axemere Gateway SDK.
// Uses cfg.ProxyURL("openai") to build a base URL with attribution labels encoded
// in the path, then makes a raw *http.Client POST to the OpenAI chat completions
// endpoint through the gateway proxy — no Axemere-specific request format needed.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

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

	// ProxyURL encodes attribution labels in the URL path.
	// Format: {GatewayURL}/proxy/{provider}[/w/{workload}][/p/{project}][/a/{account}][/c/{customer}]/
	//
	// The gateway strips all labeled segments before forwarding to the upstream.
	// Any HTTP client or third-party SDK can use this as its base URL.
	proxyBase := cfg.ProxyURL("openai")
	fmt.Printf("Proxy base URL: %s\n", proxyBase)

	// OpenAI's SDK appends /chat/completions (without /v1) to its base URL,
	// so we append /v1 to the proxy base. The gateway stops parsing attribution
	// labels at the first non-label segment (/v1) and forwards /v1/chat/completions
	// verbatim to api.openai.com.
	endpoint := strings.TrimRight(proxyBase, "/") + "/v1/chat/completions"
	fmt.Printf("POST %s\n\n", endpoint)

	// Build an OpenAI-format request body.
	body := map[string]any{
		"model":      "gpt-4o-mini",
		"max_tokens": 64,
		"messages": []map[string]any{
			{"role": "user", "content": "Say 'proxy mode works' in one sentence."},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		log.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The gateway reads workload attribution from the URL path, so no special
	// headers are required. A dummy API key satisfies SDKs that require one.
	req.Header.Set("Authorization", "Bearer any-value")

	resp, err := httpClient.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
			fmt.Printf("Gateway not reachable: %v\n", err)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
		log.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("read response: %v", err)
	}

	fmt.Printf("HTTP %d\n", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Body: %s\n", respBytes)
		return
	}

	// Parse OpenAI-format response.
	var result map[string]any
	if err := json.Unmarshal(respBytes, &result); err != nil {
		fmt.Printf("parse response: %v\nRaw: %s\n", err, respBytes)
		return
	}

	content := extractOpenAIContent(result)
	if content != "" {
		fmt.Printf("Response: %s\n", content)
	} else {
		fmt.Printf("Raw response: %s\n", respBytes)
	}

	// Governance headers injected by the gateway.
	if recordID := resp.Header.Get("X-MVGC-Record-ID"); recordID != "" {
		fmt.Printf("Record ID (header): %s\n", recordID)
	}
}

// extractOpenAIContent pulls the assistant message text from an OpenAI response.
func extractOpenAIContent(raw map[string]any) string {
	choices, ok := raw["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	msg, ok := choice["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := msg["content"].(string)
	return content
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
