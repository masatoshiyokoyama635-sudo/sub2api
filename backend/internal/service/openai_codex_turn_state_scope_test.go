//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTeamHTTPProxyConfigurationChangeIsolatesCandidate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	state := testCodexTurnStateEnvelope(now, 12, 1)
	account := newTurnStateV2Account(1, "workspace-a")
	account.Credentials["chatgpt_user_id"] = "member-a"
	account.Extra["codex_turn_state_mode"] = "reuse"
	proxyID := int64(42)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{
		ID: proxyID, Protocol: "http", Host: "proxy-a.invalid", Port: 8080,
		Username: "fixture-user", Password: "fixture-password",
	}
	svc := &OpenAIGatewayService{}
	scope := codexTurnStateCandidateScope(account, account)
	svc.openaiCodexTurnStateCandidates.Observe(scope, "model-a", state, []int{332}, now)
	prepare := func(selected *Account) *http.Request {
		c, _ := newTurnStateTestContext(t, 7, "session")
		return svc.prepareCodexTurnStateHTTP(c, selected, []byte(`{"model":"model-a"}`),
			httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil))
	}
	require.Equal(t, state, prepare(account).Header.Get(openAICodexTurnStateHeader))
	for _, tc := range []struct {
		name   string
		mutate func(*Proxy)
	}{
		{"host", func(proxy *Proxy) { proxy.Host = "proxy-b.invalid" }},
		{"port", func(proxy *Proxy) { proxy.Port = 8081 }},
		{"protocol", func(proxy *Proxy) { proxy.Protocol = "socks5" }},
		{"credentials", func(proxy *Proxy) { proxy.Password = "rotated-fixture-password" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := *account
			changedProxy := *account.Proxy
			changed.Proxy = &changedProxy
			tc.mutate(changed.Proxy)
			require.Equal(t, *account.ProxyID, *changed.ProxyID, "the same proxy record was edited")
			require.NotEqual(t, account.Proxy.URL(), changed.Proxy.URL())
			require.NotEqual(t, scope, codexTurnStateCandidateScope(&changed, &changed))
			require.Empty(t, prepare(&changed).Header.Get(openAICodexTurnStateHeader))
		})
	}
	// Reading another identity configuration must not erase the first bucket.
	require.Equal(t, state, prepare(account).Header.Get(openAICodexTurnStateHeader))
}
