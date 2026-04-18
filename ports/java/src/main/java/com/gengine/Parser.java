package com.gengine;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * Lexer + recursive-descent parser for the Java port. Grammar matches the
 * Python and C++ ports verbatim so the three MVPs can share rule files.
 */
public final class Parser {

    // ---------------------------------------------------------------------
    // Tokens
    // ---------------------------------------------------------------------

    enum TokKind {
        NUMBER, STRING, IDENT, OP, KEYWORD, EOF
    }

    record Token(TokKind kind, String text, Object value, int line, int col) { }

    private static final Set<String> KEYWORDS = Set.of(
            "rule", "desc", "salience", "begin", "end",
            "if", "else", "true", "false", "return"
    );

    // Precedence table for the expression parser. Missing operators
    // (assignment, comma) are handled as statement-level syntax.
    private static final Map<String, Integer> PRECEDENCE = Map.ofEntries(
            Map.entry("||", 1), Map.entry("&&", 2),
            Map.entry("==", 3), Map.entry("!=", 3),
            Map.entry("<", 3), Map.entry("<=", 3),
            Map.entry(">", 3), Map.entry(">=", 3),
            Map.entry("+", 4), Map.entry("-", 4),
            Map.entry("*", 5), Map.entry("/", 5), Map.entry("%", 5)
    );

    // ---------------------------------------------------------------------
    // Lexer
    // ---------------------------------------------------------------------

    static List<Token> tokenize(String src) {
        List<Token> out = new ArrayList<>();
        int i = 0, line = 1, lineStart = 0;
        while (i < src.length()) {
            char c = src.charAt(i);
            if (c == '\n') { line++; lineStart = ++i; continue; }
            if (Character.isWhitespace(c)) { i++; continue; }
            // Line comment "// ..."
            if (c == '/' && i + 1 < src.length() && src.charAt(i + 1) == '/') {
                while (i < src.length() && src.charAt(i) != '\n') i++;
                continue;
            }
            int col = i - lineStart + 1;
            // Numbers: integer or float
            if (Character.isDigit(c)) {
                int j = i;
                while (j < src.length() && Character.isDigit(src.charAt(j))) j++;
                boolean isFloat = false;
                if (j < src.length() && src.charAt(j) == '.') {
                    isFloat = true;
                    j++;
                    while (j < src.length() && Character.isDigit(src.charAt(j))) j++;
                }
                String text = src.substring(i, j);
                // NOTE: the branches return boxed Long / Double; merging them in a
                // ternary would auto-unbox and widen to double, silently losing the
                // integer type. We need to branch statementally to preserve it.
                Object num;
                if (isFloat) num = Double.parseDouble(text);
                else num = Long.parseLong(text);
                out.add(new Token(TokKind.NUMBER, text, num, line, col));
                i = j;
                continue;
            }
            // Strings: double-quoted with backslash escapes
            if (c == '"') {
                StringBuilder sb = new StringBuilder();
                int j = i + 1;
                while (j < src.length() && src.charAt(j) != '"') {
                    char ch = src.charAt(j);
                    if (ch == '\\' && j + 1 < src.length()) {
                        char esc = src.charAt(j + 1);
                        sb.append(switch (esc) {
                            case 'n' -> '\n';
                            case 't' -> '\t';
                            case 'r' -> '\r';
                            case '"' -> '"';
                            case '\\' -> '\\';
                            default -> esc;
                        });
                        j += 2;
                    } else {
                        sb.append(ch);
                        j++;
                    }
                }
                if (j >= src.length()) {
                    throw new IllegalStateException("unterminated string at line " + line);
                }
                String text = sb.toString();
                out.add(new Token(TokKind.STRING, text, text, line, col));
                i = j + 1;
                continue;
            }
            // Identifiers / keywords
            if (Character.isLetter(c) || c == '_') {
                int j = i;
                while (j < src.length() && (Character.isLetterOrDigit(src.charAt(j)) || src.charAt(j) == '_')) j++;
                String text = src.substring(i, j);
                if (KEYWORDS.contains(text)) {
                    out.add(new Token(TokKind.KEYWORD, text, text, line, col));
                } else {
                    out.add(new Token(TokKind.IDENT, text, text, line, col));
                }
                i = j;
                continue;
            }
            // Multi-char operators must be tried before single-char ones.
            String two = i + 1 < src.length() ? src.substring(i, i + 2) : "";
            if (Set.of("==", "!=", "<=", ">=", "&&", "||").contains(two)) {
                out.add(new Token(TokKind.OP, two, two, line, col));
                i += 2;
                continue;
            }
            if ("+-*/%=<>!(){},;".indexOf(c) >= 0) {
                String s = String.valueOf(c);
                out.add(new Token(TokKind.OP, s, s, line, col));
                i++;
                continue;
            }
            throw new IllegalStateException("unexpected character '" + c + "' at line " + line + ":" + col);
        }
        out.add(new Token(TokKind.EOF, "", null, line, 1));
        return out;
    }

