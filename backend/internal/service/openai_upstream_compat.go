package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

// TargetsChatGPTCodexUpstream retains the upstream turn-state feature's protocol
// predicate. This fork supports OAuth/setup tokens and has no CPR account type.
func (a *Account) TargetsChatGPTCodexUpstream() bool {
	return a.IsOpenAIOAuthLike()
}

// Only an absent binding means direct access. A failed lookup must not erase
// an account's configured proxy or send a probe directly from the server.
func resolveConfiguredProxyURL(ctx context.Context, repo ProxyRepository, proxyID *int64, loaded *Proxy) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	proxy := loaded
	if proxy == nil {
		if repo == nil {
			return "", fmt.Errorf("configured proxy %d lookup is unavailable", *proxyID)
		}
		var err error
		proxy, err = repo.GetByID(ctx, *proxyID)
		if err != nil {
			return "", fmt.Errorf("configured proxy %d lookup failed: %w", *proxyID, err)
		}
	}
	if proxy == nil {
		return "", fmt.Errorf("configured proxy %d was not found", *proxyID)
	}
	if proxy.ID != 0 && proxy.ID != *proxyID {
		return "", fmt.Errorf("configured proxy %d does not match the loaded binding", *proxyID)
	}
	raw := proxy.URL()
	resolved, _, err := proxyurl.Parse(raw)
	if err != nil || resolved == "" {
		return "", fmt.Errorf("configured proxy %d has an invalid URL", *proxyID)
	}
	return raw, nil
}

func requireOpenAIProxyBinding(account *Account, proxyURL string) error {
	if account != nil && account.Platform == PlatformOpenAI && account.ProxyID != nil && strings.TrimSpace(proxyURL) == "" {
		return fmt.Errorf("configured proxy %d is unavailable", *account.ProxyID)
	}
	return nil
}
