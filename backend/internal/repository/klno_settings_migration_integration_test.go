//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestKlnoSettingsMigrationPreservesExplicitSettingsAndRollback(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationEntClient.Tx(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	client := tx.Client()
	for _, tc := range []struct {
		name, platform, mode string
		explicit             bool
	}{
		{"reuse", "openai", "reuse", false},
		{"observe", "openai", "observe", false},
		{"off", "openai", "off", false},
		{"explicit", "openai", "reuse", true},
		{"other_platform", "anthropic", "reuse", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := map[string]any{"codex_identity_version": "v2", "codex_turn_state_mode": tc.mode, "custom_setting": "preserved", "codex_fingerprint_mode": "device"}
			if tc.explicit {
				extra["codex_experimental_fingerprint_convergence"] = false
				extra["openai_turn_state_auto"] = false
			}
			row, err := client.Account.Create().SetName("klno-migration-" + tc.name).SetPlatform(tc.platform).SetType("oauth").SetCredentials(map[string]any{"access_token": "fixture"}).SetExtra(extra).Save(ctx)
			require.NoError(t, err)
			sql, err := migrations.FS.ReadFile("241_adopt_klno_codex_settings.sql")
			require.NoError(t, err)
			_, err = client.ExecContext(ctx, string(sql))
			require.NoError(t, err)
			updated, err := client.Account.Get(ctx, row.ID)
			require.NoError(t, err)
			require.Equal(t, "v2", updated.Extra["codex_identity_version"], "previous image keeps its own configuration")
			require.Equal(t, tc.mode, updated.Extra["codex_turn_state_mode"])
			require.Equal(t, "preserved", updated.Extra["custom_setting"])
			require.Nil(t, updated.ProxyID, "migration does not rebind the normal request proxy")
			if tc.platform != "openai" {
				require.Equal(t, extra, updated.Extra)
			} else {
				require.Equal(t, !tc.explicit, updated.Extra["codex_experimental_fingerprint_convergence"])
				require.Equal(t, tc.mode == "reuse" && !tc.explicit, updated.Extra["openai_turn_state_auto"])
			}
		})
	}
}
