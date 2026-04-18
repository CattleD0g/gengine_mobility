// Public engine API for the C++ port. Mirrors Gengine.ExecuteOpts + the
// Python / Java RuleEngine.execute shape: one primary method that takes an
// options struct and an ExecMode enum.

#pragma once

#include "ast.hpp"

#include <functional>
#include <mutex>
#include <optional>
#include <shared_mutex>
#include <string>
#include <unordered_map>
#include <vector>

namespace gengine {

enum class ExecMode {
    Sort,       // serial, salience-descending
    Concurrent  // thread-per-rule fan-out
};

struct ExecOptions {
    ExecMode mode = ExecMode::Sort;
    bool continueOnError = false;
    std::vector<std::string> selected;    // empty = all rules
    unsigned maxWorkers = 8;              // concurrent mode thread cap
};

struct ExecError {
    std::string ruleName;
    std::string message;
};

struct ExecResult {
    std::unordered_map<std::string, Value> values;
    std::vector<ExecError> errors;
    bool hasErrors() const { return !errors.empty(); }
};

// Context carries user-registered variables and functions. Reads under a
// shared_mutex so the concurrent mode can run many interpreter threads.
class Context {
public:
    using Function = std::function<Value(const std::vector<Value>&)>;

    void addVar(const std::string& name, Value v);
    void addFunction(const std::string& name, Function fn);

    Value get(const std::string& name) const;
    void  set(const std::string& name, Value v);
    Value call(const std::string& name, const std::vector<Value>& args) const;

private:
    mutable std::shared_mutex mu_;
    std::unordered_map<std::string, Value>   vars_;
    std::unordered_map<std::string, Function> fns_;
};

class RuleEngine {
public:
    Context& context() { return ctx_; }

    void build(const std::string& source);
    ExecResult execute(const ExecOptions& opts = {});

private:
    Context ctx_;
    std::vector<Rule> rules_;
};

} // namespace gengine
