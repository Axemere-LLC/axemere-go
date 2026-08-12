package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Client is the Axemere Gateway SDK client.
// Create one via NewClient; it is safe for concurrent use.
type Client struct {
	cfg          *Config
	httpClient   *http.Client
	streamClient *http.Client
}

// NewClient creates a new Client from the provided Config.
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, &GatewayError{Message: "config must not be nil"}
	}
	if cfg.GatewayURL == "" {
		return nil, fmt.Errorf("GatewayURL is required; set AXEMERE_GATEWAY_URL or use WithGatewayURL()")
	}
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		// Streaming responses are long-lived; rely on context cancellation
		// rather than a client-level timeout.
		streamClient: &http.Client{
			Timeout: 0,
		},
	}, nil
}

// Execute sends a single governed request to the gateway and returns the full response.
func (c *Client) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResponse, error) {
	body, _, err := c.buildRequest(req)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(c.cfg.GatewayURL, "/") + "/v1/actions:execute"

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.cfg.GatewayToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.GatewayToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if isTimeoutErr(err) {
			return nil, &GatewayTimeoutError{}
		}
		return nil, &GatewayError{Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &GatewayError{Message: "read response: " + err.Error(), StatusCode: resp.StatusCode, Err: err}
	}

	return parseExecuteResponse(resp.StatusCode, respBytes)
}

// Stream sends a governed streaming request. It returns a channel that delivers
// StreamChunk values. The channel is closed after the final chunk or on error.
//
// IMPORTANT: the caller MUST cancel the context when finished reading — not just
// break out of the range loop. A background goroutine reads from the network and
// sends on the returned channel; it only unblocks when the channel is fully drained
// OR the context is cancelled. If you stop ranging early without cancelling, that
// goroutine leaks until the stream completes on its own. The idiomatic pattern is:
//
//	ctx, cancel := context.WithCancel(ctx)
//	defer cancel()
//	ch, err := client.Stream(ctx, req)
//	// ... range over ch; defer cancel() guarantees cleanup on early return.
func (c *Client) Stream(ctx context.Context, req ExecuteRequest) (<-chan StreamChunk, error) {
	body, _, err := c.buildRequest(req)
	if err != nil {
		return nil, err
	}

	// Add stream flag to params.
	body.Action.Params["stream"] = true

	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := strings.TrimRight(c.cfg.GatewayURL, "/") + "/v1/actions:execute"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.cfg.GatewayToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.GatewayToken)
	}

	resp, err := c.streamClient.Do(httpReq)
	if err != nil {
		if isTimeoutErr(err) {
			return nil, &GatewayTimeoutError{}
		}
		return nil, &GatewayError{Message: err.Error(), Err: err}
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBytes, _ := io.ReadAll(resp.Body)
		_, parseErr := parseExecuteResponse(resp.StatusCode, respBytes)
		if parseErr != nil {
			return nil, parseErr
		}
		return nil, &GatewayError{
			Message:    fmt.Sprintf("unexpected status %d", resp.StatusCode),
			StatusCode: resp.StatusCode,
		}
	}

	ch := make(chan StreamChunk, 8)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		readSSE(ctx, resp.Body, ch)
	}()

	return ch, nil
}

