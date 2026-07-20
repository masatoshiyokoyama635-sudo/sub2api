package main

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedVersionMatchesRelease(t *testing.T) {
	require.Equal(t, "0.1.161", strings.TrimSpace(embeddedVersion))
}

func TestProvideServiceBuildInfo(t *testing.T) {
	in := handler.BuildInfo{
		Version:   "v-test",
		BuildType: "release",
	}
	out := provideServiceBuildInfo(in)
	require.Equal(t, in.Version, out.Version)
	require.Equal(t, in.BuildType, out.BuildType)
}

func newCleanupTestEntClient(t *testing.T) (*ent.Client, *sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	return ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db))), db, mock
}

func TestProvideCleanup_WithMinimalDependencies_NoPanic(t *testing.T) {
	cfg := &config.Config{}

	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)

	tokenRefreshSvc := service.NewTokenRefreshService(
		nil,
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		cfg,
		nil,
	)
	accountExpirySvc := service.NewAccountExpiryService(nil, time.Second)
	proxyExpirySvc := service.NewProxyExpiryService(nil, time.Second)
	subscriptionExpirySvc := service.NewSubscriptionExpiryService(nil, time.Second)
	pricingSvc := service.NewPricingService(cfg, nil)
	emailQueueSvc := service.NewEmailQueueService(nil, 1)
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	idempotencyCleanupSvc := service.NewIdempotencyCleanupService(nil, cfg)
	schedulerSnapshotSvc := service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg)
	opsSystemLogSinkSvc := service.NewOpsSystemLogSink(nil)

	cleanup := provideCleanup(
		nil, // entClient
		nil, // redis
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		opsSystemLogSinkSvc,
		nil, // opsService
		nil, // opsIngressRejectAggregator
		nil, // apiKeyService
		nil, // authCacheInvalidationWorker
		schedulerSnapshotSvc,
		tokenRefreshSvc,
		accountExpirySvc,
		proxyExpirySvc,
		subscriptionExpirySvc,
		&service.UsageCleanupService{},
		idempotencyCleanupSvc,
		&service.BatchImageCleanupService{},
		nil, // batchImageWorker
		pricingSvc,
		emailQueueSvc,
		billingCacheSvc,
		&service.UsageRecordWorkerPool{},
		&service.SubscriptionService{},
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil, // grokOAuth
		nil, // openAIGateway
		nil, // scheduledTestRunner
		nil, // backupSvc
		nil, // paymentOrderExpiry
		nil, // channelMonitorRunner
		nil, // quotaFlusher
		nil, // upstreamBillingProbe
		nil, // auditLog
		nil, // promptAudit
	)

	require.NotPanics(t, func() {
		cleanup()
	})
}

type cleanupFailingConfigStore struct {
	err        error
	startCalls *atomic.Int32
}

func (s cleanupFailingConfigStore) Start(context.Context) error {
	if s.startCalls != nil {
		s.startCalls.Add(1)
	}
	return nil
}
func (s cleanupFailingConfigStore) Shutdown(context.Context) error {
	return s.err
}
func (cleanupFailingConfigStore) Active() (securityaudit.ActiveConfig, bool) {
	return securityaudit.ActiveConfig{}, false
}
func (cleanupFailingConfigStore) EffectiveMode() securityaudit.Mode { return securityaudit.ModeOff }
func (cleanupFailingConfigStore) BlockingActivationDegraded() bool  { return false }
func (cleanupFailingConfigStore) Public() securityaudit.PublicConfig {
	return securityaudit.PublicConfig{}
}
func (cleanupFailingConfigStore) Save(context.Context, securityaudit.UpdateConfigRequest, int64) (securityaudit.PublicConfig, error) {
	return securityaudit.PublicConfig{}, nil
}
func (cleanupFailingConfigStore) RuntimeState() (int64, int64, *time.Time, string) {
	return 1, 0, nil, ""
}
func (cleanupFailingConfigStore) Encrypt(value string) (string, error) { return value, nil }
func (cleanupFailingConfigStore) Decrypt(value string) (string, error) { return value, nil }

