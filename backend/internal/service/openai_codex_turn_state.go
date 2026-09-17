package service

import (
	"container/list"
	"crypto/sha256"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// openAICodexTurnStateHeader 是 Codex 的回合状态头。上游在响应头中铸造该
// 不透明 blob，客户端在同一回合的后续请求中原样回带。HTTP 从 /responses
// 与 /responses/compact 响应头捕获；原生 WS 从 response.metadata.headers
// 捕获，不能把未转发的上游握手响应头当作客户端已经收到的状态。
const openAICodexTurnStateHeader = "x-codex-turn-state"

// turn-state blob 是上游在"出站身份"（含 #5553 指纹收敛改写后的
// installation/session/thread 标识）下铸造的，同账号回放自洽；跨账号回放
// （failover 换号后客户端仍回带旧账号的 blob）是代理链独有、真实 Codex
// 永远不会产生的矛盾信号。v1 溯源表记录每个下游会话最近一次铸造该 blob 的
// 账号，出站守卫据此剥离已知异账号的回带值。
type openAICodexTurnStateOrigin struct {
	accountID int64
	expiresAt time.Time
}

// v1 retains its session-based guard. The opt-in v2 guard tracks each committed
// blob independently: a client may keep the first blob in a turn even after a
// later attempt issued a different one. Raw blobs are never retained here.
const openAICodexTurnStateMaxOrigins = 16384

type openAICodexTurnStateBlobOrigin struct {
	owner     string
	version   string
	expiresAt time.Time
}

type openAICodexTurnStateCacheEntry struct {
	key    [sha256.Size]byte
	origin openAICodexTurnStateBlobOrigin
}

// openAICodexTurnStateCache is a zero-value-ready, bounded process-local cache.
// Oldest writes are evicted at capacity; expired entries are removed on lookup
// and on subsequent writes. Other instances, restarts and evictions therefore
// yield unknown provenance and preserve the client's blob.
type openAICodexTurnStateCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]*list.Element
	order   list.List
}

func (cache *openAICodexTurnStateCache) store(key [sha256.Size]byte, origin openAICodexTurnStateBlobOrigin, now time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[[sha256.Size]byte]*list.Element)
	}
	for element := cache.order.Front(); element != nil; element = cache.order.Front() {
		entry, ok := element.Value.(openAICodexTurnStateCacheEntry)
		if !ok {
			cache.entries = make(map[[sha256.Size]byte]*list.Element)
			cache.order.Init()
			break
		}
		if now.Before(entry.origin.expiresAt) {
			break
		}
		delete(cache.entries, entry.key)
		cache.order.Remove(element)
	}
	entry := openAICodexTurnStateCacheEntry{key: key, origin: origin}
	if element, ok := cache.entries[key]; ok {
		element.Value = entry
		cache.order.MoveToBack(element)
		return
	}
	if len(cache.entries) >= openAICodexTurnStateMaxOrigins {
		oldest := cache.order.Front()
		if oldest != nil {
			if entry, ok := oldest.Value.(openAICodexTurnStateCacheEntry); ok {
				delete(cache.entries, entry.key)
				cache.order.Remove(oldest)
			} else {
				cache.entries = make(map[[sha256.Size]byte]*list.Element)
				cache.order.Init()
			}
		} else {
			cache.entries = make(map[[sha256.Size]byte]*list.Element)
		}
	}
	cache.entries[key] = cache.order.PushBack(entry)
}

func (cache *openAICodexTurnStateCache) load(key [sha256.Size]byte, now time.Time) (openAICodexTurnStateBlobOrigin, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	element, ok := cache.entries[key]
	if !ok {
		return openAICodexTurnStateBlobOrigin{}, false
	}
	entry, valid := element.Value.(openAICodexTurnStateCacheEntry)
	origin := entry.origin
	if !valid || !now.Before(origin.expiresAt) {
		delete(cache.entries, key)
		cache.order.Remove(element)
		return openAICodexTurnStateBlobOrigin{}, false
	}
	return origin, true
}

func openAICodexTurnStateKey(state string) [sha256.Size]byte {
	return sha256.Sum256([]byte(strings.TrimSpace(state)))
}

func openAICodexTurnStateIdentityVersion(c *gin.Context, account *Account) string {
	if source := codexAccountIdentitySource(c, account); source != nil {
		return source.GetCodexIdentityVersion()
	}
	return "v1"
}

// Credential shadows use the same prepared identity source as outbound identity
// projection. The version is part of ownership so a rollout/rollback cannot
// reuse a known blob minted under the other identity algorithm.
func openAICodexTurnStateOwner(c *gin.Context, account *Account) string {
	source := codexAccountIdentitySource(c, account)
	if source == nil {
		return ""
	}
	owner := codexAccountIdentityNamespace(source)
	if owner == "" {
		if source.ID <= 0 {
			return ""
		}
		owner = "id:" + strconv.FormatInt(source.ID, 10)
	}
	return owner + "\x00identity:" + source.GetCodexIdentityVersion()
}

