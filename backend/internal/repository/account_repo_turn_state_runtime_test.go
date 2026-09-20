package repository

import (
	"context"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAITurnStateRuntimePreservesLatestLockedDatabaseState(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	mock.ExpectQuery(`(?s)`+regexp.QuoteMeta("SELECT")+`.*jsonb_build_object.*`+regexp.QuoteMeta("FOR NO KEY UPDATE")).
		WithArgs(int64(7), service.PlatformOpenAI, service.AccountTypeOAuth, `{"access_token":"test"}`, nil).
		WillReturnRows(sqlmock.NewRows([]string{
			"identity", "ollama_identity", "proxy_identity", "enabled", "rate_enabled", "snapshot",
			"ollama_session", "ollama_auto", "ollama_snapshot", "turn_state_runtime",
		}).AddRow(true, false, true, nil, nil, nil, nil, nil, nil,
			[]byte(`{"openai_turn_state_hunt":{"attempts":11},"openai_turn_state_recovery_state":{"streak":3}}`)))
	account := &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test"}, Extra: map[string]any{
			"openai_turn_state_hunt": map[string]any{"attempts": 1}, "ordinary": "kept",
		}}
	merged, err := lockAndMergeAccountProbeExtra(context.Background(), client, account, nil, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"attempts": float64(11)}, merged["openai_turn_state_hunt"])
	require.Equal(t, map[string]any{"streak": float64(3)}, merged["openai_turn_state_recovery_state"])
	require.Equal(t, "kept", merged["ordinary"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpenAITurnStateRuntimeCannotResurrectClearedState(t *testing.T) {
	extra := map[string]any{"openai_turn_state_hunt": map[string]any{"attempts": 7}, "ordinary": true}
	require.NoError(t, mergeOpenAITurnStateRuntimeExtra(extra, []byte(`{"openai_turn_state_hunt":null}`)))
	require.NotContains(t, extra, "openai_turn_state_hunt")
	require.Equal(t, true, extra["ordinary"])
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{"openai_turn_state_hunt": map[string]any{"attempts": 2}}))
}

func TestOpenAITurnStateProbeUsageFilterUsesDedicatedType(t *testing.T) {
	condition, args := buildRequestTypeFilterConditionWithAlias(2, int16(service.RequestTypeTurnStateProbe), "u")
	require.Equal(t, "u.request_type = $2", condition)
	require.Equal(t, []any{int16(6)}, args)
}
