// Hand-rolled lexer + recursive-descent parser for the C++ port.
// Grammar is identical to the Python and Java MVPs so rule files can be
// shared across ports when smoke-testing.

#include "gengine/parser.hpp"

#include <cctype>
#include <string>
#include <string_view>
#include <unordered_map>
#include <unordered_set>
#include <utility>

namespace gengine {
namespace {

// ---------------------------------------------------------------------------
// Tokens
// ---------------------------------------------------------------------------

enum class TokKind { Number, String, Ident, Op, Keyword, Eof };

struct Token {
    TokKind kind;
    std::string text;
    Value value;
    int line = 0;
    int col  = 0;
};

const std::unordered_set<std::string>& keywords() {
    static const std::unordered_set<std::string> kw{
        "rule", "desc", "salience", "begin", "end",
        "if", "else", "true", "false", "return"
    };
    return kw;
}

const std::unordered_map<std::string, int>& precedence() {
    static const std::unordered_map<std::string, int> p{
        {"||", 1}, {"&&", 2},
        {"==", 3}, {"!=", 3}, {"<", 3}, {"<=", 3}, {">", 3}, {">=", 3},
        {"+", 4}, {"-", 4},
        {"*", 5}, {"/", 5}, {"%", 5}
    };
    return p;
}

// ---------------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------------

std::vector<Token> tokenize(const std::string& src) {
    std::vector<Token> out;
    std::size_t i = 0;
    int line = 1;
    std::size_t lineStart = 0;

    auto addOp = [&](std::string s, std::size_t len) {
        out.push_back({TokKind::Op, s, Value{s}, line, static_cast<int>(i - lineStart + 1)});
        i += len;
    };

    while (i < src.size()) {
        char c = src[i];
        if (c == '\n') { ++line; lineStart = ++i; continue; }
        if (std::isspace(static_cast<unsigned char>(c))) { ++i; continue; }
        // Line comment
        if (c == '/' && i + 1 < src.size() && src[i+1] == '/') {
            while (i < src.size() && src[i] != '\n') ++i;
            continue;
        }
        int col = static_cast<int>(i - lineStart + 1);

        // Numbers
        if (std::isdigit(static_cast<unsigned char>(c))) {
            std::size_t j = i;
            while (j < src.size() && std::isdigit(static_cast<unsigned char>(src[j]))) ++j;
            bool isFloat = false;
            if (j < src.size() && src[j] == '.') {
                isFloat = true;
                ++j;
                while (j < src.size() && std::isdigit(static_cast<unsigned char>(src[j]))) ++j;
            }
            std::string text = src.substr(i, j - i);
            Value v = isFloat ? Value{std::stod(text)}
                              : Value{static_cast<int64_t>(std::stoll(text))};
            out.push_back({TokKind::Number, text, v, line, col});
            i = j;
            continue;
        }

        // Strings
        if (c == '"') {
            std::string s;
            std::size_t j = i + 1;
            while (j < src.size() && src[j] != '"') {
                if (src[j] == '\\' && j + 1 < src.size()) {
                    char esc = src[j+1];
                    switch (esc) {
                        case 'n': s.push_back('\n'); break;
                        case 't': s.push_back('\t'); break;
                        case 'r': s.push_back('\r'); break;
                        case '"': s.push_back('"'); break;
                        case '\\': s.push_back('\\'); break;
                        default: s.push_back(esc); break;
                    }
                    j += 2;
                } else {
                    s.push_back(src[j++]);
                }
            }
            if (j >= src.size()) throw ParseError("unterminated string at line " + std::to_string(line));
            out.push_back({TokKind::String, s, Value{s}, line, col});
            i = j + 1;
            continue;
        }

        // Identifiers / keywords
        if (std::isalpha(static_cast<unsigned char>(c)) || c == '_') {
            std::size_t j = i;
            while (j < src.size() && (std::isalnum(static_cast<unsigned char>(src[j])) || src[j] == '_')) ++j;
            std::string text = src.substr(i, j - i);
            TokKind k = keywords().count(text) ? TokKind::Keyword : TokKind::Ident;
            out.push_back({k, text, Value{text}, line, col});
            i = j;
            continue;
        }

        // Two-char operators must be tried before one-char ones.
        if (i + 1 < src.size()) {
            std::string two = src.substr(i, 2);
            if (two == "==" || two == "!=" || two == "<=" || two == ">="
                || two == "&&" || two == "||") {
                addOp(two, 2);
                continue;
            }
        }
        if (std::string_view("+-*/%=<>!(){},;").find(c) != std::string_view::npos) {
            addOp(std::string(1, c), 1);
            continue;
        }

        throw ParseError("unexpected character '" + std::string(1, c) + "' at line "
                         + std::to_string(line) + ":" + std::to_string(col));
    }
    out.push_back({TokKind::Eof, "", {}, line, 1});
    return out;
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

class ParserImpl {
public:
    explicit ParserImpl(std::vector<Token> toks) : toks_(std::move(toks)) {}

    std::vector<Rule> parseProgram() {
        std::vector<Rule> rules;
        while (peek().kind != TokKind::Eof) rules.push_back(parseRule());
        return rules;
    }

private:
    std::vector<Token> toks_;
    std::size_t i_ = 0;

    const Token& peek(std::size_t k = 0) const { return toks_[i_ + k]; }

    const Token& eat(TokKind kind) {
        const auto& t = toks_[i_];
        if (t.kind != kind) fail("expected different kind", t);
        ++i_;
        return toks_[i_ - 1];
    }

    const Token& eatKeyword(const std::string& kw) {
        const auto& t = toks_[i_];
        if (t.kind != TokKind::Keyword || t.text != kw) fail("expected keyword '" + kw + "'", t);
        ++i_;
        return toks_[i_ - 1];
    }

    bool acceptKeyword(const std::string& kw) {
        if (peek().kind == TokKind::Keyword && peek().text == kw) { ++i_; return true; }
        return false;
    }

    bool acceptOp(const std::string& op) {
        if (peek().kind == TokKind::Op && peek().text == op) { ++i_; return true; }
        return false;
    }

    void expectOp(const std::string& op) {
        if (!acceptOp(op)) fail("expected '" + op + "'", peek());
    }

    [[noreturn]] void fail(const std::string& msg, const Token& t) {
        throw ParseError(msg + " at line " + std::to_string(t.line) + ":"
                         + std::to_string(t.col) + " (got '" + t.text + "')");
    }

    Rule parseRule() {
        eatKeyword("rule");
        std::string name = std::get<std::string>(eat(TokKind::String).value);
        Rule rule;
        rule.name = name;
        if (acceptKeyword("desc")) rule.desc = std::get<std::string>(eat(TokKind::String).value);
        if (acceptKeyword("salience")) {
            int64_t sign = acceptOp("-") ? -1 : 1;
            const Token& n = eat(TokKind::Number);
            int64_t val = std::visit([](auto&& v) -> int64_t {
                using T = std::decay_t<decltype(v)>;
                if constexpr (std::is_same_v<T, int64_t>) return v;
                else if constexpr (std::is_same_v<T, double>) return static_cast<int64_t>(v);
                else return 0;
            }, n.value);
            rule.salience = sign * val;
        }
        eatKeyword("begin");
        while (!(peek().kind == TokKind::Keyword && peek().text == "end")) {
            rule.body.push_back(parseStatement());
        }
        eatKeyword("end");
        return rule;
    }

    StmtPtr parseStatement() {
        const auto& t = peek();
        if (t.kind == TokKind::Keyword) {
            if (t.text == "if") return parseIf();
            if (t.text == "return") {
                ++i_;
                ExprPtr v;
                if (startsExpr()) v = parseExpression();
                return std::make_shared<Stmt>(Stmt{ReturnStmt{v}});
            }
            fail("unexpected keyword in statement", t);
        }
        if (t.kind == TokKind::Ident) {
            const auto& nxt = peek(1);
            if (nxt.kind == TokKind::Op && nxt.text == "=") {
                std::string target = t.text;
                ++i_; ++i_; // IDENT =
                return std::make_shared<Stmt>(Stmt{AssignStmt{target, parseExpression()}});
            }
            if (nxt.kind == TokKind::Op && nxt.text == "(") {
                return std::make_shared<Stmt>(Stmt{CallStmt{parseCall()}});
            }
        }
        fail("unexpected token in statement", t);
    }

    StmtPtr parseIf() {
        eatKeyword("if");
        ExprPtr cond = parseExpression();
        expectOp("{");
        std::vector<StmtPtr> thenBlock;
        while (!(peek().kind == TokKind::Op && peek().text == "}"))
            thenBlock.push_back(parseStatement());
        expectOp("}");
        std::vector<StmtPtr> elseBlock;
        if (acceptKeyword("else")) {
            expectOp("{");
            while (!(peek().kind == TokKind::Op && peek().text == "}"))
                elseBlock.push_back(parseStatement());
            expectOp("}");
        }
        return std::make_shared<Stmt>(Stmt{IfStmt{cond, std::move(thenBlock), std::move(elseBlock)}});
    }

    CallExpr parseCall() {
        std::string name = eat(TokKind::Ident).text;
        expectOp("(");
        CallExpr c; c.name = name;
        if (!(peek().kind == TokKind::Op && peek().text == ")")) {
            c.args.push_back(parseExpression());
            while (acceptOp(",")) c.args.push_back(parseExpression());
        }
        expectOp(")");
        return c;
    }

    bool startsExpr() const {
        const auto& t = peek();
        if (t.kind == TokKind::Number || t.kind == TokKind::String || t.kind == TokKind::Ident) return true;
        if (t.kind == TokKind::Keyword) return t.text == "true" || t.text == "false";
        if (t.kind == TokKind::Op) return t.text == "-" || t.text == "!" || t.text == "(";
        return false;
    }

    ExprPtr parseExpression(int minPrec = 0) {
        ExprPtr lhs = parseUnary();
        while (true) {
            const auto& t = peek();
            if (t.kind != TokKind::Op || !precedence().count(t.text)) break;
            int prec = precedence().at(t.text);
            if (prec < minPrec) break;
            std::string op = t.text;
            ++i_;
            ExprPtr rhs = parseExpression(prec + 1);
            lhs = std::make_shared<Expr>(Expr{BinOpExpr{op, lhs, rhs}});
        }
        return lhs;
    }

    ExprPtr parseUnary() {
        const auto& t = peek();
        if (t.kind == TokKind::Op && (t.text == "-" || t.text == "!")) {
            std::string op = t.text;
            ++i_;
            return std::make_shared<Expr>(Expr{UnaryOpExpr{op, parseUnary()}});
        }
        return parsePrimary();
    }

    ExprPtr parsePrimary() {
        const auto& t = peek();
        switch (t.kind) {
            case TokKind::Number:
                ++i_;
                return std::make_shared<Expr>(Expr{LiteralExpr{t.value}});
            case TokKind::String:
                ++i_;
                return std::make_shared<Expr>(Expr{LiteralExpr{t.value}});
            case TokKind::Keyword:
                if (t.text == "true")  { ++i_; return std::make_shared<Expr>(Expr{LiteralExpr{Value{true}}}); }
                if (t.text == "false") { ++i_; return std::make_shared<Expr>(Expr{LiteralExpr{Value{false}}}); }
                fail("unexpected keyword in expression", t);
            case TokKind::Ident:
                if (peek(1).kind == TokKind::Op && peek(1).text == "(")
                    return std::make_shared<Expr>(Expr{parseCall()});
                ++i_;
                return std::make_shared<Expr>(Expr{IdentExpr{t.text}});
            case TokKind::Op:
                if (t.text == "(") {
                    ++i_;
                    ExprPtr e = parseExpression();
                    expectOp(")");
                    return e;
                }
                fail("unexpected operator in expression", t);
            case TokKind::Eof:
                fail("unexpected end of input", t);
        }
        fail("unreachable", t);
    }
};

} // namespace

std::vector<Rule> parse(const std::string& source) {
    return ParserImpl(tokenize(source)).parseProgram();
}

} // namespace gengine
