//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenCodeResponsesRejectsDisabledImageGenerationBeforeProtocolDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses} {
		for _, signal := range []struct {
			name, mappedModel, body string
		}{
			{"account_mapped_image_model", "gpt-image-2", `{"model":"text-alias","input":"draw"}`},
			{"native_image_tool", "glm-5.3", `{"model":"text-alias","input":"draw","tools":[{"type":"image_generation"}]}`},
		} {
			t.Run(protocol+"/"+signal.name, func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: openCodeImageGateTestResponse(protocol)}
				svc := newOpenAIImageGenerationControlTestService(upstream)
				c, recorder := newOpenAIImageGenerationControlTestContext(false, "unit-test-agent/1.0")
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				account := openCodeImageGateTestAccount(protocol, signal.mappedModel)

				result, err := svc.Forward(context.Background(), c, account, []byte(signal.body))

				require.Nil(t, upstream.lastReq, "a disabled group's image request must be rejected before upstream dispatch")
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusForbidden, recorder.Code)
				require.Equal(t, "permission_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
				require.Equal(t, ImageGenerationPermissionMessage(), gjson.GetBytes(recorder.Body.Bytes(), "error.message").String())
			})
		}
	}
}

func TestOpenCodeResponsesRejectsImageIntentOnTextProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic} {
		for _, signal := range []struct {
			name, mappedModel, body string
		}{
			{"account_mapped_image_model", "gpt-image-2", `{"model":"text-alias","input":"draw"}`},
			{"native_image_tool", "glm-5.3", `{"model":"text-alias","input":"draw","tools":[{"type":"image_generation"}]}`},
		} {
			t.Run(protocol+"/"+signal.name, func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: openCodeImageGateTestResponse(protocol)}
				svc := newOpenAIImageGenerationControlTestService(upstream)
				c, _ := newOpenAIImageGenerationControlTestContext(true, "unit-test-agent/1.0")
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				account := openCodeImageGateTestAccount(protocol, signal.mappedModel)

				result, err := svc.Forward(context.Background(), c, account, []byte(signal.body))

				require.Nil(t, upstream.lastReq, "image generation must not be lowered to a text-only protocol")
				require.Nil(t, result)
				require.ErrorContains(t, err, "image generation requires a Responses-capable account")
			})
		}
	}
}

func TestOpenCodeResponsesImageGatePreservesTextRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []struct {
		protocol, path string
	}{
		{APIProtocolChatCompletions, "/zen/v1/chat/completions"},
		{APIProtocolAnthropic, "/zen/v1/messages"},
		{APIProtocolResponses, "/zen/v1/responses"},
	} {
		for _, request := range []struct{ name, body string }{
			{"plain_text", `{"model":"text-alias","stream":false,"input":"write code"}`},
			{"passive_image_namespace", `{"model":"text-alias","stream":false,"input":"write code","tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}],"tool_choice":"auto"}`},
		} {
			t.Run(route.protocol+"/"+request.name, func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: openCodeImageGateTestResponse(route.protocol)}
				svc := newOpenAIImageGenerationControlTestService(upstream)
				c, recorder := newOpenAIImageGenerationControlTestContext(false, "unit-test-agent/1.0")
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				account := openCodeImageGateTestAccount(route.protocol, "glm-5.3")

				result, err := svc.Forward(context.Background(), c, account, []byte(request.body))

				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, route.path, upstream.lastReq.URL.Path)
				require.Equal(t, "glm-5.3", gjson.GetBytes(upstream.lastBody, "model").String())
				require.Positive(t, result.Usage.InputTokens)
			})
		}
	}
}

func openCodeImageGateTestAccount(protocol, mappedModel string) *Account {
	account := openCodeMappedTestAccount()
	account.Credentials["api_protocol"] = protocol
	account.Credentials["model_mapping"] = map[string]any{"text-alias": mappedModel}
	return account
}

func openCodeImageGateTestResponse(protocol string) *http.Response {
	if protocol == APIProtocolAnthropic {
		return nativeAnthropicStreamResponse()
	}
	body := `{"id":"resp_oc_image_gate","model":"glm-5.3","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":2,"output_tokens":1}}`
	if protocol == APIProtocolChatCompletions {
		body = `{"id":"chatcmpl_oc_image_gate","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
