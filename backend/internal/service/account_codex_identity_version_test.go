package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexIdentityVersionDefaultsAndEligibility(t *testing.T) {
	require.Equal(t, "v1", (*Account)(nil).GetCodexIdentityVersion())
	require.False(t, codexIdentityV2Enabled(nil))
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		for _, version := range []any{nil, "", "v1", "v3", "V2", " v2 ", true, 2, []any{"v2"}, map[string]any{"version": "v2"}} {
			t.Run(fmt.Sprintf("%s/%v", accountType, version), func(t *testing.T) {
				account := &Account{Platform: PlatformOpenAI, Type: accountType, Extra: map[string]any{codexIdentityVersionExtraKey: version}}
				require.Equal(t, "v1", account.GetCodexIdentityVersion())
				require.False(t, codexIdentityV2Enabled(account))
			})
		}
		account := &Account{Platform: PlatformOpenAI, Type: accountType}
		require.Equal(t, "v1", account.GetCodexIdentityVersion())
		account.Extra = map[string]any{codexIdentityVersionExtraKey: "v2"}
		require.True(t, codexIdentityV2Enabled(account))
	}
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
	} {
		account.Extra = map[string]any{codexIdentityVersionExtraKey: "v2"}
		require.Equal(t, "v1", account.GetCodexIdentityVersion())
	}
}

func TestCodexIdentityVersionCreateValidatesStrictEnum(t *testing.T) {
	ctx := context.Background()
	for _, version := range []any{nil, "", "V2", " v2", true, 2, []any{"v2"}} {
		for _, path := range []string{"admin", "account"} {
			t.Run(fmt.Sprintf("%s/%v", path, version), func(t *testing.T) {
				repo := &upstreamBillingProbeAccountRepo{}
				extra := map[string]any{codexIdentityVersionExtraKey: version}
				var err error
				if path == "admin" {
					_, err = (&adminServiceImpl{accountRepo: repo}).CreateAccount(ctx, &CreateAccountInput{
						Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: extra, SkipDefaultGroupBind: true,
					})
				} else {
					_, err = NewAccountService(repo, nil).Create(ctx, CreateAccountRequest{
						Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: extra,
					})
				}
				require.ErrorContains(t, err, "codex_identity_version must be v1 or v2")
				require.Empty(t, repo.accounts)
			})
		}
	}
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		for _, version := range []string{"", "v1", "v2"} {
			extra := map[string]any{"custom": "kept"}
			if version != "" {
				extra[codexIdentityVersionExtraKey] = version
			}
			created, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: accountType}, extra)
			require.NoError(t, err)
			require.Equal(t, "kept", created.Extra["custom"])
			if version == "" {
				require.NotContains(t, created.Extra, codexIdentityVersionExtraKey)
			} else {
				require.Equal(t, version, created.GetCodexIdentityVersion())
			}
		}
	}
}

func TestCodexIdentityVersionUpdateRoundTripAndRollbackPreserveSeed(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"admin", "account"} {
		t.Run(path, func(t *testing.T) {
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
				ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Status: StatusActive,
				Extra: map[string]any{
					codexIdentityVersionExtraKey: "v2", codexFingerprintSeedExtraKey: testCodexFingerprintSeed,
					codexFingerprintModeExtraKey: "device", "custom": map[string]any{"keep": true},
				},
			}}}
			update := func(extra map[string]any) (*Account, error) {
				if path == "admin" {
					return (&adminServiceImpl{accountRepo: repo}).UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: extra})
				}
				return NewAccountService(repo, nil).Update(ctx, 1, UpdateAccountRequest{Extra: &extra})
			}
			// Import/export already round-trip extra through JSON, without another schema.
			payload, err := json.Marshal(repo.accounts[1].Extra)
			require.NoError(t, err)
			var extra map[string]any
			require.NoError(t, json.Unmarshal(payload, &extra))
			delete(extra, codexIdentityVersionExtraKey) // older client's full extra object
			updated, err := update(extra)
			require.NoError(t, err)
			require.Equal(t, "v2", updated.GetCodexIdentityVersion())
			require.Equal(t, map[string]any{"keep": true}, updated.Extra["custom"])
			for _, version := range []string{"v1", "v2"} {
				extra[codexIdentityVersionExtraKey] = version
				updated, err = update(extra)
				require.NoError(t, err)
				require.Equal(t, version, updated.GetCodexIdentityVersion())
				require.Equal(t, testCodexFingerprintSeed, updated.Extra[codexFingerprintSeedExtraKey])
				require.Equal(t, "device", updated.Extra[codexFingerprintModeExtraKey])
				require.Equal(t, map[string]any{"keep": true}, updated.Extra["custom"])
			}
		})
	}
}

func TestCodexIdentityVersionWritePathsRejectInvalidTargetsBeforeWriting(t *testing.T) {
	ctx := context.Background()
	parentID := int64(9)
	for _, target := range []struct {
		name    string
		account Account
		version any
	}{
		{"invalid_enum", Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, "v3"},
		{"api_key", Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "v2"},
		{"other_platform", Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, "v2"},
		{"shadow", Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}, "v2"},
	} {
		for _, path := range []string{"admin_update", "account_update", "extra", "bulk"} {
			t.Run(target.name+"/"+path, func(t *testing.T) {
				account := target.account
				account.ID = 1
				account.Extra = map[string]any{"custom": "before"}
				repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: &account}}
				svc := &adminServiceImpl{accountRepo: repo}
				extra := map[string]any{codexIdentityVersionExtraKey: target.version, "custom": "after"}
				var err error
				switch path {
				case "admin_update":
					_, err = svc.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: extra})
				case "account_update":
					_, err = NewAccountService(repo, nil).Update(ctx, 1, UpdateAccountRequest{Extra: &extra})
				case "extra":
					err = svc.UpdateAccountExtra(ctx, 1, extra)
				case "bulk":
					_, err = svc.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: extra})
				}
				require.Error(t, err)
				require.Equal(t, map[string]any{"custom": "before"}, repo.accounts[1].Extra)
				require.Empty(t, repo.updates)
				require.Empty(t, repo.bulkUpdates)
			})
		}
	}
}

func TestCodexIdentityVersionBulkAndExtraExplicitRollback(t *testing.T) {
	ctx := context.Background()
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken,
		Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexFingerprintSeedExtraKey: testCodexFingerprintSeed, "custom": true},
	}}}
	svc := &adminServiceImpl{accountRepo: repo}
	result, err := svc.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{
		AccountIDs: []int64{1}, Extra: map[string]any{codexIdentityVersionExtraKey: "v1"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Len(t, repo.bulkUpdates, 1)
	require.Equal(t, map[string]any{codexIdentityVersionExtraKey: "v1"}, repo.bulkUpdates[0].Extra)
	require.False(t, repo.bulkUpdates[0].EnsureCodexFingerprintSeed)

	require.NoError(t, svc.UpdateAccountExtra(ctx, 1, map[string]any{codexIdentityVersionExtraKey: "v1"}))
	require.Equal(t, "v1", repo.accounts[1].GetCodexIdentityVersion())
	require.Equal(t, testCodexFingerprintSeed, repo.accounts[1].Extra[codexFingerprintSeedExtraKey])
	require.Equal(t, true, repo.accounts[1].Extra["custom"])
}
