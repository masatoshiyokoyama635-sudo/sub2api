//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const codexTurnStateSubmissionJSON = `{"id":"resp_turnstate","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`

const codexTurnStateSubmissionSSE = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
	"data: {\"type\":\"response.completed\",\"response\":" + codexTurnStateSubmissionJSON + "}\n\n" +
	"data: [DONE]\n\n"

const codexTurnStateSubmissionFailedSSE = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_abandoned\"}}\n\n" +
	"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_abandoned\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"upstream processing failed\"}}}\n\n"

func newCodexTurnStateSubmissionRequest(t *testing.T, stream bool, response *http.Response) (*OpenAIGatewayService, *Account, *gin.Context, *httptest.ResponseRecorder, *httpUpstreamRecorder, []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	streamValue := "false"
	path := "/v1/responses/compact"
	if stream {
		streamValue = "true"
		path = "/v1/responses"
	}
	body := []byte(`{"model":"gpt-5.1","stream":` + streamValue + `,"instructions":"local-test-instructions","input":[{"role":"user","content":"hello"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Session-Id", "submission-session")
	c.Set("api_key", &APIKey{ID: 7})
	account := newTurnStateV2Account(41, "submission-credential")
	account.Credentials["access_token"] = "fixture-token"
	account.Extra["openai_passthrough"] = true
	account.Concurrency = 1
	account.Status = StatusActive
	account.Schedulable = true
	upstream := &httpUpstreamRecorder{resp: response}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			MaxLineSize: defaultMaxLineSize,
		}},
		httpUpstream: upstream,
	}
	return svc, account, c, recorder, upstream, body
}

func codexTurnStateSubmissionResponse(contentType, state string, body io.ReadCloser) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", contentType)
	header.Set(openAICodexTurnStateHeader, state)
	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: body}
}

func TestCodexIdentityV2TurnStateSubmissionUnwrittenFailuresHaveNoProvenance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stream      bool
		contentType string
		body        func() io.ReadCloser
	}{
		{"stream_failover", true, "text/event-stream", func() io.ReadCloser { return io.NopCloser(strings.NewReader(codexTurnStateSubmissionFailedSSE)) }},
		{"json_read_failure", false, "application/json", func() io.ReadCloser { return passthroughErrReadCloser{err: io.ErrUnexpectedEOF} }},
		{"sse_to_json_failover", false, "text/event-stream", func() io.ReadCloser { return io.NopCloser(strings.NewReader(codexTurnStateSubmissionFailedSSE)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const state = "not-delivered-blob"
			response := codexTurnStateSubmissionResponse(tc.contentType, state, tc.body())
			svc, account, c, recorder, upstream, body := newCodexTurnStateSubmissionRequest(t, tc.stream, response)
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.Len(t, upstream.requests, 1, "the real forwarding entry must reach the fake upstream")
			require.False(t, c.Writer.Written(), "no response may be committed before failover")
			require.Empty(t, recorder.Body.String())
			other := newTurnStateV2Account(42, "other-credential")
			require.Equal(t, state, svc.guardOpenAICodexTurnStateValue(nil, other, state), "an abandoned upstream response must not create provenance")
		})
	}
}

func TestCodexIdentityV2TurnStateSubmissionCommittedResponsesHaveProvenance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stream      bool
		contentType string
		body        string
	}{
		{"stream", true, "text/event-stream", codexTurnStateSubmissionSSE},
		{"json", false, "application/json", codexTurnStateSubmissionJSON},
		{"sse_to_json", false, "text/event-stream", codexTurnStateSubmissionSSE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const state = "actually-delivered-blob"
			response := codexTurnStateSubmissionResponse(tc.contentType, state, io.NopCloser(strings.NewReader(tc.body)))
			svc, account, c, recorder, upstream, body := newCodexTurnStateSubmissionRequest(t, tc.stream, response)
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.stream, result.Stream)
			require.Len(t, upstream.requests, 1)
			require.True(t, c.Writer.Written())
			require.Equal(t, state, recorder.Result().Header.Get(openAICodexTurnStateHeader), "inspect committed headers rather than the mutable Header map")
			require.Contains(t, recorder.Body.String(), "ok")
			if !tc.stream {
				require.NotContains(t, recorder.Body.String(), "data:", "compact must exercise the JSON response handler")
			}
			require.Equal(t, state, svc.guardOpenAICodexTurnStateValue(nil, account, state))
			require.Empty(t, svc.guardOpenAICodexTurnStateValue(nil, newTurnStateV2Account(42, "other-credential"), state))
		})
	}
}

func TestCodexIdentityV2TurnStateSubmissionAlreadyCommittedHeadersRejectPhantomBlob(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stream      bool
		contentType string
		body        string
	}{
		{"stream", true, "text/event-stream", codexTurnStateSubmissionSSE},
		{"json", false, "application/json", codexTurnStateSubmissionJSON},
		{"sse_to_json", false, "text/event-stream", codexTurnStateSubmissionSSE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := codexTurnStateSubmissionResponse(tc.contentType, "blob-b", io.NopCloser(strings.NewReader(tc.body)))
			svc, account, c, recorder, upstream, body := newCodexTurnStateSubmissionRequest(t, tc.stream, response)
			// A previous attempt/keepalive has already sent A's headers. The
			// next handler may alter Header(), but it cannot send new headers.
			c.Header(openAICodexTurnStateHeader, "blob-a")
			c.Writer.WriteHeaderNow()
			require.Equal(t, "blob-a", recorder.Result().Header.Get(openAICodexTurnStateHeader))
			_, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "blob-b", c.Writer.Header().Get(openAICodexTurnStateHeader))
			require.Equal(t, "blob-a", recorder.Result().Header.Get(openAICodexTurnStateHeader), "actual HTTP headers remain from the first attempt")
			require.Equal(t, "blob-b", svc.guardOpenAICodexTurnStateValue(nil, newTurnStateV2Account(42, "other-credential"), "blob-b"), "B was never delivered and must not be attributed")
		})
	}
}
