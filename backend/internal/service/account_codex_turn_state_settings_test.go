package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTurnStateSettingsDefaultsAndTeamLengths(t *testing.T) {
	require.Equal(t, "off", (*Account)(nil).GetCodexTurnStateMode())
	require.Equal(t, []int{292, 332}, (*Account)(nil).GetCodexTurnStateCandidateLengths())
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{codexTurnStateModeExtraKey: "reuse"}}
	require.Equal(t, "off", account.GetCodexTurnStateMode(), "v1 never opts in")
	account.Extra[codexIdentityVersionExtraKey] = "v2"
	require.Equal(t, "reuse", account.GetCodexTurnStateMode())
	account.Extra[codexTurnStateCandidateLengthsExtraKey] = []any{float64(332), json.Number("292"), 332}
	require.Equal(t, []int{332, 292}, account.GetCodexTurnStateCandidateLengths())
	account.Extra[codexTurnStateCandidateLengthsExtraKey] = []any{}
	require.Equal(t, []int{}, account.GetCodexTurnStateCandidateLengths(), "empty does not restore the defaults")
	account.Extra[codexTurnStateCandidateLengthsExtraKey] = "332"
	require.Equal(t, []int{292, 332}, account.GetCodexTurnStateCandidateLengths(), "bad historic settings use safe defaults")
	account.Type = AccountTypeAPIKey
	require.Equal(t, "off", account.GetCodexTurnStateMode())
}

func TestCodexTurnStateSettingsRejectMalformedExtra(t *testing.T) {
	for _, mode := range []any{nil, "", "auto", "REUSE", false, 1} {
		t.Run(fmt.Sprint("mode/", mode), func(t *testing.T) {
			require.Error(t, validateCodexIdentityVersionExtra(map[string]any{codexTurnStateModeExtraKey: mode}))
		})
	}
	for _, lengths := range []any{
		nil, "332", 332, []any{"332"}, []any{true}, []any{332.5}, []int{99}, []int{2049},
		[]any{math.NaN()}, []any{math.Inf(1)}, []int{100, 101, 102, 103, 104, 105, 106, 107, 108},
	} {
		t.Run(fmt.Sprint("lengths/", lengths), func(t *testing.T) {
			require.Error(t, validateCodexIdentityVersionExtra(map[string]any{codexTurnStateCandidateLengthsExtraKey: lengths}))
		})
	}
	for _, lengths := range []any{[]any{}, []int{332}, []int{100, 2048}, []int{292, 332, 292}} {
		require.NoError(t, validateCodexIdentityVersionExtra(map[string]any{codexTurnStateCandidateLengthsExtraKey: lengths}))
	}
}

func TestCodexTurnStateSettingsCreateRequiresEligibleV2(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"admin", "account"} {
		for _, version := range []string{"v1", "v2"} {
			t.Run(path+"/"+version, func(t *testing.T) {
				repo := &upstreamBillingProbeAccountRepo{}
				extra := map[string]any{
					codexIdentityVersionExtraKey: version, codexTurnStateModeExtraKey: "observe", codexTurnStateCandidateLengthsExtraKey: []any{float64(332)},
				}
				var created *Account
				var err error
				if path == "admin" {
					created, err = (&adminServiceImpl{accountRepo: repo}).CreateAccount(ctx, &CreateAccountInput{
						Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: extra, SkipDefaultGroupBind: true,
					})
				} else {
					created, err = NewAccountService(repo, nil).Create(ctx, CreateAccountRequest{
						Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: extra,
					})
				}
				if version == "v1" {
					require.ErrorContains(t, err, "require identity v2")
					require.Empty(t, repo.accounts)
				} else {
					require.NoError(t, err)
					require.Equal(t, "observe", created.GetCodexTurnStateMode())
					require.Equal(t, []int{332}, created.GetCodexTurnStateCandidateLengths())
				}
			})
		}
	}
}

