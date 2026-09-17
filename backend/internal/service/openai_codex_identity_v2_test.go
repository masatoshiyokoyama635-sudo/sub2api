package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/net/http/httpguts"
)

const identityV2Thread = "01996922-0000-7000-8000-000000000001"
const identityV2Turn = "01996922-1111-7000-8000-000000000002"

func identityV2Account(mode string) *Account {
	return &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"chatgpt_account_id": "review-upstream", "chatgpt_user_id": "review-user", "access_token": "test-only-token"},
		Extra:       map[string]any{codexIdentityVersionExtraKey: "v2", codexFingerprintModeExtraKey: mode, codexFingerprintSeedExtraKey: testCodexFingerprintSeed},
	}
}

func identityV2Body(t *testing.T, window int) []byte {
	t.Helper()
	embedded := fmt.Sprintf(`{ "session_id":%q, "thread_id":%q, "turn_id":%q, "root_turn_id":%q, "window_id":%q, "window_number":%d, "opaque":9007199254740993, "escaped":"\u0061<>&" }`, identityV2Thread, identityV2Thread, identityV2Turn, identityV2Turn, fmt.Sprintf("%s:%d", identityV2Thread, window), window)
	body, err := json.Marshal(map[string]any{
		"type": "response.create", "model": "gpt-5.5", "stream": true, "prompt_cache_key": identityV2Thread,
		"input": []map[string]any{{"type": "message", "role": "user", "content": "hello"}},
		"client_metadata": map[string]any{"session_id": identityV2Thread, "thread_id": identityV2Thread, "turn_id": identityV2Turn, "root_turn_id": identityV2Turn, "parent_thread_id": identityV2Thread,
			"x-codex-installation-id": "11111111-1111-4111-8111-222222222222", "x-codex-window-id": fmt.Sprintf("%s:%d", identityV2Thread, window), openAIWSTurnMetadataHeader: embedded},
	})
	require.NoError(t, err)
	return body
}

func identityV2Context(t *testing.T) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
	c.Set("api_key", &APIKey{ID: 42})
	return c
}

func TestCodexIdentityV2LegacyDerivationsRemainFixed(t *testing.T) {
	account := identityV2Account("off")
	for _, version := range []any{nil, "v1", "unknown", true} {
		account.Extra[codexIdentityVersionExtraKey] = version
		require.Equal(t, "10314658-33fd-443f-9179-374dbca3dff6", scopeCodexAccountIdentityValue(account, 42, "session", identityV2Thread))
		require.Equal(t, "48d57942-f55a-47cc-bc6b-c57955120148", scopeCodexAccountIdentityValue(account, 42, "thread", identityV2Thread))
		require.Equal(t, "03124da3-495d-4385-90e2-eb915d80d546", scopeCodexAccountIdentityValue(account, 42, "window", identityV2Thread+":7"))
	}
}

func TestCodexIdentityV2ContinuationStateDoesNotCrossVersion(t *testing.T) {
	c := identityV2Context(t)
	account := identityV2Account("device")
	svc := &OpenAIGatewayService{}
	account.Extra[codexIdentityVersionExtraKey] = "v1"
	legacyKey := openAICompatSessionResponseKey(c, account, "session")
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "session", "legacy-state")
	account.Extra[codexIdentityVersionExtraKey] = "v2"
	require.NotEqual(t, legacyKey, openAICompatSessionResponseKey(c, account, "session"))
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "session"))
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "session", "v2-state")
	require.Equal(t, "v2-state", svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "session"))
	account.Extra[codexFingerprintModeExtraKey] = "session"
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "session"))
	account.Extra[codexIdentityVersionExtraKey] = "v1"
	require.Equal(t, legacyKey, openAICompatSessionResponseKey(c, account, "session"))
	require.Equal(t, "legacy-state", svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "session"))
}

func TestCodexIdentityV2RelationsAndIsolation(t *testing.T) {
	account := identityV2Account("off")
	thread := scopeCodexAccountIdentityValue(account, 42, "thread", identityV2Thread)
	require.Equal(t, thread, scopeCodexAccountIdentityValue(account, 42, "session", identityV2Thread))
	require.Equal(t, thread, scopeCodexAccountIdentityValue(account, 42, "prompt-cache", identityV2Thread))
	require.Equal(t, thread+":7", scopeCodexAccountIdentityValue(account, 42, "window", identityV2Thread+":7"))
	require.Equal(t, "guardian:"+thread, scopeCodexAccountIdentityValue(account, 42, "prompt-cache", "guardian:"+identityV2Thread))
	require.NotEqual(t, thread, scopeCodexAccountIdentityValue(account, 43, "thread", identityV2Thread))
	other := *account
	other.Credentials = map[string]any{"chatgpt_account_id": "another-upstream"}
	require.NotEqual(t, thread, scopeCodexAccountIdentityValue(&other, 42, "thread", identityV2Thread))
	original, err := uuid.Parse(identityV2Thread)
	require.NoError(t, err)
	derived, err := uuid.Parse(thread)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), derived.Version())
	require.Equal(t, original[:6], derived[:6])
}

