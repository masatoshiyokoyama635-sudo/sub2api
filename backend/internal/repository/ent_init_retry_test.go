package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func transientDatabaseError(code string) error {
	return &pq.Error{Code: pq.ErrorCode(code), Message: "database is temporarily unavailable"}
}

func TestIsTransientDatabaseInitializationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "postgres is starting", err: transientDatabaseError("57P03"), want: true},
		{name: "connection failure", err: fmt.Errorf("wrapped: %w", transientDatabaseError("08006")), want: true},
		{name: "bad pooled connection", err: fmt.Errorf("wrapped: %w", driver.ErrBadConn), want: true},
		{name: "connection eof", err: io.EOF, want: true},
		{name: "truncated connection", err: io.ErrUnexpectedEOF, want: true},
		{
			name: "connection refused before postgres accepts clients",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
			},
			want: true,
		},
		{name: "connection reset", err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET}, want: true},
		{name: "broken pipe", err: &os.SyscallError{Syscall: "write", Err: syscall.EPIPE}, want: true},
		{name: "host unreachable", err: &os.SyscallError{Syscall: "connect", Err: syscall.EHOSTUNREACH}, want: true},
		{name: "network timeout", err: &net.DNSError{Err: "i/o timeout", Name: "db", IsTimeout: true}, want: true},
		{name: "temporary dns failure", err: &net.DNSError{Err: "temporary failure", Name: "db", IsTemporary: true}, want: true},
		{name: "permanent dns failure", err: &net.DNSError{Err: "no such host", Name: "invalid", IsNotFound: true}, want: false},
		{name: "invalid address", err: &net.AddrError{Err: "missing port", Addr: "db"}, want: false},
		{name: "canceled startup", err: fmt.Errorf("wrapped: %w", context.Canceled), want: false},
		{name: "expired startup", err: context.DeadlineExceeded, want: false},
		{name: "authentication failure", err: transientDatabaseError("28P01"), want: false},
		{name: "migration error", err: errors.New("migration checksum mismatch"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isTransientDatabaseInitializationError(tt.err))
		})
	}
}

func TestInitializeDatabaseWithRetryEventuallySucceeds(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		if attempts <= 3 {
			return transientDatabaseError("57P03")
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 4, attempts)
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, delays)
}

func TestInitializeDatabaseWithRetryFailsFastForPermanentError(t *testing.T) {
	attempts := 0
	waitCalled := false
	permanentErr := transientDatabaseError("28P01")
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		return permanentErr
	}, func(_ context.Context, _ time.Duration) error {
		waitCalled = true
		return nil
	})

	require.ErrorIs(t, err, permanentErr)
	require.Equal(t, 1, attempts)
	require.False(t, waitCalled)
}

func TestInitializeDatabaseWithRetryStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	err := initializeDatabaseWithRetryWithWait(ctx, func(context.Context) error {
		attempts++
		return transientDatabaseError("57P03")
	}, func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}

func TestInitializeDatabaseWithRetryReturnsLastErrorAfterLimit(t *testing.T) {
	attempts := 0
	lastErr := transientDatabaseError("08006")
	var delays []time.Duration
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		return lastErr
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})

	require.ErrorIs(t, err, lastErr)
	require.Equal(t, maxDatabaseInitializationRetries+1, attempts)
	require.Len(t, delays, maxDatabaseInitializationRetries)
	require.Equal(t, databaseInitializationRetryMax, delays[len(delays)-1])
}

func TestInitializeDatabaseWithRetryAllowsConnectionRecovery(t *testing.T) {
	attempts := 0
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		if attempts == 1 {
			return transientDatabaseError("57P03")
		}
		return nil
	}, func(_ context.Context, _ time.Duration) error { return nil })

	require.NoError(t, err)
	require.Equal(t, 2, attempts)
}