func TestCodexTurnStateSettingsStateOnlyWritesValidateTargets(t *testing.T) {
	ctx := context.Background()
	parentID := int64(9)
	for _, target := range []struct {
		name    string
		account Account
	}{
		{"v1", Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
		{"api_key", Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}},
		{"other_platform", Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}},
		{"shadow", Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}},
	} {
		for _, path := range []string{"admin_update", "account_update", "extra", "bulk"} {
			t.Run(target.name+"/"+path, func(t *testing.T) {
				account := target.account
				account.ID = 1
				account.Extra = map[string]any{"custom": "before"}
				repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: &account}}
				svc := &adminServiceImpl{accountRepo: repo}
				extra := map[string]any{codexTurnStateModeExtraKey: "reuse", codexTurnStateCandidateLengthsExtraKey: []int{332}}
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

func TestCodexTurnStateSettingsUnrelatedUpdatesPreserveConfiguration(t *testing.T) {
	ctx := context.Background()
	for _, path := range []string{"admin", "account"} {
		t.Run(path, func(t *testing.T) {
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: {
				ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Status: StatusActive,
				Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse", codexTurnStateCandidateLengthsExtraKey: []int{332}, "custom": true},
			}}}
			update := func(extra map[string]any) (*Account, error) {
				if path == "admin" {
					return (&adminServiceImpl{accountRepo: repo}).UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: extra})
				}
				return NewAccountService(repo, nil).Update(ctx, 1, UpdateAccountRequest{Extra: &extra})
			}
			updated, err := update(map[string]any{"custom": "updated"})
			require.NoError(t, err)
			require.Equal(t, "reuse", updated.GetCodexTurnStateMode())
			require.Equal(t, []int{332}, updated.GetCodexTurnStateCandidateLengths())
			require.Equal(t, "updated", updated.Extra["custom"])
			updated, err = update(map[string]any{codexIdentityVersionExtraKey: "v1"})
			require.NoError(t, err)
			require.Equal(t, "off", updated.GetCodexTurnStateMode(), "identity rollback suspends preserved settings")
			updated, err = update(map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "observe", codexTurnStateCandidateLengthsExtraKey: []any{}})
			require.NoError(t, err)
			require.Equal(t, "observe", updated.GetCodexTurnStateMode())
			require.Equal(t, []int{}, updated.GetCodexTurnStateCandidateLengths())
			updated, err = update(map[string]any{codexTurnStateModeExtraKey: "off"})
			require.NoError(t, err)
			require.Equal(t, "off", updated.GetCodexTurnStateMode())
		})
	}
}

func TestCodexTurnStateSettingsAgentIdentityDisabledAndRejectedOnCreate(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"auth_mode": " AgentIdentity "},
		Extra: map[string]any{
			codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse", codexTurnStateCandidateLengthsExtraKey: []int{332},
		},
	}
	require.Equal(t, "off", account.GetCodexTurnStateMode())
	require.Empty(t, account.GetCodexTurnStateCandidateLengths())
	for _, path := range []string{"admin", "account"} {
		t.Run(path, func(t *testing.T) {
			repo := &upstreamBillingProbeAccountRepo{}
			var err error
			if path == "admin" {
				_, err = (&adminServiceImpl{accountRepo: repo}).CreateAccount(context.Background(), &CreateAccountInput{
					Platform: account.Platform, Type: account.Type, Credentials: account.Credentials, Extra: account.Extra, SkipDefaultGroupBind: true,
				})
			} else {
				_, err = NewAccountService(repo, nil).Create(context.Background(), CreateAccountRequest{
					Platform: account.Platform, Type: account.Type, Credentials: account.Credentials, Extra: account.Extra,
				})
			}
			require.ErrorContains(t, err, "agentIdentity is not supported")
			require.Empty(t, repo.accounts)
		})
	}
}

func TestCodexTurnStateSettingsAgentIdentityUsesEffectiveUpdateCredentials(t *testing.T) {
	for _, path := range []string{"admin", "account", "extra", "bulk"} {
		for _, existingAgent := range []bool{false, true} {
			if path == "extra" && !existingAgent {
				continue // This path cannot change credentials.
			}
			t.Run(fmt.Sprintf("%s/existing=%t", path, existingAgent), func(t *testing.T) {
				credentials := map[string]any{"auth_mode": "oauth", "access_token": "unchanged"}
				if existingAgent {
					credentials["auth_mode"] = OpenAIAuthModeAgentIdentity
				}
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
					Credentials: credentials, Extra: map[string]any{codexIdentityVersionExtraKey: "v2"}}
				repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}
				extra := map[string]any{codexTurnStateModeExtraKey: "observe"}
				var nextCredentials map[string]any
				if !existingAgent {
					nextCredentials = map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}
				}
				var err error
				switch path {
				case "admin":
					_, err = (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: extra, Credentials: nextCredentials})
				case "account":
					request := UpdateAccountRequest{Extra: &extra}
					if nextCredentials != nil {
						request.Credentials = &nextCredentials
					}
					_, err = NewAccountService(repo, nil).Update(context.Background(), 1, request)
				case "extra":
					err = (&adminServiceImpl{accountRepo: repo}).UpdateAccountExtra(context.Background(), 1, extra)
				case "bulk":
					_, err = (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: extra, Credentials: nextCredentials})
				}
				require.ErrorContains(t, err, "agentIdentity is not supported")
				require.Equal(t, existingAgent, account.IsOpenAIAgentIdentity(), "validation must not mutate the saved credentials")
				require.Equal(t, "unchanged", account.Credentials["access_token"])
				require.Empty(t, repo.updates)
				require.Empty(t, repo.bulkUpdates)
			})
		}
	}
}

