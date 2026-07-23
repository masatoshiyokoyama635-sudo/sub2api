// Package server provides HTTP server initialization and configuration.
package server

import (
	"bufio"
	"context"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/http2"
)

// ProviderSet 提供服务器层的依赖
var ProviderSet = wire.NewSet(
	ProvideRouter,
	ProvideHTTPServer,
)

// ProvideRouter 提供路由器
func ProvideRouter(
	cfg *config.Config,
	handlers *handler.Handlers,
	jwtAuth middleware2.JWTAuthMiddleware,
	adminAuth middleware2.AdminAuthMiddleware,
	apiKeyAuth middleware2.APIKeyAuthMiddleware,
	auditLog middleware2.AuditLogMiddleware,
	stepUpAuth middleware2.StepUpAuthMiddleware,
	strictStepUpAuth middleware2.StrictStepUpAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	compositeResolver *service.CompositeRouteResolver,
	redisClient *redis.Client,
) *gin.Engine {
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(middleware2.Recovery())
	configureTrustedProxies(r, cfg.Server)

	// Wire up websearch Manager builder so it initializes on startup and rebuilds on config save.
	settingService.SetWebSearchManagerBuilder(context.Background(), func(cfg *service.WebSearchEmulationConfig, proxyURLs map[int64]string) {
		if cfg == nil || !cfg.Enabled || len(cfg.Providers) == 0 {
			service.SetWebSearchManager(nil)
			return
		}
		configs := make([]websearch.ProviderConfig, 0, len(cfg.Providers))
		for _, p := range cfg.Providers {
			if p.APIKey == "" {
				continue
			}
			pc := websearch.ProviderConfig{
				Type:       p.Type,
				APIKey:     p.APIKey,
				QuotaLimit: derefInt64(p.QuotaLimit),
				ExpiresAt:  p.ExpiresAt,
			}
			if p.SubscribedAt != nil {
				pc.SubscribedAt = p.SubscribedAt
			}
			if p.ProxyID != nil {
				pc.ProxyID = *p.ProxyID
				if u, ok := proxyURLs[*p.ProxyID]; ok {
					pc.ProxyURL = u
				} else {
					// Proxy configured but not found — skip this provider to prevent direct connection.
					slog.Warn("websearch: proxy not found for provider, skipping",
						"provider", p.Type, "proxy_id", *p.ProxyID)
					continue
				}
			}
			configs = append(configs, pc)
		}
		service.SetWebSearchManager(websearch.NewManager(configs, redisClient))
	})

	return SetupRouter(r, handlers, jwtAuth, adminAuth, apiKeyAuth, auditLog, stepUpAuth, strictStepUpAuth, apiKeyService, subscriptionService, opsService, settingService, compositeResolver, cfg, redisClient)
}

// HTTPServer wraps net/http.Server with lifecycle tracking. Shutdown does not
// wait for hijacked connections, so handlers and hijacked connections are
// tracked separately before infrastructure dependencies are released.
func closedHandlerChannel() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

type HTTPServer struct {
	*http.Server

	lifecycleMu  sync.Mutex
	shuttingDown bool
	handlerCount int
	handlersDone chan struct{}
	serveDone    chan struct{}
	serveOnce    sync.Once
	handler      http.Handler
	hijacked     map[net.Conn]struct{}
}

// BeginShutdown rejects new handlers before the underlying server starts its
// graceful drain.
func (s *HTTPServer) BeginShutdown() {
	if s == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.shuttingDown = true
	s.lifecycleMu.Unlock()
}

// Shutdown gracefully drains net/http connections, then closes hijacked
// connections (including WebSockets) that net/http intentionally ignores.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.BeginShutdown()
	shutdownErr := s.Server.Shutdown(ctx)
	if shutdownErr == nil {
		s.closeHijackedConnections()
	}
	return shutdownErr
}

