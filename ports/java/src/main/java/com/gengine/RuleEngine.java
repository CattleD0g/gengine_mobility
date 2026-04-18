package com.gengine;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.function.Function;

/**
 * Rule engine for the Java port. Mirrors the Go package's Gengine +
 * ExecuteOpts shape: a single {@link #execute(ExecOptions)} entry point
 * plus {@link ExecMode} and {@link ExecOptions}.
 */
public final class RuleEngine {

    public enum ExecMode { SORT, CONCURRENT }

    public static final class ExecOptions {
        public ExecMode mode = ExecMode.SORT;
        public boolean continueOnError = false;
        public List<String> selected = null;
        public int maxWorkers = 8;

        public ExecOptions() { }
        public ExecOptions(ExecMode mode) { this.mode = mode; }
    }

    public static final class Result {
        public final Map<String, Object> values;
        public final List<Throwable> errors;
        public Result(Map<String, Object> values, List<Throwable> errors) {
            this.values = values;
            this.errors = errors;
        }
        public boolean hasErrors() { return !errors.isEmpty(); }
    }

    /**
     * Context holds user-registered variables and functions. Uses
     * ConcurrentHashMap so concurrent rule evaluation does not need an
     * external lock for well-behaved accesses.
     */
    public static final class Context {
        private final Map<String, Object> variables = new ConcurrentHashMap<>();
        private final Map<String, Function<List<Object>, Object>> functions = new ConcurrentHashMap<>();

        public void add(String name, Object value) { variables.put(name, value); }
        public void addFunction(String name, Function<List<Object>, Object> fn) { functions.put(name, fn); }

        Object get(String name) {
            if (!variables.containsKey(name)) throw new RuntimeException("unknown variable '" + name + "'");
            return variables.get(name);
        }
        void set(String name, Object value) { variables.put(name, value); }

        Object call(String name, List<Object> args) {
            Function<List<Object>, Object> fn = functions.get(name);
            if (fn == null) throw new RuntimeException("unknown function '" + name + "'");
            return fn.apply(args);
        }
    }

    // ------------------------------------------------------------------

    private final Context context = new Context();
    private List<Ast.Rule> rules = List.of();

    public Context getContext() { return context; }

    public void build(String source) { this.rules = Parser.parse(source); }

    public Result execute() { return execute(new ExecOptions()); }

    public Result execute(ExecOptions opts) {
        List<Ast.Rule> selected = selectRules(opts);
        Map<String, Object> results = new ConcurrentHashMap<>();
        List<Throwable> errors = new ArrayList<>();

        if (opts.mode == ExecMode.SORT) {
            for (Ast.Rule r : selected) {
                try {
                    Object v = new Interpreter(context).runRule(r);
                    if (v != null) results.put(r.name(), v);
                } catch (Throwable t) {
                    errors.add(new RuntimeException("rule '" + r.name() + "' failed", t));
                    if (!opts.continueOnError) break;
                }
            }
        } else {
            var pool = Executors.newFixedThreadPool(Math.max(1, opts.maxWorkers));
            try {
                List<Future<?>> futures = new ArrayList<>();
                for (Ast.Rule r : selected) {
                    futures.add(pool.submit(() -> {
                        try {
                            Object v = new Interpreter(context).runRule(r);
                            if (v != null) results.put(r.name(), v);
                        } catch (Throwable t) {
                            synchronized (errors) {
                                errors.add(new RuntimeException("rule '" + r.name() + "' failed", t));
                            }
                        }
                    }));
                }
                for (Future<?> f : futures) {
                    try { f.get(); } catch (ExecutionException | InterruptedException ignored) { }
                }
            } finally {
                pool.shutdown();
            }
        }

        return new Result(new HashMap<>(results), errors);
    }

    private List<Ast.Rule> selectRules(ExecOptions opts) {
        List<Ast.Rule> chosen;
        if (opts.selected != null && !opts.selected.isEmpty()) {
            Map<String, Ast.Rule> lookup = new HashMap<>();
            for (Ast.Rule r : rules) lookup.put(r.name(), r);
            chosen = new ArrayList<>();
            for (String n : opts.selected) {
                Ast.Rule r = lookup.get(n);
                if (r != null) chosen.add(r);
            }
        } else {
            chosen = new ArrayList<>(rules);
        }
        if (opts.mode == ExecMode.SORT) {
            chosen.sort(Comparator.comparingLong(Ast.Rule::salience).reversed());
        }
        return chosen;
    }
}
