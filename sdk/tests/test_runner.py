import io
import json
import os
import tempfile
import textwrap
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock

import clawquant
from clawquant import runner


class FakeExchange:
    def __init__(self, *args, **kwargs):
        self.args = args
        self.kwargs = kwargs


class RunnerTests(unittest.TestCase):
    def test_runner_executes_strategy_and_sets_storage(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            strategy_path = Path(temp_dir) / "strategy.py"
            strategy_path.write_text(
                textwrap.dedent(
                    """
                    from clawquant import log, storage

                    def main(exchange, params):
                        storage.set("counter", params["count"])
                        log("started", {"exchange": exchange.__class__.__name__})
                    """
                ),
                encoding="utf-8",
            )

            env = {
                "CLAWQUANT_BOT_ID": "bot-1",
                "CLAWQUANT_EXCHANGE_TYPE": "binance",
                "CLAWQUANT_API_KEY": "key",
                "CLAWQUANT_SECRET": "secret",
                "CLAWQUANT_TRADING_PAIR": "BTC_USDT",
                "CLAWQUANT_PARAMS": json.dumps({"count": 2}),
                "CLAWQUANT_DATA_DIR": temp_dir,
                "CLAWQUANT_TESTNET": "1",
            }

            buffer = io.StringIO()
            with mock.patch.dict(os.environ, env, clear=False), mock.patch.object(
                runner, "Exchange", FakeExchange
            ), mock.patch("sys.argv", ["python", str(strategy_path)]), redirect_stdout(buffer):
                exit_code = runner.run()

            self.assertEqual(exit_code, 0)
            lines = [json.loads(line) for line in buffer.getvalue().splitlines()]
            self.assertEqual(lines[-1], {"type": "exit", "code": 0})
            self.assertEqual(lines[0]["type"], "log")
            self.assertEqual(clawquant.storage.get("counter"), 2)
            clawquant.storage.close()
            clawquant.storage = None


if __name__ == "__main__":
    unittest.main()