func TestCodexTurnStateActiveCollectionDefaultsAndEligibility(t *testing.T) {
	require.False(t, (*Account)(nil).GetCodexTurnStateActiveCollectionEnabled())
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		account := &Account{Platform: PlatformOpenAI, Type: accountType, Extra: map[string]any{
			codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse",
		}}
		require.False(t, account.GetCodexTurnStateActiveCollectionEnabled())
		for _, value := range []any{nil, false, "true", 1} {
			account.Extra[codexTurnStateActiveCollectionExtraKey] = value
			require.False(t, account.GetCodexTurnStateActiveCollectionEnabled())
		}
		account.Extra[codexTurnStateActiveCollectionExtraKey] = true
		require.True(t, account.GetCodexTurnStateActiveCollectionEnabled())
		for _, mode := range []string{"off", "observe"} {
			account.Extra[codexTurnStateModeExtraKey] = mode
			require.False(t, account.GetCodexTurnStateActiveCollectionEnabled())
		}
		account.Extra[codexTurnStateModeExtraKey] = "reuse"
		account.Extra[codexIdentityVersionExtraKey] = "v1"
		require.False(t, account.GetCodexTurnStateActiveCollectionEnabled())
		account.Extra[codexIdentityVersionExtraKey] = "v2"
		account.Type = AccountTypeOAuth
		account.Credentials = map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}
		require.False(t, account.GetCodexTurnStateActiveCollectionEnabled())
		account.Credentials = nil
		account.Type = AccountTypeAPIKey
		require.False(t, account.GetCodexTurnStateActiveCollectionEnabled())
	}
}

func TestCodexTurnStateActiveCollectionCreateValidatesExplicitOptIn(t *testing.T) {
	for _, path := range []string{"admin", "account"} {
		for _, tc := range []struct {
			name, version, mode string
			value               any
			wantError           bool
			wantEnabled         bool
		}{
			{"enabled", "v2", "reuse", true, false, true},
			{"disabled_v1", "v1", "off", false, false, false},
			{"non_boolean", "v2", "reuse", "true", true, false},
			{"observe", "v2", "observe", true, true, false},
			{"v1", "v1", "off", true, true, false},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				repo := &upstreamBillingProbeAccountRepo{}
				extra := map[string]any{codexIdentityVersionExtraKey: tc.version, codexTurnStateModeExtraKey: tc.mode, codexTurnStateActiveCollectionExtraKey: tc.value}
				var created *Account
				var err error
				if path == "admin" {
					created, err = (&adminServiceImpl{accountRepo: repo}).CreateAccount(context.Background(), &CreateAccountInput{
						Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: extra, SkipDefaultGroupBind: true,
					})
				} else {
					created, err = NewAccountService(repo, nil).Create(context.Background(), CreateAccountRequest{
						Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Extra: extra,
					})
				}
				if tc.wantError {
					require.Error(t, err)
					require.Empty(t, repo.accounts)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.value, created.Extra[codexTurnStateActiveCollectionExtraKey])
				require.Equal(t, tc.wantEnabled, created.GetCodexTurnStateActiveCollectionEnabled())
			})
		}
	}
}

func TestCodexTurnStateActiveCollectionAllWritesValidateTargets(t *testing.T) {
	parentID := int64(9)
	for _, path := range []string{"admin", "account", "extra", "bulk"} {
		for _, tc := range []struct {
			name   string
			mutate func(*Account)
			value  any
		}{
			{"non_boolean", func(*Account) {}, "true"},
			{"observe", func(a *Account) { a.Extra[codexTurnStateModeExtraKey] = "observe" }, true},
			{"v1", func(a *Account) { a.Extra[codexIdentityVersionExtraKey] = "v1" }, true},
			{"api_key", func(a *Account) { a.Type = AccountTypeAPIKey }, true},
			{"other_platform", func(a *Account) { a.Platform = PlatformAnthropic }, true},
			{"shadow_on", func(a *Account) { a.ParentAccountID = &parentID }, true},
			{"shadow_off", func(a *Account) { a.ParentAccountID = &parentID }, false},
			{"agent_identity", func(a *Account) {
				a.Type = AccountTypeOAuth
				a.Credentials = map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}
			}, true},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken,
					Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse"}}
				tc.mutate(account)
				repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}
				extra := map[string]any{codexTurnStateActiveCollectionExtraKey: tc.value}
				_, err := writeCodexActiveCollectionSettingsForTest(path, repo, extra)
				require.Error(t, err)
				require.NotContains(t, repo.accounts[1].Extra, codexTurnStateActiveCollectionExtraKey)
				require.Empty(t, repo.updates)
				require.Empty(t, repo.bulkUpdates)
			})
		}
	}
}

