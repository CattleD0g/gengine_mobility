package com.gengine;

import java.util.ArrayList;
import java.util.List;

/**
 * AST interpreter. Uses pattern matching on the sealed Expr/Stmt hierarchies
 * so there is no per-node visitor class. A control-flow exception
 * ({@link ReturnSignal}) unwinds the body when a `return` is executed.
 */
final class Interpreter {

    private final RuleEngine.Context ctx;

    Interpreter(RuleEngine.Context ctx) { this.ctx = ctx; }

    /** Runs the rule body and returns whatever value a top-level `return` produced, or null. */
    Object runRule(Ast.Rule rule) {
        try {
            for (Ast.Stmt s : rule.body()) exec(s);
            return null;
        } catch (ReturnSignal r) {
            return r.value;
        }
    }

    private void exec(Ast.Stmt stmt) {
        switch (stmt) {
            case Ast.AssignStmt a -> ctx.set(a.target(), eval(a.value()));
            case Ast.CallStmt c   -> eval(c.call());
            case Ast.IfStmt i -> {
                if (truthy(eval(i.cond()))) {
                    for (Ast.Stmt s : i.thenBlock()) exec(s);
                } else {
                    for (Ast.Stmt s : i.elseBlock()) exec(s);
                }
            }
            case Ast.ReturnStmt r -> throw new ReturnSignal(r.value() == null ? null : eval(r.value()));
        }
    }

    private Object eval(Ast.Expr e) {
        return switch (e) {
            case Ast.LiteralExpr l -> l.value();
            case Ast.IdentExpr i   -> ctx.get(i.name());
            case Ast.CallExpr c -> {
                List<Object> args = new ArrayList<>(c.args().size());
                for (Ast.Expr a : c.args()) args.add(eval(a));
                yield ctx.call(c.name(), args);
            }
            case Ast.UnaryOpExpr u -> {
                Object v = eval(u.operand());
                yield switch (u.op()) {
                    case "-" -> negate(v);
                    case "!" -> !truthy(v);
                    default  -> throw new RuntimeException("unknown unary op " + u.op());
                };
            }
            case Ast.BinOpExpr b -> {
                // Short-circuit logical ops before evaluating the right-hand side,
                // matching Go's behaviour so `a || expensive()` stays cheap.
                if ("&&".equals(b.op())) yield truthy(eval(b.lhs())) && truthy(eval(b.rhs()));
                if ("||".equals(b.op())) yield truthy(eval(b.lhs())) || truthy(eval(b.rhs()));
                Object lhs = eval(b.lhs());
                Object rhs = eval(b.rhs());
                yield applyBinary(b.op(), lhs, rhs);
            }
        };
    }

    // ------------------------------------------------------------------
    // Arithmetic / comparison coercions. Only the pairs that actually show
    // up in the MVP DSL are implemented; extending this is where most of
    // the gengine Go reflection code goes.
    // ------------------------------------------------------------------

    private static Object negate(Object v) {
        if (v instanceof Long l) return -l;
        if (v instanceof Double d) return -d;
        throw new RuntimeException("cannot negate " + v);
    }

    private static Object applyBinary(String op, Object lhs, Object rhs) {
        if (lhs instanceof String || rhs instanceof String) {
            if (op.equals("+")) return String.valueOf(lhs) + String.valueOf(rhs);
            return compareObjects(op, lhs, rhs);
        }
        if (lhs instanceof Number ln && rhs instanceof Number rn) {
            if (lhs instanceof Double || rhs instanceof Double) {
                double a = ln.doubleValue(), c = rn.doubleValue();
                return switch (op) {
                    case "+" -> a + c;
                    case "-" -> a - c;
                    case "*" -> a * c;
                    case "/" -> a / c;
                    case "%" -> a % c;
                    case "==" -> a == c;
                    case "!=" -> a != c;
                    case "<"  -> a < c;
                    case "<=" -> a <= c;
                    case ">"  -> a > c;
                    case ">=" -> a >= c;
                    default -> throw new RuntimeException("unknown op " + op);
                };
            }
            long a = ln.longValue(), c = rn.longValue();
            return switch (op) {
                case "+" -> a + c;
                case "-" -> a - c;
                case "*" -> a * c;
                case "/" -> a / c;
                case "%" -> a % c;
                case "==" -> a == c;
                case "!=" -> a != c;
                case "<"  -> a < c;
                case "<=" -> a <= c;
                case ">"  -> a > c;
                case ">=" -> a >= c;
                default -> throw new RuntimeException("unknown op " + op);
            };
        }
        return compareObjects(op, lhs, rhs);
    }

    @SuppressWarnings({"rawtypes", "unchecked"})
    private static Object compareObjects(String op, Object lhs, Object rhs) {
        return switch (op) {
            case "==" -> java.util.Objects.equals(lhs, rhs);
            case "!=" -> !java.util.Objects.equals(lhs, rhs);
            case "<", "<=", ">", ">=" -> {
                if (!(lhs instanceof Comparable) || !(rhs instanceof Comparable))
                    throw new RuntimeException("values are not comparable: " + lhs + " " + op + " " + rhs);
                int cmp = ((Comparable) lhs).compareTo(rhs);
                yield switch (op) {
                    case "<"  -> cmp < 0;
                    case "<=" -> cmp <= 0;
                    case ">"  -> cmp > 0;
                    case ">=" -> cmp >= 0;
                    default -> false;
                };
            }
            default -> throw new RuntimeException("unsupported op " + op + " on " + lhs + "/" + rhs);
        };
    }

    private static boolean truthy(Object v) {
        if (v == null) return false;
        if (v instanceof Boolean b) return b;
        if (v instanceof Number n) return n.doubleValue() != 0.0;
        if (v instanceof String s) return !s.isEmpty();
        return true;
    }

    /** Control-flow marker thrown by {@code return <expr>} statements. */
    private static final class ReturnSignal extends RuntimeException {
        final Object value;
        ReturnSignal(Object value) {
            super(null, null, false, false); // skip fill-in-stack-trace
            this.value = value;
        }
    }
}
