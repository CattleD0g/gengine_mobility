package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/bilibili/gengine/builder"
	"github.com/bilibili/gengine/internal/base"
)

// ExecMode selects how ExecuteOpts runs the (optionally selected) rule set.
// Existing named Execute* methods are preserved as thin shims that translate
// their arguments into an ExecOptions value.
type ExecMode int

const (
	// ModeSort (default) runs the rules serially in salience-descending order.
	ModeSort ExecMode = iota
	// ModeConcurrent runs all rules in parallel; salience is ignored.
	ModeConcurrent
	// ModeMix runs the highest-salience rule first (serial, fail-fast), then
	// the rest concurrently.
	ModeMix
	// ModeInverseMix runs all-but-last rules concurrently, then the last rule
	// (lowest salience) serially. With <=2 rules it degenerates to serial.
	ModeInverseMix
	// ModeDAG runs rules row-by-row; each row's rules run concurrently and
	// rows execute in order. Requires ExecOptions.DAG.
	ModeDAG
	// ModeNSortMConcurrent runs the first ExecOptions.N rules serially in
	// salience order, then the next ExecOptions.M rules concurrently.
	ModeNSortMConcurrent
	// ModeNConcurrentMSort runs the first ExecOptions.N rules concurrently,
	// then the next ExecOptions.M rules serially.
	ModeNConcurrentMSort
	// ModeNConcurrentMConcurrent runs two consecutive concurrent batches of
	// sizes ExecOptions.N and ExecOptions.M.
	ModeNConcurrentMConcurrent
)

// ExecOptions bundles the parameters that used to be spread across 22
// Execute* method variants. Zero value = ModeSort, all rules, fail-fast.
type ExecOptions struct {
	// Mode selects the execution strategy. Zero value is ModeSort.
	Mode ExecMode

	// Selected restricts the run to the named rules. Nil/empty means
	// "all rules registered on the builder".
	Selected []string

	// PreserveOrder, when true with Selected set, runs the listed rules in
	// the exact caller-provided order instead of re-sorting by salience.
	PreserveOrder bool

	// ContinueOnError changes fail-fast paths (serial Execute, N-M boundary
	// crossings) into collect-all paths. For pure concurrent modes the flag
	// is ignored — they always collect.
	ContinueOnError bool

	// StopTag, when non-nil, is checked after each rule in serial phases;
	// setting StopTag.StopTag=true inside a rule terminates the loop early.
	// Only honoured in serial-capable modes (Sort, Mix).
	StopTag *Stag

	// N and M size the two batches used by the ModeN* variants.
	N int
	M int

	// DAG is the rule layout for ModeDAG: one slice per layer, each layer
	// runs its rule names concurrently before moving to the next layer.
	DAG [][]string
}