func TestCodexIdentityV2ModesShareHeaderBodyAndPreserveMetadata(t *testing.T) {
	for _, mode := range []string{"off", "device", "session", "full"} {
		for _, rawPath := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/raw=%v", mode, rawPath), func(t *testing.T) {
				account := identityV2Account(mode)
				c := identityV2Context(t)
				// A stale handshake must not override the current frame's evidence.
				c.Request.Header.Set("session-id", "stale-session")
				c.Request.Header.Set("x-codex-window-id", identityV2Thread+":0")
				body := identityV2Body(t, 7)
				var result []byte
				if rawPath {
					var err error
					result, err = applyCodexIdentityV2Raw(c, account, body)
					require.NoError(t, err)
				} else {
					var decoded map[string]any
					require.NoError(t, json.Unmarshal(body, &decoded))
					require.True(t, applyCodexIdentityV2Map(c, account, decoded))
					var err error
					result, err = json.Marshal(decoded)
					require.NoError(t, err)
				}
				headers := http.Header{"Session_id": []string{"legacy"}}
				applyStagedCodexFingerprintHeaders(c, account, headers)
				applyCodexIdentityV2Headers(c, account, headers)
				cm := gjson.GetBytes(result, "client_metadata")
				for _, pair := range codexV2Carriers {
					require.Equal(t, cm.Get(pair[0]).String(), headers.Get(pair[1]), pair[0])
				}
				require.Empty(t, headers.Get("session_id"))
				require.Equal(t, headers.Get("thread-id"), headers.Get("x-client-request-id"))
				embedded := cm.Get(openAIWSTurnMetadataHeader).String()
				require.Equal(t, embedded, headers.Get(openAIWSTurnMetadataHeader))
				require.Contains(t, embedded, `"opaque":9007199254740993`)
				require.Contains(t, embedded, `"escaped":"\u0061<>&"`)
				require.Equal(t, cm.Get("turn_id").String(), gjson.Get(embedded, "turn_id").String())
				require.Equal(t, gjson.Get(embedded, "turn_id").String(), gjson.Get(embedded, "root_turn_id").String())
				require.Equal(t, cm.Get("thread_id").String()+":7", cm.Get("x-codex-window-id").String())
				require.Equal(t, cm.Get("session_id").String(), gjson.GetBytes(result, "prompt_cache_key").String())
				if mode == "session" || mode == "full" {
					require.Equal(t, cm.Get("thread_id").String(), cm.Get("parent_thread_id").String(), "references to the root thread must resolve to its final identity")
				}
			})
		}
	}
}

func TestCodexIdentityV2HeaderProjectionRejectsControlBytes(t *testing.T) {
	c := identityV2Context(t)
	account := identityV2Account("device")
	var body map[string]any
	require.NoError(t, json.Unmarshal(identityV2Body(t, 1), &body))
	cm, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	raw := "{\n  \"thread_id\":\"" + identityV2Thread + "\",\n  \"unknown\":9007199254740993\n}"
	cm[openAIWSTurnMetadataHeader] = raw
	cm["x-openai-subagent"] = "agent\nInjected: true"
	applyCodexIdentityV2Map(c, account, body)
	headers := http.Header{}
	applyCodexIdentityV2Headers(c, account, headers)
	require.Empty(t, headers.Get("x-openai-subagent"))
	for _, values := range headers {
		for _, value := range values {
			require.True(t, httpguts.ValidHeaderFieldValue(value))
		}
	}
	embedded, ok := cm[openAIWSTurnMetadataHeader].(string)
	require.True(t, ok)
	require.Contains(t, embedded, "\n")
	require.JSONEq(t, embedded, headers.Get(openAIWSTurnMetadataHeader))
}

func TestCodexIdentityV2CurrentTurnBeatsHandshake(t *testing.T) {
	c := identityV2Context(t)
	c.Request.Header.Set(openAIWSTurnMetadataHeader, `{"turn_id":"old","root_turn_id":"old","window_number":0}`)
	body := map[string]any{"client_metadata": map[string]any{"turn_id": identityV2Turn, "root_turn_id": identityV2Turn}}
	applyCodexIdentityV2Map(c, identityV2Account("device"), body)
	cm, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	embedded, ok := cm[openAIWSTurnMetadataHeader].(string)
	require.True(t, ok)
	require.Equal(t, cm["turn_id"], gjson.Get(embedded, "turn_id").Str)
	require.Equal(t, cm["root_turn_id"], gjson.Get(embedded, "root_turn_id").Str)
}

