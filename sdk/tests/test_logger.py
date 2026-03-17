import io
import json
import unittest
from contextlib import redirect_stdout

from clawquant.logger import log, log_error, log_profit, log_status, log_warn


class LoggerTests(unittest.TestCase):
    def test_log_outputs_single_line_json(self):
        buffer = io.StringIO()
        with redirect_stdout(buffer):
            log("hello", {"bot": "1"})

        line = buffer.getvalue().strip()
        payload = json.loads(line)
        self.assertEqual(payload["type"], "log")
        self.assertEqual(payload["level"], "info")
        self.assertEqual(payload["message"], "hello")
        self.assertEqual(payload["data"], {"bot": "1"})

    def test_other_log_helpers(self):
        buffer = io.StringIO()
        with redirect_stdout(buffer):
            log_warn("warn")
            log_error("boom")
            log_status("running")
            log_profit(12.5)

        lines = [json.loads(line) for line in buffer.getvalue().splitlines()]
        self.assertEqual([line["type"] for line in lines], ["log", "log", "status", "profit"])
        self.assertEqual(lines[0]["level"], "warn")
        self.assertEqual(lines[1]["level"], "error")


if __name__ == "__main__":
    unittest.main()
