//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokPartialBillingUsers struct {
	service.UserRepository
	mu     sync.Mutex
	debits []float64
}

func (r *grokPartialBillingUsers) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id, Balance: 100, Status: service.StatusActive}, nil
}

func (r *grokPartialBillingUsers) DeductBalance(_ context.Context, _ int64, cost float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.debits = append(r.debits, cost)
	return nil
}

// Exercise the shared handler's actual usage writer and balance deduction.
// The companion relay test verifies that its TTS count contains only text
// successfully written upstream before a later malformed client frame.
func TestGrokTTSStreamPartialUsageSettlesWithoutChangingRealtimeFailurePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint string
		result         *service.OpenAIForwardResult
		proxyErr       error
		wantCost       float64
	}{
		{"tts_delivered_then_malformed_event", "tts_stream", grokTTSStreamBillingResult(8, time.Second), errors.New("invalid tts stream event"), 0.000016},
		{"tts_no_delivered_text", "tts_stream", grokTTSStreamBillingResult(0, time.Second), errors.New("invalid tts stream event"), 0},
		{"tts_normal_close", "tts_stream", grokTTSStreamBillingResult(8, time.Second), nil, 0.000016},
		{"realtime_policy_failure", "realtime", grokRealtimeBillingResult("grok-voice-latest", time.Minute, true), coderws.CloseError{Code: coderws.StatusPolicyViolation}, 0},
		{"realtime_normal_close", "realtime", grokRealtimeBillingResult("grok-voice-latest", time.Minute, true), nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			users, logs := &grokPartialBillingUsers{}, &cachedBillingUsageRepo{}
			groupID := int64(24)
			repo := &openAIWSUsageHandlerAccountRepoStub{account: service.Account{
				ID: 3, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 5,
				GroupIDs: []int64{groupID}, Credentials: map[string]any{"api_key": "fixture"},
			}}
			billing := service.NewBillingCacheService(nil, users, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			gateway := service.NewOpenAIGatewayService(repo, nil, logs, nil, users, nil, nil, nil,
				cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, nil,
				&service.DeferredService{}, nil, service.NewGrokTokenProvider(repo, nil), nil, nil, nil, nil, nil)
			t.Cleanup(gateway.StopOpenAICodexTicketHarvester)
			cache := &concurrencyCacheMock{
				acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
			}
			h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billing,
				service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			ttsPrice, realtimePrice := 2.0, 1.0
			key := &service.APIKey{ID: 2, GroupID: &groupID, User: &service.User{ID: 1, Balance: 100, Status: service.StatusActive},
				Group: &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformGrok, Status: service.StatusActive,
					RateMultiplier: 1, AudioTTSPricePerMillionChars: &ttsPrice, AudioRealtimePricePerMin: &realtimePrice}}
			done := make(chan struct{})
			router := gin.New()
			router.GET("/voice", func(c *gin.Context) {
				defer close(done)
				c.Set(string(middleware2.ContextKeyAPIKey), key)
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 1, Concurrency: 5})
				h.serveGrokVoiceWebSocket(c, grokVoiceWebSocket{
					name: tc.endpoint, apiName: "fixture voice",
					open: func(context.Context, *service.Account, string) (*service.GrokRealtimeUpstream, error) {
						return &service.GrokRealtimeUpstream{}, nil
					},
					proxy: func(context.Context, *coderws.Conn, *service.GrokRealtimeUpstream) (*service.OpenAIForwardResult, error) {
						return tc.result, tc.proxyErr
					},
				})
			})
			server := httptest.NewServer(router)
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/voice", nil)
			require.NoError(t, err)
			defer func() { _ = client.CloseNow() }()
			_, _, _ = client.Read(ctx)
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("voice handler did not finish")
			}
			users.mu.Lock()
			defer users.mu.Unlock()
			logs.mu.Lock()
			defer logs.mu.Unlock()
			if tc.wantCost == 0 {
				require.Empty(t, users.debits)
				require.Empty(t, logs.logs)
				return
			}
			require.Len(t, users.debits, 1, "delivered TTS text must be charged once even if a later frame fails")
			require.InDelta(t, tc.wantCost, users.debits[0], 1e-12)
			require.Len(t, logs.logs, 1)
			require.InDelta(t, tc.wantCost, logs.logs[0].ActualCost, 1e-12)
		})
	}
}