func TestCodexIdentityV2FullConvergenceKeepsReferencesTogether(t *testing.T) {
	body := identityV2Body(t, 8)
	body, err := sjson.SetBytes(body, "prompt_cache_key", "guardian:"+identityV2Thread)
	require.NoError(t, err)
	body, err = sjson.SetBytes(body, "client_metadata.window_id", identityV2Thread+":8")
	require.NoError(t, err)
	next, err := applyCodexIdentityV2Raw(identityV2Context(t), identityV2Account("full"), body)
	require.NoError(t, err)
	cm := gjson.GetBytes(next, "client_metadata")
	require.Equal(t, "guardian:"+cm.Get("thread_id").Str, gjson.GetBytes(next, "prompt_cache_key").Str)
	require.Equal(t, cm.Get("thread_id").Str, cm.Get("parent_thread_id").Str)
	require.Equal(t, cm.Get("thread_id").Str+":8", cm.Get("window_id").Str)
}

func TestCodexIdentityV2CompatibilityBridgesUseOneIdentity(t *testing.T) {
	for _, anthropic := range []bool{false, true} {
		for _, sessionHeader := range []bool{false, true} {
			t.Run(fmt.Sprintf("messages=%v/header=%v", anthropic, sessionHeader), func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.4","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
				c := identityV2Context(t)
				if sessionHeader {
					c.Request.Header.Set("session-id", "bridge-session")
					c.Request.Header.Set("thread-id", "bridge-thread")
				}
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_v2_bridge", "gpt-5.4")}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := identityV2Account("device")
				var result *OpenAIForwardResult
				var err error
				if anthropic {
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "gpt-5.4")
				} else {
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "gpt-5.4")
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.NotNil(t, upstream.lastReq)
				cm := gjson.GetBytes(upstream.lastBody, "client_metadata")
				require.NotEmpty(t, cm.Get("session_id").Str)
				require.Equal(t, cm.Get("session_id").Str, upstream.lastReq.Header.Get("session-id"))
				require.Equal(t, cm.Get("thread_id").Str, upstream.lastReq.Header.Get("thread-id"))
				require.Empty(t, upstream.lastReq.Header.Get("session_id"))
				if anthropic {
					require.False(t, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").Exists())
				}
			})
		}
	}
}

func TestCodexIdentityV2ConvergesDifferentDevicesWithoutMergingThreads(t *testing.T) {
	account := identityV2Account("device")
	bodyA := identityV2Body(t, 1)
	bodyB, err := sjson.SetBytes(bodyA, "client_metadata.x-codex-installation-id", "other-device")
	require.NoError(t, err)
	bodyB, err = sjson.SetBytes(bodyB, "client_metadata.thread_id", "01996922-0000-7000-8000-000000000009")
	require.NoError(t, err)
	a, err := applyCodexIdentityV2Raw(identityV2Context(t), account, bodyA)
	require.NoError(t, err)
	b, err := applyCodexIdentityV2Raw(identityV2Context(t), account, bodyB)
	require.NoError(t, err)
	require.Equal(t, gjson.GetBytes(a, "client_metadata.x-codex-installation-id").Str, gjson.GetBytes(b, "client_metadata.x-codex-installation-id").Str)
	require.NotEqual(t, gjson.GetBytes(bodyA, "client_metadata.x-codex-installation-id").Str, gjson.GetBytes(a, "client_metadata.x-codex-installation-id").Str)
	require.NotEqual(t, gjson.GetBytes(a, "client_metadata.thread_id").Str, gjson.GetBytes(b, "client_metadata.thread_id").Str)
}

func TestCodexIdentityV2ClearsStaleAndPreservesUnrelatedRawBytes(t *testing.T) {
	account := identityV2Account("off")
	c := identityV2Context(t)
	body := []byte(`{ "input" : [9007199254740993, "\u0061"], "client_metadata":{"session_id":"s","thread_id":"s","unknown":9007199254740993}, "prompt_cache_key":"explicit-cache" }`)
	result, err := applyCodexIdentityV2Raw(c, account, body)
	require.NoError(t, err)
	require.Contains(t, string(result), `"input" : [9007199254740993, "\u0061"]`)
	require.Equal(t, "9007199254740993", gjson.GetBytes(result, "client_metadata.unknown").Raw)
	require.NotEqual(t, gjson.GetBytes(result, "client_metadata.session_id").Str, gjson.GetBytes(result, "prompt_cache_key").Str)
	_, err = applyCodexIdentityV2Raw(c, account, []byte(`{"input":[]}`))
	require.NoError(t, err)
	headers := http.Header{}
	applyCodexIdentityV2Headers(c, account, headers)
	require.Empty(t, headers)
	_, err = applyCodexIdentityV2Raw(c, account, identityV2Body(t, 2))
	require.NoError(t, err)
	other := identityV2Account("off")
	other.ID++
	applyCodexIdentityV2Headers(c, other, headers)
	require.Empty(t, headers, "a different selected row cannot read a prior attempt")
}

