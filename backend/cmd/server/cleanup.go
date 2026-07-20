package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

type cleanupStep struct {
	name string
	fn   func() error
}

func runParallelCleanupSteps(steps []cleanupStep) error {
	errs := make([]error, len(steps))
	var wg sync.WaitGroup
	for i := range steps {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = runCleanupStep(steps[i])
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

func runSequentialCleanupSteps(ctx context.Context, steps []cleanupStep) error {
	var errs []error
	for i := range steps {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("cleanup context expired before infrastructure shutdown: %w", err))
			break
		}
		if err := runCleanupStep(steps[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func runCleanupStep(step cleanupStep) error {
	if err := step.fn(); err != nil {
		wrapped := fmt.Errorf("%s: %w", step.name, err)
		log.Printf("[Cleanup] %v", wrapped)
		return wrapped
	}
	log.Printf("[Cleanup] %s succeeded", step.name)
	return nil
}
