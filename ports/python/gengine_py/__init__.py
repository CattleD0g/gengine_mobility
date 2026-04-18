"""
gengine_py: a minimal Python port of the Go gengine rule engine.

This is an intentionally small MVP covering a shared DSL subset with the
Java and C++ ports in this repository:

    rule "name" [desc "..."] salience N
    begin
        // statements separated by newlines or semicolons
    end

Supported statements:   assignment, if / else, function call, return
Supported expressions:  literals (int, float, bool, string), identifiers,
                        binary ops (+ - * / == != < <= > >= && ||),
                        unary !, parenthesised groups
Execution modes:        ModeSort  (serial, salience-descending)
                        ModeConcurrent (threads, no ordering)

Not included (kept Go-only for now): for / forRange, DAG, inverse-mix,
N-M batching, plugins, pools, three-level calls.
"""

from .engine import RuleEngine, ExecMode, ExecOptions, Context

__all__ = ["RuleEngine", "ExecMode", "ExecOptions", "Context"]
