package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type shutdownHTTPServerStub struct {
	mu              sync.Mutex
	events          []string
	shutdown        func(context.Context) error
	close           func() error
	waitForServe    func(context.Context) error
	waitForHandlers func(context.Context) error
}

func (s *shutdownHTTPServerStub) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *shutdownHTTPServerStub) BeginShutdown() {
	s.record("begin")
}

func (s *shutdownHTTPServerStub) Shutdown(ctx context.Context) error {
	s.record("shutdown")
	if s.shutdown == nil {
		return nil
	}
	return s.shutdown(ctx)
}

func (s *shutdownHTTPServerStub) Close() error {
	s.record("close")
	if s.close == nil {
		return nil
	}
	return s.close()
}

func (s *shutdownHTTPServerStub) WaitForServe(ctx context.Context) error {
	s.record("wait-serve")
	if s.waitForServe == nil {
		return nil
	}
	return s.waitForServe(ctx)
}

func (s *shutdownHTTPServerStub) WaitForHandlers(ctx context.Context) error {
	s.record("wait-handlers")
	if s.waitForHandlers == nil {
		return nil
	}
	return s.waitForHandlers(ctx)
}

func (s *shutdownHTTPServerStub) recordedEvents() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func TestShutdownHTTPThenCleanupForcesCloseAndWaitsBeforeCleanup(t *testing.T) {
	server := &shutdownHTTPServerStub{
		shutdown: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	var cleanupCalls atomic.Int32
	cleanup := func() error {
		server.record("cleanup")
		cleanupCalls.Add(1)
		return nil
	}

	err := shutdownHTTPThenCleanup(server, cleanup, 10*time.Millisecond, time.Second)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, int32(1), cleanupCalls.Load())
	require.Equal(t,
		[]string{"begin", "shutdown", "close", "wait-serve", "wait-handlers", "cleanup"},
		server.recordedEvents(),
	)
}

func TestShutdownHTTPThenCleanupSkipsCleanupWhenServeLoopCannotBeConfirmedStopped(t *testing.T) {
	server := &shutdownHTTPServerStub{
		shutdown: func(context.Context) error { return context.DeadlineExceeded },
		waitForServe: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	var cleanupCalls atomic.Int32

	err := shutdownHTTPThenCleanup(server, func() error {
		cleanupCalls.Add(1)
		return nil
	}, 10*time.Millisecond, 20*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, cleanupCalls.Load(), "application dependencies must stay open while the serve loop may still be running")
	require.Equal(t,
		[]string{"begin", "shutdown", "close", "wait-serve", "wait-handlers"},
		server.recordedEvents(),
	)
}

func TestShutdownHTTPThenCleanupSkipsCleanupWhenHandlersCannotBeConfirmedStopped(t *testing.T) {
	server := &shutdownHTTPServerStub{
		shutdown: func(context.Context) error { return context.DeadlineExceeded },
		waitForHandlers: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	var cleanupCalls atomic.Int32

	err := shutdownHTTPThenCleanup(server, func() error {
		cleanupCalls.Add(1)
		return nil
	}, 10*time.Millisecond, 20*time.Millisecond)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Zero(t, cleanupCalls.Load(), "application dependencies must stay open while a handler may still be running")
	require.Equal(t,
		[]string{"begin", "shutdown", "close", "wait-serve", "wait-handlers"},
		server.recordedEvents(),
	)
}

func TestShutdownHTTPThenCleanupPropagatesForcedCloseErrorAfterConfirmedStop(t *testing.T) {
	closeErr := errors.New("forced close failed")
	server := &shutdownHTTPServerStub{
		shutdown: func(context.Context) error { return context.DeadlineExceeded },
		close:    func() error { return closeErr },
	}
	var cleanupCalls atomic.Int32

	err := shutdownHTTPThenCleanup(server, func() error {
		cleanupCalls.Add(1)
		return nil
	}, 10*time.Millisecond, time.Second)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, closeErr)
	require.Equal(t, int32(1), cleanupCalls.Load())
	require.Equal(t,
		[]string{"begin", "shutdown", "close", "wait-serve", "wait-handlers"},
		server.recordedEvents(),
	)
}

func TestShutdownHTTPThenCleanupPropagatesCleanupErrorAfterSafeHTTPStop(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	server := &shutdownHTTPServerStub{}

	err := shutdownHTTPThenCleanup(server, func() error { return cleanupErr }, time.Second, time.Second)

	require.ErrorIs(t, err, cleanupErr)
	require.Equal(t,
		[]string{"begin", "shutdown", "wait-serve", "wait-handlers"},
		server.recordedEvents(),
	)
}