// openAICodexTurnStateSeed 返回溯源表键：API Key + 客户端原始会话标识。
// 客户端会话标识取自请求头（与指纹收敛的 thread 派生同源，见
// extractClientSessionID），确保同一下游会话的记录/守卫两侧使用同一键。
// 无会话标识时返回空串，表示不做跟踪（保持透传现状）。
func openAICodexTurnStateSeed(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	sessionID := extractClientSessionID(c.Request.Header)
	if sessionID == "" {
		return ""
	}
	return strconv.FormatInt(getAPIKeyIDFromContext(c), 10) + "\x00" + sessionID
}

// relayOpenAICodexTurnState 将上游响应中的 turn-state 显式写入下游响应头，
// 并记录铸造账号。必须在响应头提交点调用（WriteHeader 之前、且确认本次
// 上游响应就是将要写回客户端的响应之后）。上游无该头时主动清除 writer 上
// 可能残留的上一 failover attempt 的值——否则换号后旧账号的 blob 会粘到
// 新账号的响应上，这正是本文件要防止的跨账号矛盾。
func (s *OpenAIGatewayService) relayOpenAICodexTurnState(c *gin.Context, account *Account, upstream http.Header) {
	if c == nil || c.Writer == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		c.Writer.Header().Del(canonical)
		return
	}
	c.Writer.Header().Set(canonical, state)
	s.noteOpenAICodexTurnStateOrigin(c, account, state)
}

// stageOpenAICodexTurnState 将上游 turn-state 暂存到延迟提交的响应头集合
// （首输出守卫路径先缓存头、见到首个输出事件才提交）。此处**不**记录铸造
// 账号：该 attempt 仍可能在首输出超时后 failover，暂存头会被整体丢弃，
// 客户端从未收到该 blob。溯源必须在真正提交时记录，见
// noteStagedOpenAICodexTurnStateCommitted。
func stageOpenAICodexTurnState(dst *http.Header, upstream http.Header) {
	if dst == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		if *dst != nil {
			dst.Del(canonical)
		}
		return
	}
	if *dst == nil {
		*dst = http.Header{}
	}
	dst.Set(canonical, state)
}

