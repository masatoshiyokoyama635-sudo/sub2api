package reauthruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedRuntimeCustomVersionMapping(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"2.9.4", "2.9.4"}, {"v2.9.4", "2.9.4"},
		{"2.9.4-zz", "2.9.4"}, {"v2.9.4-zz", "2.9.4"},
		{"2.9.4-rc.1", "2.9.4-rc.1"}, {"2.9.4-rc.1-zz", "2.9.4-rc.1"},
		{"2.9.4-zz-next", "2.9.4-zz-next"}, {"development", "development"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			m := New(t.TempDir(), tc.input, "http://127.0.0.1:4040", "synthetic-worker-token")
			defer m.Stop()
			require.Equal(t, tc.want, m.version)
			require.Equal(t, "idle", m.Status().State)
		})
	}
}

func TestManagedRuntimeCustomVersionUsesVerifiedUpstreamAssets(t *testing.T) {
	for _, version := range []string{"2.9.4-zz", "v2.9.4-zz"} {
		t.Run(version, func(t *testing.T) {
			m := New(t.TempDir(), version, "http://127.0.0.1:4040", "synthetic-worker-token")
			defer m.Stop()
			calls := []string{}
			asset := "sub2api-reauth_2.9.4_linux_" + runtime.GOARCH + ".tar.gz"
			m.client = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls = append(calls, req.URL.String())
				raw := []byte("synthetic archive rejected before extraction or execution")
				if len(calls) == 1 {
					raw, _ = json.Marshal(map[string]any{"assets": []map[string]string{{"name": asset, "digest": "sha256:" + strings.Repeat("0", 64)}}})
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}
			_, err := m.prepare(context.Background())
			require.ErrorContains(t, err, "runtime checksum mismatch", "version adaptation must not bypass integrity verification")
			require.Equal(t, []string{
				"https://api.github.com/repos/ranxi2001/sub2api/releases/tags/v2.9.4",
				"https://github.com/ranxi2001/sub2api/releases/download/v2.9.4/" + asset,
			}, calls)
			require.False(t, m.started, "fixture must not launch a worker")
		})
	}
}
