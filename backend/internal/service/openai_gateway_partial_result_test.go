package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func openAIPartialResultSSE(code string) string {
	return fmt.Sprintf("data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_partial\",\"model\":\"gpt-5.5\",\"status\":\"failed\",\"output\":[],\"usage\":{\"input_tokens\":17,\"output_tokens\":3,\"input_tokens_details\":{\"cached_tokens\":2}},\"error\":{\"code\":%q,\"message\":\"upstream failed\"}}}\n\n", code)
}

func invokeOpenAIResponsesPartialRoute(t *testing.T, route, errorCode string) (*OpenAIForwardResult, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.5","stream":true,"instructions":"test","input":"hello"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"x-request-id": []string{"req_partial"},
		},
		Body: io.NopCloser(bytes.NewBufferString(openAIPartialResultSSE(errorCode))),
	}}
	svc := &OpenAIGatewayService{
		cfg:           &config.Config{},
		httpUpstream:  upstream,
		toolCorrector: NewCodexToolCorrector(),
	}
	startTime := time.Now()

	switch route {
	case "native":
		account := compatCyberOAuthAccount()
		return svc.Forward(context.Background(), c, account, body)
	case "passthrough":
		account := compatCyberOAuthAccount()
		return svc.forwardOpenAIPassthrough(context.Background(), c, account, body, body, "gpt-5.5", false, nil, true, startTime)
	case "grok":
		account := &Account{
			ID: 3, Name: "grok-api-key", Platform: PlatformGrok, Type: AccountTypeAPIKey, Concurrency: 1,
			Credentials: map[string]any{"api_key": "test-key"},
		}
		return svc.forwardGrokResponses(context.Background(), c, account, body, "gpt-5.5", true, startTime)
	default:
		t.Fatalf("unknown route %q", route)
		return nil, nil
	}
}

func TestOpenAIResponsesOuterRoutesPreserveNonFailoverPartialUsage(t *testing.T) {
	for _, route := range []string{"native", "passthrough", "grok"} {
		t.Run(route, func(t *testing.T) {
			result, err := invokeOpenAIResponsesPartialRoute(t, route, "content_policy")
			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr))
			require.NotNil(t, result)
			require.Equal(t, "resp_partial", result.ResponseID)
			require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 3, CacheReadInputTokens: 2}, result.Usage)
		})
	}
}

func TestOpenAIResponsesOuterRoutesSuppressFailoverPartialUsage(t *testing.T) {
	for _, route := range []string{"native", "passthrough", "grok"} {
		t.Run(route, func(t *testing.T) {
			result, err := invokeOpenAIResponsesPartialRoute(t, route, "server_error")
			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Nil(t, result)
		})
	}
}
