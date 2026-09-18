package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexTurnStateAdminStub struct {
	*stubAdminService
	account *service.Account
	err     error
	gotID   int64
}

func (s *codexTurnStateAdminStub) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	s.gotID = id
	return s.account, s.err
}

type codexTurnStateObserverStub struct {
	status         service.CodexTurnStateStatus
	statusAccount  *service.Account
	clearedAccount *service.Account
}

func (s *codexTurnStateObserverStub) CodexTurnStateStatus(_ context.Context, account *service.Account) service.CodexTurnStateStatus {
	s.statusAccount = account
	return s.status
}

func (s *codexTurnStateObserverStub) ClearCodexTurnState(_ context.Context, account *service.Account) {
	s.clearedAccount = account
	s.status.Models = nil
}

func codexTurnStateAdminRouter(admin *codexTurnStateAdminStub, observer codexTurnStateObserver) *gin.Engine {
	h := NewAccountHandler(admin, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h.codexTurnState = observer
	router := gin.New()
	router.GET("/accounts/:id/codex-turn-state", h.GetCodexTurnState)
	router.DELETE("/accounts/:id/codex-turn-state", h.ClearCodexTurnState)
	return router
}

func TestCodexTurnStateAdminMetadataAndClear(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := &service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "private-bearer", "email": "private@example.invalid", "chatgpt_account_id": "private-workspace"},
		Extra:       map[string]any{"codex_identity_version": "v2", "codex_turn_state_mode": "observe"},
	}
	admin := &codexTurnStateAdminStub{stubAdminService: newStubAdminService(), account: account}
	observer := &codexTurnStateObserverStub{}
	require.NoError(t, json.Unmarshal([]byte(`{"mode":"observe","identity_version":"v2","candidate_lengths":[292,332],"process_local":true,"models":[{"model":"gpt-team","observed_count":2,"last_observed_at":"2026-09-18T12:00:00Z","lengths":[{"length":332,"count":2}],"other_length_count":0,"candidate":{"hash_prefix":"abcdef012345","length":332,"issued_at":"2026-09-18T11:55:00Z","expires_at":"2026-09-18T12:55:00Z","last_observed_at":"2026-09-18T12:00:00Z","observed_count":2,"reuse_count":0}}]}`), &observer.status))
	router := codexTurnStateAdminRouter(admin, observer)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/7/codex-turn-state", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, int64(7), admin.gotID)
	require.Same(t, account, observer.statusAccount)
	require.Contains(t, recorder.Body.String(), `"length":332`)
	require.Contains(t, recorder.Body.String(), `"hash_prefix":"abcdef012345"`)
	for _, secret := range []string{"private-bearer", "private@example.invalid", "private-workspace", "access_token", "credentials", `"value"`} {
		require.NotContains(t, recorder.Body.String(), secret)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/accounts/7/codex-turn-state", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Same(t, account, observer.clearedAccount)
	require.NotContains(t, recorder.Body.String(), "abcdef012345")
	require.Equal(t, "observe", account.Extra["codex_turn_state_mode"])
	require.Equal(t, "private-bearer", account.Credentials["access_token"])
}

func TestCodexTurnStateAdminRejectsInvalidAccountsWithoutCacheAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		id      string
		account *service.Account
		err     error
		status  int
	}{
		{name: "invalid", id: "invalid", status: http.StatusBadRequest},
		{name: "zero", id: "0", status: http.StatusBadRequest},
		{name: "negative", id: "-1", status: http.StatusBadRequest},
		{name: "missing", id: "7", status: http.StatusNotFound},
		{name: "repository error", id: "7", err: infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found"), status: http.StatusNotFound},
		{name: "api key", id: "7", account: &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, status: http.StatusBadRequest},
		{name: "agent identity", id: "7", account: &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"auth_mode": service.OpenAIAuthModeAgentIdentity}}, status: http.StatusBadRequest},
		{name: "other platform", id: "7", account: &service.Account{ID: 7, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}, status: http.StatusBadRequest},
	} {
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				admin := &codexTurnStateAdminStub{stubAdminService: newStubAdminService(), account: tc.account, err: tc.err}
				observer := &codexTurnStateObserverStub{}
				router := codexTurnStateAdminRouter(admin, observer)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(method, "/accounts/"+tc.id+"/codex-turn-state", nil))
				require.Equal(t, tc.status, recorder.Code)
				require.Nil(t, observer.statusAccount)
				require.Nil(t, observer.clearedAccount)
			})
		}
	}
}

func TestCodexTurnStateAdminSupportsSetupTokenAndShadowObservations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parentID := int64(42)
	for _, tc := range []struct {
		name    string
		account *service.Account
	}{
		{name: "setup token", account: &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeSetupToken}},
		{name: "credential shadow", account: &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ParentAccountID: &parentID}},
	} {
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				admin := &codexTurnStateAdminStub{stubAdminService: newStubAdminService(), account: tc.account}
				observer := &codexTurnStateObserverStub{status: service.CodexTurnStateStatus{Mode: "observe", IdentityVersion: "v2", ProcessLocal: true}}
				router := codexTurnStateAdminRouter(admin, observer)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(method, "/accounts/7/codex-turn-state", nil))
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), `"mode":"observe"`)
				// Preserve the selected account so the service can resolve parent
				// credentials while keeping the shadow's own identity configuration.
				require.Same(t, tc.account, observer.statusAccount)
				if method == http.MethodDelete {
					require.Same(t, tc.account, observer.clearedAccount)
				}
			})
		}
	}
}

func TestCodexTurnStateAdminUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	admin := &codexTurnStateAdminStub{stubAdminService: newStubAdminService(), account: &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}}
	router := codexTurnStateAdminRouter(admin, nil)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, "/accounts/7/codex-turn-state", nil))
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	}
}
