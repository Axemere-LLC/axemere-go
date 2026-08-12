package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Axemere-LLC/axemere-go/gateway"
)

// makeClient creates a Client pointed at the given server URL.
func makeClient(t *testing.T, serverURL string, opts ...gateway.Option) *gateway.Client {
	t.Helper()
	allOpts := append([]gateway.Option{
		gateway.WithGatewayURL(serverURL),
		gateway.WithGatewayToken("tok_test"),
	}, opts...)
	cfg, err := gateway.NewConfig(allOpts...)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	c, err := gateway.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestExecute_MissingProvider(t *testing.T) {
	cfg, _ := gateway.NewConfig(gateway.WithGatewayURL("http://localhost"))
	c, _ := gateway.NewClient(cfg)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Model:    "gpt-4o",
		// Provider intentionally omitted
	})
	if err == nil {
		t.Fatal("expected error for missing provider, got nil")
	}
	var gErr *gateway.GatewayError
	if !errors.As(err, &gErr) {
		t.Errorf("want *GatewayError, got %T: %v", err, err)
	}
}

func TestExecute_MissingModel(t *testing.T) {
	cfg, _ := gateway.NewConfig(gateway.WithGatewayURL("http://localhost"))
	c, _ := gateway.NewClient(cfg)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "openai",
		// Model intentionally omitted
	})
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
	var gErr *gateway.GatewayError
	if !errors.As(err, &gErr) {
		t.Errorf("want *GatewayError, got %T: %v", err, err)
	}
}

func TestExecute_UnknownProvider(t *testing.T) {
	cfg, _ := gateway.NewConfig(gateway.WithGatewayURL("http://localhost"))
	c, _ := gateway.NewClient(cfg)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "does-not-exist",
		Model:    "some-model",
	})
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	var gErr *gateway.GatewayError
	if !errors.As(err, &gErr) {
		t.Errorf("want *GatewayError, got %T: %v", err, err)
	}
}

func TestExecute_Success_OpenAIStyle(t *testing.T) {
	response := map[string]any{
		"record_id":   "rec_123",
		"record_hash": "sha256:abc",
		"provider":    "openai",
		"model":       "gpt-4o",
		"provider_response": map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello from OpenAI",
					},
				},
			},
		},
		"metering": map[string]any{
			"cost_usd":   "0.001",
			"tokens_in":  10,
			"tokens_out": 20,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/actions:execute" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok_test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	resp, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "Hello from OpenAI" {
		t.Errorf("want content %q, got %q", "Hello from OpenAI", resp.Content)
	}
	if resp.RecordID != "rec_123" {
		t.Errorf("want RecordID rec_123, got %q", resp.RecordID)
	}
	if resp.Metering.TokensIn != 10 {
		t.Errorf("want TokensIn=10, got %d", resp.Metering.TokensIn)
	}
	if resp.Metering.TokensOut != 20 {
		t.Errorf("want TokensOut=20, got %d", resp.Metering.TokensOut)
	}
}

func TestExecute_Success_AnthropicStyle(t *testing.T) {
	response := map[string]any{
		"record_id": "rec_ant_1",
		"provider":  "anthropic",
		"model":     "claude-3-opus-20240229",
		"provider_response": map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "Hello from Anthropic"},
			},
		},
		"metering": map[string]any{
			"cost_usd": "0.002",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	resp, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "anthropic",
		Model:    "claude-3-opus-20240229",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content != "Hello from Anthropic" {
		t.Errorf("want content %q, got %q", "Hello from Anthropic", resp.Content)
	}
}

func TestExecute_Anthropic_DefaultsMaxTokensAndSeparatesSystem(t *testing.T) {
	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		response := map[string]any{
			"record_id": "rec_ant_sys",
			"provider":  "anthropic",
			"model":     "claude-3-opus-20240229",
			"provider_response": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "hi"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "hi"},
		},
		Provider: "anthropic",
		Model:    "claude-3-opus-20240229",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	action, ok := captured["action"].(map[string]any)
	if !ok {
		t.Fatalf("action missing or wrong shape: %v", captured["action"])
	}
	params, ok := action["params"].(map[string]any)
	if !ok {
		t.Fatalf("params missing or wrong shape: %v", action["params"])
	}

	if params["max_tokens"] == nil {
		t.Error("want a default max_tokens for anthropic, got nil")
	}
	if params["system"] != "be terse" {
		t.Errorf("want system %q lifted out of messages, got %v", "be terse", params["system"])
	}
	msgs, ok := params["messages"].([]any)
	if !ok {
		t.Fatalf("messages missing or wrong shape: %v", params["messages"])
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 message after extracting system, got %d: %v", len(msgs), msgs)
	}
	if msg, ok := msgs[0].(map[string]any); !ok || msg["role"] != "user" {
		t.Errorf("want remaining message to have role=user, got %v", msgs[0])
	}
}

