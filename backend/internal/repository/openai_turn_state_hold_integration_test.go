//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Exercise PostgreSQL's JSONB parameter typing and row-level CAS, rather than
// relying only on SQL mocks. All changes, including outbox events, roll back.
func TestCodexHunterHoldSQLPreservesConcurrentProviderLimits(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	ctx = dbent.NewTxContext(ctx, tx)
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	other := map[string]any{"reason": "provider_429", "rate_limit_reset_at": until.Format(time.RFC3339)}
	row, err := tx.Client().Account.Create().SetName("hunter-hold-cas").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
		SetCredentials(map[string]any{"access_token": "fixture"}).SetExtra(map[string]any{"ordinary": "kept", "model_rate_limits": map[string]any{"other": other}}).Save(ctx)
	require.NoError(t, err)
	repo := &accountRepository{client: integrationEntClient, sql: integrationDB}
	const model = "gpt.literal[1].model"
	require.NoError(t, repo.SetCodexHunterModelHold(ctx, row.ID, model, until, nil))
	current, err := tx.Client().Account.Query().Where(dbaccount.IDEQ(row.ID)).Only(ctx)
	require.NoError(t, err)
	limits := current.Extra["model_rate_limits"].(map[string]any)
	require.Equal(t, other, limits["other"])
	hold := limits[model].(map[string]any)
	require.Equal(t, service.OpenAITurnStateHoldSelectionReason, hold["reason"])
	require.Equal(t, "kept", current.Extra["ordinary"])

	// A new provider limit replaces the hunter entry. Its reason and deadline
	// must survive both a stale clear and another hold based on an empty view.
	limits[model] = other
	require.NoError(t, tx.Client().Account.UpdateOneID(row.ID).SetExtra(current.Extra).Exec(ctx))
	require.NoError(t, repo.ReleaseCodexHunterModelHold(ctx, row.ID, model, until))
	require.NoError(t, repo.SetCodexHunterModelHold(ctx, row.ID, model, until.Add(time.Minute), nil))
	current, err = tx.Client().Account.Query().Where(dbaccount.IDEQ(row.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, other, current.Extra["model_rate_limits"].(map[string]any)[model])

	// Restore the old hold, then prove only an exact generation is released.
	limits = current.Extra["model_rate_limits"].(map[string]any)
	limits[model] = hold
	require.NoError(t, tx.Client().Account.UpdateOneID(row.ID).SetExtra(current.Extra).Exec(ctx))
	require.NoError(t, repo.ReleaseCodexHunterModelHold(ctx, row.ID, model, until.Add(-time.Second)))
	current, err = tx.Client().Account.Query().Where(dbaccount.IDEQ(row.ID)).Only(ctx)
	require.NoError(t, err)
	require.Contains(t, current.Extra["model_rate_limits"], model)
	require.NoError(t, repo.ReleaseCodexHunterModelHold(ctx, row.ID, model, until))
	current, err = tx.Client().Account.Query().Where(dbaccount.IDEQ(row.ID)).Only(ctx)
	require.NoError(t, err)
	require.NotContains(t, current.Extra["model_rate_limits"], model)
	require.Equal(t, other, current.Extra["model_rate_limits"].(map[string]any)["other"])
}
