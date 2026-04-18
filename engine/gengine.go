package engine

import (
	"context"
	"sync"

	"github.com/bilibili/gengine/builder"
)

// Gengine executes a compiled rule set against a DataContext. Concurrent
// callers should share one Gengine per in-flight execution; the pool type
// (GenginePool) manages this automatically.
type Gengine struct {
	lock         sync.Mutex
	returnResult map[string]interface{}
}

func NewGengine() *Gengine {
	return &Gengine{}
}

// Stag is a user-owned stop flag that a rule (or caller) can flip to
// terminate a serial execution loop early.
type Stag struct {
	StopTag bool
}

func (g *Gengine) addResult(name string, returnResult interface{}) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.returnResult[name] = returnResult
}

// GetRulesResultMap returns a snapshot of the values rules returned on the
// most recent execution. The returned map is a copy, safe to read while the
// engine runs again.
func (g *Gengine) GetRulesResultMap() (map[string]interface{}, error) {
	g.lock.Lock()
	defer g.lock.Unlock()
	out := make(map[string]interface{}, len(g.returnResult))
	for k, v := range g.returnResult {
		out[k] = v
	}
	return out, nil
}

// ============================================================================
// Context-aware, semantically precise entry points. Prefer these in new code.
// ============================================================================

// ExecuteContext runs the rule set serially in salience-descending order.
// If continueOnErr is true, execution proceeds past failing rules and all
// errors are joined in the returned error; otherwise it stops at the first
// failure. ctx is checked between rules.
func (g *Gengine) ExecuteContext(ctx context.Context, rb *builder.RuleBuilder, continueOnErr bool) error {
	return g.ExecuteOpts(ctx, rb, ExecOptions{Mode: ModeSort, ContinueOnError: continueOnErr})
}

// ExecuteWithStopTagContext is ExecuteContext plus a user stop flag; setting
// sTag.StopTag=true from inside a rule ends the loop early.
func (g *Gengine) ExecuteWithStopTagContext(ctx context.Context, rb *builder.RuleBuilder, continueOnErr bool, sTag *Stag) error {
	return g.ExecuteOpts(ctx, rb, ExecOptions{Mode: ModeSort, ContinueOnError: continueOnErr, StopTag: sTag})
}

// ExecuteConcurrentContext runs all rules in parallel, ignoring salience.
// Panics are recovered and reported; ctx cancellation stops new launches.
func (g *Gengine) ExecuteConcurrentContext(ctx context.Context, rb *builder.RuleBuilder) error {
	return g.ExecuteOpts(ctx, rb, ExecOptions{Mode: ModeConcurrent})
}

// ExecuteMixModelContext runs the top-salience rule serially (fail-fast),
// then fans the rest out concurrently.
func (g *Gengine) ExecuteMixModelContext(ctx context.Context, rb *builder.RuleBuilder) error {
	return g.ExecuteOpts(ctx, rb, ExecOptions{Mode: ModeMix})
}

// ExecuteMixModelWithStopTagContext is ExecuteMixModelContext with a stop
// flag that short-circuits the concurrent phase when set after the first rule.
func (g *Gengine) ExecuteMixModelWithStopTagContext(ctx context.Context, rb *builder.RuleBuilder, sTag *Stag) error {
	return g.ExecuteOpts(ctx, rb, ExecOptions{Mode: ModeMix, StopTag: sTag})
}

// ExecuteDAGModelContext runs rules row-by-row: each row runs concurrently,
// rows execute in order, execution stops at the first row that errors.
func (g *Gengine) ExecuteDAGModelContext(ctx context.Context, rb *builder.RuleBuilder, dag [][]string) error {
	return g.ExecuteOpts(ctx, rb, ExecOptions{Mode: ModeDAG, DAG: dag})
}

// ============================================================================
// Legacy entry points. Kept as thin shims so existing callers keep working;
// all now route through ExecuteOpts. New code should use the *Context or
// ExecuteOpts APIs above/in exec_opts.go.
// ============================================================================

// Execute — Deprecated: use ExecuteContext or ExecuteOpts.
func (g *Gengine) Execute(rb *builder.RuleBuilder, b bool) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeSort, ContinueOnError: b})
}

// ExecuteWithStopTagDirect — Deprecated: use ExecuteWithStopTagContext.
func (g *Gengine) ExecuteWithStopTagDirect(rb *builder.RuleBuilder, b bool, sTag *Stag) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeSort, ContinueOnError: b, StopTag: sTag})
}

// ExecuteConcurrent — Deprecated: use ExecuteConcurrentContext.
func (g *Gengine) ExecuteConcurrent(rb *builder.RuleBuilder) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeConcurrent})
}