    // ---------------------------------------------------------------------
    // Parser
    // ---------------------------------------------------------------------

    private final List<Token> toks;
    private int i;

    private Parser(List<Token> toks) { this.toks = toks; }

    /** Entry point: tokenize + parse a full program. */
    public static List<Ast.Rule> parse(String source) {
        return new Parser(tokenize(source)).parseProgram();
    }

    private Token peek() { return toks.get(i); }
    private Token peek(int k) { return toks.get(i + k); }

    private Token eat(TokKind kind) {
        Token t = toks.get(i);
        if (t.kind() != kind) {
            throw new IllegalStateException("expected " + kind + " at line " + t.line() + ":" + t.col() + ", got " + t.kind() + " '" + t.text() + "'");
        }
        i++;
        return t;
    }

    private Token eatKeyword(String kw) {
        Token t = toks.get(i);
        if (t.kind() != TokKind.KEYWORD || !kw.equals(t.text())) {
            throw new IllegalStateException("expected keyword '" + kw + "' at line " + t.line() + ":" + t.col() + ", got " + t.kind() + " '" + t.text() + "'");
        }
        i++;
        return t;
    }

    private boolean acceptKeyword(String kw) {
        Token t = toks.get(i);
        if (t.kind() == TokKind.KEYWORD && kw.equals(t.text())) { i++; return true; }
        return false;
    }

    private boolean acceptOp(String op) {
        Token t = toks.get(i);
        if (t.kind() == TokKind.OP && op.equals(t.text())) { i++; return true; }
        return false;
    }

    private void expectOp(String op) {
        if (!acceptOp(op)) {
            Token t = toks.get(i);
            throw new IllegalStateException("expected '" + op + "' at line " + t.line() + ":" + t.col() + ", got '" + t.text() + "'");
        }
    }

    // --- top level --------------------------------------------------------

    private List<Ast.Rule> parseProgram() {
        List<Ast.Rule> rules = new ArrayList<>();
        while (peek().kind() != TokKind.EOF) {
            rules.add(parseRule());
        }
        return rules;
    }

    private Ast.Rule parseRule() {
        eatKeyword("rule");
        String name = (String) eat(TokKind.STRING).value();
        String desc = "";
        long salience = 0;
        if (acceptKeyword("desc")) {
            desc = (String) eat(TokKind.STRING).value();
        }
        if (acceptKeyword("salience")) {
            long sign = acceptOp("-") ? -1 : 1;
            Object n = eat(TokKind.NUMBER).value();
            salience = sign * (n instanceof Long l ? l : ((Number) n).longValue());
        }
        eatKeyword("begin");
        List<Ast.Stmt> body = new ArrayList<>();
        while (!(peek().kind() == TokKind.KEYWORD && "end".equals(peek().text()))) {
            body.add(parseStatement());
        }
        eatKeyword("end");
        return new Ast.Rule(name, desc, salience, body);
    }

    // --- statements -------------------------------------------------------

    private Ast.Stmt parseStatement() {
        Token t = peek();
        if (t.kind() == TokKind.KEYWORD) {
            return switch (t.text()) {
                case "if" -> parseIf();
                case "return" -> { i++; yield new Ast.ReturnStmt(startsExpr() ? parseExpression() : null); }
                default -> throw new IllegalStateException("unexpected keyword '" + t.text() + "' at line " + t.line());
            };
        }
        if (t.kind() == TokKind.IDENT) {
            Token nxt = peek(1);
            if (nxt.kind() == TokKind.OP && "=".equals(nxt.text())) {
                String target = (String) eat(TokKind.IDENT).value();
                eat(TokKind.OP); // =
                return new Ast.AssignStmt(target, parseExpression());
            }
            if (nxt.kind() == TokKind.OP && "(".equals(nxt.text())) {
                return new Ast.CallStmt(parseCall());
            }
        }
        throw new IllegalStateException("unexpected token '" + t.text() + "' at line " + t.line() + ":" + t.col());
    }

