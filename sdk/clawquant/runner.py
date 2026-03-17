from __future__ import annotations

import importlib.util
import inspect
import json
import os
import sys
import traceback
import uuid
from pathlib import Path
from typing import Any

import clawquant as clawquant_runtime

from .exchange import Exchange
from .logger import _send, log_error
from .storage import Storage


def run() -> int:
    try:
        strategy_path = _get_strategy_path()
        bot_id = _require_env("CLAWQUANT_BOT_ID")
        params = _load_params(os.getenv("CLAWQUANT_PARAMS", "{}"))
        data_dir = Path(os.getenv("CLAWQUANT_DATA_DIR", "./data"))
        data_dir.mkdir(parents=True, exist_ok=True)

        exchange = Exchange(
            exchange_type=_require_env("CLAWQUANT_EXCHANGE_TYPE"),
            api_key=os.getenv("CLAWQUANT_API_KEY", ""),
            secret=os.getenv("CLAWQUANT_SECRET", ""),
            trading_pair=_require_env("CLAWQUANT_TRADING_PAIR"),
            testnet=_is_truthy(os.getenv("CLAWQUANT_TESTNET", "1")),
        )

        storage = Storage(str(data_dir / "agent.db"), bot_id)
        clawquant_runtime.storage = storage

        module = _load_strategy_module(strategy_path)
        if not hasattr(module, "main"):
            raise RuntimeError("strategy file must define main(exchange, params)")

        result = module.main(exchange, params)
        if inspect.isawaitable(result):
            import asyncio

            asyncio.run(result)

        _send({"type": "exit", "code": 0})
        return 0
    except Exception as exc:  # noqa: BLE001
        log_error("strategy execution failed", {"error": str(exc)})
        traceback.print_exc(file=sys.stderr)
        _send({"type": "exit", "code": 1, "error": str(exc)})
        return 1


def _get_strategy_path() -> Path:
    if len(sys.argv) < 2:
        raise RuntimeError("strategy path is required")

    path = Path(sys.argv[1]).expanduser().resolve()
    if not path.exists():
        raise FileNotFoundError(f"strategy file not found: {path}")
    return path


def _load_strategy_module(strategy_path: Path):
    module_name = f"clawquant_strategy_{strategy_path.stem}_{uuid.uuid4().hex}"
    spec = importlib.util.spec_from_file_location(module_name, strategy_path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"failed to load strategy module: {strategy_path}")

    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _load_params(raw: str) -> dict[str, Any]:
    try:
        value = json.loads(raw or "{}")
    except json.JSONDecodeError as exc:
        raise RuntimeError("CLAWQUANT_PARAMS is not valid JSON") from exc

    if not isinstance(value, dict):
        raise RuntimeError("CLAWQUANT_PARAMS must decode to an object")
    return value


def _require_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def _is_truthy(value: str) -> bool:
    return value.strip().lower() in {"1", "true", "yes", "on"}


if __name__ == "__main__":
    raise SystemExit(run())
