package service

import (
	"context"

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
