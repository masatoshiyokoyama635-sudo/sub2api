package securityaudit

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBuildEventWhereEscapesKeywordWildcards(t *testing.T) {
	where, args := buildEventWhere(EventFilter{Keyword: `100%_done\\path`}, 1)
	if len(args) != 1 {
		t.Fatalf("expected one keyword argument, got %d", len(args))
	}
	if got, want := args[0], `%100\%\_done\\\\path%`; got != want {
		t.Fatalf("keyword pattern = %q, want %q", got, want)
	}
	if !strings.Contains(where, `ESCAPE E'\\'`) {
		t.Fatalf("keyword query must declare a literal escape character: %s", where)
	}
}

func TestPostgreSQLRepositoryJobStatusReadsCurrentState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewPostgreSQLRepository(db)
	mock.ExpectQuery(`SELECT status FROM prompt_audit_jobs WHERE id=\$1`).WithArgs(int64(17)).WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("queued"))

	status, err := repo.JobStatus(context.Background(), 17)
	if err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		t.Fatalf("status = %q, want queued", status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestShouldStorePromptAuditEvent(t *testing.T) {
	tests := []struct {
		name            string
		storePassEvents bool
		decision        EventDecision
		want            bool
	}{
		{name: "pass disabled", storePassEvents: false, decision: EventPass, want: false},
		{name: "flag disabled", storePassEvents: false, decision: EventFlag, want: true},
		{name: "critical disabled", storePassEvents: false, decision: EventCritical, want: true},
		{name: "pass enabled", storePassEvents: true, decision: EventPass, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStorePromptAuditEvent(tt.decision, tt.storePassEvents); got != tt.want {
				t.Fatalf("shouldStorePromptAuditEvent(%q, %t) = %t, want %t", tt.decision, tt.storePassEvents, got, tt.want)
			}
		})
	}
}
