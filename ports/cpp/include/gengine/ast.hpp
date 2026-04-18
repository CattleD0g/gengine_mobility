// gengine C++ port: AST types.
//
// Mirrors the Python and Java ports' AST so rule files are interchangeable
// across the three MVPs. Values are held in a `std::variant` tagged union
// rather than a class hierarchy to keep the interpreter branchless.

#pragma once

#include <memory>
#include <string>
#include <variant>
#include <vector>
#include <cstdint>

namespace gengine {

using Value = std::variant<std::monostate, bool, int64_t, double, std::string>;

struct Expr;
using ExprPtr = std::shared_ptr<Expr>;

struct LiteralExpr { Value value; };
struct IdentExpr   { std::string name; };
struct UnaryOpExpr { std::string op; ExprPtr operand; };
struct BinOpExpr   { std::string op; ExprPtr lhs; ExprPtr rhs; };
struct CallExpr    { std::string name; std::vector<ExprPtr> args; };

struct Expr {
    std::variant<LiteralExpr, IdentExpr, UnaryOpExpr, BinOpExpr, CallExpr> node;
};

struct Stmt;
using StmtPtr = std::shared_ptr<Stmt>;

struct AssignStmt { std::string target; ExprPtr value; };
struct CallStmt   { CallExpr call; };
struct IfStmt     { ExprPtr cond; std::vector<StmtPtr> thenBlock; std::vector<StmtPtr> elseBlock; };
struct ReturnStmt { ExprPtr value; }; // nullable

struct Stmt {
    std::variant<AssignStmt, CallStmt, IfStmt, ReturnStmt> node;
};

struct Rule {
    std::string name;
    std::string desc;
    int64_t salience = 0;
    std::vector<StmtPtr> body;
};

} // namespace gengine
