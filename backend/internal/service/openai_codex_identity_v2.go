package service

// Relationship-preserving identity projection, adapted from KlN-4096/sub2api
// 295f9f39562de2280523f5aada0bbea21836b330. Unlike that implementation, every
// new derivation is explicitly versioned; existing accounts retain v1 values.
import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/net/http/httpguts"
)

var codexV2Window = regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}):([0-9]{1,19})$`)
var codexV2Cache = regexp.MustCompile(`^([A-Za-z0-9_-]{1,64}):([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

func scopeCodexIdentityV2(account *Account, apiKeyID int64, kind, raw string) string {
	if kind == "window" {
		if parts := codexV2Window.FindStringSubmatch(raw); parts != nil {
			return scopeCodexIdentityV2(account, apiKeyID, "thread", parts[1]) + ":" + parts[2]
		}
	}
	if kind == "prompt-cache" {
		if parts := codexV2Cache.FindStringSubmatch(raw); parts != nil {
			return parts[1] + ":" + scopeCodexIdentityV2(account, apiKeyID, "thread", parts[2])
		}
	}
	if kind == "session" || kind == "prompt-cache" {
		kind = "thread"
	}
	seed := fmt.Sprintf("sub2api:codex-account-identity:v2:user:%d:account:%s:kind:%s:value:%s", apiKeyID, codexAccountIdentityNamespace(account), kind, raw)
	if original, err := uuid.Parse(raw); err == nil && original.Version() == 7 && original.String() == raw {
		hash := sha256.Sum256([]byte(seed))
		var derived uuid.UUID
		copy(derived[:], hash[:16])
		copy(derived[:6], original[:6])
		derived[6] = (derived[6] & 0x0f) | 0x70
		derived[8] = (derived[8] & 0x3f) | 0x80
		return derived.String()
	}
	return deriveStableUUIDv4(seed)
}

var codexV2IdentityFields = []codexAccountIdentityField{
	{name: "root_turn_id", kind: "turn"},
	{name: "parent_turn_id", kind: "turn"},
	{name: "parent_thread_id", kind: "thread"},
	{name: "forked_from_thread_id", kind: "thread"},
	{name: "x-codex-parent-thread-id", kind: "thread"},
	{name: "context_window_id", kind: "context-window"},
}

func codexIdentityFieldsFor(account *Account) []codexAccountIdentityField {
	if !codexIdentityV2Enabled(account) {
		return codexAccountIdentityFields
	}
	fields := make([]codexAccountIdentityField, 0, len(codexAccountIdentityFields)+len(codexV2IdentityFields))
	fields = append(fields, codexAccountIdentityFields...)
	return append(fields, codexV2IdentityFields...)
}

// State follows the original client scope, but cannot cross an outbound identity
// version/configuration change. This does not replace the scheduler or execution
// scope: only upstream continuation/connection state uses this suffix.
func codexIdentityV2StateKey(c *gin.Context, account *Account, raw string) string {
	source := codexAccountIdentitySource(c, account)
	if raw == "" || !codexIdentityV2Enabled(source) || account == nil {
		return raw
	}
	seed, _ := codexFingerprintSeed(account.Extra)
	hash := sha256.Sum256([]byte(fmt.Sprintf("codex-state:v2:%s:%s:%s:%s:%s", codexAccountIdentityNamespace(source), account.GetCodexFingerprintMode(), seed, account.GetOpenAIDeviceID(), raw)))
	return fmt.Sprintf("v2:%x", hash[:])
}

func scopeCodexV2TurnMetadata(raw string, account *Account, apiKeyID int64) string {
	return rewriteCodexTurnMetadataJSON(raw, false, func(metadata map[string]any) map[string]any {
		updates := make(map[string]any)
		for _, field := range codexIdentityFieldsFor(account) {
			if value, ok := metadata[field.name].(string); ok && strings.TrimSpace(value) != "" {
				updates[field.name] = scopeCodexAccountIdentityValue(account, apiKeyID, field.kind, value)
			}
		}
		return updates
	})
}

// Encode just the small identity object, without rounding unknown large integers
// or HTML-escaping the embedded JSON. The root request bytes remain untouched.
func marshalCodexIdentityJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}

func decodeCodexIdentityJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(value)
}

const codexV2SnapshotKey = "codex_identity_v2_snapshot"