// Close force-closes net/http and hijacked connections.
func (s *HTTPServer) Close() error {
	if s == nil {
		return nil
	}
	s.BeginShutdown()
	closeErr := s.Server.Close()
	s.closeHijackedConnections()
	return closeErr
}

func (s *HTTPServer) closeHijackedConnections() {
	s.lifecycleMu.Lock()
	connections := make([]net.Conn, 0, len(s.hijacked))
	for conn := range s.hijacked {
		connections = append(connections, conn)
	}
	s.lifecycleMu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

// ListenAndServe starts the HTTP server and records completion of its serve loop.
func (s *HTTPServer) ListenAndServe() error {
	return s.finishServe(s.Server.ListenAndServe())
}

// Serve starts the HTTP server on listener and records completion of its serve loop.
func (s *HTTPServer) Serve(listener net.Listener) error {
	return s.finishServe(s.Server.Serve(listener))
}

func (s *HTTPServer) finishServe(err error) error {
	s.serveOnce.Do(func() { close(s.serveDone) })
	return err
}

// WaitForServe confirms that the accept/serve loop has exited.
func (s *HTTPServer) WaitForServe(ctx context.Context) error {
	if s == nil {
		return nil
	}
	select {
	case <-s.serveDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitForHandlers confirms all accepted HTTP and hijacked handlers have returned.
func (s *HTTPServer) WaitForHandlers(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.lifecycleMu.Lock()
	done := s.handlersDone
	s.lifecycleMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *HTTPServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.beginHandler() {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	defer s.handlerDone()
	s.handler.ServeHTTP(&trackedResponseWriter{ResponseWriter: w, server: s}, r)
}

func (s *HTTPServer) beginHandler() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.shuttingDown {
		return false
	}
	s.handlerCount++
	if s.handlerCount == 1 {
		s.handlersDone = make(chan struct{})
	}
	return true
}

func (s *HTTPServer) handlerDone() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.handlerCount == 0 {
		return
	}
	s.handlerCount--
	if s.handlerCount == 0 {
		close(s.handlersDone)
	}
}

func (s *HTTPServer) trackHijacked(conn net.Conn) {
	if conn == nil {
		return
	}
	s.lifecycleMu.Lock()
	s.hijacked[conn] = struct{}{}
	// The request handler is still counted while Hijack is called, so adding
	// this connection before it returns cannot race a zero-count wait.
	s.handlerCount++
	if s.handlerCount == 1 {
		s.handlersDone = make(chan struct{})
	}
	shuttingDown := s.shuttingDown
	s.lifecycleMu.Unlock()
	if shuttingDown {
		_ = conn.Close()
	}
}

func (s *HTTPServer) untrackHijacked(conn net.Conn) {
	s.lifecycleMu.Lock()
	if _, ok := s.hijacked[conn]; ok {
		delete(s.hijacked, conn)
		s.handlerCount--
		if s.handlerCount == 0 {
			close(s.handlersDone)
		}
	}
	s.lifecycleMu.Unlock()
}

type trackedResponseWriter struct {
	http.ResponseWriter
	server *HTTPServer
}

func (w *trackedResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *trackedResponseWriter) CloseNotify() <-chan bool {
	//nolint:staticcheck // Preserve the optional legacy interface expected by gin's ResponseWriter wrapper.
	if notifier, ok := w.ResponseWriter.(http.CloseNotifier); ok {
		return notifier.CloseNotify()
	}
	closed := make(chan bool)
	return closed
}

func (w *trackedResponseWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (w *trackedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *trackedResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(struct{ io.Writer }{w.ResponseWriter}, reader)
}

func (w *trackedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}
	tracked := &trackedHijackedConn{Conn: conn, server: w.server}
	w.server.trackHijacked(tracked)
	return tracked, rw, nil
}

type trackedHijackedConn struct {
	net.Conn
	server *HTTPServer
	once   sync.Once
}

func (c *trackedHijackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.server.untrackHijacked(c) })
	return err
}

