package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardOpenAIWSV2_PreservesDisconnectedIncompleteResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		metered bool
		image   bool
	}{
		{name: "unmetered_disconnect"},
		{name: "partial_token_usage", metered: true},
		{name: "partial_image_usage", metered: true, image: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &cancelOnFirstWriteResponseWriter{cancel: cancel}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			groupID := int64(9102)
			c.Set("api_key", &APIKey{GroupID: &groupID, Group: &Group{
				ID: groupID, AllowImageGeneration: true,
			}})

			events := [][]byte{
				[]byte(`{"type":"response.created","response":{"id":"resp_partial_ws","model":"gpt-5.5"}}`),
				[]byte(`{"type":"response.output_text.delta","delta":"partial"}`),
			}
			if tt.metered {
				events = append(events, []byte(`{"type":"response.in_progress","response":{"id":"resp_partial_ws","usage":{"input_tokens":3,"output_tokens":5}}}`))
			}
			if tt.image {
				events = append(events, []byte(`{"type":"response.output_item.done","item":{"id":"img_partial_ws","type":"image_generation_call","status":"completed","result":"finished-image","size":"1024x1024"}}`))
			}
			conn := &openAIWSCancelSafeConn{openAIWSCaptureConn: &openAIWSCaptureConn{events: events}}
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(&openAIWSClientConnCancelDialer{conn: conn})
			svc := &OpenAIGatewayService{
				cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(), openaiWSPool: pool,
			}
			account := &Account{
				ID: 9102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{
					"api_key": "sk-test", "model_mapping": map[string]any{"text-alias": "gpt-5.5"},
				},
				Extra: map[string]any{"responses_websockets_v2_enabled": true},
			}
			body := []byte(`{"model":"text-alias","stream":true,"input":"hello"}`)
			if tt.image {
				body = []byte(`{"model":"text-alias","stream":true,"input":"draw","tools":[{"type":"image_generation","model":"gpt-image-2","size":"1024x1024"}]}`)
			}

			result, err := svc.Forward(ctx, c, account, body)

			require.ErrorIs(t, err, context.Canceled)
			require.NotNil(t, result, "the outer Forward route must retain the WS drain result")
			require.True(t, result.OpenAIWSMode)
			require.True(t, result.ClientDisconnect)
			require.Empty(t, result.UpstreamTerminalEvent)
			require.Equal(t, "resp_partial_ws", result.ResponseID)
			require.Equal(t, "gpt-5.5", result.UpstreamModel)
			if tt.metered {
				require.Equal(t, OpenAIUsage{InputTokens: 3, OutputTokens: 5}, result.Usage)
			} else {
				require.Equal(t, OpenAIUsage{}, result.Usage)
			}
			if tt.image {
				require.Equal(t, 1, result.ImageCount)
				require.Equal(t, "gpt-image-2", result.BillingModel)
				require.Equal(t, "1K", result.ImageSize)
				require.Equal(t, "1024x1024", result.ImageInputSize)
			} else {
				require.Equal(t, "gpt-5.5", result.BillingModel)
			}
			require.NotContains(t, writer.body.String(), "response.failed")
			require.True(t, conn.closed, "an incomplete WS connection must not be reused")
		})
	}
}
