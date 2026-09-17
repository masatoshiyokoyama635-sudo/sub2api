package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func outboundWSConfigurationForTest() openAIWSAcquireRequest {
	account := activeCodexFingerprintPoolAccountForTest(9120)
	account.Credentials = map[string]any{
		"access_token": "token-a", "chatgpt_account_id": "account-a",
		"chatgpt_user_id": "user-a", "agent_runtime_id": "runtime-a",
	}
	account.Extra[codexFingerprintModeExtraKey] = "device"
	account.Extra["codex_identity_version"] = "v2"
	account.Extra["openai_device_id"] = "device-a"
	return openAIWSAcquireRequest{
		Account: account, WSURL: "wss://upstream.invalid/responses",
		ProxyURL: "http://127.0.0.1:8001",
		Headers: http.Header{
			"Authorization":           {"Bearer token-a"},
			"User-Agent":              {"client-a"},
			"Originator":              {"origin-a"},
			"Version":                 {"1.0"},
			"Openai-Beta":             {openAIWSBetaV2Value},
			"Accept-Language":         {"en-US"},
			"Chatgpt-Account-Id":      {"account-a"},
			"X-Openai-Fedramp":        {"false"},
			"X-Codex-Installation-Id": {"installation-a"},
		},
	}
}

func TestOpenAIWSConnPoolConfigurationCompatibility(t *testing.T) {
	for _, change := range []struct {
		name string
		edit func(*openAIWSAcquireRequest)
	}{
		{"proxy", func(r *openAIWSAcquireRequest) { r.ProxyURL = "http://127.0.0.1:8002" }},
		{"endpoint", func(r *openAIWSAcquireRequest) { r.WSURL = "wss://other.invalid/responses" }},
		{"user_agent", func(r *openAIWSAcquireRequest) { r.Headers.Set("User-Agent", "client-b") }},
		{"originator", func(r *openAIWSAcquireRequest) { r.Headers.Set("Originator", "origin-b") }},
		{"client_version", func(r *openAIWSAcquireRequest) { r.Headers.Set("Version", "2.0") }},
		{"ws_protocol", func(r *openAIWSAcquireRequest) { r.Headers.Set("OpenAI-Beta", openAIWSBetaV1Value) }},
		{"language", func(r *openAIWSAcquireRequest) { r.Headers.Set("Accept-Language", "zh-CN") }},
		{"account_header", func(r *openAIWSAcquireRequest) { r.Headers.Set("Chatgpt-Account-Id", "account-b") }},
		{"fedramp", func(r *openAIWSAcquireRequest) { r.Headers.Set("X-OpenAI-Fedramp", "true") }},
		{"credential_namespace", func(r *openAIWSAcquireRequest) { r.Account.Credentials["chatgpt_user_id"] = "user-b" }},
		{"runtime", func(r *openAIWSAcquireRequest) { r.Account.Credentials["agent_runtime_id"] = "runtime-b" }},
		{"device", func(r *openAIWSAcquireRequest) { r.Account.Extra["openai_device_id"] = "device-b" }},
		{"identity_version", func(r *openAIWSAcquireRequest) { r.Account.Extra["codex_identity_version"] = "v1" }},
		{"mode", func(r *openAIWSAcquireRequest) { r.Account.Extra[codexFingerprintModeExtraKey] = "off" }},
		{"seed", func(r *openAIWSAcquireRequest) {
			r.Account.Extra[codexFingerprintSeedExtraKey] = "22222222-2222-4222-8222-222222222222"
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
			pool := newOpenAIWSConnPool(cfg)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(&openAIWSCountingDialer{})
			request := outboundWSConfigurationForTest()
			first, err := pool.Acquire(context.Background(), request)
			require.NoError(t, err)
			t.Cleanup(first.Release)
			next := cloneOpenAIWSAcquireRequest(request)
			change.edit(&next)
			second, err := pool.Acquire(context.Background(), next)
			require.NoError(t, err)
			t.Cleanup(second.Release)
			require.NotEqual(t, first.ConnID(), second.ConnID())
			select {
			case <-first.conn.closedCh:
				t.Fatal("configuration change closed an in-flight connection")
			default:
			}
			second.Release()
			first.Release()
			next.PreferredConnID, next.ForcePreferredConn = first.ConnID(), true
			_, err = pool.Acquire(context.Background(), next)
			require.ErrorIs(t, err, errOpenAIWSPreferredConnUnavailable, "an incompatible preferred connection must be rejected")
			next.PreferredConnID, next.ForcePreferredConn = "", false
			third, err := pool.Acquire(context.Background(), next)
			require.NoError(t, err)
			defer third.Release()
			require.Equal(t, second.ConnID(), third.ConnID())
		})
	}
}