// ExecuteOpts is the single primary execution entry point. All the named
// Execute* methods on Gengine and GenginePool now funnel through here.
func (g *Gengine) ExecuteOpts(ctx context.Context, rb *builder.RuleBuilder, opts ExecOptions) error {
	if rb == nil {
		return errors.New("ruleBuilder is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	g.returnResult = make(map[string]interface{})

	switch opts.Mode {
	case ModeDAG:
		return g.execDAG(ctx, rb, opts.DAG)
	case ModeNSortMConcurrent, ModeNConcurrentMSort, ModeNConcurrentMConcurrent:
		rules, err := resolveRules(rb, opts)
		if err != nil {
			return err
		}
		if opts.N <= 0 {
			return fmt.Errorf("ExecuteOpts: N must be > 0, got %d", opts.N)
		}
		if opts.M <= 0 {
			return fmt.Errorf("ExecuteOpts: M must be > 0, got %d", opts.M)
		}
		if opts.N+opts.M > len(rules) {
			return fmt.Errorf("ExecuteOpts: N+M (%d) exceeds rule count (%d)", opts.N+opts.M, len(rules))
		}
		nRules := rules[:opts.N]
		mRules := rules[opts.N : opts.N+opts.M]
		switch opts.Mode {
		case ModeNSortMConcurrent:
			return g.execNSortM(ctx, rb, nRules, mRules, opts.ContinueOnError, true /*mConcurrent*/)
		case ModeNConcurrentMSort:
			return g.execNConcurrentM(ctx, rb, nRules, mRules, opts.ContinueOnError, false /*mConcurrent*/)
		case ModeNConcurrentMConcurrent:
			return g.execNConcurrentM(ctx, rb, nRules, mRules, opts.ContinueOnError, true)
		}
	}

	rules, err := resolveRules(rb, opts)
	if err != nil {
		return err
	}

	switch opts.Mode {
	case ModeSort:
		return g.execSort(ctx, rb, rules, opts.ContinueOnError, opts.StopTag)
	case ModeConcurrent:
		return runRulesConcurrently(ctx, rules, rb.Dc, g.addResult)
	case ModeMix:
		return g.execMix(ctx, rb, rules, opts.StopTag)
	case ModeInverseMix:
		return g.execInverseMix(ctx, rb, rules)
	default:
		return fmt.Errorf("ExecuteOpts: unknown mode %d", opts.Mode)
	}
}

// resolveRules expands ExecOptions.Selected + PreserveOrder into the ordered
// slice of rule entities to execute. With no Selected list it returns the
// pre-sorted (salience-descending) rule slice from the builder.
func resolveRules(rb *builder.RuleBuilder, opts ExecOptions) ([]*base.RuleEntity, error) {
	if len(opts.Selected) == 0 {
		if len(rb.Kc.SortRules) == 0 {
			return nil, errors.New("no rule has been injected into engine")
		}
		return rb.Kc.SortRules, nil
	}

	if len(rb.Kc.RuleEntities) == 0 {
		return nil, errors.New("no rule has been injected into engine")
	}

	rules := make([]*base.RuleEntity, 0, len(opts.Selected))
	for _, name := range opts.Selected {
		if r, ok := rb.Kc.RuleEntities[name]; ok {
			rules = append(rules, r)
		}
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("no rules matched Selected=%v", opts.Selected)
	}
	if !opts.PreserveOrder && len(rules) > 1 {
		sort.SliceStable(rules, func(i, j int) bool {
			return rules[i].Salience > rules[j].Salience
		})
	}
	return rules, nil
}

func (g *Gengine) execSort(ctx context.Context, rb *builder.RuleBuilder, rules []*base.RuleEntity, continueOnErr bool, sTag *Stag) error {
	var eMsg []string
	for _, r := range rules {
		if err := ctx.Err(); err != nil {
			eMsg = append(eMsg, err.Error())
			break
		}
		v, err, bx := r.Execute(rb.Dc)
		if bx {
			g.addResult(r.RuleName, v)
		}
		if err != nil {
			if continueOnErr {
				eMsg = append(eMsg, fmt.Sprintf("rule: %q executed, error: %+v", r.RuleName, err))
			} else {
				return fmt.Errorf("rule: %q executed: %w", r.RuleName, err)
			}
		}
		if sTag != nil && sTag.StopTag {
			break
		}
	}
	if len(eMsg) > 0 {
		return errors.New(fmt.Sprintf("%+v", eMsg))
	}
	return nil
}

func (g *Gengine) execMix(ctx context.Context, rb *builder.RuleBuilder, rules []*base.RuleEntity, sTag *Stag) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(rules) == 0 {
		return errors.New("no rule has been injected into engine")
	}
	v, e, bx := rules[0].Execute(rb.Dc)
	if bx {
		g.addResult(rules[0].RuleName, v)
	}
	if e != nil {
		return fmt.Errorf("the most high priority rule: %q executed: %w", rules[0].RuleName, e)
	}
	if sTag != nil && sTag.StopTag {
		return nil
	}
	return runRulesConcurrently(ctx, rules[1:], rb.Dc, g.addResult)
}

func (g *Gengine) execInverseMix(ctx context.Context, rb *builder.RuleBuilder, rules []*base.RuleEntity) error {
	if len(rules) == 0 {
		return errors.New("no rule has been injected into engine")
	}
	if len(rules) <= 2 {
		// All serial, collect errors.
		return g.execSort(ctx, rb, rules, true, nil)
	}
	// Run all-but-last concurrently, collecting errors.
	concErr := runRulesConcurrently(ctx, rules[:len(rules)-1], rb.Dc, g.addResult)

	// Run the last rule serially; its error is reported fail-fast.
	last := rules[len(rules)-1]
	if err := ctx.Err(); err != nil {
		if concErr != nil {
			return errors.Join(concErr, err)
		}
		return err
	}
	v, lastErr, bx := last.Execute(rb.Dc)
	if bx {
		g.addResult(last.RuleName, v)
	}
	if lastErr != nil {
		if concErr != nil {
			return errors.Join(concErr, fmt.Errorf("rule: %q executed: %w", last.RuleName, lastErr))
		}
		return fmt.Errorf("rule: %q executed: %w", last.RuleName, lastErr)
	}
	return concErr
}

func (g *Gengine) execDAG(ctx context.Context, rb *builder.RuleBuilder, dag [][]string) error {
	if len(dag) == 0 {
		return nil
	}
	for i := 0; i < len(dag); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rules := make([]*base.RuleEntity, 0, len(dag[i]))
		for j := 0; j < len(dag[i]); j++ {
			if rule, ok := rb.Kc.RuleEntities[dag[i][j]]; ok {
				rules = append(rules, rule)
			}
		}
		if err := runRulesConcurrently(ctx, rules, rb.Dc, g.addResult); err != nil {
			return err
		}
	}
	return nil
}

