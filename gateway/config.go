package gateway

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 120 * time.Second

// Config holds gateway connection and default attribution settings.
// All fields can be set via environment variables read at NewConfig time.
type Config struct {
	GatewayURL      string            // AXEMERE_GATEWAY_URL
	GatewayToken    string            // AXEMERE_GATEWAY_TOKEN
	DefaultProvider string            // AXEMERE_PROVIDER
	DefaultModel    string            // AXEMERE_MODEL
	WorkloadID      string            // AXEMERE_WORKLOAD_ID
	ProjectID       string            // AXEMERE_PROJECT_ID
	AccountID       string            // AXEMERE_ACCOUNT_ID
	CustomerID      string            // AXEMERE_CUSTOMER_ID
	Labels          map[string]string // AXEMERE_LABELS (JSON string)
	ProviderAPIKey  string            // AXEMERE_PROVIDER_API_KEY
	Timeout         time.Duration     // AXEMERE_TIMEOUT_SECONDS (integer seconds)
}

// Option is a functional option for Config.
type Option func(*Config)

// WithGatewayURL sets the gateway base URL.
func WithGatewayURL(u string) Option {
	return func(c *Config) { c.GatewayURL = u }
}

// WithGatewayToken sets the bearer token for gateway authentication.
func WithGatewayToken(token string) Option {
	return func(c *Config) { c.GatewayToken = token }
}

// WithDefaultProvider sets the provider used when ExecuteRequest.Provider is empty.
func WithDefaultProvider(p string) Option {
	return func(c *Config) { c.DefaultProvider = p }
}

// WithDefaultModel sets the model used when ExecuteRequest.Model is empty.
func WithDefaultModel(m string) Option {
	return func(c *Config) { c.DefaultModel = m }
}

// WithWorkloadID sets the default workload attribution ID.
func WithWorkloadID(id string) Option {
	return func(c *Config) { c.WorkloadID = id }
}

// WithProjectID sets the default project attribution ID.
func WithProjectID(id string) Option {
	return func(c *Config) { c.ProjectID = id }
}

// WithAccountID sets the default account attribution ID.
func WithAccountID(id string) Option {
	return func(c *Config) { c.AccountID = id }
}

// WithCustomerID sets the default customer attribution ID.
func WithCustomerID(id string) Option {
	return func(c *Config) { c.CustomerID = id }
}

// WithLabels sets the default attribution labels (merged under per-call labels).
func WithLabels(labels map[string]string) Option {
	return func(c *Config) { c.Labels = labels }
}

// WithProviderAPIKey sets a provider API key forwarded to the upstream provider.
func WithProviderAPIKey(key string) Option {
	return func(c *Config) { c.ProviderAPIKey = key }
}

// WithTimeout sets the HTTP request timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Config) { c.Timeout = d }
}

// NewConfig builds a Config by reading environment variables first,
// then applying any provided functional options. Options take precedence.
// Returns an error if AXEMERE_LABELS is set but contains invalid JSON.
func NewConfig(opts ...Option) (*Config, error) {
	cfg := &Config{
		Timeout: defaultTimeout,
	}

	// Read env vars.
	cfg.GatewayURL = os.Getenv("AXEMERE_GATEWAY_URL")
	cfg.GatewayToken = os.Getenv("AXEMERE_GATEWAY_TOKEN")
	cfg.DefaultProvider = os.Getenv("AXEMERE_PROVIDER")
	cfg.DefaultModel = os.Getenv("AXEMERE_MODEL")
	cfg.WorkloadID = os.Getenv("AXEMERE_WORKLOAD_ID")
	cfg.ProjectID = os.Getenv("AXEMERE_PROJECT_ID")
	cfg.AccountID = os.Getenv("AXEMERE_ACCOUNT_ID")
	cfg.CustomerID = os.Getenv("AXEMERE_CUSTOMER_ID")
	cfg.ProviderAPIKey = os.Getenv("AXEMERE_PROVIDER_API_KEY")

	if raw := os.Getenv("AXEMERE_LABELS"); raw != "" {
		var labels map[string]string
		if err := json.Unmarshal([]byte(raw), &labels); err != nil {
			return nil, fmt.Errorf("AXEMERE_LABELS is not valid JSON: %w", err)
		}
		cfg.Labels = labels
	}

	if secs := os.Getenv("AXEMERE_TIMEOUT_SECONDS"); secs != "" {
		n, err := strconv.Atoi(secs)
		if err != nil {
			return nil, fmt.Errorf("AXEMERE_TIMEOUT_SECONDS must be an integer: %w", err)
		}
		cfg.Timeout = time.Duration(n) * time.Second
	}

	// Apply functional options (take precedence over env vars).
	for _, opt := range opts {
		opt(cfg)
	}

	return cfg, nil
}

// SetDefaults applies additional functional options to an existing Config.
func (c *Config) SetDefaults(opts ...Option) {
	for _, opt := range opts {
		opt(c)
	}
}

// ProxyURL builds a proxy base URL for use as a provider SDK base URL.
//
// Format: {GatewayURL}/proxy/{provider}[/k/{token}][/w/{workload}][/p/{project}][/a/{account}][/c/{customer}]/
//
// The provider argument overrides the Config.DefaultProvider if non-empty.
func (c *Config) ProxyURL(provider string) string {
	if provider == "" {
		provider = c.DefaultProvider
	}

	base := strings.TrimRight(c.GatewayURL, "/")
	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("/proxy/")
	sb.WriteString(url.PathEscape(provider))

	if c.GatewayToken != "" {
		sb.WriteString("/k/")
		sb.WriteString(url.PathEscape(c.GatewayToken))
	}
	if c.WorkloadID != "" {
		sb.WriteString("/w/")
		sb.WriteString(url.PathEscape(c.WorkloadID))
	}
	if c.ProjectID != "" {
		sb.WriteString("/p/")
		sb.WriteString(url.PathEscape(c.ProjectID))
	}
	if c.AccountID != "" {
		sb.WriteString("/a/")
		sb.WriteString(url.PathEscape(c.AccountID))
	}
	if c.CustomerID != "" {
		sb.WriteString("/c/")
		sb.WriteString(url.PathEscape(c.CustomerID))
	}
	sb.WriteString("/")
	return sb.String()
}
