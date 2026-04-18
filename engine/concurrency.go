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

// runRulesConcurrently fans out execution of rules using errgroup, collecting
// all rule errors (it does not cancel siblings on first error) and returning
// them joined via errors.Join. addResult is invoked for any rule that produces
// a returnable value.
func runRulesConcurrently(rules []*base.RuleEntity, dc *gctx.DataContext, addResult func(name string, v interface{})) error {
	if len(rules) == 0 {
		return nil
	}

	g, _ := errgroup.WithContext(context.Background())

	var mu sync.Mutex
	errs := make([]error, 0, len(rules))

	for _, r := range rules {
		r := r
		g.Go(func() (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					mu.Lock()
					errs = append(errs, fmt.Errorf("rule %q panicked: %v", r.RuleName, rec))
					mu.Unlock()
				}
			}()
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

	return errors.Join(errs...)
}
