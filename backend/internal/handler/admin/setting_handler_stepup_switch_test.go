package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type riskControlStepUpUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *riskControlStepUpUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

func (r *riskControlStepUpUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

type riskControlStepUpCache struct {
	service.TotpCache
	granted bool
	calls   int
	onCall  func()
}

func (c *riskControlStepUpCache) HasStepUpGrant(context.Context, int64, string) (bool, error) {
	c.calls++
	if c.onCall != nil {
		c.onCall()
	}
	return c.granted, nil
}

// step-up 开关转换的门控测试。
// 测试环境不注入认证上下文/userService，因此一旦触发校验会以 401/403/500 中止；
// 借此区分「触发了转换校验」与「直接放行到常规保存（200）」。

func newStepUpSwitchTestHandler(t *testing.T, stored map[string]string) (*SettingHandler, *settingHandlerRepoStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: stored}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

func doUpdateSettings(t *testing.T, h *SettingHandler, body map[string]any, prepare func(c *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	if prepare != nil {
		prepare(c)
	}

	h.UpdateSettings(c)
	return rec
}

// 开启开关（false→true）：无认证上下文时拒绝，且带专用错误标记。
func TestUpdateSettingsEnableStepUpRejectsWithoutSession(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ENABLE_REQUIRES_TOTP")
	require.NotEqual(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 开启开关：admin API key（机器凭证）一律拒绝，reason 与门控保持一致便于前端分流。
func TestUpdateSettingsEnableStepUpRejectsAdminAPIKey(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
}

// 开启开关：有认证会话但 userService 未注入时 fail-closed（500），不得放行。
func TestUpdateSettingsEnableStepUpFailsClosedWithoutUserService(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotEqual(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 关闭开关（true→false）本身是敏感操作：无认证上下文时被 step-up 门控以 401 拦截。
func TestUpdateSettingsDisableStepUpRequiresStepUp(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 关闭开关：admin API key 被 step-up 门控以 403 拦截。
func TestUpdateSettingsDisableStepUpRejectsAdminAPIKey(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 无状态转换（false→false）：不触发任何转换校验，常规保存成功且默认持久化为 false。
func TestUpdateSettingsStepUpNoTransitionSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	// 会话 IP/UA 绑定默认关闭：未显式提交时持久化 false。
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

// 保持开启（true→true）：不触发转换校验，常规保存不被打断。
func TestUpdateSettingsStepUpKeepEnabledSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 省略字段=保持现值：不含 step_up_enabled/session_binding_enabled 的旧客户端全量保存
// 不得把已开启的安全开关静默重置，也不触发任何转换门控。
func TestUpdateSettingsOmittedSecuritySwitchesKeepStoredValues(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled:         "true",
		service.SettingKeySessionBindingEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeySessionBindingEnabled])
}

// 省略字段在开关本就关闭时同样保持关闭（默认值路径）。
func TestUpdateSettingsOmittedSecuritySwitchesKeepDisabled(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

func TestUpdateSettingsRiskControlNonDowngradeSkipsStrictGate(t *testing.T) {
	tests := []struct {
		name   string
		stored map[string]string
		body   map[string]any
		want   string
	}{
		{name: "omitted while enabled", stored: map[string]string{service.SettingKeyRiskControlEnabled: "true"}, body: map[string]any{"registration_enabled": true}, want: "true"},
		{name: "enable", stored: map[string]string{}, body: map[string]any{"risk_control_enabled": true}, want: "true"},
		{name: "keep enabled", stored: map[string]string{service.SettingKeyRiskControlEnabled: "true"}, body: map[string]any{"risk_control_enabled": true}, want: "true"},
		{name: "keep disabled", stored: map[string]string{}, body: map[string]any{"risk_control_enabled": false}, want: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, tt.stored)

			rec := doUpdateSettings(t, h, tt.body, nil)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tt.want, repo.values[service.SettingKeyRiskControlEnabled])
		})
	}
}

func TestUpdateSettingsDisableStepUpAndRiskControlUsesSingleStrictGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled:      "true",
		service.SettingKeyRiskControlEnabled: "true",
	})
	cache := &riskControlStepUpCache{granted: true}
	userRepo := &riskControlStepUpUserRepo{user: &service.User{ID: 1, TotpEnabled: true}}
	h.SetStepUpDeps(
		service.NewTotpService(nil, nil, cache, nil, nil, nil),
		service.NewUserService(userRepo, nil, nil, nil),
	)

	rec := doUpdateSettings(t, h, map[string]any{
		"step_up_enabled":      false,
		"risk_control_enabled": false,
	}, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set(middleware.ContextKeySessionID, "settings-session")
	})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, cache.calls)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeyRiskControlEnabled])
}

