// Package main demonstrates governance outcomes beyond simple allow/deny.
// Shows: HTTP 403 deny (missing project_id), decision_trace inspection,
// HTTP 202 require_approval with optional admin-API polling, HTTP 429
// rate-limit with Retry-After, and HTTP 403 quarantine.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const adminTokenEnv = "MVGC_ADMIN_TOKEN"

type configVars struct {
	gatewayURL string
	workloadID string
	projectID  string
	customerID string
	accountID  string
}

// postAction sends an explicit action request and returns (status, body, headers).
func postAction(cfg configVars, extraFields map[string]any) (int, map[string]any, http.Header) {
	payload := map[string]any{
		"schema":       "mvgc.action_request.v2",
		"org_id":       "",
		"workload_id":  cfg.workloadID,
		"ingress_mode": "explicit_action_request",
		"action": map[string]any{
			"type":        "ai.infer",
			"method":      "POST",
			"target_host": "api.openai.com",
			"target_path": "/v1/chat/completions",
			"params": map[string]any{
				"model":      "gpt-4o-mini",
				"max_tokens": 64,
				"messages":   []map[string]any{{"role": "user", "content": "Hello."}},
			},
		},
		"attribution": map[string]any{
			"project_id":  cfg.projectID,
			"customer_id": cfg.customerID,
			"account_id":  cfg.accountID,
			"labels":      map[string]string{"source": "governance-outcomes-example"},
		},
	}
	for k, v := range extraFields {
		payload[k] = v
	}

	encoded, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, cfg.gatewayURL+"/v1/actions:execute", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, map[string]any{"error": err.Error()}, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result) //nolint:errcheck
	return resp.StatusCode, result, resp.Header
}

