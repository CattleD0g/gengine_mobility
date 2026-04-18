// Smoke tests for the C++ gengine MVP. Plain-C++ assert-based harness so
// we stay dependency-free; invoked by ctest via the CMakeLists test target.

#include "gengine/engine.hpp"

#include <cassert>
#include <iostream>
#include <mutex>
#include <string>
#include <thread>
#include <vector>

using namespace gengine;

namespace {

int failures = 0;

#define CHECK(cond) do { \
    if (!(cond)) { \
        std::cerr << "FAIL at " << __FILE__ << ":" << __LINE__ << "  " #cond << "\n"; \
        ++failures; \
    } \
} while (0)

void testSalienceOrderingAndAssignment() {
    RuleEngine engine;
    std::vector<std::string> log;
    std::mutex logMu;
    engine.context().addFunction("log", [&](const std::vector<Value>& args) -> Value {
        std::lock_guard lk(logMu);
        log.push_back(std::get<std::string>(args.at(0)));
        return Value{};
    });
    engine.context().addVar("threshold", Value{int64_t{5}});

    engine.build(R"(
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
    )");
    ExecResult res = engine.execute();
    CHECK(!res.hasErrors());
    CHECK(log.size() == 2);
    CHECK(log[0] == "high");
    CHECK(log[1] == "low");
}

void testIfElseReturn() {
    RuleEngine engine;
    engine.build(R"(
        rule "pick" salience 1
        begin
            if 2 < 1 {
                return "unreachable"
            } else {
                return "else-branch"
            }
        end
    )");
    ExecResult res = engine.execute();
    CHECK(!res.hasErrors());
    auto it = res.values.find("pick");
    CHECK(it != res.values.end());
    CHECK(std::get<std::string>(it->second) == "else-branch");
}

void testContinueOnError() {
    RuleEngine engine;
    engine.context().addFunction("boom", [](const std::vector<Value>&) -> Value {
        throw std::runtime_error("explode");
    });
    engine.build(R"(
        rule "r1" salience 2 begin boom() end
        rule "r2" salience 1 begin return 42 end
    )");

    // Fail-fast: r2 never runs.
    ExecOptions failFast; failFast.continueOnError = false;
    ExecResult a = engine.execute(failFast);
    CHECK(a.hasErrors());
    CHECK(a.values.find("r2") == a.values.end());

    // Collect: r2 runs and records its result.
    ExecOptions collect; collect.continueOnError = true;
    ExecResult b = engine.execute(collect);
    CHECK(b.hasErrors());
    auto it = b.values.find("r2");
    CHECK(it != b.values.end());
    CHECK(std::get<int64_t>(it->second) == 42);
}

void testConcurrentFanOut() {
    RuleEngine engine;
    std::mutex mu;
    std::vector<std::string> seen;
    engine.context().addFunction("record", [&](const std::vector<Value>& args) -> Value {
        std::this_thread::sleep_for(std::chrono::milliseconds(5));
        std::lock_guard lk(mu);
        seen.push_back(std::get<std::string>(args.at(0)));
        return Value{};
    });
    engine.build(R"(
        rule "a" salience 1 begin record("a") end
        rule "b" salience 1 begin record("b") end
        rule "c" salience 1 begin record("c") end
    )");
    ExecOptions opts; opts.mode = ExecMode::Concurrent;
    ExecResult res = engine.execute(opts);
    CHECK(!res.hasErrors());
    CHECK(seen.size() == 3);
    std::sort(seen.begin(), seen.end());
    CHECK(seen[0] == "a" && seen[1] == "b" && seen[2] == "c");
}

} // namespace

int main() {
    testSalienceOrderingAndAssignment();
    testIfElseReturn();
    testContinueOnError();
    testConcurrentFanOut();
    if (failures == 0) {
        std::cout << "all smoke tests passed\n";
        return 0;
    }
    std::cerr << failures << " failure(s)\n";
    return 1;
}