func writeCodexActiveCollectionSettingsForTest(path string, repo *upstreamBillingProbeAccountRepo, extra map[string]any) (*Account, error) {
	ctx := context.Background()
	svc := &adminServiceImpl{accountRepo: repo}
	switch path {
	case "admin":
		return svc.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: extra})
	case "account":
		return NewAccountService(repo, nil).Update(ctx, 1, UpdateAccountRequest{Extra: &extra})
	case "extra":
		if err := svc.UpdateAccountExtra(ctx, 1, extra); err != nil {
			return nil, err
		}
		return repo.GetByID(ctx, 1)
	case "bulk":
		if _, err := svc.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: extra}); err != nil {
			return nil, err
		}
		// The repository stub records JSONB updates without applying them.
		updated := *repo.accounts[1]
		updated.Extra = mergeMap(updated.Extra, repo.bulkUpdates[len(repo.bulkUpdates)-1].Extra)
		return &updated, nil
	}
	return nil, fmt.Errorf("unsupported settings test path %q", path)
}

func TestCodexTurnStateActiveCollectionUpdatesPreserveDormantConfiguration(t *testing.T) {
	for _, path := range []string{"admin", "account", "extra", "bulk"} {
		for _, tc := range []struct {
			name        string
			patch       map[string]any
			wantStored  bool
			wantEnabled bool
		}{
			{"unrelated", map[string]any{"custom": "updated"}, true, true},
			{"off", map[string]any{codexTurnStateModeExtraKey: "off"}, true, false},
			{"observe", map[string]any{codexTurnStateModeExtraKey: "observe"}, true, false},
			{"v1", map[string]any{codexIdentityVersionExtraKey: "v1"}, true, false},
			{"explicit_disable_with_v1", map[string]any{codexIdentityVersionExtraKey: "v1", codexTurnStateActiveCollectionExtraKey: false}, false, false},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Status: StatusActive,
					Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse", codexTurnStateActiveCollectionExtraKey: true}}
				repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}
				updated, err := writeCodexActiveCollectionSettingsForTest(path, repo, mergeMap(nil, tc.patch))
				require.NoError(t, err)
				require.Equal(t, tc.wantStored, updated.Extra[codexTurnStateActiveCollectionExtraKey])
				require.Equal(t, tc.wantEnabled, updated.GetCodexTurnStateActiveCollectionEnabled())
			})
		}
		for _, initial := range []struct{ version, mode, patchMode string }{
			{"v1", "reuse", ""}, {"v2", "reuse", ""}, {"v2", "observe", "reuse"}, {"v1", "off", "reuse"},
		} {
			t.Run(path+"/enable_from_"+initial.version+"_"+initial.mode, func(t *testing.T) {
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken, Status: StatusActive,
					Extra: map[string]any{codexIdentityVersionExtraKey: initial.version, codexTurnStateModeExtraKey: initial.mode}}
				repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}
				patch := map[string]any{
					codexIdentityVersionExtraKey: "v2", codexTurnStateActiveCollectionExtraKey: true,
				}
				if initial.patchMode != "" {
					patch[codexTurnStateModeExtraKey] = initial.patchMode
				}
				updated, err := writeCodexActiveCollectionSettingsForTest(path, repo, patch)
				require.NoError(t, err)
				require.True(t, updated.GetCodexTurnStateActiveCollectionEnabled())
			})
		}
	}
}

func TestCodexTurnStateActiveCollectionBulkValidatesAllBeforeWriting(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "reuse"}},
		2: {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{codexIdentityVersionExtraKey: "v2", codexTurnStateModeExtraKey: "observe"}},
	}}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2}, Extra: map[string]any{codexTurnStateActiveCollectionExtraKey: true},
	})
	require.ErrorContains(t, err, "reuse mode")
	require.Empty(t, repo.bulkUpdates)
	require.NotContains(t, repo.accounts[1].Extra, codexTurnStateActiveCollectionExtraKey)
}