func TestUpdateSettingsSecurityBaselineChangeReturnsStableConflictWithoutStrictGrant(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyRiskControlEnabled: "true",
	})
	cache := &riskControlStepUpCache{granted: true}
	repo.beforeAtomic = func(repo *settingHandlerRepoStub) {
		repo.values[service.SettingKeyRiskControlEnabled] = "false"
	}
	h.SetStepUpDeps(
		service.NewTotpService(nil, nil, cache, nil, nil, nil),
		service.NewUserService(&riskControlStepUpUserRepo{user: &service.User{ID: 1, TotpEnabled: true}}, nil, nil, nil),
	)

	rec := doUpdateSettings(t, h, map[string]any{
		"registration_enabled": true,
		"risk_control_enabled": false,
	}, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set(middleware.ContextKeySessionID, "settings-session")
	})

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "SETTINGS_UPDATE_CONFLICT")
	require.Zero(t, cache.calls, "a stale baseline must conflict before consuming strict authorization")
	require.Nil(t, repo.lastUpdates)
	require.NotContains(t, repo.values, service.SettingKeyRegistrationEnabled)
}

func TestUpdateSettingsStrictAuthorizationRunsInsideAtomicBoundary(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyRiskControlEnabled: "true",
	})
	cache := &riskControlStepUpCache{granted: true}
	cache.onCall = func() {
		require.True(t, repo.inAtomic, "strict authorization must use the transaction-locked security baseline")
	}
	h.SetStepUpDeps(
		service.NewTotpService(nil, nil, cache, nil, nil, nil),
		service.NewUserService(&riskControlStepUpUserRepo{user: &service.User{ID: 1, TotpEnabled: true}}, nil, nil, nil),
	)

	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": false}, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
		c.Set(middleware.ContextKeySessionID, "settings-session")
	})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, repo.atomicCalls)
	require.Equal(t, 1, cache.calls)
	require.Equal(t, "false", repo.values[service.SettingKeyRiskControlEnabled])
}

func TestUpdateSettingsValidatesFastAndPaymentBeforeAnyAtomicWrite(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "invalid fast policy",
			body: map[string]any{
				"registration_enabled": true,
				"openai_fast_policy_settings": map[string]any{
					"rules": []map[string]any{{"service_tier": "priority", "action": "invalid", "scope": "all"}},
				},
			},
		},
		{
			name: "invalid payment",
			body: map[string]any{
				"registration_enabled":                true,
				"payment_balance_recharge_multiplier": 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
			rec := doUpdateSettings(t, h, tt.body, nil)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Zero(t, repo.atomicCalls)
			require.Nil(t, repo.lastUpdates)
			require.NotContains(t, repo.values, service.SettingKeyRegistrationEnabled)
		})
	}
}

func TestUpdateSettingsDisableRiskControlAlwaysRequiresSessionBoundStepUpBeforeWrite(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(*gin.Context)
		grant      bool
		wantStatus int
		wantCode   string
	}{
		{
			name: "admin api key",
			prepare: func(c *gin.Context) {
				c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
			},
			grant:      true,
			wantStatus: http.StatusForbidden,
			wantCode:   "STEP_UP_ADMIN_API_KEY_FORBIDDEN",
		},
		{
			name: "jwt without sid",
			prepare: func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
			},
			grant:      true,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "STEP_UP_SESSION_REQUIRED",
		},
		{
			name: "jwt without grant",
			prepare: func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
				c.Set(middleware.ContextKeySessionID, "settings-session")
			},
			grant:      false,
			wantStatus: http.StatusForbidden,
			wantCode:   "STEP_UP_REQUIRED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{
				service.SettingKeyRiskControlEnabled: "true",
			})
			userRepo := &riskControlStepUpUserRepo{user: &service.User{ID: 1, TotpEnabled: true}}
			userService := service.NewUserService(userRepo, nil, nil, nil)
			totpService := service.NewTotpService(nil, nil, &riskControlStepUpCache{granted: tt.grant}, nil, nil, nil)
			h.SetStepUpDeps(totpService, userService)

			rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": false}, tt.prepare)

			require.Equal(t, tt.wantStatus, rec.Code)
			require.Contains(t, rec.Body.String(), tt.wantCode)
			require.Nil(t, repo.lastUpdates, "risk-control downgrade must be rejected before any settings write")
			require.Equal(t, "true", repo.values[service.SettingKeyRiskControlEnabled])
		})
	}
}
