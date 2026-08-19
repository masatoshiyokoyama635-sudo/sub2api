package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// compatCyberOAuthAccount 是 compat cyber 测试共用的 OAuth 账号。
func compatCyberOAuthAccount() *Account {
	return &Account{
		ID:          1,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
}

// compatCyberUpstreamSSE 构造上游 responses SSE：response.created 后 response.failed(cyber_policy)。
func compatCyberUpstreamSSE() string {
	return strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_cyber","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		"",
		`event: response.failed`,
		`data: {"type":"response.failed","response":{"id":"resp_cyber","object":"response","model":"gpt-5.5","status":"failed","output":[],"usage":{"input_tokens":17,"output_tokens":3,"total_tokens":20,"input_tokens_details":{"cached_tokens":2}},"error":{"code":"cyber_policy","message":"flagged for cyber policy"}}}`,
		"",
	}, "\n")
}

func compatCyberUpstreamRecorder() *httpUpstreamRecorder {
	return &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_cyber"}},
		Body:       io.NopCloser(strings.NewReader(compatCyberUpstreamSSE())),
	}}
}

// C-1: chat completions 非流式客户端（buffered 路径）cyber 命中——不 failover、标记已设、
// 以 chat 错误格式回写，并把已解析 usage 交给 handler 正常记账。
func TestForwardAsChatCompletions_BufferedCyberPolicyNoFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	svc := &OpenAIGatewayService{httpUpstream: compatCyberUpstreamRecorder()}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 3, CacheReadInputTokens: 2}, result.Usage)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "cyber must NOT trigger failover")
	mark := GetOpsCyberPolicy(c)
	require.NotNil(t, mark, "cyber mark must be set for handler-side recording")
	require.Equal(t, "cyber_policy", mark.Code)
	require.True(t, c.Writer.Written(), "cyber error must be written to client (passthrough)")
}

// I-1: chat completions 流式客户端 cyber 命中——保留 terminal usage，
// 由 handler 走 RecordUsage(CyberBlocked=true)。
func TestForwardAsChatCompletions_StreamCyberPolicyPreservesResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	svc := &OpenAIGatewayService{httpUpstream: compatCyberUpstreamRecorder()}

	result, err := svc.ForwardAsChatCompletions(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 3, CacheReadInputTokens: 2}, result.Usage)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "cyber must NOT trigger failover")
	require.NotNil(t, GetOpsCyberPolicy(c), "cyber mark must be set")
	require.Contains(t, rec.Body.String(), "data: [DONE]", "stream must terminate with [DONE]")
}

// anthropic 非流式客户端（buffered 路径）cyber 命中——不 failover、标记已设、
// 以 anthropic 错误格式回写，并返回已解析 usage。
func TestForwardAsAnthropic_BufferedCyberPolicyNoFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.5","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":false}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	svc := &OpenAIGatewayService{httpUpstream: compatCyberUpstreamRecorder()}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 3, CacheReadInputTokens: 2}, result.Usage)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "cyber must NOT trigger failover")
	mark := GetOpsCyberPolicy(c)
	require.NotNil(t, mark, "cyber mark must be set")
	require.Equal(t, "cyber_policy", mark.Code)
	require.True(t, c.Writer.Written(), "anthropic cyber error must be written to client")
	require.Contains(t, rec.Body.String(), `"type":"error"`, "must use anthropic error envelope")
}

// anthropic 流式客户端 cyber 命中——不 failover、标记已设、下发 anthropic SSE error 事件、保留 usage。
func TestForwardAsAnthropic_StreamCyberPolicyNoFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.5","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	svc := &OpenAIGatewayService{httpUpstream: compatCyberUpstreamRecorder()}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 3, CacheReadInputTokens: 2}, result.Usage)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "cyber must NOT trigger failover")
	require.NotNil(t, GetOpsCyberPolicy(c), "cyber mark must be set")
	require.Contains(t, rec.Body.String(), "event: error", "must emit anthropic SSE error event")
}

func TestCompatCyberPolicyWithoutObservedBillingKeepsNilResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.5","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := compatCyberUpstreamRecorder()
	upstream.resp.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(compatCyberUpstreamSSE(), `,"usage":{"input_tokens":17,"output_tokens":3,"total_tokens":20,"input_tokens_details":{"cached_tokens":2}}`, "")))
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
	require.Error(t, err)
	require.Nil(t, result)
}

func compatBufferedFailedUpstreamRecorder(code string) *httpUpstreamRecorder {
	sse := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_failed","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		"",
		`data: {"type":"response.failed","response":{"id":"resp_failed","object":"response","model":"gpt-5.5","status":"failed","output":[],"usage":{"input_tokens":23,"output_tokens":4,"total_tokens":27},"error":{"code":"` + code + `","message":"upstream failed"}}}`,
		"",
	}, "\n")
	return &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_failed"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
}

func TestCompatBufferedFailedResponsePartialUsage(t *testing.T) {
	for _, endpoint := range []string{"chat", "messages"} {
		t.Run(endpoint+"_non_failover", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			svc := &OpenAIGatewayService{httpUpstream: compatBufferedFailedUpstreamRecorder("content_policy")}

			var result *OpenAIForwardResult
			var err error
			if endpoint == "chat" {
				body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
			} else {
				body := []byte(`{"model":"gpt-5.5","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":false}`)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
				result, err = svc.ForwardAsAnthropic(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
			}

			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr))
			require.NotNil(t, result)
			require.Equal(t, OpenAIUsage{InputTokens: 23, OutputTokens: 4}, result.Usage)
		})

		t.Run(endpoint+"_failover", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			svc := &OpenAIGatewayService{httpUpstream: compatBufferedFailedUpstreamRecorder("server_error")}

			var result *OpenAIForwardResult
			var err error
			if endpoint == "chat" {
				body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hi"}],"stream":false}`)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
			} else {
				body := []byte(`{"model":"gpt-5.5","max_tokens":1024,"messages":[{"role":"user","content":"hi"}],"stream":false}`)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
				result, err = svc.ForwardAsAnthropic(context.Background(), c, compatCyberOAuthAccount(), body, "", "gpt-5.5")
			}

			require.Error(t, err)
			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.Nil(t, result)
		})
	}
}
