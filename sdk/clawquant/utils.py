import time
from collections.abc import Callable
from typing import TypeVar


T = TypeVar("T")


def sleep(ms: int) -> None:
    time.sleep(ms / 1000.0)


def retry(func: Callable[[], T], max_retries: int = 3, delay_ms: int = 1000) -> T:
    last_error = None
    for attempt in range(max_retries):
        try:
            return func()
        except Exception as exc:  # noqa: BLE001
            last_error = exc
            if attempt == max_retries - 1:
                raise
            sleep(delay_ms)

    raise last_error  # pragma: no cover
