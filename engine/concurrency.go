package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bilibili/gengine/internal/base"
	"golang.org/x/sync/errgroup"

	gctx "github.com/bilibili/gengine/context"
)

// runRulesConcurrently fans out execution of rules using errgroup, honouring
// ctx cancellation between rule launches and collecting all rule errors so
// the caller receives the full picture rather than just the first failure.
// addResult is invoked for any rule that produces a returnable value.
// Panics inside a rule are recovered and turned into a regular rule error.
func runRulesConcurrently(ctx context.Context, rules []*base.RuleEntity, dc *gctx.DataContext, addResult func(name string, v interface{})) error {
	if len(rules) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	g, gctx := errgroup.WithContext(ctx)

	var mu sync.Mutex
	errs := make([]error, 0, len(rules))

	for _, r := range rules {
		r := r
		if err := gctx.Err(); err != nil {
			break
		}
		g.Go(func() (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("rule %q panicked: %v", r.RuleName, rec))
					mu.Unlock()
				}
			}()
			if err := gctx.Err(); err != nil {
				return nil
			}
			v, execErr, ok := r.Execute(dc)
			if ok {
				addResult(r.RuleName, v)
			}
			if execErr != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("rule %q executed: %w", r.RuleName, execErr))
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		errs = append(errs, ctxErr)
	}
	return errors.Join(errs...)
}
