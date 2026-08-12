package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const passiveImageNamespacePayload = `{
	"type":"response.create",
	"model":"client-model",
	"stream":false,
	"input":"write code",
	"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}],
	"tool_choice":"auto"
}`

func TestOpenAIGatewayServiceForward_ChannelMappedPassiveImageNamespaceRemainsNonExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		payload       string
		passthrough   bool
		wantForbidden bool
	}{
		{
			name:    "managed passive namespace with auto is forwarded",
			payload: passiveImageNamespacePayload,
		},
		{
			name:        "passthrough passive namespace with auto is forwarded",
			payload:     passiveImageNamespacePayload,
			passthrough: true,
		},
		{
			name:          "managed native image tool is blocked",
			payload:       `{"model":"client-model","stream":false,"input":"draw","tools":[{"type":"image_generation"}],"tool_choice":"auto"}`,
			wantForbidden: true,
		},
		{
			name:          "passthrough explicit image tool choice is blocked",
			payload:       `{"model":"client-model","stream":false,"input":"draw","tools":[{"type":"namespace","name":"image_gen"}],"tool_choice":{"type":"namespace","name":"image_gen"}}`,
			passthrough:   true,
			wantForbidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_mapped_text","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}`)),
			}}
			svc := newOpenAIImageGenerationControlTestService(upstream)
			c, recorder := newOpenAIImageGenerationControlTestContext(false, "unit-test-agent/1.0")
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

			// This is the body shape passed to Forward after handler-level channel
			// mapping. Mapping intentionally leaves the request-scoped hint unknown.
			mappedBody := svc.ReplaceModelInBody([]byte(tt.payload), "gpt-5.4")
			_, hintKnown := getOpenAIImageIntentHint(c)
			require.False(t, hintKnown)
			account := newOpenAIImageGenerationControlTestAccount()
			if tt.passthrough {
				account.Extra = map[string]any{"openai_passthrough": true}
			}

			result, err := svc.Forward(context.Background(), c, account, mappedBody)

			if tt.wantForbidden {
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusForbidden, recorder.Code)
				require.Equal(t, "permission_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
				require.Nil(t, upstream.lastReq)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotEqual(t, http.StatusForbidden, recorder.Code)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
		})
	}
}

func TestOpenAIGatewayServiceResponsesWebSocket_PassiveImageNamespaceRemainsNonExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		payload       string
		wantForbidden bool
	}{
		{
			name:    "passive namespace with auto is forwarded",
			payload: passiveImageNamespacePayload,
		},
		{
			name:          "native image tool is blocked",
			payload:       `{"type":"response.create","model":"client-model","stream":false,"input":"draw","tools":[{"type":"image_generation"}],"tool_choice":"auto"}`,
			wantForbidden: true,
		},
		{
			name:          "explicit image tool choice is blocked",
			payload:       `{"type":"response.create","model":"client-model","stream":false,"input":"draw","tools":[{"type":"namespace","name":"image_gen"}],"tool_choice":{"type":"namespace","name":"image_gen"}}`,
			wantForbidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstreamWrites, proxyErr, clientCloseErr := runOpenAIWSImageIntentGateCase(t, tt.payload)

			if tt.wantForbidden {
				var policyClose *OpenAIWSClientCloseError
				require.ErrorAs(t, proxyErr, &policyClose)
				require.Equal(t, coderws.StatusPolicyViolation, policyClose.StatusCode())
				require.Equal(t, ImageGenerationPermissionMessage(), policyClose.Reason())
				require.ErrorAs(t, clientCloseErr, new(coderws.CloseError))
				var clientClose coderws.CloseError
				require.ErrorAs(t, clientCloseErr, &clientClose)
				require.Equal(t, coderws.StatusPolicyViolation, clientClose.Code)
				require.Equal(t, ImageGenerationPermissionMessage(), clientClose.Reason)
				require.Zero(t, upstreamWrites)
				return
			}
			require.NoError(t, proxyErr)
			require.NoError(t, clientCloseErr)
			require.Equal(t, 1, upstreamWrites)
		})
	}
}

func runOpenAIWSImageIntentGateCase(t *testing.T, payload string) (int, error, error) {
	t.Helper()

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

	upstreamConn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_passive_namespace","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	upstreamDialer := &openAIWSCaptureDialer{conn: upstreamConn}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(upstreamDialer)
	defer pool.Close()

	svc := &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     &httpUpstreamRecorder{},
		cache:            &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
		openaiWSPool:     pool,
	}
	account := &Account{
		ID:          8811,
		Name:        "openai-passive-image-intent-ws",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "sk-test",
			"model_mapping": map[string]any{
				"client-model": "gpt-5.4",
			},
		},
		Extra: map[string]any{"responses_websockets_v2_enabled": true},
	}
	groupID := int64(8812)
	apiKey := &APIKey{
		ID:      8813,
		GroupID: &groupID,
		Group: &Group{
			ID:                   groupID,
			AllowImageGeneration: false,
		},
	}

	serverErrCh := make(chan error, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErrCh <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		msgType, firstMessage, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
			serverErrCh <- errors.New("unexpected websocket message type")
			return
		}

		recorder := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(recorder)
		ginCtx.Request = r.Clone(r.Context())
		ginCtx.Set("api_key", apiKey)
		proxyErr := svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, nil)
		var closeErr *OpenAIWSClientCloseError
		if errors.As(proxyErr, &closeErr) {
			_ = conn.Close(closeErr.StatusCode(), closeErr.Reason())
		}
		serverErrCh <- proxyErr
	}))
	defer wsServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(payload))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, clientReadErr := clientConn.Read(readCtx)
	cancelRead()
	if clientReadErr == nil {
		require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
		require.Equal(t, "resp_passive_namespace", gjson.GetBytes(event, "response.id").String())
		require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	}

	var proxyErr error
	select {
	case proxyErr = <-serverErrCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for websocket image-intent gate result")
	}

	upstreamConn.mu.Lock()
	upstreamWrites := len(upstreamConn.writes)
	upstreamConn.mu.Unlock()
	return upstreamWrites, proxyErr, clientReadErr
}
