package com.gengine;

import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

class RuleEngineTest {

    @Test
    void salienceOrderingAndAssignment() {
        RuleEngine engine = new RuleEngine();
        List<String> log = Collections.synchronizedList(new ArrayList<>());
        engine.getContext().addFunction("log", args -> { log.add((String) args.get(0)); return null; });
        engine.getContext().add("threshold", 5L);
        engine.build("""
                rule "higher" salience 10
                begin
                    x = 5 + 3
                    if x > threshold {
                        log("high")
                    }
                end
                rule "lower" salience 1
                begin
                    log("low")
                end
                """);
        RuleEngine.Result res = engine.execute();
        assertFalse(res.hasErrors(), () -> "errors: " + res.errors);
        assertEquals(List.of("high", "low"), log);
    }

    @Test
    void ifElseAndReturnValue() {
        RuleEngine engine = new RuleEngine();
        engine.build("""
                rule "pick" salience 1
                begin
                    if 2 < 1 {
                        return "unreachable"
                    } else {
                        return "else-branch"
                    }
                end
                """);
        RuleEngine.Result res = engine.execute();
        assertFalse(res.hasErrors());
        assertEquals("else-branch", res.values.get("pick"));
    }

    @Test
    void continueOnError() {
        RuleEngine engine = new RuleEngine();
        engine.getContext().addFunction("boom", args -> { throw new RuntimeException("explode"); });
        engine.build("""
                rule "r1" salience 2 begin boom() end
                rule "r2" salience 1 begin return 42 end
                """);

        // Fail-fast: r2 should not have run.
        RuleEngine.ExecOptions optsFail = new RuleEngine.ExecOptions();
        optsFail.continueOnError = false;
        RuleEngine.Result failFast = engine.execute(optsFail);
        assertTrue(failFast.hasErrors());
        assertNull(failFast.values.get("r2"));

        // Collect: r2 ran and produced its value.
        RuleEngine.ExecOptions optsCollect = new RuleEngine.ExecOptions();
        optsCollect.continueOnError = true;
        RuleEngine.Result collect = engine.execute(optsCollect);
        assertTrue(collect.hasErrors());
        assertEquals(42L, collect.values.get("r2"));
    }

    @Test
    void concurrentFanOut() throws Exception {
        RuleEngine engine = new RuleEngine();
        List<String> seen = Collections.synchronizedList(new ArrayList<>());
        engine.getContext().addFunction("record", args -> {
            try { Thread.sleep(5); } catch (InterruptedException ignored) { }
            seen.add((String) args.get(0));
            return null;
        });
        engine.build("""
                rule "a" salience 1 begin record("a") end
                rule "b" salience 1 begin record("b") end
                rule "c" salience 1 begin record("c") end
                """);
        RuleEngine.ExecOptions opts = new RuleEngine.ExecOptions(RuleEngine.ExecMode.CONCURRENT);
        RuleEngine.Result res = engine.execute(opts);
        assertFalse(res.hasErrors(), () -> "errors: " + res.errors);
        List<String> sorted = new ArrayList<>(seen);
        Collections.sort(sorted);
        assertEquals(List.of("a", "b", "c"), sorted);
    }
}