// buildRequest validates the request and constructs the actionRequest wire body.
func (c *Client) buildRequest(req ExecuteRequest) (actionRequest, providerRoute, error) {
	provider := req.Provider
	if provider == "" {
		provider = c.cfg.DefaultProvider
	}
	if provider == "" {
		return actionRequest{}, providerRoute{}, &GatewayError{Message: "provider must be set"}
	}

	model := req.Model
	if model == "" {
		model = c.cfg.DefaultModel
	}
	if model == "" {
		return actionRequest{}, providerRoute{}, &GatewayError{Message: "model must be set"}
	}

	route, ok := providerRoutes[provider]
	if !ok {
		return actionRequest{}, providerRoute{}, &GatewayError{Message: "unknown provider: " + provider}
	}

	targetHost := req.TargetHost
	if targetHost == "" {
		targetHost = route.host
	}
	if targetHost == "" {
		return actionRequest{}, providerRoute{}, &GatewayError{Message: "provider " + provider + " has no default host; set ExecuteRequest.TargetHost explicitly"}
	}

	targetPath := route.path
	if strings.Contains(targetPath, "{model}") {
		targetPath = strings.ReplaceAll(targetPath, "{model}", model)
	}

	// Build params: model + messages first, then caller overrides.
	var params map[string]any
	if provider == "anthropic" {
		params = buildAnthropicParams(model, req.Messages)
	} else {
		params = map[string]any{
			"model":    model,
			"messages": req.Messages,
		}
	}
	for k, v := range req.Params {
		params[k] = v
	}

	// Merge labels: config base, then per-call overrides.
	labels := make(map[string]string, len(c.cfg.Labels)+len(req.Labels))
	for k, v := range c.cfg.Labels {
		labels[k] = v
	}
	for k, v := range req.Labels {
		labels[k] = v
	}
	if len(labels) == 0 {
		labels = nil
	}

	// Resolve workload/project/account/customer with per-call priority.
	workloadID := req.WorkloadID
	if workloadID == "" {
		workloadID = c.cfg.WorkloadID
	}
	projectID := req.ProjectID
	if projectID == "" {
		projectID = c.cfg.ProjectID
	}
	accountID := req.AccountID
	if accountID == "" {
		accountID = c.cfg.AccountID
	}
	customerID := req.CustomerID
	if customerID == "" {
		customerID = c.cfg.CustomerID
	}

	credHint := req.ProviderAPIKey
	if credHint == "" {
		credHint = c.cfg.ProviderAPIKey
	}

	// Prefer a time-ordered UUIDv7; fall back to v4 if the system clock/entropy
	// source rejects v7 rather than panicking on an otherwise-valid request.
	requestID, err := uuid.NewV7()
	if err != nil {
		requestID = uuid.New()
	}

	ar := actionRequest{
		Schema:          "mvgc.action_request.v2",
		RequestID:       requestID.String(),
		OrgID:           "",
		WorkloadID:      workloadID,
		IngressMode:     "explicit_action_request",
		DelegationToken: req.DelegationToken,
		CredentialHint:  credHint,
		Action: actionBlock{
			Type:       "ai.infer",
			Method:     "POST",
			TargetHost: targetHost,
			TargetPath: targetPath,
			Params:     params,
		},
		Attribution: attributionBlock{
			ProjectID:  projectID,
			AccountID:  accountID,
			CustomerID: customerID,
			Labels:     labels,
		},
	}

	return ar, route, nil
}

// defaultAnthropicMaxTokens is sent when the caller does not supply max_tokens
// via ExecuteRequest.Params. Anthropic's Messages API rejects requests that
// omit it ("max_tokens: Field required"), unlike OpenAI-compatible APIs where
// it is optional.
const defaultAnthropicMaxTokens = 1024

// buildAnthropicParams shapes params for Anthropic's Messages API: a
// "system"-role message is not valid inside messages there, so it is lifted
// out into a separate top-level "system" field, and max_tokens is required.
func buildAnthropicParams(model string, messages []Message) map[string]any {
	anthropicMessages := make([]Message, 0, len(messages))
	var system string
	for _, m := range messages {
		if m.Role == "system" {
			if system == "" {
				system = m.Content
			}
			continue
		}
		anthropicMessages = append(anthropicMessages, m)
	}

	params := map[string]any{
		"model":      model,
		"messages":   anthropicMessages,
		"max_tokens": defaultAnthropicMaxTokens,
	}
	if system != "" {
		params["system"] = system
	}
	return params
}

