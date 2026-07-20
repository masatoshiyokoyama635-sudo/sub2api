package securityaudit

import (
	"context"
	"errors"
)

type promptLifecycleState uint8

const (
	promptLifecycleNew promptLifecycleState = iota
	promptLifecycleStarting
	promptLifecycleRunning
	promptLifecycleStopping
	promptLifecycleStopped
)

// ErrPromptAuditNotRestartable is returned when a component is started after
// shutdown has begun or completed. Prompt Audit components are single-run
// lifecycles; restarting one would leave dependencies from the previous run
// racing with the new run.
var ErrPromptAuditNotRestartable = errors.New("prompt audit component cannot be restarted")

func closedLifecycleChannel() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func waitPromptLifecycle(ctx context.Context, done <-chan struct{}, result func() error) error {
	if done == nil {
		return result()
	}
	// Prefer an already completed lifecycle over a caller context that is also
	// done. The component's saved shutdown result is authoritative once drain
	// completion has been published.
	select {
	case <-done:
		return result()
	default:
	}
	select {
	case <-done:
		return result()
	case <-ctx.Done():
		// Completion may have raced with context cancellation after the select was
		// armed. Check it once more before returning the waiter's context error.
		select {
		case <-done:
			return result()
		default:
			return ctx.Err()
		}
	}
}
