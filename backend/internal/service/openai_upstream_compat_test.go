package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type upstreamCompatProxyRepo struct {
	ProxyRepository
	proxy *Proxy
	err   error
	calls []int64
}

func (r *upstreamCompatProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	r.calls = append(r.calls, id)
	return r.proxy, r.err
}

func TestUpstreamCompatProxyBindingDoesNotFallBackToDirect(t *testing.T) {
	id := int64(9)
	for _, tc := range []struct {
		name   string
		repo   ProxyRepository
		loaded *Proxy
	}{
		{"missing_repository", nil, nil},
		{"lookup_error", &upstreamCompatProxyRepo{err: errors.New("lookup unavailable")}, nil},
		{"missing_row", &upstreamCompatProxyRepo{}, nil},
		{"wrong_binding", nil, &Proxy{ID: 10, Protocol: "http", Host: "proxy.invalid", Port: 8080}},
		{"invalid_protocol", nil, &Proxy{ID: id, Protocol: "file", Host: "proxy.invalid", Port: 8080}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := resolveConfiguredProxyURL(context.Background(), tc.repo, &id, tc.loaded)
			require.Error(t, err)
			require.Empty(t, value)
		})
	}
	account := &Account{Platform: PlatformOpenAI, ProxyID: &id}
	require.Error(t, requireOpenAIProxyBinding(account, ""))
	require.NoError(t, requireOpenAIProxyBinding(account, "socks5://proxy.invalid:1080"))
}

func TestUpstreamCompatProxyResolutionPreservesConfiguredEndpoint(t *testing.T) {
	id := int64(9)
	proxy := &Proxy{ID: id, Protocol: "socks5", Host: "proxy.invalid", Port: 1080, Username: "fixture-user", Password: "fixture-pass"}
	repo := &upstreamCompatProxyRepo{proxy: proxy}
	value, err := resolveConfiguredProxyURL(context.Background(), repo, &id, nil)
	require.NoError(t, err)
	require.Equal(t, proxy.URL(), value)
	require.Equal(t, []int64{id}, repo.calls)
	loadedValue, err := resolveConfiguredProxyURL(context.Background(), repo, &id, proxy)
	require.NoError(t, err)
	require.Equal(t, value, loadedValue)
	require.Len(t, repo.calls, 1, "loaded binding should not be resolved a second time")
	value, err = resolveConfiguredProxyURL(context.Background(), repo, nil, proxy)
	require.NoError(t, err)
	require.Empty(t, value, "only a nil binding means direct access")
}

func TestUpstreamCompatTargetsOnlySupportedCodexAccounts(t *testing.T) {
	require.False(t, (*Account)(nil).TargetsChatGPTCodexUpstream())
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		require.True(t, (&Account{Platform: PlatformOpenAI, Type: kind}).TargetsChatGPTCodexUpstream())
	}
	require.False(t, (&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}).TargetsChatGPTCodexUpstream())
	require.False(t, (&Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}).TargetsChatGPTCodexUpstream())
}
