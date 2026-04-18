// Interpreter + rule engine implementation for the C++ port.
//
// The execution model mirrors the other MVP ports: a single execute() entry
// that branches on ExecMode. `return <expr>` is implemented with a thrown
// ReturnSignal that the top-level per-rule runner catches; this keeps the
// interpreter a pure pattern match rather than a trampoline.

#include "gengine/engine.hpp"
#include "gengine/parser.hpp"

#include <algorithm>
#include <cmath>
#include <future>
#include <sstream>
#include <stdexcept>
#include <thread>
#include <vector>

namespace gengine {
namespace {

// Control-flow marker thrown by `return <expr>` statements.
struct ReturnSignal {
    Value value;
};

// Promote both sides of an arithmetic op to double when either is double;
// otherwise keep int64. Mirrors the Java port's applyBinary.
bool valueIsTruthy(const Value& v) {
    return std::visit([](auto&& x) -> bool {
        using T = std::decay_t<decltype(x)>;
        if constexpr (std::is_same_v<T, std::monostate>) return false;
        else if constexpr (std::is_same_v<T, bool>)        return x;
        else if constexpr (std::is_same_v<T, int64_t>)     return x != 0;
        else if constexpr (std::is_same_v<T, double>)      return x != 0.0;
        else if constexpr (std::is_same_v<T, std::string>) return !x.empty();
    }, v);
}

Value negateValue(const Value& v) {
    return std::visit([](auto&& x) -> Value {
        using T = std::decay_t<decltype(x)>;
        if constexpr (std::is_same_v<T, int64_t>) return Value{-x};
        else if constexpr (std::is_same_v<T, double>) return Value{-x};
        else throw std::runtime_error("cannot negate non-numeric value");
    }, v);
}

// Extract a double if v holds any numeric kind.
std::optional<double> asDouble(const Value& v) {
    return std::visit([](auto&& x) -> std::optional<double> {
        using T = std::decay_t<decltype(x)>;
        if constexpr (std::is_same_v<T, int64_t>) return static_cast<double>(x);
        else if constexpr (std::is_same_v<T, double>) return x;
        else if constexpr (std::is_same_v<T, bool>) return x ? 1.0 : 0.0;
        else return std::nullopt;
    }, v);
}

Value applyNumericBinary(const std::string& op, const Value& lhs, const Value& rhs) {
    bool anyDouble = std::holds_alternative<double>(lhs) || std::holds_alternative<double>(rhs);
    if (anyDouble) {
        double a = asDouble(lhs).value_or(0.0), b = asDouble(rhs).value_or(0.0);
        if (op == "+") return Value{a + b};
        if (op == "-") return Value{a - b};
        if (op == "*") return Value{a * b};
        if (op == "/") return Value{a / b};
        if (op == "%") return Value{std::fmod(a, b)};
        if (op == "==") return Value{a == b};
        if (op == "!=") return Value{a != b};
        if (op == "<")  return Value{a < b};
        if (op == "<=") return Value{a <= b};
        if (op == ">")  return Value{a > b};
        if (op == ">=") return Value{a >= b};
    } else {
        int64_t a = std::get<int64_t>(lhs), b = std::get<int64_t>(rhs);
        if (op == "+") return Value{a + b};
        if (op == "-") return Value{a - b};
        if (op == "*") return Value{a * b};
        if (op == "/") return Value{b == 0 ? 0 : a / b};
        if (op == "%") return Value{b == 0 ? 0 : a % b};
        if (op == "==") return Value{a == b};
        if (op == "!=") return Value{a != b};
        if (op == "<")  return Value{a < b};
        if (op == "<=") return Value{a <= b};
        if (op == ">")  return Value{a > b};
        if (op == ">=") return Value{a >= b};
    }
    throw std::runtime_error("unknown numeric op " + op);
}

Value applyStringOrCompare(const std::string& op, const Value& lhs, const Value& rhs) {
    auto ls = std::get_if<std::string>(&lhs);
    auto rs = std::get_if<std::string>(&rhs);
    if (op == "+" && ls && rs) return Value{*ls + *rs};
    if (op == "==") return Value{lhs == rhs};
    if (op == "!=") return Value{!(lhs == rhs)};
    if (ls && rs && (op == "<" || op == "<=" || op == ">" || op == ">=")) {
        int cmp = ls->compare(*rs);
        if (op == "<")  return Value{cmp < 0};
        if (op == "<=") return Value{cmp <= 0};
        if (op == ">")  return Value{cmp > 0};
        if (op == ">=") return Value{cmp >= 0};
    }
    throw std::runtime_error("unsupported op " + op + " on these operand types");
}

class Interpreter {
public:
    explicit Interpreter(const Context& ctx) : ctx_(ctx) {}

    // Runs the rule body. Returns the `return <expr>` value, or monostate
    // if none was emitted.
    Value runRule(const Rule& rule) {
        try {
            for (auto& s : rule.body) exec(*s);
        } catch (ReturnSignal& sig) {
            return sig.value;
        }
        return Value{};
    }

private:
    const Context& ctx_;