// noteStagedOpenAICodexTurnStateCommitted 在暂存响应头真正写入下游时记录
// 铸造账号——只有此刻客户端才确定收到了该 blob，溯源表才与客户端持有的
// 值一致（否则被 failover 丢弃的 attempt 会污染溯源，导致后续误剥离）。
func (s *OpenAIGatewayService) noteStagedOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, staged http.Header) {
	if staged == nil || strings.TrimSpace(staged.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	s.noteOpenAICodexTurnStateOrigin(c, account, staged.Get(openAICodexTurnStateHeader))
}

func extractOpenAICodexTurnState(upstream http.Header) string {
	if upstream == nil {
		return ""
	}
	return strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
}

// noteOpenAICodexTurnStateProvenance 记录（下游会话 → 铸造账号）。
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenance(c *gin.Context, account *Account) {
	if s == nil || account == nil || account.ID <= 0 {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	if seed == "" {
		return
	}
	s.openaiCodexTurnStateOrigins.Store(seed, openAICodexTurnStateOrigin{
		accountID: account.ID,
		expiresAt: time.Now().Add(s.openAIWSSessionStickyTTL()),
	})
	s.sweepOpenAICodexTurnStateOrigins()
}

// noteOpenAICodexTurnStateOrigin must only run at the response commit point.
// Record both versions in the bounded blob cache so switching to v2 can reject
// a blob minted by v1, and switching back to v1 can reject a known v2 blob.
// Legacy session provenance remains available for unchanged v1 behavior.
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateOrigin(c *gin.Context, account *Account, state string) {
	s.noteOpenAICodexTurnStateBlobOrigin(c, account, state)
	if strings.TrimSpace(state) != "" && openAICodexTurnStateIdentityVersion(c, account) != "v2" {
		s.noteOpenAICodexTurnStateProvenance(c, account)
	}
}

func (s *OpenAIGatewayService) noteOpenAICodexTurnStateBlobOrigin(c *gin.Context, account *Account, state string) {
	if s == nil || account == nil || strings.TrimSpace(state) == "" {
		return
	}
	owner := openAICodexTurnStateOwner(c, account)
	if owner == "" {
		return
	}
	now := time.Now()
	s.openaiCodexTurnStateV2Origins.store(openAICodexTurnStateKey(state), openAICodexTurnStateBlobOrigin{
		owner:     owner,
		version:   openAICodexTurnStateIdentityVersion(c, account),
		expiresAt: now.Add(s.openAIWSSessionStickyTTL()),
	}, now)
}

// noteOpenAICodexTurnStateFromWSEvent observes metadata only after the frame was
// successfully forwarded. A handshake-only blob is not a client-visible WS
// response.metadata event and must not be recorded through this helper.
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateFromWSEvent(c *gin.Context, account *Account, frame []byte) {
	if s == nil || account == nil || !containsASCIIFold(frame, []byte(openAICodexTurnStateHeader)) || !gjson.ValidBytes(frame) {
		return
	}
	if gjson.GetBytes(frame, "type").String() != "response.metadata" {
		return
	}
	headers := gjson.GetBytes(frame, "headers")
	if !headers.IsObject() {
		return
	}
	headers.ForEach(func(key, value gjson.Result) bool {
		if strings.EqualFold(key.String(), openAICodexTurnStateHeader) && value.Type == gjson.String {
			// WS did not update the legacy session table before v2. Observe
			// its blob without altering the default v1 HTTP guard semantics.
			s.noteOpenAICodexTurnStateBlobOrigin(c, account, value.String())
		}
		return true
	})
}

// Unknown provenance deliberately passes through. This cache does not provide
// shared provenance across gateway instances.
func (s *OpenAIGatewayService) openAICodexTurnStateMintedByOther(c *gin.Context, account *Account, state string) bool {
	if s == nil || account == nil || strings.TrimSpace(state) == "" {
		return false
	}
	origin, ok := s.openaiCodexTurnStateV2Origins.load(openAICodexTurnStateKey(state), time.Now())
	if !ok {
		return false
	}
	if openAICodexTurnStateIdentityVersion(c, account) != "v2" {
		return origin.version == "v2"
	}
	owner := openAICodexTurnStateOwner(c, account)
	return owner == "" || owner != origin.owner
}

func (s *OpenAIGatewayService) guardOpenAICodexTurnStateValue(c *gin.Context, account *Account, state string) string {
	state = strings.TrimSpace(state)
	if s.openAICodexTurnStateMintedByOther(c, account, state) {
		return ""
	}
	return state
}

// Frame guards leave v1 traffic byte-for-byte unchanged unless it carries a
// known v2 blob after rollback. v2 applies the same ownership rule as HTTP.
func (s *OpenAIGatewayService) guardOpenAICodexWSFrameTurnState(c *gin.Context, account *Account, payload []byte) []byte {
	if s == nil || account == nil || !containsASCIIFold(payload, []byte(openAICodexTurnStateHeader)) || !gjson.ValidBytes(payload) {
		return payload
	}
	metadata := gjson.GetBytes(payload, "client_metadata")
	if !metadata.IsObject() {
		return payload
	}
	metadata.ForEach(func(key, value gjson.Result) bool {
		if strings.EqualFold(key.String(), openAICodexTurnStateHeader) && value.Type == gjson.String &&
			s.openAICodexTurnStateMintedByOther(c, account, value.String()) {
			if next, err := sjson.DeleteBytes(payload, "client_metadata."+key.String()); err == nil {
				payload = next
			}
		}
		return true
	})
	return payload
}

// guardOpenAICodexTurnStateEcho 出站守卫：客户端回带的 turn-state 若已知由
// 其他账号铸造则剥离，同账号或无溯源记录时保持原样。只剥离、不注入——
// /responses 路径的客户端是真实 Codex，会按自身回合语义自行回带；服务端
// 注入是 Claude 兼容桥（无法回带的客户端）的专属行为。
func (s *OpenAIGatewayService) guardOpenAICodexTurnStateEcho(c *gin.Context, account *Account, h http.Header) {
	if s == nil || h == nil || account == nil {
		return
	}
	if strings.TrimSpace(h.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	if s.openAICodexTurnStateMintedByOther(c, account, h.Get(openAICodexTurnStateHeader)) {
		h.Del(openAICodexTurnStateHeader)
		return
	}
	if openAICodexTurnStateIdentityVersion(c, account) == "v2" {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	if seed == "" {
		return
	}
	raw, ok := s.openaiCodexTurnStateOrigins.Load(seed)
	if !ok {
		return
	}
	origin, ok := raw.(openAICodexTurnStateOrigin)
	if !ok {
		s.openaiCodexTurnStateOrigins.Delete(seed)
		return
	}
	if !origin.expiresAt.IsZero() && time.Now().After(origin.expiresAt) {
		s.openaiCodexTurnStateOrigins.Delete(seed)
		return
	}
	if origin.accountID != account.ID {
		h.Del(openAICodexTurnStateHeader)
	}
}

// sweepOpenAICodexTurnStateOrigins 机会式清扫过期溯源记录：每 256 次写入
// 全量遍历一轮，防止仅靠读侧惰性删除导致的慢泄漏（会话键无上界）。
func (s *OpenAIGatewayService) sweepOpenAICodexTurnStateOrigins() {
	if s.openaiCodexTurnStateWrites.Add(1)%256 != 0 {
		return
	}
	now := time.Now()
	s.openaiCodexTurnStateOrigins.Range(func(key, value any) bool {
		origin, ok := value.(openAICodexTurnStateOrigin)
		if !ok || (!origin.expiresAt.IsZero() && now.After(origin.expiresAt)) {
			s.openaiCodexTurnStateOrigins.Delete(key)
		}
		return true
	})
}
