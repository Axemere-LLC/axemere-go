// Package main demonstrates the three gateway target routing modes.
// Mode 1: path-prefix routing via cfg.ProxyURL("openai") — zero-config for known providers.
// Mode 2: explicit X-MVGC-Target-Host header — escape hatch for any host.
// Mode 3: Azure OpenAI via cfg.ProxyURL("azure_openai") — skip gracefully if not configured.
//
// Routing priority (highest to lowest):
//   X-MVGC-Target-Host header  >  /proxy/{provider}/ path prefix  >  400 error
package main

import (
	"bufio"
	"bytes"
	"context"
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

	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	demoPathPrefix(ctx, cfg, httpClient)
	demoExplicitTargetHost(ctx, cfg, httpClient)
	demoAzure(ctx, cfg, httpClient)
}

// demoPathPrefix uses /proxy/openai/ as the base URL. The gateway resolves the
// target host from the provider registry — no X-MVGC-Target-Host header needed.
func demoPathPrefix(ctx context.Context, cfg *gateway.Config, httpClient *http.Client) {
	fmt.Println("=== Mode 1: Path-prefix routing (/proxy/openai/) ===")
	proxyBase := cfg.ProxyURL("openai")
	// OpenAI expects /v1/chat/completions; the gateway stops parsing attribution
	// labels at /v1 and forwards the rest verbatim to api.openai.com.
	endpoint := strings.TrimRight(proxyBase, "/") + "/v1/chat/completions"
	fmt.Printf("SDK base_url -> %s  =>  api.openai.com\n", proxyBase)
	fmt.Printf("POST %s\n", endpoint)

	body := map[string]any{
		"model":      "gpt-4o-mini",
		"max_tokens": 64,
		"messages":   []map[string]any{{"role": "user", "content": "Say 'path-prefix routing works' in one sentence."}},
	}
	encoded, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer any-value")

	resp, err := httpClient.Do(req)
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	printOpenAIResponse(resp)
	fmt.Println()
}

// demoExplicitTargetHost passes X-MVGC-Target-Host to override path-based resolution.
// This is the escape hatch for any host not in the provider registry: custom
// OpenAI-compatible endpoints, self-hosted models, or any dynamic hostname.
func demoExplicitTargetHost(ctx context.Context, cfg *gateway.Config, httpClient *http.Client) {
	fmt.Println("=== Mode 2: Explicit X-MVGC-Target-Host header ===")
	targetHost := "api.openai.com"
	endpoint := strings.TrimRight(cfg.GatewayURL, "/") + "/v1/chat/completions"
	fmt.Printf("Header: X-MVGC-Target-Host: %s  (explicit override)\n", targetHost)
	fmt.Printf("POST %s\n", endpoint)

	body := map[string]any{
		"model":      "gpt-4o-mini",
		"max_tokens": 64,
		"messages":   []map[string]any{{"role": "user", "content": "Say 'explicit routing works' in one sentence."}},
		"stream":     false,
	}
	encoded, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MVGC-Target-Host", targetHost)
	if cfg.WorkloadID != "" {
		req.Header.Set("X-MVGC-Workload-ID", cfg.WorkloadID)
	}
	if cfg.ProjectID != "" {
		req.Header.Set("X-MVGC-Project-ID", cfg.ProjectID)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			os.Exit(0)
		}
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	printOpenAIResponse(resp)
	fmt.Println()
}

