package service

import (
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// maxPersistedSessionIDLength bounds the persisted client session identifier to the
// usage_logs.session_id column width (VARCHAR(255)). Longer values are rejected so
// distinct identifiers can never alias through truncation.
const maxPersistedSessionIDLength = 255

// clientSessionIDHeaders extends the OpenAI-compatible sticky-session signals with
// native protocol identifiers that are safe to persist but must not alter OpenAI
// scheduling behavior.
var clientSessionIDHeaders = append(
	append([]string(nil), explicitOpenAIHeaderSessionNames...),
	claudeCodeSessionHeader,
)

const (
	openAIClientSessionKindThread  = "thread"
	openAIClientSessionKindSession = "session"
)

var openAIThreadIdentityHeaders = []string{
	"conversation_id",
	"thread_id",
	"thread-id",
	codeBuddyConversationHeader,
}

var openAISessionIdentityHeaders = []string{
	"session_id",
	"session-id",
	openCodeSessionIDHeader,
	openCodeNativeSessionHeader,
}

type openAIClientSessionIdentity struct {
	kind  string
	value string
}

// ClaudeCodeSessionIDFromHeader returns the stable Claude Code conversation
// identifier carried by X-Claude-Code-Session-Id. It is intentionally exposed
// separately from ExtractClientSessionID: callers that use it for routing must
// make that scope explicit rather than accidentally changing every protocol's
// session semantics.
func ClaudeCodeSessionIDFromHeader(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return sanitizeSessionID(c.GetHeader(claudeCodeSessionHeader))
}

// ExtractClientSessionID resolves the explicit client-provided session identifier from
// request headers for usage-log correlation and returns it sanitized. It is
// protocol-agnostic and shared by every gateway handler so all supported protocols
// record session_id through one seam. Returns "" when no valid identifier is present.
//
// This value feeds only usage_logs.session_id persistence. It does NOT affect sticky
// routing, account selection, request_id semantics, or upstream prompt caching, which
// keep their own (intentionally broader) session-signal resolution.
func ExtractClientSessionID(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	for _, header := range clientSessionIDHeaders {
		if sessionID := sanitizeSessionID(c.GetHeader(header)); sessionID != "" {
			return sessionID
		}
	}
	if isGrokRequestContext(c) {
		if sessionID := sanitizeSessionID(c.GetHeader(grokConversationIDHeader)); sessionID != "" {
			return sessionID
		}
	}
	return ""
}

// ExtractOpenAIClientSessionID resolves the explicit OpenAI conversation
// identity from request headers and client_metadata. Thread identities take
// precedence over session identities. A conflicting header/body value of the
// same kind is rejected instead of silently choosing one, so usage correlation
// and Cyber blocking cannot disagree about which conversation was identified.
// When no OpenAI identity is present, the legacy Claude Code session header is
// retained for usage correlation. It is not used by Cyber block-key derivation.
//
// prompt_cache_key and X-Session-Affinity are intentionally excluded: they are
// scheduling/cache hints, not reliable conversation identities.
func ExtractOpenAIClientSessionID(c *gin.Context, body []byte) string {
	identity, ok, rejected := resolveOpenAIClientSessionIdentity(c, body)
	if !ok {
		if !rejected {
			return ClaudeCodeSessionIDFromHeader(c)
		}
		return ""
	}
	return identity.value
}

func resolveOpenAIClientSessionIdentity(c *gin.Context, body []byte) (openAIClientSessionIdentity, bool, bool) {
	if c == nil || c.Request == nil {
		return openAIClientSessionIdentity{}, false, false
	}

	view := openAIRequestPayloadView(body)
	bodyThread, invalidBodyThread := openAIClientMetadataIdentity(view, "client_metadata.thread_id")
	bodySession, invalidBodySession := openAIClientMetadataIdentity(view, "client_metadata.session_id")

	headerThread, conflictingThreadHeaders := openAIIdentityHeader(c, openAIThreadIdentityHeaders)
	if conflictingThreadHeaders || invalidBodyThread || openAIIdentityValuesConflict(headerThread, bodyThread) {
		return openAIClientSessionIdentity{}, false, true
	}
	if headerThread != "" {
		return openAIClientSessionIdentity{kind: openAIClientSessionKindThread, value: headerThread}, true, false
	}
	if bodyThread != "" {
		return openAIClientSessionIdentity{kind: openAIClientSessionKindThread, value: bodyThread}, true, false
	}

	headerSession, conflictingSessionHeaders := openAIIdentityHeader(c, openAISessionIdentityHeaders)
	if conflictingSessionHeaders || invalidBodySession || openAIIdentityValuesConflict(headerSession, bodySession) {
		return openAIClientSessionIdentity{}, false, true
	}
	if headerSession != "" {
		return openAIClientSessionIdentity{kind: openAIClientSessionKindSession, value: headerSession}, true, false
	}
	if bodySession != "" {
		return openAIClientSessionIdentity{kind: openAIClientSessionKindSession, value: bodySession}, true, false
	}
	return openAIClientSessionIdentity{}, false, false
}

func openAIIdentityHeader(c *gin.Context, headers []string) (string, bool) {
	var resolved string
	for _, header := range headers {
		raw := c.GetHeader(header)
		if strings.TrimSpace(raw) == "" {
			continue
		}
		value := sanitizeSessionID(raw)
		if value == "" {
			return "", true
		}
		if resolved != "" && resolved != value {
			return "", true
		}
		resolved = value
	}
	return resolved, false
}

func openAIClientMetadataIdentity(view gjson.Result, path string) (string, bool) {
	result := view.Get(path)
	if !result.Exists() {
		return "", false
	}
	if result.Type != gjson.String {
		return "", true
	}
	if strings.TrimSpace(result.String()) == "" {
		return "", false
	}
	value := sanitizeSessionID(result.String())
	return value, value == ""
}

func openAIIdentityValuesConflict(headerValue, bodyValue string) bool {
	return headerValue != "" && bodyValue != "" && headerValue != bodyValue
}

// sanitizeSessionID normalizes a raw client-supplied session identifier for safe
// persistence: it trims surrounding whitespace, rejects the value outright if it
// contains any control character (CR/LF/tab/NUL/…) so a log- or header-injection style
// payload cannot slip into stored correlation data, and rejects values longer than
// the DB column bound. Absent or invalid input yields "".
func sanitizeSessionID(raw string) string {
	if !utf8.ValidString(raw) {
		return ""
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	count := 0
	for _, r := range trimmed {
		if r < 0x20 || r == 0x7f {
			// An explicit correlation id never legitimately contains control
			// characters; drop the whole value rather than persist a mangled or
			// partially-injected identifier.
			return ""
		}
		count++
		if count > maxPersistedSessionIDLength {
			return ""
		}
	}
	return trimmed
}
