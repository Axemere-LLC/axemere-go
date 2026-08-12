package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Axemere-LLC/axemere-go/gateway"
)

// TestProviderRoutes asserts the host and path the client sends on the wire
// for every provider in the MVGC gateway's provider registry. Host/path
// values must match internal/types/provider_registry.go (Hosts[0]) and
// openai_compat/profiles.go exactly, or requests either get denied by
// auto-policy (wrong host) or 404 upstream (wrong path).
func TestProviderRoutes(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		wantHost string
		wantPath string
	}{
		{"openai", "gpt-4o", "api.openai.com", "/v1/chat/completions"},
		{"anthropic", "claude-3-opus", "api.anthropic.com", "/v1/messages"},
		{"gemini", "gemini-1.5-pro", "generativelanguage.googleapis.com", "/v1beta/models/gemini-1.5-pro:generateContent"},
		{"google", "gemini-1.5-pro", "generativelanguage.googleapis.com", "/v1beta/models/gemini-1.5-pro:generateContent"},
		{"cohere", "command-r", "api.cohere.com", "/v2/chat"},
		{"mistral", "mistral-large", "api.mistral.ai", "/v1/chat/completions"},
		{"groq", "llama3-70b", "api.groq.com", "/openai/v1/chat/completions"},
		{"deepseek", "deepseek-chat", "api.deepseek.com", "/v1/chat/completions"},
		{"together", "meta-llama/Llama-3", "api.together.ai", "/v1/chat/completions"},
		{"minimax", "abab6.5s-chat", "api.minimax.io", "/v1/chat/completions"},
		{"moonshot", "moonshot-v1-8k", "api.moonshot.ai", "/v1/chat/completions"},
		{"zhipu", "glm-4", "api.z.ai", "/api/paas/v4/chat/completions"},
		{"xai", "grok-2", "api.x.ai", "/v1/chat/completions"},
		{"perplexity", "sonar", "api.perplexity.ai", "/chat/completions"},
		{"openrouter", "openrouter/auto", "openrouter.ai", "/api/v1/chat/completions"},
		{"nvidia-nim", "meta/llama3-70b", "integrate.api.nvidia.com", "/v1/chat/completions"},
		{"upstage", "solar-1-mini", "api.upstage.ai", "/v1/chat/completions"},
		{"fireworks", "accounts/fireworks/models/llama", "api.fireworks.ai", "/inference/v1/chat/completions"},
		{"qwen", "qwen-max", "dashscope-intl.aliyuncs.com", "/v1/chat/completions"},
		{"bytedance", "doubao-pro", "ark.cn-beijing.volces.com", "/api/v3/chat/completions"},
		{"stepfun", "step-1", "api.stepfun.com", "/v1/chat/completions"},
		{"bedrock", "anthropic.claude-3", "bedrock-runtime.us-east-1.amazonaws.com", ""},
		{"vertex", "gemini-1.5-pro", "us-central1-aiplatform.googleapis.com", ""},
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			var captured map[string]any

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&captured)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"record_id": "rec_route_test"})
			}))
			defer srv.Close()

			c := makeClient(t, srv.URL)
			_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
				Messages: []gateway.Message{{Role: "user", Content: "hi"}},
				Provider: tc.provider,
				Model:    tc.model,
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}

			action, _ := captured["action"].(map[string]any)
			if action["target_host"] != tc.wantHost {
				t.Errorf("target_host = %v, want %v", action["target_host"], tc.wantHost)
			}
			if action["target_path"] != tc.wantPath {
				t.Errorf("target_path = %v, want %v", action["target_path"], tc.wantPath)
			}
			if action["type"] != "ai.infer" {
				t.Errorf("action.type = %v, want ai.infer", action["type"])
			}
		})
	}
}

func TestExecute_AzureOpenAI_RequiresTargetHost(t *testing.T) {
	cfg, _ := gateway.NewConfig(gateway.WithGatewayURL("http://localhost"))
	c, _ := gateway.NewClient(cfg)
	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Provider: "azure_openai",
		Model:    "gpt-4o",
		// TargetHost intentionally omitted
	})
	if err == nil {
		t.Fatal("expected error for azure_openai without TargetHost, got nil")
	}
}

func TestExecute_TargetHostOverride(t *testing.T) {
	var captured map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"record_id": "rec_override"})
	}))
	defer srv.Close()

	c := makeClient(t, srv.URL)

	t.Run("azure_openai uses caller-supplied TargetHost", func(t *testing.T) {
		_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
			Messages:   []gateway.Message{{Role: "user", Content: "hi"}},
			Provider:   "azure_openai",
			Model:      "gpt-4o",
			TargetHost: "my-resource.openai.azure.com",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		action, _ := captured["action"].(map[string]any)
		if action["target_host"] != "my-resource.openai.azure.com" {
			t.Errorf("target_host = %v, want my-resource.openai.azure.com", action["target_host"])
		}
	})

	t.Run("TargetHost overrides the registry default for a provider that already has one", func(t *testing.T) {
		_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
			Messages:   []gateway.Message{{Role: "user", Content: "hi"}},
			Provider:   "openai",
			Model:      "gpt-4o",
			TargetHost: "custom.proxy.internal",
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		action, _ := captured["action"].(map[string]any)
		if action["target_host"] != "custom.proxy.internal" {
			t.Errorf("target_host = %v, want custom.proxy.internal", action["target_host"])
		}
	})
}