// demoAzure demonstrates Azure OpenAI routing. Azure uses customer-specific
// hostnames (e.g., myresource.openai.azure.com), so /proxy/azure_openai/ is
// NOT supported — the gateway returns 400 directing you to use X-MVGC-Target-Host.
// Skip gracefully if AXEMERE_AZURE_ENDPOINT is not set or credential not found.
func demoAzure(ctx context.Context, cfg *gateway.Config, httpClient *http.Client) {
	fmt.Println("=== Mode 3: Azure OpenAI (X-MVGC-Target-Host from env) ===")

	azureEndpoint := os.Getenv("AXEMERE_AZURE_ENDPOINT")
	if azureEndpoint == "" {
		fmt.Println("AXEMERE_AZURE_ENDPOINT not set — skipping Azure demo.")
		fmt.Println("Set AXEMERE_AZURE_ENDPOINT=myresource.openai.azure.com to run this section.")

		// Show what happens with /proxy/azure_openai/ so users understand the error.
		demoAzurePathPrefixError(ctx, cfg, httpClient)
		return
	}

	deployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
	if deployment == "" {
		deployment = "gpt-4o-mini"
	}
	fmt.Printf("Azure endpoint: %s  Deployment: %s\n", azureEndpoint, deployment)
	fmt.Printf("X-MVGC-Target-Host: %s\n", azureEndpoint)

	// Azure OpenAI API path format: /openai/deployments/{deployment}/chat/completions
	azurePath := fmt.Sprintf("/openai/deployments/%s/chat/completions?api-version=2024-02-01", deployment)
	endpoint := strings.TrimRight(cfg.GatewayURL, "/") + azurePath
	fmt.Printf("POST %s\n", endpoint)

	body := map[string]any{
		"messages":   []map[string]any{{"role": "user", "content": "Say 'Azure routing works' in one sentence."}},
		"max_tokens": 64,
	}
	encoded, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MVGC-Target-Host", azureEndpoint)
	if cfg.WorkloadID != "" {
		req.Header.Set("X-MVGC-Workload-ID", cfg.WorkloadID)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			os.Exit(0)
		}
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	if resp.StatusCode == http.StatusOK {
		var result map[string]any
		json.Unmarshal(respBytes, &result) //nolint:errcheck
		if content := extractOpenAIContent(result); content != "" {
			fmt.Printf("Response: %s\n", content)
		}
	} else {
		bodyStr := string(respBytes)
		if strings.Contains(strings.ToLower(bodyStr), "credential") {
			fmt.Println("Azure credential not configured in this gateway — skipping.")
			fmt.Println("Add an Azure credential to the gateway to enable this section.")
		} else {
			fmt.Printf("Body: %s\n", bodyStr[:min(len(bodyStr), 300)])
		}
	}
	fmt.Println()
}

// demoAzurePathPrefixError shows the actionable 400 returned for /proxy/azure_openai/.
// The gateway detects azure_openai has no static host and returns 400 with instructions.
func demoAzurePathPrefixError(ctx context.Context, cfg *gateway.Config, httpClient *http.Client) {
	fmt.Println("\n=== What happens with /proxy/azure_openai/ ===")
	endpoint := strings.TrimRight(cfg.GatewayURL, "/") + "/proxy/azure_openai/v1/chat/completions"

	body := map[string]any{
		"model":    "gpt-4o-mini",
		"messages": []map[string]any{{"role": "user", "content": "test"}},
	}
	encoded, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if cfg.WorkloadID != "" {
		req.Header.Set("X-MVGC-Workload-ID", cfg.WorkloadID)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if isConnRefused(err) {
			return // already warned above
		}
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %d  (expected 400)\n", resp.StatusCode)
	if len(respBytes) > 300 {
		respBytes = respBytes[:300]
	}
	fmt.Printf("Body: %s\n", respBytes)
}

func printOpenAIResponse(resp *http.Response) {
	respBytes, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n", resp.StatusCode)
	if resp.StatusCode == http.StatusOK {
		var result map[string]any
		json.Unmarshal(respBytes, &result) //nolint:errcheck
		if content := extractOpenAIContent(result); content != "" {
			fmt.Printf("Response: %s\n", content)
		}
	} else {
		if len(respBytes) > 300 {
			respBytes = respBytes[:300]
		}
		fmt.Printf("Body: %s\n", respBytes)
	}
}

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
