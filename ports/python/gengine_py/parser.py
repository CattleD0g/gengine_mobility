"""Hand-rolled lexer + recursive-descent parser for the gengine MVP DSL.

No third-party dependencies: keeps `pip install` out of the way for the
port's smoke tests.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any, List, Optional


# ---------------------------------------------------------------------------
# AST node types. Kept as small dataclasses rather than a class hierarchy
# because the interpreter dispatches on type() in a single match statement.
# ---------------------------------------------------------------------------


@dataclass
class Literal:
    value: Any


@dataclass
class Ident:
    name: str


@dataclass
class BinOp:
    op: str
    lhs: Any
    rhs: Any


@dataclass
class UnaryOp:
    op: str
    operand: Any


@dataclass
class Call:
    name: str
    args: List[Any]


@dataclass
class Assign:
    target: str
    value: Any


@dataclass
class If:
    cond: Any
    then_block: List[Any]
    else_block: List[Any] = field(default_factory=list)


@dataclass
class Return:
    value: Optional[Any]


@dataclass
class Rule:
    name: str
    desc: str = ""
    salience: int = 0
    body: List[Any] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Lexer
# ---------------------------------------------------------------------------

# Token kinds are strings rather than an enum to keep the parser compact.
# `value` carries the already-coerced literal for NUMBER / STRING tokens.

_TOKEN_SPEC = [
    ("NUMBER", r"\d+\.\d+|\d+"),
    ("STRING", r'"(?:\\.|[^"\\])*"'),
    ("OP", r"==|!=|<=|>=|&&|\|\||[+\-*/%=<>!(){},;]"),
    ("IDENT", r"[A-Za-z_][A-Za-z_0-9]*"),
    ("NEWLINE", r"\n"),
    ("SKIP", r"[ \t\r]+"),
    ("COMMENT", r"//[^\n]*"),
]
_TOKEN_RE = re.compile("|".join(f"(?P<{n}>{p})" for n, p in _TOKEN_SPEC))

# Reserved words that must not be treated as identifiers. Matches the Go
# port's keywords where relevant; extras like `desc` are absorbed by the
# rule-header parser and never reach expression parsing.
_KEYWORDS = {"rule", "desc", "salience", "begin", "end",
             "if", "else", "true", "false", "return"}


@dataclass
class Token:
    kind: str
    value: Any
    line: int
    col: int


def tokenize(source: str) -> List[Token]:
    tokens: List[Token] = []
    line = 1
    line_start = 0
    pos = 0
    for m in _TOKEN_RE.finditer(source):
        if m.start() != pos:
            col = pos - line_start + 1
            raise SyntaxError(f"unexpected character {source[pos]!r} at line {line}:{col}")
        pos = m.end()
        kind = m.lastgroup
        text = m.group()
        col = m.start() - line_start + 1
        if kind == "SKIP" or kind == "COMMENT":
            continue
        if kind == "NEWLINE":
            line += 1
            line_start = m.end()
            continue
        if kind == "NUMBER":
            tokens.append(Token(kind, float(text) if "." in text else int(text), line, col))
        elif kind == "STRING":
            # Strip surrounding quotes and apply a minimal unescape set.
            raw = text[1:-1]
            tokens.append(Token(kind, bytes(raw, "utf-8").decode("unicode_escape"), line, col))
        elif kind == "IDENT" and text in _KEYWORDS:
            tokens.append(Token(text, text, line, col))
        else:
            tokens.append(Token(kind, text, line, col))
    if pos != len(source):
        raise SyntaxError(f"unexpected trailing input at offset {pos}")
    tokens.append(Token("EOF", None, line, 1))
    return tokens


# ---------------------------------------------------------------------------
# Parser
# ---------------------------------------------------------------------------


class Parser:
    def __init__(self, tokens: List[Token]):
        self._toks = tokens
        self._i = 0

    # --- helpers ----------------------------------------------------------

    def _peek(self, k: int = 0) -> Token:
        return self._toks[self._i + k]

    def _eat(self, kind: str, value: Optional[Any] = None) -> Token:
        tok = self._toks[self._i]
        if tok.kind != kind or (value is not None and tok.value != value):
            want = kind if value is None else f"{kind}({value!r})"
            raise SyntaxError(f"expected {want} at line {tok.line}:{tok.col}, got {tok.kind}={tok.value!r}")
        self._i += 1
        return tok

    def _accept(self, kind: str, value: Optional[Any] = None) -> Optional[Token]:
        tok = self._toks[self._i]
        if tok.kind == kind and (value is None or tok.value == value):
            self._i += 1
            return tok
        return None

    # --- top-level --------------------------------------------------------

    def parse_program(self) -> List[Rule]:
        rules: List[Rule] = []
        while self._peek().kind != "EOF":
            rules.append(self._parse_rule())
        return rules

    def _parse_rule(self) -> Rule:
        self._eat("rule")
        name_tok = self._eat("STRING")
        rule = Rule(name=name_tok.value)
        if self._accept("desc"):
            rule.desc = self._eat("STRING").value
        if self._accept("salience"):
            sign = -1 if self._accept("OP", "-") else 1
            rule.salience = sign * self._eat("NUMBER").value
        self._eat("begin")
        while self._peek().kind != "end":
            rule.body.append(self._parse_statement())
        self._eat("end")
        return rule

    # --- statements -------------------------------------------------------

    def _parse_statement(self) -> Any:
        tok = self._peek()
        if tok.kind == "if":
            return self._parse_if()
        if tok.kind == "return":
            self._i += 1
            val = None
            if self._peek().kind not in {"end", "if", "return", "IDENT"} or self._starts_expr():
                val = self._parse_expression()
            return Return(val)
        if tok.kind == "IDENT":
            # lookahead: assignment vs. call
            nxt = self._peek(1)
            if nxt.kind == "OP" and nxt.value == "=":
                self._i += 1  # IDENT
                self._i += 1  # =
                return Assign(target=tok.value, value=self._parse_expression())
            if nxt.kind == "OP" and nxt.value == "(":
                return self._parse_call()
        raise SyntaxError(f"unexpected token {tok.kind}={tok.value!r} at line {tok.line}:{tok.col}")

    def _starts_expr(self) -> bool:
        t = self._peek()
        if t.kind in {"NUMBER", "STRING", "IDENT", "true", "false"}:
            return True
        return t.kind == "OP" and t.value in {"-", "!", "("}

    def _parse_if(self) -> If:
        self._eat("if")
        cond = self._parse_expression()
        self._eat("OP", "{")
        then_block = self._parse_block_body()
        self._eat("OP", "}")
        else_block: List[Any] = []
        if self._accept("else"):
            self._eat("OP", "{")
            else_block = self._parse_block_body()
            self._eat("OP", "}")
        return If(cond=cond, then_block=then_block, else_block=else_block)

    def _parse_block_body(self) -> List[Any]:
        body: List[Any] = []
        while not (self._peek().kind == "OP" and self._peek().value == "}"):
            body.append(self._parse_statement())
        return body

    def _parse_call(self) -> Call:
        name = self._eat("IDENT").value
        self._eat("OP", "(")
        args: List[Any] = []
        if not (self._peek().kind == "OP" and self._peek().value == ")"):
            args.append(self._parse_expression())
            while self._accept("OP", ","):
                args.append(self._parse_expression())
        self._eat("OP", ")")
        return Call(name=name, args=args)

    # --- expressions (Pratt-lite with fixed precedences) ------------------

    _PRECEDENCE = {
        "||": 1, "&&": 2,
        "==": 3, "!=": 3, "<": 3, "<=": 3, ">": 3, ">=": 3,
        "+": 4, "-": 4,
        "*": 5, "/": 5, "%": 5,
    }

    def _parse_expression(self, min_prec: int = 0) -> Any:
        lhs = self._parse_unary()
        while True:
            t = self._peek()
            if t.kind != "OP" or t.value not in self._PRECEDENCE:
                break
            prec = self._PRECEDENCE[t.value]
            if prec < min_prec:
                break
            op = t.value
            self._i += 1
            rhs = self._parse_expression(prec + 1)
            lhs = BinOp(op=op, lhs=lhs, rhs=rhs)
        return lhs

    def _parse_unary(self) -> Any:
        if self._peek().kind == "OP" and self._peek().value in {"-", "!"}:
            op = self._peek().value
            self._i += 1
            return UnaryOp(op=op, operand=self._parse_unary())
        return self._parse_primary()

    def _parse_primary(self) -> Any:
        t = self._peek()
        if t.kind == "NUMBER" or t.kind == "STRING":
            self._i += 1
            return Literal(t.value)
        if t.kind == "true":
            self._i += 1
            return Literal(True)
        if t.kind == "false":
            self._i += 1
            return Literal(False)
        if t.kind == "IDENT":
            if self._peek(1).kind == "OP" and self._peek(1).value == "(":
                return self._parse_call()
            self._i += 1
            return Ident(t.value)
        if t.kind == "OP" and t.value == "(":
            self._i += 1
            e = self._parse_expression()
            self._eat("OP", ")")
            return e
        raise SyntaxError(f"unexpected token {t.kind}={t.value!r} at line {t.line}:{t.col}")


def parse(source: str) -> List[Rule]:
    """Tokenize and parse a full program into a list of Rule AST nodes."""
    return Parser(tokenize(source)).parse_program()