// ExecuteMixModel — Deprecated: use ExecuteMixModelContext.
func (g *Gengine) ExecuteMixModel(rb *builder.RuleBuilder) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeMix})
}

// ExecuteMixModelWithStopTagDirect — Deprecated: use ExecuteMixModelWithStopTagContext.
func (g *Gengine) ExecuteMixModelWithStopTagDirect(rb *builder.RuleBuilder, sTag *Stag) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeMix, StopTag: sTag})
}

// ExecuteSelectedRules — Deprecated: use ExecuteOpts with Selected + ContinueOnError.
func (g *Gengine) ExecuteSelectedRules(rb *builder.RuleBuilder, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeSort, Selected: names, ContinueOnError: true})
}

// ExecuteSelectedRulesWithControl — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedRulesWithControl(rb *builder.RuleBuilder, b bool, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeSort, Selected: names, ContinueOnError: b})
}

// ExecuteSelectedRulesWithControlAsGivenSortedName — Deprecated: use ExecuteOpts with PreserveOrder.
func (g *Gengine) ExecuteSelectedRulesWithControlAsGivenSortedName(rb *builder.RuleBuilder, b bool, sortedNames []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeSort, Selected: sortedNames, PreserveOrder: true, ContinueOnError: b})
}

// ExecuteSelectedRulesWithControlAndStopTag — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedRulesWithControlAndStopTag(rb *builder.RuleBuilder, b bool, sTag *Stag, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeSort, Selected: names, ContinueOnError: b, StopTag: sTag})
}

// ExecuteSelectedRulesWithControlAndStopTagAsGivenSortedName — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedRulesWithControlAndStopTagAsGivenSortedName(rb *builder.RuleBuilder, b bool, sTag *Stag, sortedNames []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeSort, Selected: sortedNames, PreserveOrder: true, ContinueOnError: b, StopTag: sTag})
}

// ExecuteSelectedRulesConcurrent — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedRulesConcurrent(rb *builder.RuleBuilder, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeConcurrent, Selected: names})
}

// ExecuteSelectedRulesMixModel — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedRulesMixModel(rb *builder.RuleBuilder, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeMix, Selected: names})
}

// ExecuteInverseMixModel — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteInverseMixModel(rb *builder.RuleBuilder) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeInverseMix})
}

// ExecuteSelectedRulesInverseMixModel — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedRulesInverseMixModel(rb *builder.RuleBuilder, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeInverseMix, Selected: names})
}

// ExecuteNSortMConcurrent — Deprecated: use ExecuteOpts with Mode=ModeNSortMConcurrent.
func (g *Gengine) ExecuteNSortMConcurrent(nSort, mConcurrent int, rb *builder.RuleBuilder, b bool) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeNSortMConcurrent, N: nSort, M: mConcurrent, ContinueOnError: b})
}

// ExecuteNConcurrentMSort — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteNConcurrentMSort(nConcurrent, mSort int, rb *builder.RuleBuilder, b bool) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeNConcurrentMSort, N: nConcurrent, M: mSort, ContinueOnError: b})
}

// ExecuteNConcurrentMConcurrent — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteNConcurrentMConcurrent(nConcurrent, mConcurrent int, rb *builder.RuleBuilder, b bool) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeNConcurrentMConcurrent, N: nConcurrent, M: mConcurrent, ContinueOnError: b})
}

// ExecuteSelectedNSortMConcurrent — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedNSortMConcurrent(nSort, mConcurrent int, rb *builder.RuleBuilder, b bool, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeNSortMConcurrent, N: nSort, M: mConcurrent, ContinueOnError: b, Selected: names})
}

// ExecuteSelectedNConcurrentMSort — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedNConcurrentMSort(nConcurrent, mSort int, rb *builder.RuleBuilder, b bool, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeNConcurrentMSort, N: nConcurrent, M: mSort, ContinueOnError: b, Selected: names})
}

// ExecuteSelectedNConcurrentMConcurrent — Deprecated: use ExecuteOpts.
func (g *Gengine) ExecuteSelectedNConcurrentMConcurrent(nConcurrent, mConcurrent int, rb *builder.RuleBuilder, b bool, names []string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeNConcurrentMConcurrent, N: nConcurrent, M: mConcurrent, ContinueOnError: b, Selected: names})
}

// ExecuteDAGModel — Deprecated: use ExecuteDAGModelContext.
func (g *Gengine) ExecuteDAGModel(rb *builder.RuleBuilder, dag [][]string) error {
	return g.ExecuteOpts(context.Background(), rb, ExecOptions{Mode: ModeDAG, DAG: dag})
}
