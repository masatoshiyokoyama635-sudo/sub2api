package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

// CyberSessionBlockStore 是 cyber 会话屏蔽表的存取接口。
// repository 层 gatewayCache 附带实现（类型断言探测接入，不改 GatewayCache
// 共享接口）；测试 stub 不实现时屏蔽能力自动降级关闭。
type CyberSessionBlockStore interface {
	SetCyberSessionBlocked(ctx context.Context, scopeKey string, keys []string, ttl time.Duration) error
	IsCyberSessionScopeActive(ctx context.Context, scopeKey string) (bool, error)
	FindCyberSessionBlocked(ctx context.Context, keys []string) (string, error)
}

// CyberSessionExplicitBlockKey accepts explicit conversation-scoped headers or
// client_metadata only. Cache/affinity hints and transcript similarity are not
// session identities. API keys define the authenticated namespace; relays
// sharing a key must supply a distinct thread/session ID for each downstream
// conversation.
func CyberSessionExplicitBlockKey(apiKeyID int64, c *gin.Context, body []byte) string {
	if apiKeyID <= 0 || c == nil || c.Request == nil {
		return ""
	}
	identity, ok, _ := resolveOpenAIClientSessionIdentity(c, body)
	if !ok {
		return ""
	}
	return cyberSessionExplicitBlockKeyForIdentity(apiKeyID, identity)
}

func cyberSessionExplicitBlockKeyForIdentity(apiKeyID int64, identity openAIClientSessionIdentity) string {
	raw := "cyber-explicit-session:v3|api_key=" + strconv.FormatInt(apiKeyID, 10) + "|kind=" + identity.kind + "|id=" + identity.value
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func cyberSessionExplicitBlockLookupKeys(apiKeyID int64, c *gin.Context, body []byte) []string {
	identity, ok, _ := resolveOpenAIClientSessionIdentity(c, body)
	if !ok {
		return nil
	}
	key := cyberSessionExplicitBlockKeyForIdentity(apiKeyID, identity)

	// Read the former untyped v2 key until its configured TTL naturally
	// expires. New writes use only v3 so thread and session namespaces cannot
	// collide after this version.
	legacyRaw := "cyber-explicit-session:v2|api_key=" + strconv.FormatInt(apiKeyID, 10) + "|session=" + identity.value
	legacySum := sha256.Sum256([]byte(legacyRaw))
	return []string{key, hex.EncodeToString(legacySum[:])}
}

// cyberSessionBlockStore 探测 cache 是否具备屏蔽存储能力。
// 注意：若未来以装饰器包装 GatewayCache（如日志/指标装饰器），该装饰器必须同时实现
// CyberSessionBlockStore，否则会话屏蔽能力将静默降级关闭
// （编译断言 var _ service.CyberSessionBlockStore = (*gatewayCache)(nil) 只覆盖
// *gatewayCache 本体，无法覆盖其外层包装）。
func (s *OpenAIGatewayService) cyberSessionBlockStore() CyberSessionBlockStore {
	if s == nil || s.cache == nil {
		return nil
	}
	store, ok := s.cache.(CyberSessionBlockStore)
	if !ok {
		return nil
	}
	return store
}

// CyberSessionBlockRuntime 返回 (开关, TTL)。开关默认关。
// 委托给 SettingService.GetCyberSessionBlockRuntime，进程内缓存避免热路径 DB 往返。
func (s *OpenAIGatewayService) CyberSessionBlockRuntime(ctx context.Context) (bool, time.Duration) {
	if s == nil || s.settingService == nil {
		return false, time.Hour
	}
	return s.settingService.GetCyberSessionBlockRuntime(ctx)
}

// MarkCyberSessionBlocked 把会话写入屏蔽表（写入点：cyber 命中后）。
// 开关关闭、key 为空或存储不可用时静默跳过。
func (s *OpenAIGatewayService) MarkCyberSessionBlocked(ctx context.Context, scopeKey string, keys []string) {
	if s == nil || len(keys) == 0 {
		return
	}
	enabled, ttl := s.CyberSessionBlockRuntime(ctx)
	if !enabled {
		return
	}
	store := s.cyberSessionBlockStore()
	if store == nil {
		return
	}
	if err := store.SetCyberSessionBlocked(ctx, scopeKey, keys, ttl); err != nil {
		logger.LegacyPrintf("service.openai_gateway", "cyber session block write failed: err=%v", err)
	}
}

// FindCyberSessionBlockedForRequest blocks only an exact session identity.
// Unknown identity and storage failures remain fail-open: the upstream still
// evaluates the request. Source metadata and history length cannot prove that
// a request belongs to a previously blocked session.
func (s *OpenAIGatewayService) FindCyberSessionBlockedForRequest(ctx context.Context, apiKeyID int64, c *gin.Context, body []byte, _ string, _ string) string {
	enabled, _ := s.CyberSessionBlockRuntime(ctx)
	if !enabled {
		return ""
	}
	keys := cyberSessionExplicitBlockLookupKeys(apiKeyID, c, body)
	if len(keys) == 0 {
		return ""
	}
	store := s.cyberSessionBlockStore()
	if store == nil {
		return ""
	}
	matched, err := store.FindCyberSessionBlocked(ctx, keys)
	if err != nil {
		logger.LegacyPrintf("service.openai_gateway", "cyber explicit session read failed: err=%v", err)
		return ""
	}
	return matched
}
