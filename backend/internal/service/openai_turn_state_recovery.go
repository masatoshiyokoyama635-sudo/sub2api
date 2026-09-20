package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"
)

// Recovery checks use the account's normal proxy independently of the hunter
// toggle. A streak of accepted state envelopes and matching response models marks
// recovery; this is a local observation and never rewrites account configuration.
// A new natural miss clears the observation so later checks can start again.
const (
	// openAITurnStateRecoveryExtraKey 是管理员写的配置。
	openAITurnStateRecoveryExtraKey = "openai_turn_state_recovery"
	// openAITurnStateRecoveryStateExtraKey 是探测写的运行态。调度中性键。
	openAITurnStateRecoveryStateExtraKey = "openai_turn_state_recovery_state"

	defaultOpenAITurnStateRecoveryStreak        = 5
	defaultOpenAITurnStateRecoveryMinMinutes    = 30
	defaultOpenAITurnStateRecoveryMaxMinutes    = 90
	defaultOpenAITurnStateRecoveryCooldownHours = 16

	openAITurnStateRecoveryMaxStreak        = 50
	openAITurnStateRecoveryMaxMinutes       = 1440
	openAITurnStateRecoveryMaxCooldownHours = 168
	openAITurnStateRecoveryLastKeep         = 5
	// openAITurnStateRecoveryTrafficWindow 是「最近有流量的模型」往回看的窗口（配置没填模型时）。
	openAITurnStateRecoveryTrafficWindow = 24 * time.Hour
)

