package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Client is the Axemere Gateway SDK client.
// Create one via NewClient; it is safe for concurrent use.
type Client struct {
	cfg           *Config
	httpClient    *http.Client
	streamClient  *http.Client
}

// NewClient creates a new Client from the provided Config.
func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, &GatewayError{Message: "config must not be nil"}
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
	body, route, err := c.buildRequest(req)
	if err != nil {
		return nil, err
	}
	_ = route

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
		return nil, &GatewayError{Message: err.Error()}
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &GatewayError{Message: "read response: " + err.Error(), StatusCode: resp.StatusCode}
	}

	return parseExecuteResponse(resp.StatusCode, respBytes)
}

// Stream sends a governed streaming request. It returns a channel that delivers
// StreamChunk values. The channel is closed after the final chunk or on error.
// The caller must drain the channel to avoid leaking the background goroutine.
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
		return nil, &GatewayError{Message: err.Error()}
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

	targetPath := route.path
	if strings.Contains(targetPath, "{model}") {
		targetPath = strings.ReplaceAll(targetPath, "{model}", model)
	}

	// Build params: model + messages first, then caller overrides.
	params := map[string]any{
		"model":    model,
		"messages": req.Messages,
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

	ar := actionRequest{
		Schema:          "mvgc.action_request.v2",
		RequestID:       uuid.Must(uuid.NewV7()).String(),
		OrgID:           "",
		WorkloadID:      workloadID,
		IngressMode:     "explicit_action_request",
		DelegationToken: req.DelegationToken,
		CredentialHint:  credHint,
		Action: actionBlock{
			Type:       "http",
			Method:     "POST",
			TargetHost: route.host,
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
	if statusCode == http.StatusPaymentRequired || statusCode == 429 {
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

	// Happy path: decode the response.
	resp := &ExecuteResponse{}

	resp.RecordID, _ = raw["record_id"].(string)
	resp.RecordHash, _ = raw["record_hash"].(string)
	resp.Provider, _ = raw["provider"].(string)
	resp.Model, _ = raw["model"].(string)

	if provResp, ok := raw["provider_response"].(map[string]any); ok {
		resp.ProviderResponse = provResp
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
// It tries OpenAI-style choices[0].message.content first, then
// Anthropic-style content[0].text, falling back to the top-level
// "result" or "content" string field.
func extractContent(raw map[string]any) string {
	// Top-level "content" or "result" shortcut (gateway may pre-extract).
	if c, ok := raw["content"].(string); ok && c != "" {
		return c
	}
	if c, ok := raw["result"].(string); ok && c != "" {
		return c
	}

	provResp, _ := raw["provider_response"].(map[string]any)
	if provResp == nil {
		return ""
	}

	// OpenAI: choices[0].message.content
	if choices, ok := provResp["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					return content
				}
			}
		}
	}

	// Anthropic: content[0].text
	if contents, ok := provResp["content"].([]any); ok && len(contents) > 0 {
		if block, ok := contents[0].(map[string]any); ok {
			if text, ok := block["text"].(string); ok {
				return text
			}
		}
	}

	return ""
}

// readSSE reads Server-Sent Events from r and sends StreamChunk values to ch.
// It is run in a goroutine; ch is closed by the caller after this returns.
func readSSE(ctx context.Context, r io.Reader, ch chan<- StreamChunk) {
	scanner := bufio.NewScanner(r)
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
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "deadline exceeded") ||
		isNetTimeoutErr(err)
}

func isNetTimeoutErr(err error) bool {
	type timeoutError interface {
		Timeout() bool
	}
	var te timeoutError
	// Walk the error chain.
	for e := err; e != nil; {
		if t, ok := e.(timeoutError); ok && t.Timeout() {
			return true
		}
		// Unwrap one level.
		type unwrapper interface{ Unwrap() error }
		if u, ok := e.(unwrapper); ok {
			e = u.Unwrap()
		} else {
			break
		}
	}
	return te != nil
}