func TestExecute_Anthropic_CallerMaxTokensOverridesDefault(t *testing.T) {
	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		response := map[string]any{
			"record_id": "rec_ant_override",
			"provider":  "anthropic",
			"provider_response": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": "hi"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "anthropic",
		Model:    "claude-3-opus-20240229",
		Params:   map[string]any{"max_tokens": 2048},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	action := captured["action"].(map[string]any)
	params := action["params"].(map[string]any)
	if params["max_tokens"] != float64(2048) {
		t.Errorf("want caller-supplied max_tokens=2048 to win, got %v", params["max_tokens"])
	}
}

func TestExecute_UpstreamProviderError(t *testing.T) {
	// Reproduces a real gateway response: the gateway's own HTTP status and
	// decision are both "success" (200, allow), but the upstream provider
	// rejected the request — that failure only appears nested in
	// result.status_code/result.body. Without checking it explicitly, this
	// silently reads as a successful empty response instead of an error.
	response := map[string]any{
		"record_id": "rec_upstream_err",
		"decision":  "allow",
		"result": map[string]any{
			"status_code": 400,
			"body": map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    "invalid_request_error",
					"message": "max_tokens: Field required",
				},
			},
		},
		"metering": map[string]any{"tokens_in": 0, "tokens_out": 0},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "anthropic",
		Model:    "claude-3-opus-20240229",
	})
	if err == nil {
		t.Fatal("expected GatewayError for a nested upstream 400, got nil")
	}
	var gErr *gateway.GatewayError
	if !errors.As(err, &gErr) {
		t.Fatalf("want *GatewayError, got %T: %v", err, err)
	}
	if gErr.StatusCode != 400 {
		t.Errorf("want StatusCode 400, got %d", gErr.StatusCode)
	}
	if !strings.Contains(gErr.Error(), "max_tokens: Field required") {
		t.Errorf("want error message to include the upstream message, got %q", gErr.Error())
	}
}

func TestExecute_PolicyDenied(t *testing.T) {
	response := map[string]any{
		"decision":  "deny",
		"reason":    "policy: blocked by rule test-rule",
		"record_id": "rec_deny_1",
		"trace":     map[string]any{"rule": "test-rule"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "do something blocked"}},
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err == nil {
		t.Fatal("expected PolicyDeniedError, got nil")
	}
	var pErr *gateway.PolicyDeniedError
	if !errors.As(err, &pErr) {
		t.Errorf("want *PolicyDeniedError, got %T: %v", err, err)
	}
	if pErr.RecordID != "rec_deny_1" {
		t.Errorf("want RecordID rec_deny_1, got %q", pErr.RecordID)
	}
	if !strings.Contains(pErr.Error(), "policy denied") {
		t.Errorf("error message should contain 'policy denied', got %q", pErr.Error())
	}
}

func TestExecute_QuotaExceeded(t *testing.T) {
	response := map[string]any{
		"message":     "quota exceeded",
		"upgrade_url": "https://axemere.ai/upgrade",
		"retry_after": float64(60),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err == nil {
		t.Fatal("expected QuotaExceededError, got nil")
	}
	var qErr *gateway.QuotaExceededError
	if !errors.As(err, &qErr) {
		t.Errorf("want *QuotaExceededError, got %T: %v", err, err)
	}
	if qErr.UpgradeURL != "https://axemere.ai/upgrade" {
		t.Errorf("want UpgradeURL, got %q", qErr.UpgradeURL)
	}
	if qErr.RetryAfter != 60 {
		t.Errorf("want RetryAfter=60, got %d", qErr.RetryAfter)
	}
}

func TestExecute_GatewayTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGatewayTimeout)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err == nil {
		t.Fatal("expected GatewayTimeoutError, got nil")
	}
	var tErr *gateway.GatewayTimeoutError
	if !errors.As(err, &tErr) {
		t.Errorf("want *GatewayTimeoutError, got %T: %v", err, err)
	}
}

