//go:build unit

package server

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHTTPServerLifecycleNilAndTimeoutPaths(t *testing.T) {
	var nilServer *HTTPServer
	nilServer.BeginShutdown()
	require.NoError(t, nilServer.Shutdown(context.Background()))
	require.NoError(t, nilServer.Close())
	require.NoError(t, nilServer.WaitForServe(context.Background()))
	require.NoError(t, nilServer.WaitForHandlers(context.Background()))

	srv := &HTTPServer{serveDone: make(chan struct{}), hijacked: make(map[net.Conn]struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	require.ErrorIs(t, srv.WaitForServe(ctx), context.DeadlineExceeded)
}

func TestHTTPServerRejectsHandlersAfterShutdownBegins(t *testing.T) {
	srv := ProvideHTTPServer(ingressTestConfig(), gin.New())
	srv.BeginShutdown()
	recorder := httptest.NewRecorder()
	srv.serveHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestHTTPServerTracksHijackAfterShutdownBegins(t *testing.T) {
	srv := &HTTPServer{serveDone: make(chan struct{}), hijacked: make(map[net.Conn]struct{})}
	srv.BeginShutdown()
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	tracked := &trackedHijackedConn{Conn: serverConn, server: srv}
	srv.trackHijacked(tracked)
	_, err := clientConn.Write([]byte("x"))
	require.Error(t, err)
	require.Empty(t, srv.hijacked)
}

func TestHTTPServerWaitsForHijackedConnectionAfterHandlerReturns(t *testing.T) {
	srv := &HTTPServer{serveDone: make(chan struct{}), hijacked: make(map[net.Conn]struct{})}
	serverConn, clientConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	tracked := &trackedHijackedConn{Conn: serverConn, server: srv}
	srv.trackHijacked(tracked)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	require.ErrorIs(t, srv.WaitForHandlers(ctx), context.DeadlineExceeded)
	cancel()

	require.NoError(t, tracked.Close())
	finishedCtx, cancelFinished := context.WithTimeout(context.Background(), time.Second)
	defer cancelFinished()
	require.NoError(t, srv.WaitForHandlers(finishedCtx))
}

func TestTrackedResponseWriterReadFromFallback(t *testing.T) {
	base := httptest.NewRecorder()
	writer := &trackedResponseWriter{ResponseWriter: base}

	written, err := writer.ReadFrom(bytes.NewBufferString("fallback"))
	require.NoError(t, err)
	require.Equal(t, int64(len("fallback")), written)
	require.Equal(t, "fallback", base.Body.String())

}
