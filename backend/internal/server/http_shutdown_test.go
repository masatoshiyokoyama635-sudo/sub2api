//go:build unit

package server

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHTTPServerForcedCloseTerminatesActiveHandler(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	r := gin.New()
	r.GET("/active", func(c *gin.Context) {
		close(started)
		<-c.Request.Context().Done()
		close(stopped)
	})
	srv, addr := startHTTPShutdownTestServer(t, r)

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, err := http.Get("http://" + addr + "/active")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	awaitShutdownSignal(t, started)

	srv.BeginShutdown()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShutdown()
	require.ErrorIs(t, srv.Shutdown(shutdownCtx), context.DeadlineExceeded)
	require.NoError(t, srv.Close())

	waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	require.NoError(t, srv.WaitForServe(waitCtx))
	require.NoError(t, srv.WaitForHandlers(waitCtx))
	awaitShutdownSignal(t, stopped)
	awaitShutdownSignal(t, requestDone)
}

func TestHTTPServerWaitForHandlersFailsWhileStubbornHandlerRuns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	r := gin.New()
	r.GET("/stubborn", func(c *gin.Context) {
		close(started)
		<-release
		c.Status(http.StatusNoContent)
	})
	srv, addr := startHTTPShutdownTestServer(t, r)

	go func() {
		resp, err := http.Get("http://" + addr + "/stubborn")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	awaitShutdownSignal(t, started)

	srv.BeginShutdown()
	require.NoError(t, srv.Close())
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 20*time.Millisecond)
	require.ErrorIs(t, srv.WaitForHandlers(waitCtx), context.DeadlineExceeded)
	cancelWait()

	close(release)
	finishedCtx, cancelFinished := context.WithTimeout(context.Background(), time.Second)
	defer cancelFinished()
	require.NoError(t, srv.WaitForHandlers(finishedCtx))
}

func TestHTTPServerBeginShutdownClosesHijackedHandler(t *testing.T) {
	hijacked := make(chan struct{})
	handlerDone := make(chan struct{})
	r := gin.New()
	r.GET("/hijack", func(c *gin.Context) {
		conn, rw, err := c.Writer.Hijack()
		if err != nil {
			return
		}
		defer close(handlerDone)
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		_ = rw.Flush()
		close(hijacked)
		_, _ = io.Copy(io.Discard, conn)
	})
	srv, addr := startHTTPShutdownTestServer(t, r)

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = io.WriteString(conn, "GET /hijack HTTP/1.1\r\nHost: test\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	awaitShutdownSignal(t, hijacked)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	require.NoError(t, srv.Shutdown(shutdownCtx))
	require.NoError(t, srv.WaitForServe(shutdownCtx))
	require.NoError(t, srv.WaitForHandlers(shutdownCtx))
	awaitShutdownSignal(t, handlerDone)
}

func TestHTTPServerListenAndServeRecordsServeCompletion(t *testing.T) {
	srv := ProvideHTTPServer(ingressTestConfig(), gin.New())
	srv.Addr = "127.0.0.1:-1"
	err := srv.ListenAndServe()
	require.Error(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, srv.WaitForServe(ctx))
}

type responseCapabilityRecorder struct {
	*httptest.ResponseRecorder
	closed  chan bool
	flushed bool
	pushed  string
}

func (w *responseCapabilityRecorder) Flush()                   { w.flushed = true }
func (w *responseCapabilityRecorder) CloseNotify() <-chan bool { return w.closed }
func (w *responseCapabilityRecorder) Push(target string, _ *http.PushOptions) error {
	w.pushed = target
	return nil
}
func (w *responseCapabilityRecorder) ReadFrom(reader io.Reader) (int64, error) {
	return io.Copy(w.ResponseRecorder, reader)
}

func TestTrackedResponseWriterPreservesResponseCapabilities(t *testing.T) {
	base := &responseCapabilityRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closed:           make(chan bool, 1),
	}
	writer := &trackedResponseWriter{ResponseWriter: base}

	writer.Flush()
	require.True(t, base.flushed)
	require.Equal(t, (<-chan bool)(base.closed), writer.CloseNotify())
	require.NoError(t, writer.Push("/asset", nil))
	require.Equal(t, "/asset", base.pushed)
	require.Same(t, base, writer.Unwrap())

	written, err := writer.ReadFrom(bytes.NewBufferString("stream"))
	require.NoError(t, err)
	require.Equal(t, int64(len("stream")), written)
	require.Equal(t, "stream", base.Body.String())
}

func TestTrackedResponseWriterUnsupportedCapabilitiesFailSafely(t *testing.T) {
	base := httptest.NewRecorder()
	writer := &trackedResponseWriter{ResponseWriter: base}

	require.ErrorIs(t, writer.Push("/asset", nil), http.ErrNotSupported)
	_, _, err := writer.Hijack()
	require.ErrorIs(t, err, http.ErrNotSupported)
	require.NotNil(t, writer.CloseNotify())
}

func startHTTPShutdownTestServer(t *testing.T, router *gin.Engine) (*HTTPServer, string) {
	t.Helper()
	srv := ProvideHTTPServer(ingressTestConfig(), router)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.WaitForServe(ctx)
		_ = srv.WaitForHandlers(ctx)
	})
	return srv, ln.Addr().String()
}

func awaitShutdownSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for shutdown signal")
	}
}