func TestOpenAIWSConnPoolConfigurationIgnoresTokenAndTurnRefresh(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	dialer := &openAIWSCountingDialer{}
	pool.setClientDialerForTest(dialer)
	request := outboundWSConfigurationForTest()
	first, err := pool.Acquire(context.Background(), request)
	require.NoError(t, err)
	first.Release()
	next := cloneOpenAIWSAcquireRequest(request)
	next.Account.Credentials["access_token"] = "token-refreshed"
	next.Account.Credentials["task_id"] = "task-refreshed"
	next.Headers.Set("Authorization", "AgentAssertion refreshed-per-dial")
	next.Headers.Set("X-Codex-Turn-Metadata", `{"turn_id":"turn-b"}`)
	next.Headers.Set("X-Codex-Turn-State", "state-b")
	next.Headers.Set(openAICodexRoutingHintHeader, "model=gpt-5.6-codex;tier=priority")
	next.WSURL = " " + next.WSURL + " "
	next.ProxyURL = " " + next.ProxyURL + " "
	second, err := pool.Acquire(context.Background(), next)
	require.NoError(t, err)
	defer second.Release()
	require.Equal(t, first.ConnID(), second.ConnID())
	require.True(t, second.Reused())
	require.Equal(t, 1, dialer.DialCount())
}

func TestOpenAIWSConnPoolIdentityVersionRollbackReusesOnlyMatchingConnection(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	dialer := &openAIWSCountingDialer{}
	pool.setClientDialerForTest(dialer)
	request := outboundWSConfigurationForTest()
	request.Account.Extra["codex_identity_version"] = "v1"
	legacy, err := pool.Acquire(context.Background(), request)
	require.NoError(t, err)
	legacy.Release()
	request.Account.Extra["codex_identity_version"] = "v2"
	modern, err := pool.Acquire(context.Background(), request)
	require.NoError(t, err)
	modern.Release()
	require.NotEqual(t, legacy.ConnID(), modern.ConnID())
	delete(request.Account.Extra, "codex_identity_version")
	restored, err := pool.Acquire(context.Background(), request)
	require.NoError(t, err)
	defer restored.Release()
	require.Equal(t, legacy.ConnID(), restored.ConnID(), "removing the version opt-in restores the v1 pool identity")
	require.Equal(t, 2, dialer.DialCount())
}

func TestOpenAIWSConnPoolShadowKeepsOwnFingerprintIsolation(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, change := range []struct {
			name string
			edit func(*openAIWSAcquireRequest)
		}{
			{"thread", func(r *openAIWSAcquireRequest) { r.Headers.Set("Thread-Id", "thread-b") }},
			{"session", func(r *openAIWSAcquireRequest) { r.Headers.Set("Session-Id", "session-b") }},
			{"window", func(r *openAIWSAcquireRequest) { r.Headers.Set("X-Codex-Window-Id", "window-b") }},
			{"seed", func(r *openAIWSAcquireRequest) {
				r.Account.Extra[codexFingerprintSeedExtraKey] = "33333333-3333-4333-8333-333333333333"
			}},
			{"device", func(r *openAIWSAcquireRequest) { r.Account.Extra["openai_device_id"] = "shadow-device-b" }},
			{"mode", func(r *openAIWSAcquireRequest) { r.Account.Extra[codexFingerprintModeExtraKey] = "device" }},
		} {
			t.Run(version+"/"+change.name, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				pool := newOpenAIWSConnPool(cfg)
				t.Cleanup(pool.Close)
				dialer := &openAIWSCountingDialer{}
				pool.setClientDialerForTest(dialer)
				request := outboundWSConfigurationForTest()
				request.IdentitySource = request.Account
				request.IdentitySource.Extra["codex_identity_version"] = version
				request.IdentitySource.Extra[codexFingerprintModeExtraKey] = "off"
				request.Account = activeCodexFingerprintPoolAccountForTest(9121)
				request.Account.ParentAccountID = &request.IdentitySource.ID
				request.Account.Extra["openai_device_id"] = "shadow-device-a"
				request.Headers = stableOpenAIWSIdentityHeadersForTest()
				first, err := pool.Acquire(context.Background(), request)
				require.NoError(t, err)
				first.Release()
				next := cloneOpenAIWSAcquireRequest(request)
				change.edit(&next)
				second, err := pool.Acquire(context.Background(), next)
				require.NoError(t, err)
				defer second.Release()
				require.NotEqual(t, first.ConnID(), second.ConnID(), "parent mode=off must not bypass the shadow's fingerprint compatibility")
				require.Equal(t, version, second.conn.handshakeCompatibility.identityVersion)
				require.Equal(t, 2, dialer.DialCount())
			})
		}
	}
}

