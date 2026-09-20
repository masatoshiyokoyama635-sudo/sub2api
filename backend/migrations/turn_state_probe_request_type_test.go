package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTurnStateProbeRequestTypeMigrationPreservesExistingTypes(t *testing.T) {
	data, err := FS.ReadFile("239_allow_turn_state_probe_usage_request_type.sql")
	require.NoError(t, err)
	sql := string(data)
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS usage_logs_request_type_check")
	require.Contains(t, sql, "CHECK (request_type >= 0 AND request_type <= 6) NOT VALID")
	require.NotContains(t, sql, "UPDATE usage_logs")
}
