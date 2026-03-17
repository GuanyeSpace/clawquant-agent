import json
import sys
import threading
from typing import Any


_LOCK = threading.Lock()


def log(message: str, *args: Any) -> None:
    payload = _args_to_data(args)
    _send(
        {
            "type": "log",
            "level": "info",
            "message": str(message),
            "data": payload,
        }
    )


def log_warn(message: str, data: Any = None) -> None:
    _send(
        {
            "type": "log",
            "level": "warn",
            "message": str(message),
            "data": _normalize_data(data),
        }
    )


def log_error(message: str, data: Any = None) -> None:
    _send(
        {
            "type": "log",
            "level": "error",
            "message": str(message),
            "data": _normalize_data(data),
        }
    )


def log_profit(value: float) -> None:
    _send({"type": "profit", "value": value})


def log_status(message: str) -> None:
    _send({"type": "status", "message": str(message)})


def _send(msg: dict[str, Any]) -> None:
    line = json.dumps(msg, ensure_ascii=False, separators=(",", ":"), default=str)
    with _LOCK:
        sys.stdout.write(line + "\n")
        sys.stdout.flush()


def _normalize_data(data: Any) -> Any:
    if data is None:
        return {}
    return data


def _args_to_data(args: tuple[Any, ...]) -> Any:
    if not args:
        return {}
    if len(args) == 1 and isinstance(args[0], dict):
        return args[0]
    return {"args": list(args)}