    void exec(const Stmt& s) {
        std::visit([&](auto&& node) {
            using T = std::decay_t<decltype(node)>;
            if constexpr (std::is_same_v<T, AssignStmt>) {
                const_cast<Context&>(ctx_).set(node.target, eval(*node.value));
            } else if constexpr (std::is_same_v<T, CallStmt>) {
                // Build args and invoke for side effects.
                std::vector<Value> args;
                for (auto& a : node.call.args) args.push_back(eval(*a));
                ctx_.call(node.call.name, args);
            } else if constexpr (std::is_same_v<T, IfStmt>) {
                if (valueIsTruthy(eval(*node.cond))) {
                    for (auto& t : node.thenBlock) exec(*t);
                } else {
                    for (auto& t : node.elseBlock) exec(*t);
                }
            } else if constexpr (std::is_same_v<T, ReturnStmt>) {
                Value v{};
                if (node.value) v = eval(*node.value);
                throw ReturnSignal{v};
            }
        }, s.node);
    }

    Value eval(const Expr& e) {
        return std::visit([&](auto&& node) -> Value {
            using T = std::decay_t<decltype(node)>;
            if constexpr (std::is_same_v<T, LiteralExpr>) {
                return node.value;
            } else if constexpr (std::is_same_v<T, IdentExpr>) {
                return ctx_.get(node.name);
            } else if constexpr (std::is_same_v<T, CallExpr>) {
                std::vector<Value> args;
                for (auto& a : node.args) args.push_back(eval(*a));
                return ctx_.call(node.name, args);
            } else if constexpr (std::is_same_v<T, UnaryOpExpr>) {
                Value v = eval(*node.operand);
                if (node.op == "-") return negateValue(v);
                if (node.op == "!") return Value{!valueIsTruthy(v)};
                throw std::runtime_error("unknown unary op " + node.op);
            } else if constexpr (std::is_same_v<T, BinOpExpr>) {
                // Short-circuit logicals before evaluating RHS.
                if (node.op == "&&") return Value{valueIsTruthy(eval(*node.lhs)) && valueIsTruthy(eval(*node.rhs))};
                if (node.op == "||") return Value{valueIsTruthy(eval(*node.lhs)) || valueIsTruthy(eval(*node.rhs))};
                Value lhs = eval(*node.lhs);
                Value rhs = eval(*node.rhs);
                if (std::holds_alternative<std::string>(lhs) || std::holds_alternative<std::string>(rhs))
                    return applyStringOrCompare(node.op, lhs, rhs);
                return applyNumericBinary(node.op, lhs, rhs);
            }
            return Value{};
        }, e.node);
    }
};

} // namespace

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

void Context::addVar(const std::string& name, Value v) {
    std::unique_lock lk(mu_);
    vars_[name] = std::move(v);
}

void Context::addFunction(const std::string& name, Function fn) {
    std::unique_lock lk(mu_);
    fns_[name] = std::move(fn);
}

Value Context::get(const std::string& name) const {
    std::shared_lock lk(mu_);
    auto it = vars_.find(name);
    if (it == vars_.end()) throw std::runtime_error("unknown variable '" + name + "'");
    return it->second;
}

void Context::set(const std::string& name, Value v) {
    std::unique_lock lk(mu_);
    vars_[name] = std::move(v);
}

Value Context::call(const std::string& name, const std::vector<Value>& args) const {
    Function fn;
    {
        std::shared_lock lk(mu_);
        auto it = fns_.find(name);
        if (it == fns_.end()) throw std::runtime_error("unknown function '" + name + "'");
        fn = it->second;
    }
    return fn(args);
}

// ---------------------------------------------------------------------------
// RuleEngine
// ---------------------------------------------------------------------------

void RuleEngine::build(const std::string& source) {
    rules_ = parse(source);
}

ExecResult RuleEngine::execute(const ExecOptions& opts) {
    // Rule selection: optional name-list filter, then salience sort for Sort mode.
    std::vector<const Rule*> chosen;
    if (!opts.selected.empty()) {
        std::unordered_map<std::string, const Rule*> lookup;
        for (auto& r : rules_) lookup[r.name] = &r;
        for (auto& n : opts.selected) {
            auto it = lookup.find(n);
            if (it != lookup.end()) chosen.push_back(it->second);
        }
    } else {
        for (auto& r : rules_) chosen.push_back(&r);
    }
    if (opts.mode == ExecMode::Sort) {
        std::sort(chosen.begin(), chosen.end(),
                  [](const Rule* a, const Rule* b) { return a->salience > b->salience; });
    }

    ExecResult res;
    std::mutex m;

    auto runOne = [&](const Rule* r) {
        try {
            Value v = Interpreter(ctx_).runRule(*r);
            std::lock_guard lk(m);
            if (!std::holds_alternative<std::monostate>(v)) res.values[r->name] = v;
        } catch (const std::exception& ex) {
            std::lock_guard lk(m);
            res.errors.push_back({r->name, ex.what()});
        } catch (...) {
            std::lock_guard lk(m);
            res.errors.push_back({r->name, "unknown error"});
        }
    };

    if (opts.mode == ExecMode::Sort) {
        for (const Rule* r : chosen) {
            std::size_t before = res.errors.size();
            runOne(r);
            if (res.errors.size() > before && !opts.continueOnError) break;
        }
    } else {
        // Concurrent: one thread per rule, capped at maxWorkers to avoid
        // hammering the scheduler on large rule sets.
        std::vector<std::thread> threads;
        std::size_t idx = 0;
        unsigned cap = std::max(1u, opts.maxWorkers);
        while (idx < chosen.size()) {
            std::size_t batchEnd = std::min(chosen.size(), idx + cap);
            for (std::size_t k = idx; k < batchEnd; ++k)
                threads.emplace_back(runOne, chosen[k]);
            for (auto& t : threads) t.join();
            threads.clear();
            idx = batchEnd;
        }
    }

    return res;
}

} // namespace gengine
