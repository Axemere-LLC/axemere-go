package gateway_test

// Live integration tests — require real gateway credentials.
//
// These tests run against the managed Axemere AI Gateway and are skipped
// automatically when AXEMERE_GATEWAY_TOKEN is not set. To run them:
//
//	source tests/.env
//	go test github.com/Axemere-LLC/axemere-go/gateway -run Live -v
//
// The gateway URL defaults to https://us.gw.axemere.ai unless
// AXEMERE_GATEWAY_URL is set.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Axemere-LLC/axemere-go/gateway"
)

const liveProvider = "anthropic"
const liveModel = "claude-haiku-4-5-20251001"

func liveGatewayURL() string {
	if u := os.Getenv("AXEMERE_GATEWAY_URL"); u != "" {
		return u
	}
	return "https://us.gw.axemere.ai"
}

func makeLiveClient(t *testing.T, token string) *gateway.Client {
	t.Helper()
	cfg, err := gateway.NewConfig(
		gateway.WithGatewayURL(liveGatewayURL()),
		gateway.WithGatewayToken(token),
	)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	c, err := gateway.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func skipIfNoToken(t *testing.T) string {
	t.Helper()
	token := os.Getenv("AXEMERE_GATEWAY_TOKEN")
	if token == "" {
		t.Skip("AXEMERE_GATEWAY_TOKEN not set — skipping live gateway test")
	}
	return token
}

// TestLive_BasicCompletion verifies a minimal completion returns non-empty content.
func TestLive_BasicCompletion(t *testing.T) {
	token := skipIfNoToken(t)
	c := makeLiveClient(t, token)

	resp, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Provider: liveProvider,
		Model:    liveModel,
		Messages: []gateway.Message{
			{Role: "user", Content: "Reply with the single word: pong"},
		},
		Params: map[string]any{"max_tokens": 10},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty content from live gateway")
	}
	if resp.RecordID == "" {
		t.Error("expected non-empty record_id from live gateway")
	}
	if resp.Metering.TokensOut == 0 {
		t.Error("expected tokens_out > 0")
	}
}

// TestLive_Streaming verifies streaming delivers content chunks and a final chunk with metering.
func TestLive_Streaming(t *testing.T) {
	token := skipIfNoToken(t)
	c := makeLiveClient(t, token)

	ch, err := c.Stream(context.Background(), gateway.ExecuteRequest{
		Provider: liveProvider,
		Model:    liveModel,
		Messages: []gateway.Message{
			{Role: "user", Content: "Count to three: 1, 2, 3."},
		},
		Params: map[string]any{"max_tokens": 20},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var sb strings.Builder
	var finalChunk *gateway.StreamChunk

	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.IsFinal {
			c := chunk
			finalChunk = &c
		} else {
			sb.WriteString(chunk.Content)
		}
	}

	if sb.Len() == 0 {
		t.Error("expected non-empty assembled content from stream")
	}
	if finalChunk == nil {
		t.Fatal("expected a final chunk marking end of stream")
	}
	if finalChunk.Metering == nil {
		t.Error("expected metering data on final streaming chunk")
	}
}

// TestLive_BadToken verifies that an invalid token causes a GatewayError.
func TestLive_BadToken(t *testing.T) {
	skipIfNoToken(t)
	c := makeLiveClient(t, "axm_k_invalid_token_for_testing")

	_, err := c.Execute(context.Background(), gateway.ExecuteRequest{
		Provider: liveProvider,
		Model:    liveModel,
		Messages: []gateway.Message{{Role: "user", Content: "hi"}},
		Params: map[string]any{"max_tokens": 5},
	})
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
	var gErr *gateway.GatewayError
	if !errors.As(err, &gErr) {
		t.Errorf("expected *GatewayError, got %T: %v", err, err)
	}
}
