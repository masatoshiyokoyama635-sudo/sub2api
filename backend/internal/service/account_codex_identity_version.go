package service

import (
	"maps"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const codexIdentityVersionExtraKey = "codex_identity_version"

// GetCodexIdentityVersion keeps existing accounts on the original identity
// algorithm. Only an exact, explicit v2 value enables the new algorithm.
// Callers handling credential shadows must resolve the credential account first.
func (a *Account) GetCodexIdentityVersion() string {
	if a != nil && a.IsOpenAIOAuthLike() {
		if version, ok := a.Extra[codexIdentityVersionExtraKey].(string); ok && version == "v2" {
			return "v2"
		}
	}
	return "v1"
}

func codexIdentityV2Enabled(account *Account) bool {
	return account.GetCodexIdentityVersion() == "v2"
}

func validateCodexIdentityVersionExtra(extra map[string]any) error {
	value, provided := extra[codexIdentityVersionExtraKey]
	if !provided {
		return nil
	}
	version, ok := value.(string)
	if !ok || (version != "v1" && version != "v2") {
		return infraerrors.BadRequest("INVALID_CODEX_IDENTITY_VERSION", "codex_identity_version must be v1 or v2")
	}
	return nil
}

func validateCodexIdentityVersionTarget(account *Account, extra map[string]any) error {
	if err := validateCodexIdentityVersionExtra(extra); err != nil {
		return err
	}
	if _, provided := extra[codexIdentityVersionExtraKey]; !provided {
		return nil
	}
	if account == nil || !account.IsOpenAIOAuthLike() {
		return infraerrors.BadRequest("CODEX_IDENTITY_VERSION_ACCOUNT_INVALID", "codex_identity_version requires an OpenAI OAuth or setup-token account")
	}
	if account.IsCredentialShadow() {
		return infraerrors.BadRequest("CODEX_IDENTITY_VERSION_INHERITED", "Codex identity version is inherited from the credential account; edit the parent account")
	}
	return nil
}

// An omitted field is not a request to migrate an account. This also protects
// existing v2 accounts edited by older clients that send a full extra object.
func preserveCodexIdentityVersionForUpdate(account *Account, extra map[string]any) map[string]any {
	if account == nil {
		return extra
	}
	if _, provided := extra[codexIdentityVersionExtraKey]; provided {
		return extra
	}
	current, exists := account.Extra[codexIdentityVersionExtraKey]
	if !exists {
		return extra
	}
	prepared := maps.Clone(extra)
	if prepared == nil {
		prepared = make(map[string]any, 1)
	}
	prepared[codexIdentityVersionExtraKey] = current
	return prepared
}