func TestExecute_GenericError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"internal server error"}`)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err == nil {
		t.Fatal("expected GatewayError, got nil")
	}
	var gErr *gateway.GatewayError
	if !errors.As(err, &gErr) {
		t.Errorf("want *GatewayError, got %T: %v", err, err)
	}
	if gErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("want StatusCode 500, got %d", gErr.StatusCode)
	}
}

func TestExecute_VerifiesRequestBody(t *testing.T) {
	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		response := map[string]any{
			"record_id": "rec_check",
			"provider":  "openai",
			"model":     "gpt-4o",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL,
		gateway.WithWorkloadID("wl_unit"),
		gateway.WithProjectID("proj_unit"),
	)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages:   []gateway.Message{{Role: "user", Content: "hello"}},
		Provider:   "openai",
		Model:      "gpt-4o",
		WorkloadID: "wl_override",
		Labels:     map[string]string{"test": "true"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if captured["schema"] != "mvgc.action_request.v2" {
		t.Errorf("want schema mvgc.action_request.v2, got %v", captured["schema"])
	}
	if captured["ingress_mode"] != "explicit_action_request" {
		t.Errorf("want ingress_mode explicit_action_request, got %v", captured["ingress_mode"])
	}
	if captured["workload_id"] != "wl_override" {
		t.Errorf("want workload_id wl_override (per-call override), got %v", captured["workload_id"])
	}
	if captured["request_id"] == "" {
		t.Error("want non-empty request_id")
	}

	action, _ := captured["action"].(map[string]any)
	if action["target_host"] != "api.openai.com" {
		t.Errorf("want target_host api.openai.com, got %v", action["target_host"])
	}

	attribution, _ := captured["attribution"].(map[string]any)
	if attribution["project_id"] != "proj_unit" {
		t.Errorf("want project_id proj_unit (from config), got %v", attribution["project_id"])
	}
}

func TestStream_BasicSSE(t *testing.T) {
	sseBody := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"type\":\"mvgc_metering\",\"record_id\":\"rec_stream_1\",\"metering\":{\"cost_usd\":\"0.001\",\"tokens_in\":5,\"tokens_out\":2}}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	ch, err := c.Stream(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var content strings.Builder
	var finalChunk *gateway.StreamChunk

	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.IsFinal {
			c := chunk
			finalChunk = &c
		} else {
			content.WriteString(chunk.Content)
		}
	}

	if content.String() != "Hello world" {
		t.Errorf("want content %q, got %q", "Hello world", content.String())
	}
	if finalChunk == nil {
		t.Fatal("expected final chunk")
	}
	if finalChunk.RecordID != "rec_stream_1" {
		t.Errorf("want RecordID rec_stream_1, got %q", finalChunk.RecordID)
	}
	if finalChunk.Metering == nil {
		t.Fatal("expected non-nil Metering on final chunk")
	}
	if finalChunk.Metering.TokensIn != 5 {
		t.Errorf("want TokensIn=5, got %d", finalChunk.Metering.TokensIn)
	}
}

func TestStream_PolicyDeniedOnConnect(t *testing.T) {
	response := map[string]any{
		"decision": "deny",
		"reason":   "blocked by policy",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Stream(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err == nil {
		t.Fatal("expected PolicyDeniedError, got nil")
	}
	var pErr *gateway.PolicyDeniedError
	if !errors.As(err, &pErr) {
		t.Errorf("want *PolicyDeniedError, got %T: %v", err, err)
	}
}

func TestExecute_CredentialHint(t *testing.T) {
	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"record_id": "rec_cred",
			"metering":  map[string]any{"cost_usd": "0", "tokens_in": 0, "tokens_out": 0},
			"provider_response": map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
			},
		})
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages:       []gateway.Message{{Role: "user", Content: "hi"}},
		Provider:       "openai",
		Model:          "gpt-4o",
		ProviderAPIKey: "sk-byok-test",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured["credential_hint"] != "sk-byok-test" {
		t.Errorf("want credential_hint sk-byok-test, got %v", captured["credential_hint"])
	}
}

func TestNewClient_NilConfig(t *testing.T) {
	_, err := gateway.NewClient(nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}
