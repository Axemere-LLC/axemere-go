package gateway_test

import (
	"testing"
	"time"

	"github.com/axemere-llc/axemere-go/gateway"
)

func TestNewConfig_Defaults(t *testing.T) {
	cfg, err := gateway.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Timeout != 120*time.Second {
		t.Errorf("want default timeout 120s, got %v", cfg.Timeout)
	}
	if cfg.GatewayURL != "" {
		t.Errorf("want empty GatewayURL, got %q", cfg.GatewayURL)
	}
}

func TestNewConfig_EnvVars(t *testing.T) {
	t.Setenv("AXEMERE_GATEWAY_URL", "http://localhost:7080")
	t.Setenv("AXEMERE_WORKLOAD_ID", "wl_test")
	t.Setenv("AXEMERE_PROVIDER", "openai")
	t.Setenv("AXEMERE_MODEL", "gpt-4o")

	cfg, err := gateway.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GatewayURL != "http://localhost:7080" {
		t.Errorf("want GatewayURL http://localhost:7080, got %q", cfg.GatewayURL)
	}
	if cfg.WorkloadID != "wl_test" {
		t.Errorf("want WorkloadID wl_test, got %q", cfg.WorkloadID)
	}
	if cfg.DefaultProvider != "openai" {
		t.Errorf("want DefaultProvider openai, got %q", cfg.DefaultProvider)
	}
	if cfg.DefaultModel != "gpt-4o" {
		t.Errorf("want DefaultModel gpt-4o, got %q", cfg.DefaultModel)
	}
}

func TestNewConfig_OptionsOverrideEnv(t *testing.T) {
	t.Setenv("AXEMERE_GATEWAY_URL", "http://from-env:7080")

	cfg, err := gateway.NewConfig(
		gateway.WithGatewayURL("http://from-option:9090"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GatewayURL != "http://from-option:9090" {
		t.Errorf("option should override env; got %q", cfg.GatewayURL)
	}
}

func TestNewConfig_TimeoutEnvVar(t *testing.T) {
	t.Setenv("AXEMERE_TIMEOUT_SECONDS", "30")

	cfg, err := gateway.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("want 30s timeout, got %v", cfg.Timeout)
	}
}

func TestNewConfig_InvalidLabels(t *testing.T) {
	t.Setenv("AXEMERE_LABELS", "not-json")

	_, err := gateway.NewConfig()
	if err == nil {
		t.Fatal("expected error for invalid AXEMERE_LABELS, got nil")
	}
}

func TestNewConfig_ValidLabels(t *testing.T) {
	t.Setenv("AXEMERE_LABELS", `{"env":"prod","team":"ml"}`)

	cfg, err := gateway.NewConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Labels["env"] != "prod" {
		t.Errorf("want Labels[env]=prod, got %q", cfg.Labels["env"])
	}
	if cfg.Labels["team"] != "ml" {
		t.Errorf("want Labels[team]=ml, got %q", cfg.Labels["team"])
	}
}

func TestConfig_ProxyURL(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *gateway.Config
		provider string
		want     string
	}{
		{
			name: "no token no attribution",
			cfg: &gateway.Config{
				GatewayURL: "https://gateway.example.com",
			},
			provider: "openai",
			want:     "https://gateway.example.com/proxy/openai/",
		},
		{
			name: "with token only",
			cfg: &gateway.Config{
				GatewayURL:   "https://gateway.example.com",
				GatewayToken: "tok_abc",
			},
			provider: "anthropic",
			want:     "https://gateway.example.com/proxy/anthropic/k/tok_abc/",
		},
		{
			name: "all fields",
			cfg: &gateway.Config{
				GatewayURL:   "https://gateway.example.com",
				GatewayToken: "tok_abc",
				WorkloadID:   "wl_1",
				ProjectID:    "proj_1",
				AccountID:    "acct_1",
				CustomerID:   "cust_1",
			},
			provider: "openai",
			want:     "https://gateway.example.com/proxy/openai/k/tok_abc/w/wl_1/p/proj_1/a/acct_1/c/cust_1/",
		},
		{
			name: "trailing slash stripped from base",
			cfg: &gateway.Config{
				GatewayURL: "https://gateway.example.com/",
			},
			provider: "openai",
			want:     "https://gateway.example.com/proxy/openai/",
		},
		{
			name: "falls back to DefaultProvider when provider arg is empty",
			cfg: &gateway.Config{
				GatewayURL:      "https://gw.example.com",
				DefaultProvider: "cohere",
			},
			provider: "",
			want:     "https://gw.example.com/proxy/cohere/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cfg.ProxyURL(tc.provider)
			if got != tc.want {
				t.Errorf("ProxyURL(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

func TestConfig_SetDefaults(t *testing.T) {
	cfg, _ := gateway.NewConfig()
	cfg.SetDefaults(gateway.WithDefaultProvider("openai"), gateway.WithDefaultModel("gpt-4o"))
	if cfg.DefaultProvider != "openai" {
		t.Errorf("want DefaultProvider openai, got %q", cfg.DefaultProvider)
	}
	if cfg.DefaultModel != "gpt-4o" {
		t.Errorf("want DefaultModel gpt-4o, got %q", cfg.DefaultModel)
	}
}
