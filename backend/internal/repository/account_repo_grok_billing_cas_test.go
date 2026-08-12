package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUpdateGrokBillingSnapshotIfIdentityUnchanged(t *testing.T) {
	for _, tt := range []struct {
		name     string
		affected int64
		wantErr  error
	}{
		{name: "success", affected: 1},
		{name: "identity changed", affected: 0, wantErr: service.ErrGrokBillingProbeIdentityChanged},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })

			mock.ExpectExec(regexp.QuoteMeta("UPDATE accounts")).
				WithArgs(sqlmock.AnyArg(), int64(11), service.PlatformGrok, service.AccountTypeOAuth, `{"access_token":"access"}`, nil).
				WillReturnResult(sqlmock.NewResult(0, tt.affected))

			repo := newAccountRepositoryWithSQL(client, db, nil)
			got, err := repo.UpdateGrokBillingSnapshotIfIdentityUnchanged(context.Background(), 11, service.GrokBillingProbeIdentity{
				CredentialsJSON: `{"access_token":"access"}`,
			}, &xai.BillingSummary{StatusCode: 200})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.False(t, got)
			} else {
				require.NoError(t, err)
				require.True(t, got)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
