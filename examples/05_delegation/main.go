// Package main demonstrates MVGC delegation tokens (wire spec mvgc.delegation.v2).
// Generates an ephemeral Ed25519 key pair, builds and signs a delegation token,
// then shows: token structure, a delegated request, scope enforcement (embed-only
// token trying ai.infer), and a budget-constrained token.
//
// NOTE: A real gateway will reject tokens unless MVGC_DELEGATION_VERIFY_KEY matches
// the public key used here. This example demonstrates the wire format and logic
// independent of gateway key configuration.
package main

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// createDelegationToken builds and signs a delegation token.
// The unsigned body is JCS-encoded (map keys sorted alphabetically by json.Marshal)
// before signing; the sig is appended and the full object is returned as JSON.
func createDelegationToken(
	privKey ed25519.PrivateKey,
	orgID, workloadID, issuedBy, audience string,
	ttl time.Duration,
	actionsAllow, targetsAllow []string,
	usdMax string,
	maxRequests int,
) string {
	now := time.Now().UTC()
	expires := now.Add(ttl)

	budget := map[string]any{}
	if usdMax != "" {
		budget["usd_max"] = usdMax
	}
	if maxRequests > 0 {
		budget["max_requests"] = maxRequests
	}

	// Using map[string]any ensures json.Marshal sorts keys alphabetically —
	// the minimal JCS encoding needed for the signature to verify correctly.
	unsigned := map[string]any{
		"audience":      audience,
		"budget":        budget,
		"delegation_id": uuid.Must(uuid.NewV7()).String(),
		"depth":         0,
		"expires_at":    expires.Format("2006-01-02T15:04:05Z"),
		"issued_at":     now.Format("2006-01-02T15:04:05Z"),
		"issued_by":     issuedBy,
		"jti":           uuid.Must(uuid.NewV7()).String(),
		"org_id":        orgID,
		"schema":        "mvgc.delegation.v2",
		"scope": map[string]any{
			"actions_allow": actionsAllow,
			"targets_allow": targetsAllow,
		},
		"workload_id": workloadID,
	}

	canonical, err := json.Marshal(unsigned)
	if err != nil {
		log.Fatalf("marshal unsigned token: %v", err)
	}

	sig := ed25519.Sign(privKey, canonical)
	pubKey := privKey.Public().(ed25519.PublicKey)
	// key_id is the first 8 bytes of the public key, base64url-encoded without padding.
	keyID := base64.RawURLEncoding.EncodeToString(pubKey[:8])

	signed := make(map[string]any, len(unsigned)+1)
	for k, v := range unsigned {
		signed[k] = v
	}
	signed["sig"] = map[string]any{
		"algorithm": "ed25519",
		"key_id":    keyID,
		"sig":       base64.StdEncoding.EncodeToString(sig),
	}

	out, err := json.Marshal(signed)
	if err != nil {
		log.Fatalf("marshal signed token: %v", err)
	}
	return string(out)
}

// postAction sends a raw explicit action request to the gateway.
func postAction(gatewayURL, workloadID, projectID, customerID, accountID, delegationToken string, params map[string]any) (int, map[string]any) {
	payload := map[string]any{
		"schema":           "mvgc.action_request.v2",
		"org_id":           "",
		"workload_id":      workloadID,
		"ingress_mode":     "explicit_action_request",
		"delegation_token": delegationToken,
		"action": map[string]any{
			"type":        "ai.infer",
			"method":      "POST",
			"target_host": "api.openai.com",
			"target_path": "/v1/chat/completions",
			"params":      params,
		},
		"attribution": map[string]any{
			"project_id":  projectID,
			"customer_id": customerID,
			"account_id":  accountID,
			"labels":      map[string]string{"source": "delegation-example"},
		},
	}

	encoded, _ := json.Marshal(payload)
	resp, err := http.Post(gatewayURL+"/v1/actions:execute", "application/json", bytes.NewReader(encoded)) //nolint:noctx
	if err != nil {
		return 0, map[string]any{"error": err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(body, &result) //nolint:errcheck
	return resp.StatusCode, result
}

func demoShowTokenStructure(tokenJSON string) {
	fmt.Println("\n--- Delegation token structure ---")
	var tok map[string]any
	json.Unmarshal([]byte(tokenJSON), &tok) //nolint:errcheck
	// Print without the sig for readability.
	display := make(map[string]any)
	for k, v := range tok {
		if k != "sig" {
			display[k] = v
		}
	}
	pretty, _ := json.MarshalIndent(display, "", "  ")
	fmt.Println(string(pretty))
	if sig, ok := tok["sig"].(map[string]any); ok {
		fmt.Printf("  [sig.key_id=%s  algorithm=%s]\n", sig["key_id"], sig["algorithm"])
	}
}

func demoDelegatedRequest(cfg configVars, tokenJSON string) {
	fmt.Println("\n--- Delegated request (ai.infer allowed) ---")
	status, body := postAction(
		cfg.gatewayURL, cfg.workloadID, cfg.projectID, cfg.customerID, cfg.accountID,
		tokenJSON,
		map[string]any{
			"model":      "gpt-4o-mini",
			"max_tokens": 64,
			"messages":   []map[string]any{{"role": "user", "content": "Say 'hello from delegation' in one sentence."}},
		},
	)
	fmt.Printf("HTTP %d  decision=%s\n", status, strOrDash(body, "decision"))
	if result, ok := body["result"].(map[string]any); ok {
		if resultBody, ok := result["body"].(map[string]any); ok {
			if choices, ok := resultBody["choices"].([]any); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]any); ok {
					if msg, ok := choice["message"].(map[string]any); ok {
						fmt.Printf("Response: %s\n", msg["content"])
					}
				}
			}
		}
	}
	if reason := strOrDash(body, "reason"); reason != "—" {
		fmt.Printf("Reason: %s\n", reason)
	}
}

