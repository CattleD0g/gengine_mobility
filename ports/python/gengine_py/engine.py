"""Interpreter + rule engine for the Python port.

Concurrency model: ExecMode.CONCURRENT uses a ThreadPoolExecutor because
rule bodies may call user-registered I/O-bound functions; the GIL means
this is not CPU-parallel, but it faithfully matches the Go engine's fan-
out shape for users who only need ordering semantics. For CPU-bound
workloads callers should run the engine in a multiprocessing worker.
"""

from __future__ import annotations

import threading
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from enum import Enum
from typing import Any, Callable, Dict, List, Optional

from .parser import (
    Assign,
    BinOp,
    Call,
    Ident,
    If,
    Literal,
    Return,
    Rule,
    UnaryOp,
    parse,
)


# ---------------------------------------------------------------------------
# Public API types. Mirrors the Go ExecOptions/ExecMode for parity.
# ---------------------------------------------------------------------------


class ExecMode(Enum):
    SORT = "sort"            # serial, salience-descending
    CONCURRENT = "concurrent"  # threaded fan-out, ignores salience


@dataclass
class ExecOptions:
    mode: ExecMode = ExecMode.SORT
    continue_on_error: bool = False
    selected: Optional[List[str]] = None
    max_workers: int = 8


# A `_RETURN` sentinel unwinds a rule body when `return <expr>` runs.
# It carries the return value so the engine can stash it per-rule.
class _ReturnSignal(Exception):
    def __init__(self, value: Any):
        super().__init__("return")
        self.value = value


# ---------------------------------------------------------------------------
# Context: holds user-registered variables and functions. Thread-safe for
# reads during concurrent execution; writes from rule bodies are guarded
# by a lock so concurrent `x = ...` assignments are well-defined.
# ---------------------------------------------------------------------------


@dataclass
class Context:
    variables: Dict[str, Any] = field(default_factory=dict)
    functions: Dict[str, Callable[..., Any]] = field(default_factory=dict)
    _lock: threading.Lock = field(default_factory=threading.Lock, repr=False)

    def add(self, name: str, value: Any) -> None:
        with self._lock:
            if callable(value) and not isinstance(value, type):
                self.functions[name] = value
            else:
                self.variables[name] = value

    def get(self, name: str) -> Any:
        with self._lock:
            if name in self.variables:
                return self.variables[name]
        raise NameError(f"unknown variable {name!r}")

    def set(self, name: str, value: Any) -> None:
        with self._lock:
            self.variables[name] = value

    def call(self, name: str, args: List[Any]) -> Any:
        with self._lock:
            fn = self.functions.get(name)
        if fn is None:
            raise NameError(f"unknown function {name!r}")
        return fn(*args)


# ---------------------------------------------------------------------------
# Interpreter
# ---------------------------------------------------------------------------


class _Interpreter:
    def __init__(self, ctx: Context):
        self._ctx = ctx

    def run_rule(self, rule: Rule) -> Any:
        try:
            for stmt in rule.body:
                self._exec(stmt)
            return None
        except _ReturnSignal as r:
            return r.value

    def _exec(self, node: Any) -> None:
        if isinstance(node, Assign):
            self._ctx.set(node.target, self._eval(node.value))
            return
        if isinstance(node, Call):
            self._eval(node)
            return
        if isinstance(node, If):
            if self._truthy(self._eval(node.cond)):
                for s in node.then_block:
                    self._exec(s)
            else:
                for s in node.else_block:
                    self._exec(s)
            return
        if isinstance(node, Return):
            raise _ReturnSignal(self._eval(node.value) if node.value is not None else None)
        raise TypeError(f"unknown statement type {type(node).__name__}")

    def _eval(self, node: Any) -> Any:
        if isinstance(node, Literal):
            return node.value
        if isinstance(node, Ident):
            return self._ctx.get(node.name)
        if isinstance(node, Call):
            args = [self._eval(a) for a in node.args]
            return self._ctx.call(node.name, args)
        if isinstance(node, UnaryOp):
            v = self._eval(node.operand)
            if node.op == "-":
                return -v
            if node.op == "!":
                return not self._truthy(v)
            raise ValueError(f"unknown unary op {node.op}")
        if isinstance(node, BinOp):
            # Short-circuit logical ops before evaluating the RHS so that
            # `a || expensive()` and `a && dangerous()` behave like Go.
            if node.op == "&&":
                return self._truthy(self._eval(node.lhs)) and self._truthy(self._eval(node.rhs))
            if node.op == "||":
                return self._truthy(self._eval(node.lhs)) or self._truthy(self._eval(node.rhs))
            lhs, rhs = self._eval(node.lhs), self._eval(node.rhs)
            match node.op:
                case "+": return lhs + rhs
                case "-": return lhs - rhs
                case "*": return lhs * rhs
                case "/": return lhs / rhs
                case "%": return lhs % rhs
                case "==": return lhs == rhs
                case "!=": return lhs != rhs
                case "<": return lhs < rhs
                case "<=": return lhs <= rhs
                case ">": return lhs > rhs
                case ">=": return lhs >= rhs
            raise ValueError(f"unknown binary op {node.op}")
        raise TypeError(f"unknown expression type {type(node).__name__}")

    @staticmethod
    def _truthy(v: Any) -> bool:
        return bool(v)


# ---------------------------------------------------------------------------
# RuleEngine: user-facing API
# ---------------------------------------------------------------------------


class RuleEngine:
    """
    Usage:

        engine = RuleEngine()
        engine.build('rule "r1" salience 10 begin ... end')
        engine.context.add("threshold", 5)
        engine.context.add("log", print)
        results, err = engine.execute(ExecOptions(mode=ExecMode.SORT))
    """

    def __init__(self) -> None:
        self.context = Context()
        self._rules: List[Rule] = []

    def build(self, source: str) -> None:
        self._rules = parse(source)

    def execute(self, opts: ExecOptions = ExecOptions()) -> tuple[Dict[str, Any], Optional[Exception]]:
        interp = _Interpreter(self.context)
        rules = self._select(opts)
        results: Dict[str, Any] = {}
        errors: List[Exception] = []

        if opts.mode is ExecMode.SORT:
            for r in rules:
                try:
                    results[r.name] = interp.run_rule(r)
                except Exception as exc:
                    errors.append(exc)
                    if not opts.continue_on_error:
                        break
        else:
            # CONCURRENT: independent threads, always collect errors.
            lock = threading.Lock()
            with ThreadPoolExecutor(max_workers=opts.max_workers) as pool:
                futures = {pool.submit(interp.run_rule, r): r for r in rules}
                for fut in as_completed(futures):
                    r = futures[fut]
                    try:
                        value = fut.result()
                        with lock:
                            results[r.name] = value
                    except Exception as exc:
                        with lock:
                            errors.append(exc)

        if errors:
            # Aggregate into a single exception so callers can use one
            # check; individual errors are accessible via .args[1:].
            return results, ExceptionGroup("rule execution errors", errors)
        return results, None

    def _select(self, opts: ExecOptions) -> List[Rule]:
        rules = list(self._rules)
        if opts.selected:
            lookup = {r.name: r for r in rules}
            rules = [lookup[n] for n in opts.selected if n in lookup]
        if opts.mode is ExecMode.SORT:
            rules.sort(key=lambda r: r.salience, reverse=True)
        return rules