// execNSortM runs an N-serial, M-concurrent pattern. continueOnErr applies to
// the serial phase: false means return on the first rule error; true means
// collect and proceed. The concurrent phase always collects.
func (g *Gengine) execNSortM(ctx context.Context, rb *builder.RuleBuilder, nRules, mRules []*base.RuleEntity, continueOnErr, mConcurrent bool) error {
	_ = mConcurrent // variant reserved for symmetry; M is always concurrent here
	var eMsg []string
	for _, r := range nRules {
		if err := ctx.Err(); err != nil {
			eMsg = append(eMsg, err.Error())
			break
		}
		v, err, bx := r.Execute(rb.Dc)
		if bx {
			g.addResult(r.RuleName, v)
		}
		if err != nil {
			if continueOnErr {
				eMsg = append(eMsg, fmt.Sprintf("rule: %q executed, error: %+v", r.RuleName, err))
			} else {
				return fmt.Errorf("rule: %q executed: %w", r.RuleName, err)
			}
		}
	}
	if err := runRulesConcurrently(ctx, mRules, rb.Dc, g.addResult); err != nil {
		eMsg = append(eMsg, err.Error())
	}
	if len(eMsg) > 0 {
		return errors.New(fmt.Sprintf("%+v", eMsg))
	}
	return nil
}

// execNConcurrentM runs an N-concurrent-then-M pattern. continueOnErr, when
// false, short-circuits after the N phase if any rule errored. mConcurrent
// chooses whether M runs in parallel or serially.
func (g *Gengine) execNConcurrentM(ctx context.Context, rb *builder.RuleBuilder, nRules, mRules []*base.RuleEntity, continueOnErr, mConcurrent bool) error {
	concErr := runRulesConcurrently(ctx, nRules, rb.Dc, g.addResult)
	if !continueOnErr && concErr != nil {
		return concErr
	}
	if mConcurrent {
		mErr := runRulesConcurrently(ctx, mRules, rb.Dc, g.addResult)
		if concErr != nil && mErr != nil {
			return errors.Join(concErr, mErr)
		}
		if concErr != nil {
			return concErr
		}
		return mErr
	}
	// M-serial with continueOnErr semantics.
	var eMsg []string
	if concErr != nil {
		eMsg = append(eMsg, concErr.Error())
	}
	for _, r := range mRules {
		if err := ctx.Err(); err != nil {
			eMsg = append(eMsg, err.Error())
			break
		}
		v, err, bx := r.Execute(rb.Dc)
		if bx {
			g.addResult(r.RuleName, v)
		}
		if err != nil {
			if continueOnErr {
				eMsg = append(eMsg, fmt.Sprintf("rule: %q executed, error: %+v", r.RuleName, err))
			} else {
				return fmt.Errorf("rule: %q executed: %w", r.RuleName, err)
			}
		}
	}
	if len(eMsg) > 0 {
		return errors.New(fmt.Sprintf("%+v", eMsg))
	}
	return nil
}

