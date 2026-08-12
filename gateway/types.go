package gateway

// Message is a single conversation turn sent to the provider.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CostBreakdownItem is one line item within a metering cost breakdown.
type CostBreakdownItem struct {
	Label          string `json:"label"`
	Tokens         int    `json:"tokens"`
	RatePerMillion string `json:"rate_per_million"`
	SubtotalUSD    string `json:"subtotal_usd"`
}

// Metering carries token counts and cost information returned by the gateway.
type Metering struct {
	CostUSD                 string              `json:"cost_usd"`
	TokensIn                int                 `json:"tokens_in"`
	TokensOut               int                 `json:"tokens_out"`
	BytesIn                 int                 `json:"bytes_in"`
	BytesOut                int                 `json:"bytes_out"`
	CacheHitTokens          *int                `json:"cache_hit_tokens,omitempty"`
	CacheMissTokens         *int                `json:"cache_miss_tokens,omitempty"`
	CacheCreationTokens     *int                `json:"cache_creation_tokens,omitempty"`
	ReasoningTokens         *int                `json:"reasoning_tokens,omitempty"`
	PricingConfigVersion    *int                `json:"pricing_config_version,omitempty"`
	OrgPricingConfigVersion *int                `json:"org_pricing_config_version,omitempty"`
	MarkupMultiplierApplied string              `json:"markup_multiplier_applied"`
	CostBreakdown           []CostBreakdownItem `json:"cost_breakdown"`
}

// ExecuteRequest describes a single governed action request.
type ExecuteRequest struct {
	Messages        []Message
	Provider        string // overrides Config.DefaultProvider; required if not set in Config
	Model           string // overrides Config.DefaultModel; required if not set in Config
	TargetHost      string // overrides the provider's registry host; required for azure_openai
	WorkloadID      string
	ProjectID       string
	AccountID       string
	CustomerID      string
	Labels          map[string]string // merged on top of Config.Labels; per-call keys win
	ProviderAPIKey  string
	DelegationToken string
	Params          map[string]any // extra provider parameters merged into action.params
}

// ExecuteResponse is the parsed gateway response for a non-streaming request.
type ExecuteResponse struct {
	Content          string         `json:"content"`
	RecordID         string         `json:"record_id"`
	Metering         Metering       `json:"metering"`
	Provider         string         `json:"provider"`
	Model            string         `json:"model"`
	RecordHash       string         `json:"record_hash"`
	ProviderResponse map[string]any `json:"provider_response,omitempty"`
}

// StreamChunk is one token delta delivered over a streaming response.
// IsFinal is true on the last chunk; RecordID and Metering are populated only then.
// Err is non-nil when the stream terminates with an error.
type StreamChunk struct {
	Content  string
	IsFinal  bool
	RecordID string
	Metering *Metering
	Err      error
}

// actionRequest is the wire body for POST /v1/actions:execute.
type actionRequest struct {
	Schema          string           `json:"schema"`
	RequestID       string           `json:"request_id"`
	OrgID           string           `json:"org_id"`
	WorkloadID      string           `json:"workload_id"`
	IngressMode     string           `json:"ingress_mode"`
	DelegationToken string           `json:"delegation_token,omitempty"`
	CredentialHint  string           `json:"credential_hint,omitempty"`
	Action          actionBlock      `json:"action"`
	Attribution     attributionBlock `json:"attribution"`
}

type actionBlock struct {
	Type       string         `json:"type"`
	Method     string         `json:"method"`
	TargetHost string         `json:"target_host"`
	TargetPath string         `json:"target_path"`
	Params     map[string]any `json:"params"`
}

type attributionBlock struct {
	ProjectID  string            `json:"project_id,omitempty"`
	AccountID  string            `json:"account_id,omitempty"`
	CustomerID string            `json:"customer_id,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}