func newCleanupWithInfrastructure(
	t *testing.T,
	entClient *ent.Client,
	rdb *redis.Client,
	promptAudit *securityaudit.PromptService,
) any {
	t.Helper()
	cfg := &config.Config{}
	oauthSvc := service.NewOAuthService(nil, nil)
	openAIOAuthSvc := service.NewOpenAIOAuthService(nil, nil)
	geminiOAuthSvc := service.NewGeminiOAuthService(nil, nil, nil, nil, cfg)
	antigravityOAuthSvc := service.NewAntigravityOAuthService(nil)
	tokenRefreshSvc := service.NewTokenRefreshService(
		nil, oauthSvc, openAIOAuthSvc, geminiOAuthSvc, antigravityOAuthSvc,
		nil, nil, cfg, nil,
	)

	return provideCleanup(
		entClient,
		rdb,
		&service.OpsMetricsCollector{},
		&service.OpsAggregationService{},
		&service.OpsAlertEvaluatorService{},
		&service.OpsCleanupService{},
		&service.OpsScheduledReportService{},
		service.NewOpsSystemLogSink(nil),
		nil,
		nil,
		nil,
		nil,
		service.NewSchedulerSnapshotService(nil, nil, nil, nil, cfg),
		tokenRefreshSvc,
		service.NewAccountExpiryService(nil, time.Second),
		service.NewProxyExpiryService(nil, time.Second),
		service.NewSubscriptionExpiryService(nil, time.Second),
		&service.UsageCleanupService{},
		service.NewIdempotencyCleanupService(nil, cfg),
		&service.BatchImageCleanupService{},
		nil,
		service.NewPricingService(cfg, nil),
		service.NewEmailQueueService(nil, 1),
		service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil),
		&service.UsageRecordWorkerPool{},
		&service.SubscriptionService{},
		oauthSvc,
		openAIOAuthSvc,
		geminiOAuthSvc,
		antigravityOAuthSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		promptAudit,
	)
}

func invokeCleanup(cleanup any) error {
	switch fn := cleanup.(type) {
	case func():
		fn()
		return nil
	case func() error:
		return fn()
	default:
		return errors.New("unexpected cleanup function type")
	}
}

func TestProvideCleanupDoesNotStartPartiallyInitializedPromptAudit(t *testing.T) {
	var startCalls atomic.Int32
	promptAudit := securityaudit.NewPromptService(
		cleanupFailingConfigStore{startCalls: &startCalls},
		securityaudit.NewPostgreSQLRepository(nil),
		securityaudit.NewRedisPayloadStore(nil),
		securityaudit.NewOpenAICompatibleScanner(),
		securityaudit.NewAtomicMetrics(),
	)
	cleanup := newCleanupWithInfrastructure(t, nil, nil, promptAudit)

	require.NoError(t, invokeCleanup(cleanup))
	require.Zero(t, startCalls.Load(), "cleanup must use idempotent Shutdown without starting a new Prompt Audit lifecycle")
}

func TestProvideCleanupPromptAuditFailurePreservesInfrastructureAndPropagatesError(t *testing.T) {
	tests := []struct {
		name      string
		promptErr error
	}{
		{name: "shutdown error", promptErr: errors.New("prompt audit shutdown failed")},
		{name: "shutdown timeout", promptErr: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			miniRedis := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			entClient, db, mock := newCleanupTestEntClient(t)
			t.Cleanup(func() { _ = db.Close() })
			promptAudit := securityaudit.NewPromptService(
				cleanupFailingConfigStore{err: tt.promptErr},
				securityaudit.NewPostgreSQLRepository(nil),
				securityaudit.NewRedisPayloadStore(nil),
				securityaudit.NewOpenAICompatibleScanner(),
				securityaudit.NewAtomicMetrics(),
			)
			require.NoError(t, promptAudit.Start(context.Background()))
			cleanup := newCleanupWithInfrastructure(t, entClient, rdb, promptAudit)

			assert.Equal(t, reflect.TypeOf((func() error)(nil)), reflect.TypeOf(cleanup), "cleanup must expose shutdown failure to its caller")
			err := invokeCleanup(cleanup)

			assert.ErrorIs(t, err, tt.promptErr)
			assert.NoError(t, rdb.Ping(context.Background()).Err(), "Redis must stay open when Prompt Audit did not stop")
			mock.ExpectPing()
			assert.NoError(t, db.PingContext(context.Background()), "Ent/PostgreSQL must stay open when Prompt Audit did not stop")
		})
	}
}
