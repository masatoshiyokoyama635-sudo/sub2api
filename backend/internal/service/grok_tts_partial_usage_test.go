//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestProxyGrokTTSStreamConnRetainsDeliveredUsageAfterMalformedEvent(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_text_delivered", true: "text_delivered"}[delivered], func(t *testing.T) {
			stub := &grokTTSStreamUpstreamStub{toClient: make(chan []byte)}
			type proxyResult struct {
				chars int64
				err   error
			}
			results := make(chan proxyResult, 1)
			svc := &OpenAIGatewayService{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer func() { _ = conn.CloseNow() }()
				chars, err := svc.ProxyGrokTTSStreamConn(r.Context(), conn, &GrokRealtimeUpstream{conn: stub})
				results <- proxyResult{chars, err}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = client.CloseNow() }()
			if delivered {
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"text.delta","delta":"你好，world"}`)))
				require.Eventually(t, func() bool {
					stub.mu.Lock()
					defer stub.mu.Unlock()
					return len(stub.written) == 1
				}, time.Second, time.Millisecond)
			}
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{`)))
			select {
			case result := <-results:
				require.ErrorContains(t, result.err, "invalid tts stream event")
				require.Equal(t, map[bool]int64{false: 0, true: 8}[delivered], result.chars)
				stub.mu.Lock()
				defer stub.mu.Unlock()
				require.Len(t, stub.written, map[bool]int{false: 0, true: 1}[delivered], "the malformed event must never reach upstream")
			case <-ctx.Done():
				t.Fatal("TTS relay did not finish")
			}
		})
	}
}