// openAITurnStateRecoveryConfig 是 extra.openai_turn_state_recovery 的形态。
type openAITurnStateRecoveryConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// Model 探哪个模型。留空时使用最近有真实流量且观测过回合状态的模型。
	Model string `json:"model,omitempty"`
	// StreakTarget 连续多少次 合格候选 算恢复；连续同样多次失败进冷却。
	StreakTarget int `json:"streak_target,omitempty"`
	// MinMinutes / MaxMinutes 是两次探测之间的随机间隔，用户口径「模拟正常人使用」。
	MinMinutes int `json:"min_minutes,omitempty"`
	MaxMinutes int `json:"max_minutes,omitempty"`
	// CooldownHours 连续失败够数后的冷却时长。
	CooldownHours   int    `json:"cooldown_hours,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// UsageAPIKeyID 记账用的 API Key；留空回落猎手配置里的那把。独立一项是因为本功能
	// 猎手关着也能开，而猎手的那个字段在猎手关着时页面上根本没有入口（第一轮评审 S3）。
	UsageAPIKeyID int64 `json:"usage_api_key_id,omitempty"`
}

// applyDefaults 补默认值并压上限，与猎手配置同一套取舍（extra 可能绕过 handler 校验落库）。
func (cfg *openAITurnStateRecoveryConfig) applyDefaults() {
	cfg.StreakTarget = openAITurnStateHuntBound(cfg.StreakTarget, defaultOpenAITurnStateRecoveryStreak, openAITurnStateRecoveryMaxStreak)
	cfg.MinMinutes = openAITurnStateHuntBound(cfg.MinMinutes, defaultOpenAITurnStateRecoveryMinMinutes, openAITurnStateRecoveryMaxMinutes)
	cfg.MaxMinutes = openAITurnStateHuntBound(cfg.MaxMinutes, defaultOpenAITurnStateRecoveryMaxMinutes, openAITurnStateRecoveryMaxMinutes)
	if cfg.MaxMinutes < cfg.MinMinutes {
		cfg.MaxMinutes = cfg.MinMinutes // 填反了按固定间隔走，别让区间变负数
	}
	cfg.CooldownHours = openAITurnStateHuntBound(cfg.CooldownHours, defaultOpenAITurnStateRecoveryCooldownHours, openAITurnStateRecoveryMaxCooldownHours)
	cfg.Model = strings.TrimSpace(cfg.Model)
	effort := strings.TrimSpace(cfg.ReasoningEffort)
	if _, known := openAITurnStateHuntReasoningEfforts[effort]; !known {
		effort = defaultOpenAITurnStateHuntReasoningEffort
	}
	cfg.ReasoningEffort = effort
}

func (cfg openAITurnStateRecoveryConfig) cooldown() time.Duration {
	return time.Duration(cfg.CooldownHours) * time.Hour
}

// interval spreads checks across the configured interval range.
func (cfg openAITurnStateRecoveryConfig) interval() time.Duration {
	lo := time.Duration(cfg.MinMinutes) * time.Minute
	hi := time.Duration(cfg.MaxMinutes) * time.Minute
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(rand.Int64N(int64(hi-lo)+1))
}

// readOpenAITurnStateRecoveryConfig 走 json 往返，与 readOpenAITurnStateHunterConfig 同一套取舍。
func readOpenAITurnStateRecoveryConfig(a *Account) (openAITurnStateRecoveryConfig, bool) {
	var cfg openAITurnStateRecoveryConfig
	if a == nil || a.Extra == nil {
		return cfg, false
	}
	raw, ok := a.Extra[openAITurnStateRecoveryExtraKey]
	if !ok || raw == nil {
		return cfg, false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return cfg, false
	}
	if err := json.Unmarshal(encoded, &cfg); err != nil {
		return cfg, false
	}
	cfg.applyDefaults()
	return cfg, true
}

// openAITurnStateRecoveryState 是 extra.openai_turn_state_recovery_state 的形态。
type openAITurnStateRecoveryState struct {
	// Streak 连续铸出 合格候选 的次数；FailStreak 连续失败（非候选形态 / 非 200 / 传输错误）的次数。
	// 两者互斥：一次成功清零失败计数，反之亦然。
	Streak     int `json:"streak,omitempty"`
	FailStreak int `json:"fail_streak,omitempty"`
	// NextAt 下一次探测时刻。冷却也写在这里（冷却与随机间隔对调用方是同一件事：等到点）。
	NextAt time.Time `json:"next_at"`
	// RecoveredAt 判定恢复的时刻。非零 = 标记挂着、不再探测。
	//
	// 两个时间字段用 omitzero 而不是 omitempty：omitempty 对 struct 不生效，零值会落库成
	// "0001-01-01T00:00:00Z"，而 JS 的 Date 认这个字符串——页面从第一次探测起就写着「已恢复」
	//（第一轮评审 B2）。前端另有 >0 的保险，老数据里已经写进去的零值也不会误判。
	RecoveredAt time.Time `json:"recovered_at,omitzero"`
	// CoolingUntil 只为页面能说清「在冷却」而不是「在等下一次」，判定仍看 NextAt。
	CoolingUntil          time.Time                    `json:"cooling_until,omitzero"`
	Last                  []openAITurnStateHuntAttempt `json:"last,omitempty"`
	LastError             string                       `json:"last_error,omitempty"`
	UpdatedAt             time.Time                    `json:"updated_at"`
	AuthBlockedCredential string                       `json:"auth_blocked_credential,omitempty"`
	RateLimitUntil        time.Time                    `json:"rate_limit_until,omitzero"`
}

func readOpenAITurnStateRecoveryState(a *Account) openAITurnStateRecoveryState {
	var st openAITurnStateRecoveryState
	if a == nil || a.Extra == nil {
		return st
	}
	raw, ok := a.Extra[openAITurnStateRecoveryStateExtraKey]
	if !ok || raw == nil {
		return st
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(encoded, &st); err != nil {
		return openAITurnStateRecoveryState{}
	}
	return st
}

func (st *openAITurnStateRecoveryState) push(attempt openAITurnStateHuntAttempt) {
	st.Last = append([]openAITurnStateHuntAttempt{attempt}, st.Last...)
	if len(st.Last) > openAITurnStateRecoveryLastKeep {
		st.Last = st.Last[:openAITurnStateRecoveryLastKeep]
	}
	st.LastError = attempt.Error
}

// probeRecovery 是恢复探测的一次判定 + 至多一次探测。挂在猎手的 ticker 上，但**不看猎手开关**。
// 返回是否真的探了：调用方据此把每个 tick 的恢复探测限制在一个账号（见 runOnce）。
func (s *OpenAITurnStateHunterService) probeRecovery(ctx context.Context, account *Account, deadline time.Time) bool {
	if s == nil || s.gateway == nil || s.accountRepo == nil || account == nil {
		return false
	}
	if account.Status != StatusActive || !account.IsOpenAIOAuthLike() || !codexIdentityV2Enabled(account) || account.GetCodexTurnStateMode() == "off" || account.IsOpenAIAgentIdentity() || account.IsCredentialShadow() {
		return false
	}
	cfg, ok := readOpenAITurnStateRecoveryConfig(account)
	if !ok || !cfg.Enabled {
		return false
	}
	now := s.now()
	if codexHunterCredentialBlocked(account) || now.Before(codexHunterProbeNotBefore(account)) {
		return false
	}
	st := readOpenAITurnStateRecoveryState(account)
	st.AuthBlockedCredential = ""
	// 判定恢复后不再探：结论已经有了，继续探只是白付额度。
	if !st.RecoveredAt.IsZero() || now.Before(st.NextAt) || now.After(deadline) {
		return false
	}
	model := s.recoveryModel(account, cfg, now)
	if model == "" {
		// 不知道该探哪个模型（进程内没流量记录、也没观测过）：等下一窗，别瞎探一个上游不认的名字。
		// 也按一次失败记：配错了的账号最多写 streak_target 次就进冷却，不会每 30–90 分钟白写一次库。
		st.LastError = "no model to probe"
		st.Streak, st.FailStreak = 0, st.FailStreak+1
		if st.FailStreak >= cfg.StreakTarget {
			st.FailStreak = 0
			st.CoolingUntil = now.Add(cfg.cooldown())
			st.NextAt = st.CoolingUntil
		} else {
			st.NextAt = now.Add(cfg.interval())
		}
		st.UpdatedAt = now
		s.persistRecovery(ctx, account, st)
		return false
	}
	attempt := s.probeOwnExit(ctx, account, model, cfg)
	st.push(attempt)
	st.CoolingUntil = time.Time{}
	if attempt.Healthy {
		st.FailStreak, st.Streak = 0, st.Streak+1
		st.NextAt = now.Add(cfg.interval())
		if st.Streak >= cfg.StreakTarget {
			st.RecoveredAt = s.now()
			slog.Info("openai_turn_state_recovered", "account_id", account.ID, "model", model, "streak", st.Streak)
		}
	} else {
		// 非候选形态、非 200、传输错误都算一次失败：这个计数只决定「要不要歇会儿再探」，
		// 把它们分开只会多一条路径，省下的额度是同一笔。
		st.Streak, st.FailStreak = 0, st.FailStreak+1
		if st.FailStreak >= cfg.StreakTarget {
			st.FailStreak = 0 // 冷却结束后重新数，不要一醒来就又满
			st.CoolingUntil = now.Add(cfg.cooldown())
			st.NextAt = st.CoolingUntil
			slog.Info("openai_turn_state_recovery_cooldown", "account_id", account.ID, "model", model,
				"until", st.CoolingUntil.UTC().Format(time.RFC3339))
		} else {
			st.NextAt = now.Add(cfg.interval())
		}
	}
	if attempt.Status == 401 {
		st.AuthBlockedCredential = codexHunterCredentialFingerprint(account)
	}
	if attempt.Status == 429 {
		delay := cfg.interval()
		if attempt.RetryAfter > delay {
			delay = attempt.RetryAfter
		}
		st.RateLimitUntil = s.now().Add(delay)
		if st.RateLimitUntil.After(st.NextAt) {
			st.NextAt = st.RateLimitUntil
		}
	}
	st.UpdatedAt = s.now()
	s.persistRecovery(ctx, account, st)
	slog.Info("openai_turn_state_recovery_attempt",
		"account_id", account.ID, "model", model, "proxy_id", attempt.ProxyID, "status", attempt.Status,
		"chars", attempt.Chars, "healthy", attempt.Healthy, "error", attempt.Error,
		"streak", st.Streak, "fail_streak", st.FailStreak)
	return true
}

// recoveryModel uses the explicit model, otherwise the most recent qualifying
// production model. It does not invent a model when no traffic has been observed.
func (s *OpenAITurnStateHunterService) recoveryModel(account *Account, cfg openAITurnStateRecoveryConfig, now time.Time) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	if model := s.gateway.openAITurnStateLatestTrafficModel(account, now.Add(-openAITurnStateRecoveryTrafficWindow)); model != "" {
		return model
	}
	return ""
}

// A nil proxy tells the gateway to retain the normal account endpoint. The
// probe's transport remains isolated from production connections.
func (s *OpenAITurnStateHunterService) probeOwnExit(ctx context.Context, account *Account, model string, cfg openAITurnStateRecoveryConfig) openAITurnStateHuntAttempt {
	if s.probeFunc == nil {
		return openAITurnStateHuntAttempt{At: s.now(), Model: model, Error: "probe unavailable", preflight: true}
	}
	usageKeyID := cfg.UsageAPIKeyID
	if usageKeyID <= 0 {
		hunter, _ := readOpenAITurnStateHunterConfig(account)
		usageKeyID = hunter.UsageAPIKeyID
	}
	attempt := s.probeFunc(ctx, account, model, openAITurnStateHunterConfig{ReasoningEffort: cfg.ReasoningEffort, UsageAPIKeyID: usageKeyID}, nil)
	if attempt.At.IsZero() {
		attempt.At = s.now()
	}
	if attempt.Model == "" {
		attempt.Model = model
	}
	return attempt
}

func (s *OpenAITurnStateHunterService) persistRecovery(ctx context.Context, account *Account, st openAITurnStateRecoveryState) {
	encoded, err := json.Marshal(st)
	if err != nil {
		return
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[openAITurnStateRecoveryStateExtraKey] = generic
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{openAITurnStateRecoveryStateExtraKey: generic}); err != nil {
		slog.Warn("openai_turn_state_recovery_persist_failed", "account_id", account.ID, "error", err)
	}
}

// resetOpenAITurnStateRecovery 在账号又自然铸出 非候选形态 时清掉「已恢复」标记与连胜，从头攒。
// 只有标记挂着时才写库：这是响应热路径，判定恢复之后每条 非候选形态 都写一次就太贵了。
func (s *OpenAIGatewayService) resetOpenAITurnStateRecovery(ctx context.Context, account *Account) {
	if s == nil || s.accountRepo == nil || account == nil {
		return
	}
	st := readOpenAITurnStateRecoveryState(account)
	if st.RecoveredAt.IsZero() && st.Streak == 0 {
		return
	}
	st.RecoveredAt, st.Streak = time.Time{}, 0
	st.UpdatedAt = time.Now().UTC()
	encoded, err := json.Marshal(st)
	if err != nil {
		return
	}
	var generic map[string]any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{openAITurnStateRecoveryStateExtraKey: generic}); err != nil {
		slog.Warn("openai_turn_state_recovery_reset_failed", "account_id", account.ID, "error", err)
		return
	}
	slog.Info("openai_turn_state_recovery_reset", "account_id", account.ID)
}
