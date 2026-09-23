package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

var (
	// ErrGrokBillingProbeCASUnavailable is returned when a repository cannot
	// atomically bind a billing result to the account identity used to fetch it.
	ErrGrokBillingProbeCASUnavailable = infraerrors.New(
		500,
		"GROK_BILLING_PROBE_CAS_UNAVAILABLE",
		"Grok billing snapshot persistence is unavailable",
	)
	// ErrGrokBillingProbeIdentityChanged prevents a result fetched with one
	// OAuth identity from being stored on a replacement identity.
	ErrGrokBillingProbeIdentityChanged = infraerrors.Conflict(
		"GROK_BILLING_PROBE_IDENTITY_CHANGED",
		"Grok account identity changed during billing probe; retry the probe",
	)
)

// GrokBillingIdentity is the immutable account snapshot used by a billing
// probe. CredentialsJSON intentionally contains the complete JSONB credential
// document, not just the access token, so OAuth reauthorization and endpoint or
// header configuration changes invalidate the write as well.
type GrokBillingIdentity struct {
	Platform                  string
	Type                      string
	CredentialsJSON           string
	ProxyID                   *int64
	OAuthSubject              string
	NormalizedBaseURL         string
	TokenHash                 string
	HeaderOverrideFingerprint string
	Fingerprint               string
}

// GrokBillingProbeIdentity is retained as the explicit probe-facing name.
type GrokBillingProbeIdentity = GrokBillingIdentity

// GrokBillingExtraKey is exported for the repository's narrow CAS adapter.
const GrokBillingExtraKey = "grok_billing_snapshot"

// GrokBillingSnapshotCAS is deliberately narrower than AccountRepository.
// Billing persistence must fail closed when this capability is absent; callers
// must never fall back to UpdateExtra for this snapshot.
type GrokBillingSnapshotCAS interface {
	UpdateGrokBillingSnapshotIfIdentityUnchanged(
		context.Context,
		int64,
		GrokBillingProbeIdentity,
		*xai.BillingSummary,
	) (bool, error)
}

func buildGrokBillingIdentity(account *Account, token string) (GrokBillingIdentity, error) {
	if account == nil {
		return GrokBillingIdentity{}, errors.New("account is nil")
	}
	credentialsJSON, err := json.Marshal(account.Credentials)
	if err != nil {
		return GrokBillingIdentity{}, fmt.Errorf("marshal Grok billing credentials: %w", err)
	}

	proxyID := cloneGrokProxyID(account.ProxyID)
	subject := strings.TrimSpace(account.GetCredential("sub"))
	baseURL := strings.TrimRight(strings.TrimSpace(account.GetGrokBaseURL()), "/")
	accessToken := strings.TrimSpace(token)
	if accessToken == "" {
		accessToken = strings.TrimSpace(account.GetGrokAccessToken())
	}
	headerOverridesJSON, err := json.Marshal(account.GetHeaderOverrides())
	if err != nil {
		return GrokBillingIdentity{}, fmt.Errorf("marshal Grok billing header overrides: %w", err)
	}

	identity := GrokBillingIdentity{
		Platform:                  account.Platform,
		Type:                      account.Type,
		CredentialsJSON:           string(credentialsJSON),
		ProxyID:                   proxyID,
		OAuthSubject:              subject,
		NormalizedBaseURL:         baseURL,
		TokenHash:                 sha256Hex(accessToken),
		HeaderOverrideFingerprint: sha256Hex(string(headerOverridesJSON)),
	}
	identity.Fingerprint = sha256Hex(strings.Join([]string{
		identity.Platform,
		identity.Type,
		identity.CredentialsJSON,
		int64PointerString(identity.ProxyID),
		identity.OAuthSubject,
		identity.NormalizedBaseURL,
		identity.TokenHash,
		identity.HeaderOverrideFingerprint,
	}, "\x00"))
	return identity, nil
}

func grokBillingIdentityEqual(left, right GrokBillingProbeIdentity) bool {
	return left.Fingerprint == right.Fingerprint && left.Fingerprint != ""
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func int64PointerString(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}