func TestCloneOpenAIWSAcquireRequestSnapshotsOutboundConfiguration(t *testing.T) {
	require.Nil(t, snapshotOpenAIOutboundAccount(nil))
	request := outboundWSConfigurationForTest()
	proxyID, parentID := int64(7), int64(8)
	request.Account.ProxyID = &proxyID
	request.Account.ParentAccountID = &parentID
	request.Account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 8001}
	request.IdentitySource = snapshotOpenAIOutboundAccount(request.Account)
	snapshot := cloneOpenAIWSAcquireRequest(request)
	originalKey := normalizeOpenAIWSHandshakeCompatibility(snapshot)
	request.Account.Credentials["access_token"] = "token-new"
	request.Account.Extra["codex_identity_version"] = "v1"
	request.Account.Proxy.Host = "proxy-new.invalid"
	proxyID, parentID = 9, 10
	request.IdentitySource.Credentials["chatgpt_user_id"] = "user-new"
	request.IdentitySource.Extra["codex_identity_version"] = "v1"
	request.Headers.Set("User-Agent", "client-new")
	require.Equal(t, "token-a", snapshot.Account.GetCredential("access_token"))
	require.Equal(t, "v2", snapshot.Account.Extra["codex_identity_version"])
	require.Equal(t, "127.0.0.1", snapshot.Account.Proxy.Host)
	require.Equal(t, int64(7), *snapshot.Account.ProxyID)
	require.Equal(t, int64(8), *snapshot.Account.ParentAccountID)
	require.Equal(t, originalKey, normalizeOpenAIWSHandshakeCompatibility(snapshot))
	require.NotEqual(t, originalKey, normalizeOpenAIWSHandshakeCompatibility(request))
}

func TestOpenAIWSConnPoolPrewarmDiscardsPreviousShadowIdentitySnapshot(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 2
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	dialer := newOpenAIWSFirstDialBlockingCaptureDialer()
	pool.setClientDialerForTest(dialer)
	request := outboundWSConfigurationForTest()
	source := request.Account
	source.Extra["codex_identity_version"] = "v1"
	request.IdentitySource = source
	request.Account = &Account{ID: 9121, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &source.ID}
	ap := pool.getOrCreateAccountPool(request.Account.ID)
	ap.mu.Lock()
	ap.lastAcquire = cloneOpenAIWSAcquireRequestPtr(&request)
	ap.mu.Unlock()
	pool.ensureTargetIdleAsync(request.Account.ID)
	select {
	case <-dialer.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("prewarm dial did not start")
	}
	// An administrator changes the credential owner's version while a delayed
	// shadow-account dial is still using the previous configuration.
	source.Extra["codex_identity_version"] = "v2"
	ap.mu.Lock()
	ap.lastAcquire = cloneOpenAIWSAcquireRequestPtr(&request)
	ap.mu.Unlock()
	close(dialer.releaseFirst)
	require.Eventually(t, func() bool {
		ap.mu.Lock()
		defer ap.mu.Unlock()
		if ap.prewarmActive || len(ap.conns) != 1 {
			return false
		}
		for _, conn := range ap.conns {
			return conn != nil && conn.handshakeCompatibility.identityVersion == "v2" &&
				conn.handshakeCompatibility.credentialIdentity == codexAccountIdentityNamespace(source)
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, 2, dialer.DialCount(), "a dial from the old credential-owner snapshot must be replaced")
}
