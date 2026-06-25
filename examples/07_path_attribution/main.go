// Package main demonstrates path-based attribution in transparent proxy mode.
// Shows workload_id, project_id, account_id, customer_id, and labels flowing
// through three request patterns: full attribution via ExecuteRequest fields,
// per-call override on a second request, and proxy mode with customer_id in URL.
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
	fmt.Printf("Workload: %s\n", cfg.WorkloadID)
	fmt.Printf("Project:  %s\n\n", cfg.ProjectID)

	client, err := gateway.NewClient(cfg)
	if err != nil {
		log.Fatalf("client: %v", err)
	}

	ctx := context.Background()

	// --- Pattern 1: Full attribution via ExecuteRequest fields ---
	fmt.Println("=== Pattern 1: Full attribution via ExecuteRequest ===")
	fmt.Printf("workload_id=%s project_id=%s account_id=%s customer_id=%s\n",
		cfg.WorkloadID, cfg.ProjectID, cfg.AccountID, cfg.CustomerID)

	resp1, err := client.Execute(ctx, gateway.ExecuteRequest{
		Provider:   "openai",
		Model:      "gpt-4o-mini",
		WorkloadID: cfg.WorkloadID,
		ProjectID:  cfg.ProjectID,
		AccountID:  cfg.AccountID,
		CustomerID: cfg.CustomerID,
		Labels:     map[string]string{"env": "demo", "team": "platform"},
		Messages:   []gateway.Message{{Role: "user", Content: "Say 'attribution works' in one sentence."}},
		Params:     map[string]any{"max_tokens": 32},
	})
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Response:  %s\n", resp1.Content)
		printMetering(resp1.Metering)
		fmt.Printf("Record ID: %s\n\n", resp1.RecordID)
	}

	// --- Pattern 2: Per-call attribution override ---
	fmt.Println("=== Pattern 2: Per-call override (different project_id) ===")
	overrideProject := "proj-override-demo"
	fmt.Printf("Config project_id=%s  Override project_id=%s\n", cfg.ProjectID, overrideProject)

	resp2, err := client.Execute(ctx, gateway.ExecuteRequest{
		Provider:   "anthropic",
		Model:      "claude-haiku-4-5-20251001",
		WorkloadID: cfg.WorkloadID,
		ProjectID:  overrideProject, // overrides cfg.ProjectID for this call only
		AccountID:  cfg.AccountID,
		CustomerID: cfg.CustomerID,
		Labels:     map[string]string{"env": "demo", "override": "true"},
		Messages:   []gateway.Message{{Role: "user", Content: "Say 'per-call override works' in one sentence."}},
		Params:     map[string]any{"max_tokens": 32},
	})
	if err != nil {
		if isConnRefused(err) {
			fmt.Printf("Gateway not reachable: %v\n", err)
			os.Exit(0)
		}
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Response:  %s\n", resp2.Content)
		printMetering(resp2.Metering)
		fmt.Printf("Record ID: %s\n\n", resp2.RecordID)
	}

	// --- Pattern 3: Proxy mode — per-customer SaaS pattern ---
	// Each customer gets a dedicated ProxyURL with their customer_id encoded in the path.
	// No per-request headers needed; attribution flows automatically on every request.
	fmt.Println("=== Pattern 3: Proxy mode — per-customer SaaS ===")

	customers := []struct {
		id    string
		query string
	}{
		{"cust-acme-corp", "Say 'ACME here' in three words."},
		{"cust-globex-ind", "Say 'Globex here' in three words."},
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	for _, cust := range customers {
		// Build a config scoped to this customer and generate the proxy URL.
		custCfg, _ := gateway.NewConfig(
			gateway.WithGatewayURL(cfg.GatewayURL),
			gateway.WithWorkloadID(cfg.WorkloadID),
			gateway.WithProjectID(cfg.ProjectID),
			gateway.WithAccountID(cfg.AccountID),
			gateway.WithCustomerID(cust.id),
		)
		proxyBase := custCfg.ProxyURL("openai")
		endpoint := strings.TrimRight(proxyBase, "/") + "/v1/chat/completions"
		// The gateway token is embedded in the proxy URL path (/k/<token>); print a
		// redacted form so the token is never logged.
		fmt.Printf("Customer %s: POST %s\n", cust.id, redactProxyURL(proxyBase))

		body := map[string]any{
			"model":      "gpt-4o-mini",
			"max_tokens": 32,
			"messages":   []map[string]any{{"role": "user", "content": cust.query}},
		}
		encoded, _ := json.Marshal(body)

		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer any-value")

		pResp, pErr := httpClient.Do(req)
		if pErr != nil {
			if strings.Contains(pErr.Error(), "connection refused") || strings.Contains(pErr.Error(), "no such host") {
				fmt.Printf("Gateway not reachable: %v\n", pErr)
				os.Exit(0)
			}
			fmt.Printf("  Error: %v\n", pErr)
			continue
		}
		defer pResp.Body.Close()

		respBytes, _ := io.ReadAll(pResp.Body)
		fmt.Printf("  HTTP %d\n", pResp.StatusCode)
		if pResp.StatusCode == http.StatusOK {
			var result map[string]any
			json.Unmarshal(respBytes, &result) //nolint:errcheck
			if content := extractOpenAIContent(result); content != "" {
				fmt.Printf("  Response: %s\n", content)
			}
		}
		if rid := pResp.Header.Get("X-MVGC-Record-ID"); rid != "" {
			fmt.Printf("  Record ID: %s\n", rid)
		}
		fmt.Println()
	}
}

func printMetering(m gateway.Metering) {
	if m.CostUSD == "" && m.TokensIn == 0 {
		return
	}
	fmt.Printf("Metering:  tokens_in=%d tokens_out=%d cost_usd=%s\n",
		m.TokensIn, m.TokensOut, m.CostUSD)
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
