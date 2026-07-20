package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newAtomicSettingRepoMock(t *testing.T) (*settingRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repository := NewSettingRepository(client)
	repo, ok := repository.(*settingRepository)
	require.True(t, ok)
	return repo, mock
}

func expectSettingsAdvisoryLock(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_xact_lock"}).AddRow(nil))
}

func securityBaselineRows(stepUp, risk bool) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows([]string{"id", "key", "value", "updated_at"}).
		AddRow(1, service.SettingKeyStepUpEnabled, boolString(stepUp), now).
		AddRow(2, service.SettingKeyRiskControlEnabled, boolString(risk), now)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func expectSecurityBaselineRead(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery(`SELECT .* FROM .*settings.*`).
		WithArgs(service.SettingKeyStepUpEnabled, service.SettingKeyRiskControlEnabled).
		WillReturnRows(rows)
}

func TestSettingRepositoryUpdateSettingsAtomicLocksReadsAuthorizesAndCommits(t *testing.T) {
	repo, mock := newAtomicSettingRepoMock(t)
	authorized := false
	mock.ExpectBegin()
	expectSettingsAdvisoryLock(mock)
	expectSecurityBaselineRead(mock, securityBaselineRows(true, true))
	mock.ExpectQuery(`INSERT INTO "settings"`).
		WithArgs("site_name", sqlmock.AnyArg(), "atomic").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	err := repo.UpdateSettingsAtomic(context.Background(), service.SettingAtomicUpdate{
		Updates: map[string]string{"site_name": "atomic"},
		Baseline: service.SettingSecurityBaseline{
			StepUpEnabled:      true,
			RiskControlEnabled: true,
		},
		Authorize: func(current service.SettingSecurityBaseline) error {
			authorized = true
			require.Equal(t, service.SettingSecurityBaseline{StepUpEnabled: true, RiskControlEnabled: true}, current)
			return nil
		},
	})

	require.NoError(t, err)
	require.True(t, authorized)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingRepositoryUpdateSettingsAtomicBaselineConflictIsStableAndRollsBack(t *testing.T) {
	repo, mock := newAtomicSettingRepoMock(t)
	authorizeCalls := 0
	mock.ExpectBegin()
	expectSettingsAdvisoryLock(mock)
	expectSecurityBaselineRead(mock, securityBaselineRows(false, true))
	mock.ExpectRollback()

	err := repo.UpdateSettingsAtomic(context.Background(), service.SettingAtomicUpdate{
		Updates: map[string]string{
			service.SettingKeyStepUpEnabled:      "false",
			service.SettingKeyRiskControlEnabled: "false",
		},
		Baseline: service.SettingSecurityBaseline{StepUpEnabled: true, RiskControlEnabled: true},
		Authorize: func(service.SettingSecurityBaseline) error {
			authorizeCalls++
			return nil
		},
	})

	require.ErrorIs(t, err, service.ErrSettingsUpdateConflict)
	require.Equal(t, "SETTINGS_UPDATE_CONFLICT", infraerrors.Reason(err))
	require.Zero(t, authorizeCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingRepositoryUpdateSettingsAtomicNonPostgresSkipsAdvisoryLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close() })
	repository := NewSettingRepository(client)
	repo, ok := repository.(*settingRepository)
	require.True(t, ok)

	mock.ExpectBegin()
	expectSecurityBaselineRead(mock, securityBaselineRows(false, false))
	mock.ExpectQuery(`INSERT INTO .*settings.*`).
		WithArgs("site_name", sqlmock.AnyArg(), "sqlite-compatible").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	err = repo.UpdateSettingsAtomic(context.Background(), service.SettingAtomicUpdate{
		Updates:  map[string]string{"site_name": "sqlite-compatible"},
		Baseline: service.SettingSecurityBaseline{},
	})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetMultipleWithClientEmptyKeysReturnsEmptyWithoutQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { _ = client.Close() })

	values, err := getMultipleWithClient(context.Background(), client, nil)

	require.NoError(t, err)
	require.Empty(t, values)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettingRepositoryUpdateSettingsAtomicStrictAuthorizationFailureRollsBackBeforeWrite(t *testing.T) {
	repo, mock := newAtomicSettingRepoMock(t)
	authErr := errors.New("strict authorization rejected")
	mock.ExpectBegin()
	expectSettingsAdvisoryLock(mock)
	expectSecurityBaselineRead(mock, securityBaselineRows(true, true))
	mock.ExpectRollback()

	err := repo.UpdateSettingsAtomic(context.Background(), service.SettingAtomicUpdate{
		Updates: map[string]string{
			service.SettingKeyStepUpEnabled:      "false",
			service.SettingKeyRiskControlEnabled: "false",
		},
		Baseline:  service.SettingSecurityBaseline{StepUpEnabled: true, RiskControlEnabled: true},
		Authorize: func(service.SettingSecurityBaseline) error { return authErr },
	})

	require.ErrorIs(t, err, authErr)
	require.NoError(t, mock.ExpectationsWereMet())
}