func demoScopeEnforcement(cfg configVars, privKey ed25519.PrivateKey) {
	fmt.Println("\n--- Scope enforcement: ai.infer blocked (only ai.embed allowed) ---")
	embedOnlyToken := createDelegationToken(
		privKey,
		"", cfg.workloadID,
		"governance-service", cfg.workloadID,
		5*time.Minute,
		[]string{"ai.embed"}, // does NOT include ai.infer
		[]string{"api.openai.com"},
		"", 0,
	)
	status, body := postAction(
		cfg.gatewayURL, cfg.workloadID, cfg.projectID, cfg.customerID, cfg.accountID,
		embedOnlyToken,
		map[string]any{
			"model":      "gpt-4o-mini",
			"max_tokens": 64,
			"messages":   []map[string]any{{"role": "user", "content": "hello"}},
		},
	)
	fmt.Printf("HTTP %d  decision=%s\n", status, strOrDash(body, "decision"))
	fmt.Printf("Reason: %s\n", strOrDash(body, "reason"))
}

func demoBudgetConstrainedToken(cfg configVars, privKey ed25519.PrivateKey) {
	fmt.Println("\n--- Budget-constrained delegation token ($0.001 cap) ---")
	microToken := createDelegationToken(
		privKey,
		"", cfg.workloadID,
		"governance-service", cfg.workloadID,
		5*time.Minute,
		[]string{"ai.infer"},
		nil,
		"0.001", 1,
	)
	var tok map[string]any
	json.Unmarshal([]byte(microToken), &tok) //nolint:errcheck
	if budget, ok := tok["budget"].(map[string]any); ok {
		fmt.Printf("Token budget: usd_max=%v  max_requests=%v\n", budget["usd_max"], budget["max_requests"])
	}
	status, body := postAction(
		cfg.gatewayURL, cfg.workloadID, cfg.projectID, cfg.customerID, cfg.accountID,
		microToken,
		map[string]any{
			"model":      "gpt-4o-mini",
			"max_tokens": 512,
			"messages":   []map[string]any{{"role": "user", "content": "Write a 400-word essay about governance."}},
		},
	)
	fmt.Printf("HTTP %d  decision=%s\n", status, strOrDash(body, "decision"))
	if reason := strOrDash(body, "reason"); reason != "—" {
		fmt.Printf("Reason: %s\n", reason)
	} else {
		fmt.Println("Request succeeded (budget not yet exceeded at token level)")
	}
}

type configVars struct {
	gatewayURL string
	workloadID string
	projectID  string
	customerID string
	accountID  string
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

	fmt.Printf("Gateway: %s\n", cfg.gatewayURL)
	fmt.Printf("Workload: %s  Project: %s\n", cfg.workloadID, cfg.projectID)
	fmt.Println()
	fmt.Println("NOTE: This example uses an ephemeral Ed25519 key pair.")
	fmt.Println("      A real gateway will reject tokens unless MVGC_DELEGATION_VERIFY_KEY")
	fmt.Println("      matches the public key used here. The demos show the wire format")
	fmt.Println("      and scope enforcement logic independent of gateway key config.")

	// Generate an ephemeral signing key. In production the private key lives in the
	// issuing governance service; the public key is configured in the gateway.
	pubKeyBytes, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("generate key: %v", err)
	}
	_ = pubKeyBytes

	tokenJSON := createDelegationToken(
		privKey,
		"", cfg.workloadID,
		"governance-service", cfg.workloadID,
		time.Hour,
		[]string{"ai.infer", "ai.embed"},
		[]string{"api.openai.com", "api.anthropic.com"},
		"5.00", 0,
	)

	demoShowTokenStructure(tokenJSON)

	// Check for connectivity before making gateway calls.
	if _, err := http.Get(cfg.gatewayURL + "/healthz"); err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
			fmt.Printf("\nGateway not reachable: %v\n", err)
			fmt.Println("Is the gateway running? Try: docker compose up -d")
			fmt.Println("(token structure above was generated locally without a gateway)")
			os.Exit(0)
		}
	}

	demoDelegatedRequest(cfg, tokenJSON)
	demoScopeEnforcement(cfg, privKey)
	demoBudgetConstrainedToken(cfg, privKey)
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
