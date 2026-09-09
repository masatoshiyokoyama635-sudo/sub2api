package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HTTP POST /v1/responses -> forwardOpenAIWSV2 keeps the canonical outbound
// tier separate from response.completed.service_tier for usage-time billing.
func TestForwardOpenAIWSV2_KeepsOutboundAndObservedServiceTiersSeparate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name        string
		requestTier string
		stream      bool
	}{
		{name: "priority_nonstream", requestTier: "priority", stream: false},
		{name: "fast_stream", requestTier: "fast", stream: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("User-Agent", "unit-test-agent/1.0")

			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.Enabled = true
			cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
			cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

			captureConn := &openAIWSCaptureConn{
				events: [][]byte{
					[]byte(`{"type":"response.completed","response":{"id":"resp_tier_v2","model":"gpt-5.5","status":"completed","service_tier":"default","usage":{"input_tokens":1,"output_tokens":1}}}`),
				},
			}
			captureDialer := &openAIWSCaptureDialer{conn: captureConn}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(captureDialer)

			svc := &OpenAIGatewayService{
				cfg:              cfg,
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}
			account := &Account{
				ID:          5882,
				Name:        "openai-ws-v2-tier",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true},
			}

			body := []byte(fmt.Sprintf(
				`{"model":"gpt-5.5","stream":%t,"service_tier":%q,"input":[{"type":"input_text","text":"hi"}]}`,
				tc.stream, tc.requestTier,
			))
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.OpenAIWSMode, "must take HTTP POST → forwardOpenAIWSV2, not HTTP fallback")
			require.Equal(t, tc.stream, result.Stream)
			require.Equal(t, "resp_tier_v2", result.RequestID)
			require.NotNil(t, result.ServiceTier)
			require.Equal(t, "priority", *result.ServiceTier)
			require.Equal(t, "default", result.UpstreamResponseServiceTier)
			require.Equal(t, "priority", captureConn.lastWrite["service_tier"],
				"outbound WS payload still carries the requested Fast tier")
		})
	}
}

func TestForwardOpenAIWSV2_MarksCyberPolicyForFailureEventShapes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		upstreamEvent []byte
		wantError     bool
		wantInput     int
		wantOutput    int
	}{
		{
			name:          "error_before_return",
			upstreamEvent: []byte(`{"type":"error","error":{"type":"rate_limit_error","code":"cyber_policy","message":"rate limit exceeded by cyber policy"},"usage":{"input_tokens":5,"output_tokens":1}}`),
			wantError:     true,
			wantInput:     5,
			wantOutput:    1,
		},
		{
			name:          "response_failed_terminal",
			upstreamEvent: []byte(`{"type":"response.failed","response":{"id":"resp_cyber","status":"failed","error":{"code":"cyber_policy","message":"blocked by cyber policy"},"usage":{"input_tokens":9,"output_tokens":2}}}`),
			wantInput:     9,
			wantOutput:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)

			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			captureConn := &openAIWSCaptureConn{events: [][]byte{append([]byte(nil), tt.upstreamEvent...)}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
			svc := &OpenAIGatewayService{
				cfg:              cfg,
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}
			account := &Account{
				ID: 5883, Name: "openai-ws-v2-cyber", Platform: PlatformOpenAI,
				Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true},
			}

			result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`))
			if tt.wantError {
				require.Error(t, err)
				require.Nil(t, result)
				var failoverErr *UpstreamFailoverError
				require.False(t, errors.As(err, &failoverErr))
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
			}

			mark := GetOpsCyberPolicy(c)
			require.NotNil(t, mark)
			require.Equal(t, "cyber_policy", mark.Code)
			require.Equal(t, tt.wantInput, mark.UpstreamInTok)
			require.Equal(t, tt.wantOutput, mark.UpstreamOutTok)
		})
	}
}

// Every exit that returns usage must retain completed image outputs and the
// requested effort, including when the client disconnects before completion.
func TestForwardOpenAIWSV2_PreservesAccountingMetadataOnEveryUsageReturn(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		stream     bool
		disconnect bool
		complete   bool
	}{
		{name: "completed_stream", stream: true, complete: true},
		{name: "completed_nonstream", complete: true},
		{name: "partial_after_stream_disconnect", stream: true, disconnect: true},
		{name: "completed_after_nonstream_disconnect", disconnect: true, complete: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)

			const image = `{"id":"img_accounting","type":"image_generation_call","result":"completed-image","size":"3840x2160"}`
			events := [][]byte{
				[]byte(`{"type":"response.output_item.done","response":{"id":"resp_accounting","usage":{"input_tokens":3,"output_tokens":5}},"item":` + image + `}`),
			}
			if tt.complete {
				events = append(events, []byte(`{"type":"response.completed","response":{"id":"resp_accounting","model":"gpt-5.5","output":[`+image+`],"usage":{"input_tokens":7,"output_tokens":11}}}`))
			}
			conn := &openAIWSAccountingCancelConn{openAIWSCaptureConn: &openAIWSCaptureConn{events: events}}
			if tt.disconnect {
				conn.cancel = cancel
			}
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(&openAIWSClientConnCancelDialer{conn: conn})
			svc := &OpenAIGatewayService{cfg: cfg, toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
			account := &Account{
				ID: 5884, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
			}
			reqBody := map[string]any{
				"model": "gpt-5.5", "stream": tt.stream, "input": "draw a landscape",
				"reasoning": map[string]any{"effort": "max"},
			}
			result, err := svc.forwardOpenAIWSV2(ctx, c, account, reqBody, "", "sk-test",
				OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
				false, tt.stream, "gpt-5.5", "gpt-5.5", time.Now(), 1, "", nil)
			if tt.complete {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.NotNil(t, result)
			assert.True(t, result.OpenAIWSMode)
			assert.Equal(t, tt.disconnect, result.ClientDisconnect)
			assert.Equal(t, "resp_accounting", result.ResponseID)
			if tt.complete {
				assert.Equal(t, 7, result.Usage.InputTokens)
				assert.Equal(t, 11, result.Usage.OutputTokens)
			} else {
				assert.Equal(t, 3, result.Usage.InputTokens)
				assert.Equal(t, 5, result.Usage.OutputTokens)
			}
			assert.Equal(t, 1, result.ImageCount, "bill each completed image once, even without a terminal event")
			assert.Equal(t, []string{"3840x2160"}, result.ImageOutputSizes)
			ApplyOpenAIImageBillingResolution(result)
			assert.Equal(t, ImageBillingSize4K, result.ImageSize, "billing must use the observed output size")
			require.NotNil(t, result.ReasoningEffort)
			assert.Equal(t, "xhigh", *result.ReasoningEffort)
			require.NotNil(t, result.RequestedReasoningEffort)
			assert.Equal(t, "max", *result.RequestedReasoningEffort, "retain the requested effort separately from the forwarded scale")
		})
	}
}

type openAIWSAccountingCancelConn struct {
	*openAIWSCaptureConn
	cancel context.CancelFunc
}

func (c *openAIWSAccountingCancelConn) ReadMessage(ctx context.Context) ([]byte, error) {
	message, err := c.openAIWSCaptureConn.ReadMessage(ctx)
	if err == nil && c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	return message, err
}
