"""Smoke tests for the Python gengine MVP port.

Run with: python -m unittest discover -s ports/python/tests
or:       cd ports/python && python -m unittest discover -s tests
"""

import threading
import time
import unittest

from gengine_py import ExecMode, ExecOptions, RuleEngine


class TestParseAndSort(unittest.TestCase):
    def test_salience_ordering_and_assignment(self):
        log = []
        engine = RuleEngine()
        engine.context.add("log", log.append)
        engine.context.add("threshold", 5)
        engine.build(
            '''
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
            '''
        )
        results, err = engine.execute(ExecOptions(mode=ExecMode.SORT))
        self.assertIsNone(err)
        self.assertEqual(log, ["high", "low"])
        self.assertIn("higher", results)
        self.assertIn("lower", results)

    def test_if_else_and_return_value(self):
        engine = RuleEngine()
        engine.build(
            '''
            rule "pick" salience 1
            begin
                if 2 < 1 {
                    return "unreachable"
                } else {
                    return "else-branch"
                }
            end
            '''
        )
        results, err = engine.execute()
        self.assertIsNone(err)
        self.assertEqual(results["pick"], "else-branch")

    def test_continue_on_error(self):
        engine = RuleEngine()

        def boom():
            raise RuntimeError("explode")

        engine.context.add("boom", boom)
        engine.build(
            '''
            rule "r1" salience 2 begin boom() end
            rule "r2" salience 1 begin return 42 end
            '''
        )
        # Fail-fast: second rule does not run.
        _, err = engine.execute(ExecOptions(continue_on_error=False))
        self.assertIsNotNone(err)
        # Collect: second rule still runs and records its result.
        results, err = engine.execute(ExecOptions(continue_on_error=True))
        self.assertIsNotNone(err)
        self.assertEqual(results.get("r2"), 42)


class TestConcurrent(unittest.TestCase):
    def test_concurrent_fanout(self):
        engine = RuleEngine()
        seen: list[str] = []
        lock = threading.Lock()

        def record(name: str) -> None:
            time.sleep(0.01)  # give the scheduler a chance to interleave
            with lock:
                seen.append(name)

        engine.context.add("record", record)
        engine.build(
            'rule "a" salience 1 begin record("a") end '
            'rule "b" salience 1 begin record("b") end '
            'rule "c" salience 1 begin record("c") end '
        )
        _, err = engine.execute(ExecOptions(mode=ExecMode.CONCURRENT))
        self.assertIsNone(err)
        self.assertEqual(sorted(seen), ["a", "b", "c"])


if __name__ == "__main__":
    unittest.main()
