from __future__ import annotations

from typing import Any

import ccxt

from .logger import log_error


class Exchange:
    def __init__(
        self,
        exchange_type: str,
        api_key: str,
        secret: str,
        trading_pair: str,
        testnet: bool = True,
    ):
        self.exchange_type = exchange_type.lower().strip()
        self.symbol = trading_pair.replace("_", "/")
        self.base_currency, self.quote_currency = self.symbol.split("/", 1)
        self.client = self._create_client(api_key, secret, testnet)

    def get_ticker(self) -> dict[str, float] | None:
        try:
            ticker = self.client.fetch_ticker(self.symbol)
            return {
                "last": _to_float(ticker.get("last")),
                "buy": _to_float(ticker.get("bid")),
                "sell": _to_float(ticker.get("ask")),
                "volume": _to_float(ticker.get("baseVolume") or ticker.get("quoteVolume")),
            }
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error("get_ticker", exc)
            return None

    def get_depth(self, limit: int = 20) -> dict[str, list[list[float]]] | None:
        try:
            order_book = self.client.fetch_order_book(self.symbol, limit=limit)
            return {
                "asks": _normalize_levels(order_book.get("asks")),
                "bids": _normalize_levels(order_book.get("bids")),
            }
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error("get_depth", exc)
            return None

    def get_records(self, timeframe: str = "1h", limit: int = 100) -> list[dict[str, float]] | None:
        try:
            candles = self.client.fetch_ohlcv(self.symbol, timeframe=timeframe, limit=limit)
            return [
                {
                    "time": int(item[0]),
                    "open": _to_float(item[1]),
                    "high": _to_float(item[2]),
                    "low": _to_float(item[3]),
                    "close": _to_float(item[4]),
                    "volume": _to_float(item[5]),
                }
                for item in candles
            ]
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error("get_records", exc)
            return None

    def get_account(self) -> dict[str, float] | None:
        try:
            balance = self.client.fetch_balance()
            total = balance.get("total", {})
            used = balance.get("used", {})
            free = balance.get("free", {})
            currency = self.quote_currency if self.quote_currency in total else next(iter(total), self.quote_currency)
            return {
                "balance": _to_float(total.get(currency)),
                "frozen": _to_float(used.get(currency)),
                "available": _to_float(free.get(currency)),
            }
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error("get_account", exc)
            return None

    def buy(self, price: float, amount: float) -> str | None:
        return self._create_order("buy", price, amount)

    def sell(self, price: float, amount: float) -> str | None:
        return self._create_order("sell", price, amount)

    def cancel_order(self, order_id: str) -> bool:
        try:
            self.client.cancel_order(order_id, self.symbol)
            return True
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error("cancel_order", exc)
            return False

    def get_orders(self) -> list[dict[str, Any]] | None:
        try:
            orders = self.client.fetch_open_orders(self.symbol)
            return [dict(order) for order in orders]
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error("get_orders", exc)
            return None

    def get_position(self) -> dict[str, Any] | None:
        try:
            if not hasattr(self.client, "fetch_positions"):
                return None

            positions = self.client.fetch_positions([self.symbol])
            if not positions:
                return None

            position = positions[0]
            return {
                "symbol": position.get("symbol"),
                "side": position.get("side"),
                "contracts": _to_float(position.get("contracts") or position.get("positionAmt")),
                "entry_price": _to_float(position.get("entryPrice")),
                "mark_price": _to_float(position.get("markPrice")),
                "unrealized_pnl": _to_float(position.get("unrealizedPnl")),
                "raw": position,
            }
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error("get_position", exc)
            return None

    def _create_client(self, api_key: str, secret: str, testnet: bool):
        supported = {"binance", "okx"}
        if self.exchange_type not in supported:
            raise ValueError(f"unsupported exchange type: {self.exchange_type}")

        exchange_class = getattr(ccxt, self.exchange_type)
        client = exchange_class(
            {
                "apiKey": api_key,
                "secret": secret,
                "enableRateLimit": True,
            }
        )

        if testnet and hasattr(client, "set_sandbox_mode"):
            client.set_sandbox_mode(True)

        return client

    def _create_order(self, side: str, price: float, amount: float) -> str | None:
        try:
            if price == -1:
                order = self.client.create_order(self.symbol, "market", side, amount, None)
            else:
                order = self.client.create_order(self.symbol, "limit", side, amount, price)
            return order.get("id")
        except Exception as exc:  # noqa: BLE001
            _log_exchange_error(f"{side}_order", exc)
            return None


def _normalize_levels(levels: Any) -> list[list[float]]:
    return [[_to_float(price), _to_float(amount)] for price, amount, *_ in (levels or [])]


def _to_float(value: Any) -> float:
    if value is None:
        return 0.0
    return float(value)


def _log_exchange_error(action: str, exc: Exception) -> None:
    log_error(f"exchange.{action} failed", {"error": str(exc)})