func configureTrustedProxies(r *gin.Engine, cfg config.ServerConfig) {
	if cfg.TrustedProxiesConfigured {
		if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
			log.Printf("Failed to set trusted proxies: %v", err)
			_ = r.SetTrustedProxies(nil)
		}
		if len(cfg.TrustedProxies) == 0 && cfg.Mode == "release" {
			log.Printf("Warning: server.trusted_proxies is explicitly empty; forwarded client IP trust is disabled")
		}
	} else {
		if err := r.SetTrustedProxies(nil); err != nil {
			log.Printf("Failed to disable trusted proxies: %v", err)
		}
		if cfg.Mode == "release" {
			log.Printf("Warning: server.trusted_proxies is not configured; disabling the forwarded-IP compatibility switch will use direct peer addresses only")
		}
	}
}

// ProvideHTTPServer 提供 HTTP 服务器
func ProvideHTTPServer(cfg *config.Config, router *gin.Engine) *HTTPServer {
	httpHandler := http.Handler(router)
	standardServer := &http.Server{
		Addr:           cfg.Server.Address(),
		Handler:        httpHandler,
		MaxHeaderBytes: cfg.Server.MaxHeaderBytes,
		// ReadHeaderTimeout: 读取请求头的超时时间，防止慢速请求头攻击
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeout) * time.Second,
		// IdleTimeout: 空闲连接超时时间，释放不活跃的连接资源
		IdleTimeout: time.Duration(cfg.Server.IdleTimeout) * time.Second,
		// 注意：不设置 WriteTimeout，因为流式响应可能持续十几分钟
		// 不设置 ReadTimeout，因为大请求体可能需要较长时间读取
	}

	globalMaxSize := cfg.Server.MaxRequestBodySize
	if globalMaxSize <= 0 {
		globalMaxSize = cfg.Gateway.MaxBodySize
	}
	if globalMaxSize > 0 {
		httpHandler = http.MaxBytesHandler(httpHandler, globalMaxSize)
		log.Printf("Global max request body size: %d bytes (%.2f MB)", globalMaxSize, float64(globalMaxSize)/(1<<20))
	}

	// 根据配置决定是否启用 H2C
	if cfg.Server.H2C.Enabled {
		h2cConfig := cfg.Server.H2C
		if err := http2.ConfigureServer(standardServer, &http2.Server{
			MaxConcurrentStreams:         h2cConfig.MaxConcurrentStreams,
			IdleTimeout:                  time.Duration(h2cConfig.IdleTimeout) * time.Second,
			MaxReadFrameSize:             uint32(h2cConfig.MaxReadFrameSize),
			MaxUploadBufferPerConnection: int32(h2cConfig.MaxUploadBufferPerConnection),
			MaxUploadBufferPerStream:     int32(h2cConfig.MaxUploadBufferPerStream),
		}); err != nil {
			log.Printf("Failed to configure HTTP/2 Cleartext (h2c): %v", err)
		} else {
			protocols := new(http.Protocols)
			protocols.SetHTTP1(true)
			protocols.SetUnencryptedHTTP2(true)
			standardServer.Protocols = protocols
			log.Printf("HTTP/2 Cleartext (h2c) enabled: max_concurrent_streams=%d, idle_timeout=%ds, max_read_frame_size=%d, max_upload_buffer_per_connection=%d, max_upload_buffer_per_stream=%d",
				h2cConfig.MaxConcurrentStreams,
				h2cConfig.IdleTimeout,
				h2cConfig.MaxReadFrameSize,
				h2cConfig.MaxUploadBufferPerConnection,
				h2cConfig.MaxUploadBufferPerStream,
			)
		}
	}

	managedServer := &HTTPServer{
		Server:       standardServer,
		handlersDone: closedHandlerChannel(),
		serveDone:    make(chan struct{}),
		handler:      httpHandler,
		hijacked:     make(map[net.Conn]struct{}),
	}
	standardServer.Handler = http.HandlerFunc(managedServer.serveHTTP)
	return managedServer
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