// parseExecuteResponse decodes a gateway response body into ExecuteResponse or an error.
func parseExecuteResponse(statusCode int, body []byte) (*ExecuteResponse, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &GatewayError{
			Message:    fmt.Sprintf("invalid JSON response (status %d)", statusCode),
			StatusCode: statusCode,
		}
	}

	// Check for policy denial.
	if decision, _ := raw["decision"].(string); decision == "deny" {
		reason, _ := raw["reason"].(string)
		recordID, _ := raw["record_id"].(string)
		trace, _ := raw["trace"].(map[string]any)
		return nil, &PolicyDeniedError{
			GatewayError: GatewayError{
				Message:      fmt.Sprintf("policy denied: %s", reason),
				StatusCode:   statusCode,
				ResponseBody: raw,
			},
			Reason:   reason,
			Trace:    trace,
			RecordID: recordID,
		}
	}

	// Check for quota exceeded.
	if statusCode == http.StatusPaymentRequired || statusCode == http.StatusTooManyRequests {
		msg, _ := raw["message"].(string)
		if msg == "" {
			msg = fmt.Sprintf("quota exceeded (status %d)", statusCode)
		}
		upgradeURL, _ := raw["upgrade_url"].(string)
		retryAfter, _ := raw["retry_after"].(float64)
		return nil, &QuotaExceededError{
			GatewayError: GatewayError{
				Message:      msg,
				StatusCode:   statusCode,
				ResponseBody: raw,
			},
			UpgradeURL: upgradeURL,
			RetryAfter: int(retryAfter),
		}
	}

	// Check for gateway timeout.
	if statusCode == http.StatusGatewayTimeout {
		return nil, &GatewayTimeoutError{}
	}

	// Generic non-2xx error.
	if statusCode < 200 || statusCode >= 300 {
		msg, _ := raw["message"].(string)
		if msg == "" {
			msg = fmt.Sprintf("gateway error (status %d)", statusCode)
		}
		return nil, &GatewayError{
			Message:      msg,
			StatusCode:   statusCode,
			ResponseBody: raw,
		}
	}

	// The gateway's own HTTP status only reflects the gateway request itself —
	// a policy "allow" is still wrapped in a 200 even when the upstream
	// provider rejected the request (e.g. Anthropic 400s on a missing
	// max_tokens). That failure only shows up nested in result.status_code,
	// so it must be checked explicitly or it silently reads as success with
	// empty content and zero metering.
	var resultObj map[string]any
	if ro, ok := raw["result"].(map[string]any); ok {
		resultObj = ro
		if upstreamStatus, ok := resultObj["status_code"].(float64); ok && upstreamStatus >= 400 {
			bodyObj, _ := resultObj["body"].(map[string]any)
			return nil, &GatewayError{
				Message:      fmt.Sprintf("upstream provider returned %d: %s", int(upstreamStatus), extractErrorMessage(bodyObj)),
				StatusCode:   int(upstreamStatus),
				ResponseBody: bodyObj,
			}
		}
	}

	// Happy path: decode the response.
	resp := &ExecuteResponse{}

	resp.RecordID, _ = raw["record_id"].(string)
	resp.RecordHash, _ = raw["record_hash"].(string)
	resp.Provider, _ = raw["provider"].(string)

	// Gateway returns the provider body in result.body.
	if bodyObj, ok := resultObj["body"].(map[string]any); ok {
		resp.ProviderResponse = bodyObj
		resp.Model, _ = bodyObj["model"].(string)
	}

	resp.Content = extractContent(raw)

	if mRaw, ok := raw["metering"]; ok {
		mBytes, err := json.Marshal(mRaw)
		if err == nil {
			_ = json.Unmarshal(mBytes, &resp.Metering)
		}
	}

	return resp, nil
}

// extractContent pulls the assistant text from a gateway response map.
// The gateway places the provider body in result.body; falls back to a
// legacy top-level provider_response field for compatibility.
func extractContent(raw map[string]any) string {
	// Top-level "content" shortcut (gateway may pre-extract in some modes).
	if c, ok := raw["content"].(string); ok && c != "" {
		return c
	}

	// Standard gateway shape: result.body
	var body map[string]any
	if resultObj, ok := raw["result"].(map[string]any); ok {
		body, _ = resultObj["body"].(map[string]any)
	}
	// Legacy fallback: top-level provider_response
	if body == nil {
		body, _ = raw["provider_response"].(map[string]any)
	}
	if body == nil {
		return ""
	}

	// Anthropic: content[0].text
	if contents, ok := body["content"].([]any); ok && len(contents) > 0 {
		if block, ok := contents[0].(map[string]any); ok {
			if text, ok := block["text"].(string); ok {
				return text
			}
		}
	}

	// OpenAI: choices[0].message.content
	if choices, ok := body["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					return content
				}
			}
		}
	}

	return ""
}

