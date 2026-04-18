package com.gengine;

import java.util.Collections;
import java.util.List;

/**
 * AST node types for the Java port. Modelled as a sealed-interface hierarchy
 * so the interpreter can pattern-match without a visitor pass. Only the
 * shared DSL subset is represented; loops and DAG nodes are intentionally
 * out of scope (see README / package-info).
 */
public final class Ast {
    private Ast() { }

    public sealed interface Expr permits LiteralExpr, IdentExpr, BinOpExpr, UnaryOpExpr, CallExpr { }
    public sealed interface Stmt permits AssignStmt, CallStmt, IfStmt, ReturnStmt { }

    public record LiteralExpr(Object value) implements Expr { }
    public record IdentExpr(String name) implements Expr { }
    public record BinOpExpr(String op, Expr lhs, Expr rhs) implements Expr { }
    public record UnaryOpExpr(String op, Expr operand) implements Expr { }
    public record CallExpr(String name, List<Expr> args) implements Expr { }

    public record AssignStmt(String target, Expr value) implements Stmt { }
    public record CallStmt(CallExpr call) implements Stmt { }
    public record IfStmt(Expr cond, List<Stmt> thenBlock, List<Stmt> elseBlock) implements Stmt { }
    public record ReturnStmt(Expr value /* nullable */) implements Stmt { }

    /** A parsed rule. `body` is a flat list of statements, executed top-down. */
    public record Rule(String name, String desc, long salience, List<Stmt> body) {
        public Rule {
            body = Collections.unmodifiableList(body);
        }
    }
}