type codexIdentityV2Snapshot struct {
	accountID int64
	source    string
	headers   http.Header
}

// Fields with independent evidence may be reconstructed from a relayed body.
// Current frame values take precedence over the long-lived handshake.
var codexV2Carriers = [][3]string{
	{"session_id", "session-id", "session_id"},
	{"thread_id", "thread-id", "thread_id"},
	{"x-codex-installation-id", "x-codex-installation-id", "installation_id"},
	{"x-codex-window-id", "x-codex-window-id", "window_id"},
	{"parent_thread_id", "x-codex-parent-thread-id", "parent_thread_id"},
	{"x-openai-subagent", "x-openai-subagent", "x-openai-subagent"},
}

func codexMetadataText(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func prepareCodexV2Metadata(c *gin.Context, metadata map[string]any) {
	var inbound http.Header
	if c != nil && c.Request != nil {
		inbound = c.Request.Header
	}
	raw := codexMetadataText(metadata, openAIWSTurnMetadataHeader)
	if raw == "" && inbound != nil {
		raw = inbound.Get(openAIWSTurnMetadataHeader)
		if strings.TrimSpace(raw) != "" {
			metadata[openAIWSTurnMetadataHeader] = raw
		}
	}
	embedded := gjson.Parse(raw)
	for _, field := range codexV2Carriers {
		value := codexMetadataText(metadata, field[0])
		if value == "" {
			if candidate := embedded.Get(field[2]); candidate.Type == gjson.String {
				value = strings.TrimSpace(candidate.Str)
			}
		}
		if value == "" && inbound != nil {
			value = strings.TrimSpace(inbound.Get(field[1]))
			if value == "" && field[0] == "session_id" {
				value = extractClientSessionID(inbound)
			}
		}
		if value != "" {
			metadata[field[0]] = value
		}
	}
	for _, name := range []string{"turn_id", "root_turn_id", "parent_turn_id", "forked_from_thread_id", "context_window_id"} {
		if codexMetadataText(metadata, name) == "" {
			if value := embedded.Get(name); value.Type == gjson.String && strings.TrimSpace(value.Str) != "" {
				metadata[name] = value.Str
			}
		}
	}
	// Keep flat and embedded identities coherent without reserializing unknown
	// metadata, or fabricating an embedded object when none was supplied.
	if raw != "" {
		metadata[openAIWSTurnMetadataHeader] = rewriteCodexTurnMetadataJSON(raw, false, func(existing map[string]any) map[string]any {
			updates := map[string]any{}
			for _, field := range codexV2Carriers {
				if value := codexMetadataText(metadata, field[0]); value != "" && existing[field[2]] != nil {
					updates[field[2]] = value
				}
			}
			for _, name := range []string{"turn_id", "root_turn_id", "parent_turn_id", "forked_from_thread_id", "context_window_id"} {
				if value := codexMetadataText(metadata, name); value != "" {
					updates[name] = value
				}
			}
			return updates
		})
	}
}

func resolveCodexV2Fingerprint(c *gin.Context, account *Account, metadata map[string]any) *codexFingerprintIDs {
	source := codexAccountIdentitySource(c, account)
	thread := codexMetadataText(metadata, "thread_id")
	if thread == "" {
		thread = codexMetadataText(metadata, "session_id")
	}
	if thread != "" {
		thread = scopeCodexAccountIdentityValue(source, getAPIKeyIDFromContext(c), "thread", thread)
	}
	ids := resolveCodexFingerprintIDs(account, thread, account.GetCodexFingerprintMode())
	if ids == nil {
		return nil
	}
	ids.identityV2 = true
	// Session convergence collapses the session, not the independent thread
	// graph. Use the same final thread namespace as parent/cache references.
	if ids.mode == codexFingerprintSession && thread != "" {
		ids.threadID = thread
	}
	if rawTurn := codexMetadataText(metadata, "turn_id"); rawTurn != "" && ids.turnID != "" {
		ids.turnID = scopeCodexAccountIdentityValue(source, getAPIKeyIDFromContext(c), "turn", rawTurn)
	}
	ids.windowNumber = "0"
	if parts := codexV2Window.FindStringSubmatch(codexMetadataText(metadata, "x-codex-window-id")); parts != nil {
		ids.windowNumber = parts[2]
	} else if n := gjson.Get(codexMetadataText(metadata, openAIWSTurnMetadataHeader), "window_number"); n.Exists() {
		if raw := n.String(); len(raw) <= 19 {
			if _, err := strconv.ParseUint(raw, 10, 64); err == nil {
				ids.windowNumber = raw
			}
		}
	}
	if ids.threadID != "" {
		ids.windowID = ids.threadID + ":" + ids.windowNumber
	}
	return ids
}

func applyCodexV2FingerprintMetadata(metadata map[string]any, ids *codexFingerprintIDs) bool {
	updates := map[string]any{"installation_id": ids.installationID}
	metadata["x-codex-installation-id"] = ids.installationID
	if _, exists := metadata["installation_id"]; exists {
		metadata["installation_id"] = ids.installationID
	}
	if ids.mode != codexFingerprintDevice {
		oldTurn := codexMetadataText(metadata, "turn_id")
		if oldTurn != "" && codexMetadataText(metadata, "root_turn_id") == oldTurn {
			metadata["root_turn_id"] = ids.turnID
		}
		metadata["session_id"], metadata["thread_id"], metadata["turn_id"] = ids.sessionID, ids.threadID, ids.turnID
		metadata["x-codex-window-id"] = ids.windowID
		for key, value := range map[string]string{"session-id": ids.sessionID, "thread-id": ids.threadID, "window_id": ids.windowID, "x-client-request-id": ids.threadID} {
			if _, exists := metadata[key]; exists {
				metadata[key] = value
			}
		}
		if ids.mode == codexFingerprintFull {
			for _, key := range []string{"parent_thread_id", "forked_from_thread_id", "x-codex-parent-thread-id"} {
				if codexMetadataText(metadata, key) != "" {
					metadata[key] = ids.threadID
					updates[key] = ids.threadID
				}
			}
		}
		updates["session_id"], updates["thread_id"], updates["turn_id"] = ids.sessionID, ids.threadID, ids.turnID
		updates["window_id"] = ids.windowID
		number, _ := strconv.ParseUint(ids.windowNumber, 10, 64)
		updates["window_number"], updates["turn_started_at_unix_ms"] = number, ids.turnStartedAtUnixMs
	}
	if raw := codexMetadataText(metadata, openAIWSTurnMetadataHeader); raw != "" {
		metadata[openAIWSTurnMetadataHeader] = rewriteCodexTurnMetadataJSON(raw, true, func(existing map[string]any) map[string]any {
			if turn := codexMetadataText(existing, "turn_id"); ids.turnID != "" && turn != "" && codexMetadataText(existing, "root_turn_id") == turn {
				updates["root_turn_id"] = ids.turnID
			}
			return updates
		})
	}
	return true
}

func stageCodexIdentityV2(c *gin.Context, account *Account, metadata map[string]any) {
	if c == nil {
		return
	}
	headers := make(http.Header)
	for _, field := range codexV2Carriers {
		if value := codexMetadataText(metadata, field[0]); value != "" && httpguts.ValidHeaderFieldValue(value) {
			headers.Set(field[1], value)
		}
	}
	if raw := codexMetadataText(metadata, openAIWSTurnMetadataHeader); raw != "" {
		// A body string may contain pretty-printed JSON. Compact only its header
		// projection; raw body metadata retains its original spelling.
		if !httpguts.ValidHeaderFieldValue(raw) {
			var compact bytes.Buffer
			if json.Compact(&compact, []byte(raw)) == nil {
				raw = compact.String()
			}
		}
		if httpguts.ValidHeaderFieldValue(raw) {
			headers.Set(openAIWSTurnMetadataHeader, raw)
		}
	}
	c.Set(codexV2SnapshotKey, codexIdentityV2Snapshot{accountID: account.ID, source: codexAccountIdentityNamespace(codexAccountIdentitySource(c, account)), headers: headers})
}

// Called only once on the unscoped request/response.create, before building its
// HTTP or WS headers. Execution scope and scheduling retain the original input.
func applyCodexIdentityV2Map(c *gin.Context, account *Account, body map[string]any) bool {
	if !codexIdentityV2Enabled(codexAccountIdentitySource(c, account)) || body == nil {
		return false
	}
	stageCodexFingerprintIDs(c, nil)
	metadata, _ := body["client_metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	prepareCodexV2Metadata(c, metadata)
	ids := resolveCodexV2Fingerprint(c, account, metadata)
	if len(metadata) > 0 {
		body["client_metadata"] = metadata
	}
	applyCodexAccountIdentityClientMetadataMap(body, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c))
	if ids != nil {
		applyCodexFingerprintClientMetadata(body, ids)
		if ids.mode == codexFingerprintFull {
			if key, ok := body["prompt_cache_key"].(string); ok {
				if parts := codexV2Cache.FindStringSubmatch(key); parts != nil {
					body["prompt_cache_key"] = parts[1] + ":" + ids.threadID
				}
			}
		}
	}
	stageCodexFingerprintIDs(c, ids)
	metadata, _ = body["client_metadata"].(map[string]any)
	if _, exists := metadata["x-client-request-id"]; exists && codexMetadataText(metadata, "thread_id") != "" {
		metadata["x-client-request-id"] = metadata["thread_id"]
	}
	stageCodexIdentityV2(c, account, metadata)
	return true
}

func applyCodexIdentityV2Raw(c *gin.Context, account *Account, body []byte) ([]byte, error) {
	if !codexIdentityV2Enabled(codexAccountIdentitySource(c, account)) {
		next, _, err := applyCodexAccountIdentityClientMetadataRaw(body, codexAccountIdentitySource(c, account), getAPIKeyIDFromContext(c))
		return next, err
	}
	stageCodexFingerprintIDs(c, nil)
	if c != nil {
		c.Set(codexV2SnapshotKey, nil)
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return body, nil
	}
	part := map[string]any{}
	cm := gjson.GetBytes(body, "client_metadata")
	if cm.IsObject() {
		var metadata map[string]any
		if err := decodeCodexIdentityJSON([]byte(cm.Raw), &metadata); err != nil {
			return body, err
		}
		part["client_metadata"] = metadata
	}
	if cache := gjson.GetBytes(body, "prompt_cache_key"); cache.Type == gjson.String {
		part["prompt_cache_key"] = cache.Str
	}
	applyCodexIdentityV2Map(c, account, part)
	next := body
	if metadata, exists := part["client_metadata"]; exists {
		raw, err := marshalCodexIdentityJSON(metadata)
		if err != nil {
			return body, err
		}
		next, err = sjson.SetRawBytes(next, "client_metadata", raw)
		if err != nil {
			return body, err
		}
	}
	if cache, ok := part["prompt_cache_key"].(string); ok {
		var err error
		next, err = sjson.SetBytes(next, "prompt_cache_key", cache)
		if err != nil {
			return body, err
		}
	}
	return next, nil
}

func applyCodexIdentityV2Headers(c *gin.Context, account *Account, headers http.Header) {
	source := codexAccountIdentitySource(c, account)
	if c == nil || headers == nil || !codexIdentityV2Enabled(source) {
		return
	}
	value, _ := c.Get(codexV2SnapshotKey)
	snapshot, ok := value.(codexIdentityV2Snapshot)
	if !ok || snapshot.accountID != account.ID || snapshot.source != codexAccountIdentityNamespace(source) {
		return
	}
	for name, values := range snapshot.headers {
		headers[name] = append([]string(nil), values...)
	}
	if thread := headers.Get("thread-id"); thread != "" {
		headers.Set("x-client-request-id", thread)
	}
	if headers.Get("session-id") != "" {
		headers.Del("session_id")
		headers.Del("conversation_id")
	}
}

// Compatibility clients do not necessarily speak Codex metadata. The bridge's
// existing stable session key is independent evidence; namespace it once just
// like a native client, without introducing a new cache key into Messages bodies.
func seedCodexIdentityV2BridgeMetadata(c *gin.Context, account *Account, body map[string]any, sessionKey string) {
	if !codexIdentityV2Enabled(codexAccountIdentitySource(c, account)) || body == nil {
		return
	}
	metadata, _ := body["client_metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	prepareCodexV2Metadata(c, metadata)
	if codexMetadataText(metadata, "session_id") == "" && strings.TrimSpace(sessionKey) != "" {
		metadata["session_id"] = sessionKey
	}
	if codexMetadataText(metadata, "thread_id") == "" && codexMetadataText(metadata, "session_id") != "" {
		metadata["thread_id"] = metadata["session_id"]
	}
	if len(metadata) > 0 {
		body["client_metadata"] = metadata
	}
}