// extractErrorMessage pulls a human-readable message out of a provider error
// body shaped like {"error": {"message": "..."}} (OpenAI/Anthropic) or
// {"error": "..."}. Falls back to the raw body when neither shape matches.
func extractErrorMessage(body map[string]any) string {
	if body == nil {
		return ""
	}
	switch e := body["error"].(type) {
	case map[string]any:
		if msg, ok := e["message"].(string); ok {
			return msg
		}
		return fmt.Sprintf("%v", e)
	case string:
		return e
	}
	return fmt.Sprintf("%v", body)
}

// readSSE reads Server-Sent Events from r and sends StreamChunk values to ch.
// It is run in a goroutine; ch is closed by the caller after this returns.
func readSSE(ctx context.Context, r io.Reader, ch chan<- StreamChunk) {
	scanner := bufio.NewScanner(r)
	// Raise the per-line cap to 1MB; SSE data lines (full JSON events) can exceed
	// the default 64KB bufio.Scanner limit and would otherwise abort the stream.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var recordID string
	var metering *Metering

	send := func(chunk StreamChunk) bool {
		select {
		case <-ctx.Done():
			return false
		case ch <- chunk:
			return true
		}
	}

	for scanner.Scan() {
		if ctx.Err() != nil {
			send(StreamChunk{Err: ctx.Err()})
			return
		}

		line := scanner.Text()

		// Skip comment lines and empty lines.
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			send(StreamChunk{IsFinal: true, RecordID: recordID, Metering: metering})
			return
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		// Metering event: capture and do NOT forward as a content chunk.
		if evType, _ := event["type"].(string); evType == "mvgc_metering" {
			if rid, ok := event["record_id"].(string); ok {
				recordID = rid
			}
			if mRaw, ok := event["metering"]; ok {
				mBytes, err := json.Marshal(mRaw)
				if err == nil {
					m := &Metering{}
					if err := json.Unmarshal(mBytes, m); err == nil {
						metering = m
					}
				}
			}
			continue
		}

		// Standard OpenAI-style delta.
		if choices, ok := event["choices"].([]any); ok && len(choices) > 0 {
			choice, _ := choices[0].(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			content, _ := delta["content"].(string)
			finishReason, _ := choice["finish_reason"].(string)

			if content != "" {
				if !send(StreamChunk{Content: content}) {
					return
				}
			}

			if finishReason != "" && finishReason != "null" {
				send(StreamChunk{IsFinal: true, RecordID: recordID, Metering: metering})
				return
			}
			continue
		}

		// Anthropic-style content_block_delta.
		if evType, _ := event["type"].(string); evType == "content_block_delta" {
			if delta, ok := event["delta"].(map[string]any); ok {
				if text, ok := delta["text"].(string); ok && text != "" {
					if !send(StreamChunk{Content: text}) {
						return
					}
				}
			}
			continue
		}

		// Anthropic message_stop signals end of stream.
		if evType, _ := event["type"].(string); evType == "message_stop" {
			send(StreamChunk{IsFinal: true, RecordID: recordID, Metering: metering})
			return
		}
	}

	if err := scanner.Err(); err != nil {
		send(StreamChunk{Err: err})
		return
	}

	// Reached EOF without [DONE]; send final chunk anyway.
	send(StreamChunk{IsFinal: true, RecordID: recordID, Metering: metering})
}

// isTimeoutErr reports whether err is a deadline exceeded / timeout error.
// It relies on the error chain (errors.Is / errors.As) rather than string
// matching, so it correctly identifies context deadlines and net.Error timeouts.
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
