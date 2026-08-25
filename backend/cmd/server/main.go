package main

//go:generate go run github.com/google/wire/cmd/wire

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/Wei-Shaw/sub2api/internal/web"

	"github.com/gin-gonic/gin"
)

//go:embed VERSION
var embeddedVersion string

// Build-time variables (can be set by ldflags)
var (
	Version   = ""
	Commit    = "unknown"
	Date      = "unknown"
	BuildType = "source" // "source" for manual builds, "release" for CI builds (set by ldflags)
)

func init() {
	// 如果 Version 已通过 ldflags 注入（例如 -X main.Version=...），则不要覆盖。
	if strings.TrimSpace(Version) != "" {
		return
	}

	// 默认从 embedded VERSION 文件读取版本号（编译期打包进二进制）。
	Version = strings.TrimSpace(embeddedVersion)
	if Version == "" {
		Version = "0.0.0-dev"
	}
}

// initLogger configures the default slog handler based on gin.Mode().
// In non-release mode, Debug level logs are enabled.
func main() {
	logger.InitBootstrap()
	defer logger.Sync()

	// Parse command line flags
	setupMode := flag.Bool("setup", false, "Run setup wizard in CLI mode")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		log.Printf("Sub2API %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return
	}

	// CLI setup mode
	if *setupMode {
		if err := setup.RunCLI(); err != nil {
			log.Fatalf("Setup failed: %v", err)
		}
		return
	}

	// Check if setup is needed
	if setup.NeedsSetup() {
		// Check if auto-setup is enabled (for Docker deployment)
		if setup.AutoSetupEnabled() {
			log.Println("Auto setup mode enabled...")
			if err := setup.AutoSetupFromEnv(); err != nil {
				log.Fatalf("Auto setup failed: %v", err)
			}
			// Continue to main server after auto-setup
		} else {
			log.Println("First run detected, starting setup wizard...")
			runSetupServer()
			return
		}
	}

	// Normal server mode
	if err := runMainServer(); err != nil {
		log.Printf("Server exited with error: %v", err)
	}
}

func runSetupServer() {
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS(config.CORSConfig{}))
	r.Use(middleware.SecurityHeaders(config.CSPConfig{Enabled: true, Policy: config.DefaultCSPPolicy}, nil))

	// Register setup routes
	setup.RegisterRoutes(r)

	// Serve embedded frontend if available
	if web.HasEmbeddedFrontend() {
		r.Use(web.ServeEmbeddedFrontend())
	}

	// Get server address from config.yaml or environment variables (SERVER_HOST, SERVER_PORT)
	// This allows users to run setup on a different address if needed
	addr := config.GetServerAddress()
	log.Printf("Setup wizard available at http://%s", addr)
	log.Println("Complete the setup wizard to configure Sub2API")

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
		Protocols:         protocols,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Failed to start setup server: %v", err)
	}
}

const (
	httpGracefulShutdownTimeout = 5 * time.Second
	httpForcedShutdownTimeout   = 5 * time.Second
)

type shutdownHTTPServer interface {
	BeginShutdown()
	Shutdown(context.Context) error
	Close() error
	WaitForServe(context.Context) error
	WaitForHandlers(context.Context) error
}

func shutdownHTTPThenCleanup(
	server shutdownHTTPServer,
	cleanup func() error,
	gracefulTimeout time.Duration,
	forcedTimeout time.Duration,
) error {
	if server == nil {
		return errors.New("HTTP server unavailable during shutdown")
	}

	server.BeginShutdown()
	gracefulCtx, cancelGraceful := context.WithTimeout(context.Background(), gracefulTimeout)
	shutdownErr := server.Shutdown(gracefulCtx)
	cancelGraceful()

	var closeErr error
	if shutdownErr != nil {
		closeErr = server.Close()
	}

	forcedCtx, cancelForced := context.WithTimeout(context.Background(), forcedTimeout)
	serveErr := server.WaitForServe(forcedCtx)
	handlerErr := server.WaitForHandlers(forcedCtx)
	cancelForced()

	httpErr := errors.Join(shutdownErr, closeErr, serveErr, handlerErr)
	if serveErr != nil || handlerErr != nil {
		return httpErr
	}
	if cleanup == nil {
		return httpErr
	}
	return errors.Join(httpErr, cleanup())
}

func runMainServer() error {
	cfg, err := config.LoadForBootstrap()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := logger.Init(logger.OptionsFromConfig(cfg.Log)); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	if cfg.RunMode == config.RunModeSimple {
		log.Println("⚠️  WARNING: Running in SIMPLE mode - billing and quota checks are DISABLED")
	}

	buildInfo := handler.BuildInfo{
		Version:   Version,
		BuildType: BuildType,
	}

	app, err := initializeApplication(buildInfo)
	if err != nil {
		return fmt.Errorf("failed to initialize application: %w", err)
	}
	if app.PluginManager != nil {
		if err := app.PluginManager.Start(context.Background()); err != nil {
			log.Printf("Plugin manager started in degraded state: %v", err)
		}
	}
	if app.PromptAudit != nil {
		if err := app.PromptAudit.Start(context.Background()); err != nil {
			// Startup continues so unrelated APIs stay up. Fail-closed (unavailable)
			// applies only when a persisted blocking policy was observed; without
			// blocking intent, Prompt Audit stays ModeOff so the gateway remains
			// usable and administrators can still disable the feature (#4560).
			log.Printf("Prompt Audit started in degraded state: %v", err)
		}
	}

	serverErr := make(chan error, 1)
	go func() {
		err := app.Server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
	}()

	log.Printf("Server started on %s", app.Server.Addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	var serveErr error
	select {
	case <-quit:
		log.Println("Shutting down server...")
	case serveErr = <-serverErr:
		if serveErr != nil {
			serveErr = fmt.Errorf("failed to start server: %w", serveErr)
		}
		log.Println("HTTP server stopped; shutting down application...")
	}

	shutdownErr := shutdownHTTPThenCleanup(
		app.Server,
		app.Cleanup,
		httpGracefulShutdownTimeout,
		httpForcedShutdownTimeout,
	)
	if shutdownErr != nil {
		return errors.Join(serveErr, fmt.Errorf("server shutdown failed: %w", shutdownErr))
	}

	log.Println("Server exited")
	return serveErr
}