func pollApproval(cfg configVars, approvalID, adminToken string, maxPolls int) map[string]any {
	client := &http.Client{Timeout: 10 * time.Second}
	for attempt := 1; attempt <= maxPolls; attempt++ {
		req, _ := http.NewRequest(http.MethodGet,
			cfg.gatewayURL+"/v1/admin/approvals/"+approvalID, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("  Poll %d: error: %v\n", attempt, err)
			break
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Printf("  Poll %d: HTTP %d\n", attempt, resp.StatusCode)
			break
		}
		var data map[string]any
		body, _ := io.ReadAll(resp.Body)
		json.Unmarshal(body, &data) //nolint:errcheck
		status, _ := data["status"].(string)
		fmt.Printf("  Poll %d: status=%s\n", attempt, status)
		if status == "approved" || status == "denied" {
			return data
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func demoPolicyDeny(cfg configVars) {
	fmt.Println("\n--- HTTP 403 deny: missing project_id ---")
	noProjCfg := configVars{
		gatewayURL: cfg.gatewayURL,
		workloadID: cfg.workloadID,
		projectID:  "", // intentionally empty
		customerID: cfg.customerID,
		accountID:  cfg.accountID,
	}
	status, body, _ := postAction(noProjCfg, nil)
	fmt.Printf("HTTP %d  decision=%s\n", status, strOrDash(body, "decision"))
	fmt.Printf("Reason: %s\n", strOrDash(body, "reason"))
	if trace, ok := body["decision_trace"].(map[string]any); ok {
		if codes, ok := trace["reason_codes"].([]any); ok && len(codes) > 0 {
			fmt.Printf("Reason codes: %v\n", codes)
		}
	}
}

func demoDecisionTrace(cfg configVars) {
	fmt.Println("\n--- decision_trace on a successful allow ---")
	status, body, _ := postAction(cfg, nil)
	fmt.Printf("HTTP %d  decision=%s\n", status, strOrDash(body, "decision"))
	trace, ok := body["decision_trace"].(map[string]any)
	if !ok {
		fmt.Println("No decision_trace in response.")
		return
	}
	fmt.Printf("Schema:       %s\n", strOrDash(trace, "schema"))
	fmt.Printf("Decision:     %s\n", strOrDash(trace, "decision"))
	fmt.Printf("Evaluated at: %s\n", strOrDash(trace, "evaluated_at"))
	if attrs, ok := trace["attributes"].(map[string]any); ok {
		count := 0
		for k, v := range attrs {
			if count >= 4 {
				break
			}
			fmt.Printf("  %s: %v\n", k, v)
			count++
		}
	}
}

func demoRequireApproval(cfg configVars, adminToken string) {
	fmt.Println("\n--- HTTP 202 require_approval ---")
	// The label risk_level=high triggers a require_approval rule in the mock gateway policy.
	status, body, _ := postAction(cfg, map[string]any{
		"attribution": map[string]any{
			"project_id":  cfg.projectID,
			"customer_id": cfg.customerID,
			"account_id":  cfg.accountID,
			"labels":      map[string]string{"risk_level": "high"},
		},
	})
	fmt.Printf("HTTP %d  decision=%s\n", status, strOrDash(body, "decision"))

	switch status {
	case 202:
		approvalID, _ := body["approval_id"].(string)
		recordID, _ := body["record_id"].(string)
		fmt.Printf("approval_id: %s\n", approvalID)
		fmt.Printf("record_id:   %s\n", recordID)

		if adminToken != "" && approvalID != "" {
			fmt.Println("\nAuto-approving via admin API (simulating human review)...")
			client := &http.Client{Timeout: 10 * time.Second}
			approveBody, _ := json.Marshal(map[string]any{"decided_by": "example-script"})
			req, _ := http.NewRequest(http.MethodPost,
				cfg.gatewayURL+"/v1/admin/approvals/"+approvalID+"/approve",
				bytes.NewReader(approveBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			resp, err := client.Do(req)
			if err != nil {
				fmt.Printf("Approve error: %v\n", err)
			} else {
				resp.Body.Close()
				fmt.Printf("Approve response: HTTP %d\n", resp.StatusCode)
			}

			fmt.Println("Polling for final status...")
			result := pollApproval(cfg, approvalID, adminToken, 6)
			if result != nil {
				fmt.Printf("Final status: %s  decided_by=%s\n",
					strOrDash(result, "status"), strOrDash(result, "decided_by"))
			}
		} else {
			fmt.Println("(set MVGC_ADMIN_TOKEN to auto-approve and poll the decision)")
		}
	case 403:
		fmt.Println("Gateway returned 403 — MVGC_APPROVAL_ENABLED may be false (legacy mode)")
		fmt.Printf("Reason: %s\n", strOrDash(body, "reason"))
	default:
		bodyJSON, _ := json.Marshal(body)
		fmt.Printf("Unexpected status; body: %s\n", bodyJSON)
	}
}

func demoRateLimit(cfg configVars) {
	fmt.Println("\n--- HTTP 429 rate_limit: rapid-fire requests ---")
	for i := 1; i <= 50; i++ {
		status, body, headers := postAction(cfg, nil)
		if status == 429 {
			retryAfter := headers.Get("Retry-After")
			if retryAfter == "" {
				retryAfter = "?"
			}
			fmt.Printf("  Request %d: HTTP 429  Retry-After: %ss\n", i, retryAfter)
			fmt.Printf("  decision=%s  reason=%s\n",
				strOrDash(body, "decision"), strOrDash(body, "reason"))
			return
		}
		if i%10 == 0 {
			fmt.Printf("  Sent %d requests — no 429 yet (decision=%s)\n",
				i, strOrDash(body, "decision"))
		}
	}
	fmt.Println("  Sent 50 requests without receiving a 429.")
	fmt.Println("  Check that your gateway policy has a rate_limit rule for this workload.")
}

func demoQuarantine(cfg configVars) {
	fmt.Println("\n--- HTTP 403 quarantine ---")
	// quarantine_trigger=true is a label that triggers a quarantine rule in mock policy.
	status, body, _ := postAction(cfg, map[string]any{
		"attribution": map[string]any{
			"project_id":  cfg.projectID,
			"customer_id": cfg.customerID,
			"account_id":  cfg.accountID,
			"labels": map[string]string{
				"source":             "governance-outcomes-example",
				"quarantine_trigger": "true",
			},
		},
	})
	fmt.Printf("HTTP %d  decision=%s\n", status, strOrDash(body, "decision"))
	if decision, _ := body["decision"].(string); decision == "quarantine" {
		fmt.Printf("record_id: %s\n", strOrDash(body, "record_id"))
		fmt.Println("The record is stored in the quarantine table.")
		fmt.Println("Admin can review via: GET /v1/admin/quarantine")
		fmt.Println("Admin can release via: POST /v1/admin/quarantine/{id}/release")
	} else if status == 403 {
		fmt.Printf("Reason: %s\n", strOrDash(body, "reason"))
		fmt.Println("(configure a quarantine rule in your policy bundle to see 'quarantine' decision)")
	} else {
		bodyJSON, _ := json.Marshal(body)
		fmt.Printf("Unexpected status; body: %s\n", bodyJSON)
	}
}

func strOrDash(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return "—"
}

func main() {
	loadDotEnv(".env")

	cfg := configVars{
		gatewayURL: envOrDefault("AXEMERE_GATEWAY_URL", "http://localhost:7080"),
		workloadID: envOrDefault("AXEMERE_WORKLOAD_ID", "wl-prod-app-1"),
		projectID:  os.Getenv("AXEMERE_PROJECT_ID"),
		customerID: os.Getenv("AXEMERE_CUSTOMER_ID"),
		accountID:  os.Getenv("AXEMERE_ACCOUNT_ID"),
	}
	adminToken := os.Getenv(adminTokenEnv)

	fmt.Printf("Gateway:     %s\n", cfg.gatewayURL)
	fmt.Printf("Workload:    %s  Project: %s\n", cfg.workloadID, cfg.projectID)
	if adminToken != "" {
		fmt.Printf("Admin token: set\n")
	} else {
		fmt.Printf("Admin token: not set (set %s to enable approval polling)\n", adminTokenEnv)
	}

	// Connectivity check.
	probe, err := http.Get(cfg.gatewayURL + "/healthz") //nolint:noctx
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
			fmt.Printf("\nGateway not reachable: %v\n", err)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			os.Exit(0)
		}
	} else {
		probe.Body.Close()
	}

	demoPolicyDeny(cfg)
	demoDecisionTrace(cfg)
	demoRequireApproval(cfg, adminToken)
	demoRateLimit(cfg)
	demoQuarantine(cfg)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