func TestCodexIdentityV2HTTPForwardPaths(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%v", passthrough), func(t *testing.T) {
			account := identityV2Account("device")
			account.Extra["openai_oauth_passthrough"] = passthrough
			c := identityV2Context(t)
			body := identityV2Body(t, 3)
			body, err := sjson.DeleteBytes(body, "type")
			require.NoError(t, err)
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
			_, err = svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, upstream.lastReq)
			cm := gjson.GetBytes(upstream.lastBody, "client_metadata")
			require.Equal(t, cm.Get("thread_id").Str, upstream.lastReq.Header.Get("thread-id"))
			require.Equal(t, cm.Get("session_id").Str, upstream.lastReq.Header.Get("session-id"))
			require.Equal(t, cm.Get("thread_id").Str+":3", upstream.lastReq.Header.Get("x-codex-window-id"))
		})
	}
}

type identityV2WSDialer struct {
	conn    openAIWSClientConn
	headers chan http.Header
}

func (d *identityV2WSDialer) Dial(_ context.Context, _ string, h http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.headers <- h.Clone()
	return d.conn, http.StatusSwitchingProtocols, http.Header{}, nil
}

type identityV2WSConn struct{ *stagedPassthroughConn }

func (c *identityV2WSConn) WriteJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, payload)
}

func TestCodexIdentityV2NativeWSTwoTurns(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModePassthrough, OpenAIWSIngressModeCtxPool} {
		t.Run(mode, func(t *testing.T) {
			account := identityV2Account("device")
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			upstream := newStagedPassthroughConn()
			dialer := &identityV2WSDialer{conn: &identityV2WSConn{upstream}, headers: make(chan http.Header, 8)}
			svc := newPassthroughLifecycleService(cfg, upstream)
			svc.openaiWSPassthroughDialer = dialer
			if mode == OpenAIWSIngressModeCtxPool {
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(dialer)
				svc.openaiWSPool = pool
				defer pool.Close()
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(c *gin.Context) *OpenAIWSIngressHooks {
				c.Set("api_key", &APIKey{ID: 42})
				return nil
			})
			defer server.Close()
			defer cancel(context.Canceled)
			dialCtx, stopDial := context.WithTimeout(ctx, 3*time.Second)
			client, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), &coderws.DialOptions{HTTPHeader: http.Header{"User-Agent": []string{"codex_cli_rs/0.144.1"}, "X-Codex-Turn-Metadata": []string{`{"turn_id":"stale","window_number":0}`}}})
			stopDial()
			require.NoError(t, err)
			defer func() { _ = client.CloseNow() }()
			var header http.Header
			for turn := 1; turn <= 2; turn++ {
				writeCtx, stopWrite := context.WithTimeout(ctx, 3*time.Second)
				err = client.Write(writeCtx, coderws.MessageText, identityV2Body(t, turn))
				stopWrite()
				require.NoError(t, err)
				forwarded := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
				if turn == 1 {
					select {
					case header = <-dialer.headers:
					case <-time.After(3 * time.Second):
						t.Fatal("missing handshake")
					}
				}
				cm := gjson.GetBytes(forwarded, "client_metadata")
				require.Equal(t, header.Get("thread-id"), cm.Get("thread_id").Str)
				require.Equal(t, header.Get("x-codex-installation-id"), cm.Get("x-codex-installation-id").Str)
				require.Equal(t, cm.Get("thread_id").Str+fmt.Sprintf(":%d", turn), cm.Get("x-codex-window-id").Str)
				embedded := cm.Get(openAIWSTurnMetadataHeader).Str
				require.Equal(t, int64(turn), gjson.Get(embedded, "window_number").Int())
				require.NotEqual(t, "stale", gjson.Get(embedded, "turn_id").Str)
				upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_v2_%d","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`, turn))
				_, err = readPassthroughLifecycleFrame(t, client, 3*time.Second)
				require.NoError(t, err)
			}
			cancel(context.Canceled)
			_ = client.CloseNow()
			select {
			case <-serverErr:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not stop")
			}
		})
	}
}