    private Ast.IfStmt parseIf() {
        eatKeyword("if");
        Ast.Expr cond = parseExpression();
        expectOp("{");
        List<Ast.Stmt> thenBlock = parseBlockBody();
        expectOp("}");
        List<Ast.Stmt> elseBlock = List.of();
        if (acceptKeyword("else")) {
            expectOp("{");
            elseBlock = parseBlockBody();
            expectOp("}");
        }
        return new Ast.IfStmt(cond, thenBlock, elseBlock);
    }

    private List<Ast.Stmt> parseBlockBody() {
        List<Ast.Stmt> body = new ArrayList<>();
        while (!(peek().kind() == TokKind.OP && "}".equals(peek().text()))) {
            body.add(parseStatement());
        }
        return body;
    }

    private Ast.CallExpr parseCall() {
        String name = (String) eat(TokKind.IDENT).value();
        expectOp("(");
        List<Ast.Expr> args = new ArrayList<>();
        if (!(peek().kind() == TokKind.OP && ")".equals(peek().text()))) {
            args.add(parseExpression());
            while (acceptOp(",")) args.add(parseExpression());
        }
        expectOp(")");
        return new Ast.CallExpr(name, args);
    }

    // --- expressions (precedence climbing) --------------------------------

    private boolean startsExpr() {
        Token t = peek();
        if (t.kind() == TokKind.NUMBER || t.kind() == TokKind.STRING || t.kind() == TokKind.IDENT) return true;
        if (t.kind() == TokKind.KEYWORD) return "true".equals(t.text()) || "false".equals(t.text());
        if (t.kind() == TokKind.OP) return "-".equals(t.text()) || "!".equals(t.text()) || "(".equals(t.text());
        return false;
    }

    private Ast.Expr parseExpression() { return parseExpression(0); }

    private Ast.Expr parseExpression(int minPrec) {
        Ast.Expr lhs = parseUnary();
        while (true) {
            Token t = peek();
            if (t.kind() != TokKind.OP || !PRECEDENCE.containsKey(t.text())) break;
            int prec = PRECEDENCE.get(t.text());
            if (prec < minPrec) break;
            String op = t.text();
            i++;
            Ast.Expr rhs = parseExpression(prec + 1);
            lhs = new Ast.BinOpExpr(op, lhs, rhs);
        }
        return lhs;
    }

    private Ast.Expr parseUnary() {
        Token t = peek();
        if (t.kind() == TokKind.OP && ("-".equals(t.text()) || "!".equals(t.text()))) {
            i++;
            return new Ast.UnaryOpExpr(t.text(), parseUnary());
        }
        return parsePrimary();
    }

    private Ast.Expr parsePrimary() {
        Token t = peek();
        switch (t.kind()) {
            case NUMBER: i++; return new Ast.LiteralExpr(t.value());
            case STRING: i++; return new Ast.LiteralExpr(t.value());
            case KEYWORD:
                if ("true".equals(t.text())) { i++; return new Ast.LiteralExpr(Boolean.TRUE); }
                if ("false".equals(t.text())) { i++; return new Ast.LiteralExpr(Boolean.FALSE); }
                throw new IllegalStateException("unexpected keyword '" + t.text() + "' in expression");
            case IDENT:
                if (peek(1).kind() == TokKind.OP && "(".equals(peek(1).text())) return parseCall();
                i++;
                return new Ast.IdentExpr((String) t.value());
            case OP:
                if ("(".equals(t.text())) {
                    i++;
                    Ast.Expr e = parseExpression();
                    expectOp(")");
                    return e;
                }
                throw new IllegalStateException("unexpected operator '" + t.text() + "' at line " + t.line());
            default:
                throw new IllegalStateException("unexpected token kind " + t.kind());
        }
    }
}
