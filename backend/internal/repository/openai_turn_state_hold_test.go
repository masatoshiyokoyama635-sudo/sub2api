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
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newCodexHunterHoldRepositoryTest(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return &accountRepository{client: client, sql: db}, mock
}

func TestCodexHunterHoldSetCASAndAtomicOutbox(t *testing.T) {
	repo, mock := newCodexHunterHoldRepositoryTest(t)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts.*ARRAY\['model_rate_limits', \$2::text\]::text\[\].*COALESCE\(extra #> ARRAY\['model_rate_limits', \$2::text\]::text\[\], 'null'::jsonb\) = \$4::jsonb`).
		WithArgs(int64(7), "gpt.a[1]", sqlmock.AnyArg(), "null").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).WithArgs(service.SchedulerOutboxEventAccountChanged, int64(7), nil, nil, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.SetCodexHunterModelHold(context.Background(), 7, "gpt.a[1]", time.Now().Add(time.Hour), nil))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexHunterHoldDoesNotOverwriteActiveProviderLimit(t *testing.T) {
	repo, mock := newCodexHunterHoldRepositoryTest(t)
	for _, limit := range []map[string]any{
		{"reason": "provider_429", "rate_limit_reset_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		{"reason": "provider_429", "rate_limit_reset_at": "invalid"},
	} {
		require.NoError(t, repo.SetCodexHunterModelHold(context.Background(), 7, "gpt-test", time.Now().Add(time.Hour), limit))
	}
	require.NoError(t, mock.ExpectationsWereMet(), "active or malformed provider limits must issue no writes")
}

func TestCodexHunterHoldConcurrentProviderWriteWins(t *testing.T) {
	repo, mock := newCodexHunterHoldRepositoryTest(t)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts.*\$4::jsonb`).WithArgs(int64(7), "gpt-test", sqlmock.AnyArg(), "null").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	require.NoError(t, repo.SetCodexHunterModelHold(context.Background(), 7, "gpt-test", time.Now().Add(time.Hour), nil))
	require.NoError(t, mock.ExpectationsWereMet(), "a CAS miss cannot publish a scheduler event")
}

func TestCodexHunterHoldReleaseComparesReasonAndDeadline(t *testing.T) {
	for _, changed := range []int64{0, 1} {
		repo, mock := newCodexHunterHoldRepositoryTest(t)
		deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
		mock.ExpectBegin()
		mock.ExpectExec(`(?s)UPDATE accounts.*`+regexp.QuoteMeta("(extra->'model_rate_limits') - ($2::text)")+`.*`+regexp.QuoteMeta("ARRAY['model_rate_limits', $2::text, 'reason']::text[] = $3")+`.*`+regexp.QuoteMeta("ARRAY['model_rate_limits', $2::text, 'rate_limit_reset_at']::text[] = $4")).
			WithArgs(int64(7), "gpt-test", service.OpenAITurnStateHoldSelectionReason, deadline.Format(time.RFC3339Nano)).WillReturnResult(sqlmock.NewResult(0, changed))
		if changed > 0 {
			mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
		}
		mock.ExpectCommit()
		require.NoError(t, repo.ReleaseCodexHunterModelHold(context.Background(), 7, "gpt-test", deadline))
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCodexHunterHoldReleaseOutboxFailureRollsBack(t *testing.T) {
	repo, mock := newCodexHunterHoldRepositoryTest(t)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()
	require.ErrorContains(t, repo.ReleaseCodexHunterModelHold(context.Background(), 7, "gpt-test", time.Now().Add(time.Hour)), "outbox unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}
