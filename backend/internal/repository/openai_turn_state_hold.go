package repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// SetCodexHunterModelHold compares the selected snapshot. A real rate limit
// installed after selection always wins over a hunter hold.
func (r *accountRepository) SetCodexHunterModelHold(ctx context.Context, id int64, model string, until time.Time, expected map[string]any) error {
	if id <= 0 || strings.TrimSpace(model) == "" || !time.Now().Before(until) {
		return nil
	}
	if len(expected) > 0 {
		raw, _ := expected["rate_limit_reset_at"].(string)
		deadline, err := time.Parse(time.RFC3339, raw)
		// Active, unknown and malformed provider limits cannot be replaced.
		if err != nil || time.Now().Before(deadline) {
			return nil
		}
	}
	previous, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{
		"rate_limited_at":     time.Now().UTC().Format(time.RFC3339),
		"rate_limit_reset_at": until.UTC().Format(time.RFC3339),
		"reason":              service.OpenAITurnStateHoldSelectionReason,
	})
	if err != nil {
		return err
	}
	return r.mutateCodexHunterModelHold(ctx, id, `UPDATE accounts SET
        extra = jsonb_set(
            jsonb_set(COALESCE(extra, '{}'::jsonb), '{model_rate_limits}'::text[], COALESCE(extra->'model_rate_limits', '{}'::jsonb), true),
            ARRAY['model_rate_limits', $2::text]::text[], $3::jsonb, true
        ), updated_at=NOW()
        WHERE id=$1 AND deleted_at IS NULL
        AND COALESCE(extra #> ARRAY['model_rate_limits', $2::text]::text[], 'null'::jsonb) = $4::jsonb`,
		id, model, string(payload), string(previous))
}

func (r *accountRepository) ReleaseCodexHunterModelHold(ctx context.Context, id int64, model string, expected time.Time) error {
	if id <= 0 || strings.TrimSpace(model) == "" || expected.IsZero() {
		return nil
	}
	return r.mutateCodexHunterModelHold(ctx, id, `UPDATE accounts SET
        extra = jsonb_set(extra, '{model_rate_limits}'::text[], (extra->'model_rate_limits') - ($2::text)), updated_at=NOW()
        WHERE id=$1 AND deleted_at IS NULL
        AND extra #>> ARRAY['model_rate_limits', $2::text, 'reason']::text[] = $3
        AND extra #>> ARRAY['model_rate_limits', $2::text, 'rate_limit_reset_at']::text[] = $4`,
		id, model, service.OpenAITurnStateHoldSelectionReason, expected.UTC().Format(time.RFC3339Nano))
}

// The account update and outbox event commit together. A failed outbox write
// must not leave peer schedulers with a hold that has already been released.
func (r *accountRepository) mutateCodexHunterModelHold(ctx context.Context, id int64, query string, args ...any) error {
	baseCtx := ctx
	existingTx := dbent.TxFromContext(ctx)
	client := clientFromContext(ctx, r.client)
	var tx *dbent.Tx
	if existingTx == nil {
		var err error
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return err
		}
		if tx != nil {
			defer func() { _ = tx.Rollback() }()
			ctx = dbent.NewTxContext(ctx, tx)
			client = tx.Client()
		}
	}
	result, err := client.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed > 0 {
		if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			return err
		}
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if changed > 0 && existingTx == nil {
		r.syncSchedulerAccountSnapshot(baseCtx, id)
	}
	return nil
}
