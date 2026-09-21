-- Migrate the retired local v2 selectors to the upstream klno switches.
-- Keep the old keys inert so rolling back to the previous image still reads
-- its original configuration. Explicit upstream settings always take priority.
UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb)
    || CASE WHEN NOT (COALESCE(extra, '{}'::jsonb) ? 'codex_experimental_fingerprint_convergence')
            THEN jsonb_build_object('codex_experimental_fingerprint_convergence', true)
            ELSE '{}'::jsonb END
    || CASE WHEN NOT (COALESCE(extra, '{}'::jsonb) ? 'openai_turn_state_auto')
            THEN jsonb_build_object('openai_turn_state_auto', COALESCE(extra->>'codex_turn_state_mode', 'off') = 'reuse')
            ELSE '{}'::jsonb END,
    updated_at = NOW()
WHERE deleted_at IS NULL
    AND platform = 'openai'
    AND type IN ('oauth', 'setup-token')
    AND extra->>'codex_identity_version' = 'v2';
