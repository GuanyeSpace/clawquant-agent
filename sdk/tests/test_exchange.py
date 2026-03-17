import unittest
from unittest import mock

from clawquant.exchange import Exchange


class FakeClient:
    def __init__(self):
        self.sandbox = False

    def set_sandbox_mode(self, enabled):
        self.sandbox = enabled

    def fetch_ticker(self, symbol):
        self.last_symbol = symbol
        return {"last": 101.5, "bid": 101.0, "ask": 102.0, "baseVolume": 4}

    def fetch_order_book(self, symbol, limit=20):
        self.last_depth = (symbol, limit)
        return {"asks": [[102, 1]], "bids": [[101, 2]]}

    def fetch_ohlcv(self, symbol, timeframe="1h", limit=100):
        self.last_records = (symbol, timeframe, limit)
        return [[1700000000000, 1, 2, 0.5, 1.5, 100]]

    def fetch_balance(self):
        return {
            "total": {"USDT": 1000},
            "used": {"USDT": 100},
            "free": {"USDT": 900},
        }

    def create_order(self, symbol, order_type, side, amount, price):
        self.last_order = (symbol, order_type, side, amount, price)
        return {"id": f"{side}-123"}

    def cancel_order(self, order_id, symbol):
        self.last_cancel = (order_id, symbol)

    def fetch_open_orders(self, symbol):
        self.last_open_orders = symbol
        return [{"id": "1", "symbol": symbol}]

    def fetch_positions(self, symbols):
        self.last_positions = symbols
        return [
            {
                "symbol": symbols[0],
                "side": "long",
                "contracts": 2,
                "entryPrice": 100,
                "markPrice": 110,
                "unrealizedPnl": 20,
            }
        ]


class ExchangeTests(unittest.TestCase):
    @mock.patch("clawquant.exchange.ccxt.binance")
    def test_exchange_methods(self, mock_binance):
        client = FakeClient()
        mock_binance.return_value = client

        exchange = Exchange("binance", "key", "secret", "BTC_USDT")
        self.assertTrue(client.sandbox)
        self.assertEqual(exchange.symbol, "BTC/USDT")

        self.assertEqual(exchange.get_ticker()["last"], 101.5)
        self.assertEqual(exchange.get_depth()["asks"][0], [102.0, 1.0])
        self.assertEqual(exchange.get_records()[0]["close"], 1.5)
        self.assertEqual(exchange.get_account()["available"], 900.0)
        self.assertEqual(exchange.buy(-1, 1), "buy-123")
        self.assertEqual(exchange.sell(100, 1), "sell-123")
        self.assertTrue(exchange.cancel_order("1"))
        self.assertEqual(exchange.get_orders()[0]["id"], "1")
        self.assertEqual(exchange.get_position()["side"], "long")

        self.assertEqual(client.last_order, ("BTC/USDT", "limit", "sell", 1, 100))

    @mock.patch("clawquant.exchange.ccxt.okx")
    @mock.patch("clawquant.exchange.log_error")
    def test_exchange_returns_none_on_error(self, mock_log_error, mock_okx):
        client = FakeClient()
        client.fetch_ticker = mock.Mock(side_effect=RuntimeError("boom"))
        mock_okx.return_value = client

        exchange = Exchange("okx", "key", "secret", "ETH_USDT")
        self.assertIsNone(exchange.get_ticker())
        self.assertTrue(mock_log_error.called)


if __name__ == "__main__":
    unittest.main()
