//go:build unit

package service

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokBillingCASRepoStub struct {
	AccountRepository

	mu                 sync.Mutex
	account            *Account
	getCalls           int
	changeOnGetCall    int
	casCalls           int
	updateExtraCalls   int
	lastExpected       GrokBillingProbeIdentity
	lastBilling        *xai.BillingSummary
	forceCASApplied    *bool
	forceCASErr        error
	refreshPersistCall int
}

func (r *grokBillingCASRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getCalls++
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	if r.changeOnGetCall > 0 && r.getCalls == r.changeOnGetCall {
		r.account.Credentials = mergeMap(r.account.Credentials, map[string]any{
			"access_token": "replacement-token",
			"sub":          "replacement-subject",
		})
	}
	return cloneGrokBillingTestAccount(r.account), nil
}

func (r *grokBillingCASRepoStub) UpdateExtra(_ context.Context, _ int64, _ map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateExtraCalls++
	return nil
}

func (r *grokBillingCASRepoStub) UpdateGrokBillingSnapshotIfIdentityUnchanged(
	_ context.Context,
	accountID int64,
	expected GrokBillingProbeIdentity,
	billing *xai.BillingSummary,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.casCalls++
	r.lastExpected = expected
	r.lastBilling = billing
	if r.forceCASErr != nil {
		return false, r.forceCASErr
	}
	if r.forceCASApplied != nil && !*r.forceCASApplied {
		return false, ErrGrokBillingProbeIdentityChanged
	}
	if r.account == nil || r.account.ID != accountID {
		return false, ErrGrokBillingProbeIdentityChanged
	}
	if r.account.Extra == nil {
		r.account.Extra = make(map[string]any)
	}
	r.account.Extra[grokBillingExtraKey] = billing
	return true, nil
}

func (r *grokBillingCASRepoStub) UpdateGrokOAuthCredentialsIfUnchanged(
	_ context.Context,
	id int64,
	expectedCredentials map[string]any,
	expectedProxyID *int64,
	credentials map[string]any,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshPersistCall++
	if r.account == nil || r.account.ID != id ||
		!reflect.DeepEqual(r.account.Credentials, expectedCredentials) ||
		!grokCredentialProxyIDsEqual(r.account.ProxyID, expectedProxyID) {
		return false, nil
	}
	r.account.Credentials = mergeMap(nil, credentials)
	return true, nil
}

func cloneGrokBillingTestAccount(account *Account) *Account {
	if account == nil {
		return nil
	}
	clone := *account
	clone.Credentials = mergeMap(nil, account.Credentials)
	clone.Extra = mergeMap(nil, account.Extra)
	clone.ProxyID = cloneGrokProxyID(account.ProxyID)
	return &clone
}

type grokBillingNoCASRepoStub struct {
	AccountRepository
	account          *Account
	updateExtraCalls int
}

func (r *grokBillingNoCASRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return cloneGrokBillingTestAccount(r.account), nil
}

func (r *grokBillingNoCASRepoStub) UpdateExtra(_ context.Context, _ int64, _ map[string]any) error {
	r.updateExtraCalls++
	return nil
}

type grokBillingRefreshExecutorStub struct {
	credentials map[string]any
}

func (e *grokBillingRefreshExecutorStub) CanRefresh(account *Account) bool {
	return account != nil && account.IsGrokOAuth()
}

func (e *grokBillingRefreshExecutorStub) NeedsRefresh(_ *Account, _ time.Duration) bool {
	return true
}

func (e *grokBillingRefreshExecutorStub) Refresh(_ context.Context, _ *Account) (map[string]any, error) {
	return mergeMap(nil, e.credentials), nil
}

func (e *grokBillingRefreshExecutorStub) CacheKey(account *Account) string {
	return GrokTokenCacheKey(account)
}

func TestGrokBillingProbeRejectsIdentityChangedBeforePersistence(t *testing.T) {
	account := healthyGrokQuotaOAuthAccount(701)
	account.Credentials["sub"] = "original-subject"
	repo := &grokBillingCASRepoStub{account: account, changeOnGetCall: 3}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), &grokHybridUpstream{}, nil)

	result, err := svc.ProbeBilling(context.Background(), account.ID)

	require.ErrorIs(t, err, ErrGrokBillingProbeIdentityChanged)
	if result != nil {
		require.False(t, result.Persisted)
	}
	require.Zero(t, repo.casCalls, "service pre-persist reread should reject a known stale identity")
	require.Zero(t, repo.updateExtraCalls, "billing snapshots must never fall back to UpdateExtra")
}

func TestGrokBillingProbeUsesDurableIdentityAfterTokenRefresh(t *testing.T) {
	account := healthyGrokQuotaOAuthAccount(702)
	account.Credentials["access_token"] = "expired-token"
	account.Credentials["refresh_token"] = "old-refresh-token"
	account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	account.Credentials["sub"] = "stable-subject"
	account.Credentials["base_url"] = "https://cli-chat-proxy.grok.com/"
	account.Credentials[credKeyHeaderOverrideEnabled] = true
	account.Credentials[credKeyHeaderOverrides] = map[string]any{"X-Route": "stable"}
	repo := &grokBillingCASRepoStub{account: account}
	provider := NewGrokTokenProvider(repo, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), &grokBillingRefreshExecutorStub{credentials: map[string]any{
		"access_token":               "refreshed-token",
		"refresh_token":              "new-refresh-token",
		"expires_at":                 time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		"sub":                        "stable-subject",
		"base_url":                   "https://cli-chat-proxy.grok.com/",
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides:       map[string]any{"x-route": "stable"},
	}})
	svc := NewGrokQuotaService(repo, nil, provider, &grokHybridUpstream{}, nil)

	result, err := svc.ProbeBilling(context.Background(), account.ID)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Persisted)
	require.Equal(t, 1, repo.refreshPersistCall)
	require.Equal(t, 1, repo.casCalls)
	require.Contains(t, repo.lastExpected.CredentialsJSON, "refreshed-token")
	require.NotContains(t, repo.lastExpected.CredentialsJSON, "expired-token")
	require.Equal(t, "stable-subject", repo.lastExpected.OAuthSubject)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1", repo.lastExpected.NormalizedBaseURL)
	require.NotEmpty(t, repo.lastExpected.TokenHash)
	require.NotEmpty(t, repo.lastExpected.HeaderOverrideFingerprint)
	require.Zero(t, repo.updateExtraCalls)
}

func TestGrokBillingProbeRequiresCASWriterWithoutUpdateExtraFallback(t *testing.T) {
	account := healthyGrokQuotaOAuthAccount(703)
	repo := &grokBillingNoCASRepoStub{account: account}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), &grokHybridUpstream{}, nil)

	result, err := svc.ProbeBilling(context.Background(), account.ID)

	require.ErrorIs(t, err, ErrGrokBillingProbeCASUnavailable)
	require.NotNil(t, result)
	require.False(t, result.Persisted)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Zero(t, repo.updateExtraCalls)
}
