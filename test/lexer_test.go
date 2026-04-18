package test

import (
	"strings"
	"testing"

	"github.com/bilibili/gengine/builder"
	"github.com/bilibili/gengine/context"
)

// Test_lexer asserts that the ANTLR-generated lexer rejects a rule whose
// body contains tokens that are not part of the gengine DSL grammar (in
// this case Chinese characters, chosen because they exercise multi-byte
// UTF-8 paths and are guaranteed not to match any lexer rule in
// internal/iantlr/gengine.g4).
//
// Historical note on why this test was rewritten
// ----------------------------------------------
// The previous version of Test_lexer deliberately panicked when
// BuildRuleFromString returned an error:
//
//	e1 := ruleBuilder.BuildRuleFromString(lexer_rule)
//	if e1 != nil { panic(e1) }
//
// Since an error is the *expected* outcome for this input, the test was
// guaranteed to panic on every run. `go test` treats a panic inside a
// test as a failure, so this file has failed for the lifetime of the
// repository even though the engine was behaving correctly. That made it
// impossible to use `go test ./test/...` as a health signal: a real
// regression and this intentional-panic look identical in the output.
//
// The rewrite inverts the assertion: we now *require* BuildRuleFromString
// to return an error, and we require that error to mention a lexer
// "token recognition" failure so that a future refactor that silently
// swallows lexer errors would fail this test instead of passing it.
// The engine.Execute step was removed because it was unreachable under
// the original panic and irrelevant to what the test claims to verify.
func Test_lexer(t *testing.T) {
	// Rule body `规则管理` ("rule management") is valid UTF-8 but contains
	// no tokens defined in the gengine grammar, so the lexer must reject
	// it before the parser ever runs.
	const lexerRule = `
rule "test" salience 1
begin
规则管理
end
`
	dc := context.NewDataContext()
	rb := builder.NewRuleBuilder(dc)

	err := rb.BuildRuleFromString(lexerRule)
	if err == nil {
		t.Fatal("expected BuildRuleFromString to reject non-grammar characters, got nil error")
	}

	// The error surface from GengineErrorListener aggregates each bad
	// character as its own "token recognition error". We don't pin the
	// exact count because that is an ANTLR implementation detail, but we
	// do require the canonical phrase so a future change that hides
	// lexer failures behind a generic message is caught here.
	if !strings.Contains(err.Error(), "token recognition error") {
		t.Fatalf("expected lexer error to mention 'token recognition error', got: %v", err)
	}
}
