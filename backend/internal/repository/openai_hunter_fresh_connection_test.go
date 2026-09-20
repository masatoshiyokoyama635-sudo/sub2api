package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexHunterFreshConnectionsKeepGenerationPool(t *testing.T) {
	proxy:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){ _,_=io.WriteString(w,r.RemoteAddr) }))
	defer proxy.Close()
	u:=NewHTTPUpstream(nil)
	request:=func(fresh bool)string{
		ctx:=context.Background()
		if fresh { ctx=service.WithHTTPUpstreamFreshConnection(ctx) }
		r,err:=http.NewRequestWithContext(ctx,http.MethodGet,"http://fixture.invalid/",nil); require.NoError(t,err)
		resp,err:=u.Do(r,proxy.URL,41,1);require.NoError(t,err)
		body,err:=io.ReadAll(resp.Body);require.NoError(t,err);require.NoError(t,resp.Body.Close())
		return string(body)
	}
	normal:=request(false)
	first,second:=request(true),request(true)
	require.NotEqual(t,normal,first)
	require.NotEqual(t,first,second)
	require.Equal(t,normal,request(false),"generation must retain its existing proxy connection")
}
