import json
import sqlite3
import threading
from pathlib import Path
from typing import Any


class Storage:
    def __init__(self, db_path: str, bot_id: str):
        self._db_path = Path(db_path)
        self._db_path.parent.mkdir(parents=True, exist_ok=True)
        self._bot_id = bot_id
        self._lock = threading.RLock()
        self._conn = sqlite3.connect(self._db_path, check_same_thread=False)
        self._conn.execute("PRAGMA journal_mode = WAL;")
        self._conn.execute(
            """
            CREATE TABLE IF NOT EXISTS storage (
                bot_id TEXT NOT NULL,
                key TEXT NOT NULL,
                value TEXT,
                PRIMARY KEY(bot_id, key)
            );
            """
        )
        self._conn.commit()

    def get(self, key: str, default: Any = None) -> Any:
        with self._lock:
            row = self._conn.execute(
                "SELECT value FROM storage WHERE bot_id = ? AND key = ?",
                (self._bot_id, key),
            ).fetchone()

        if row is None or row[0] is None:
            return default

        try:
            return json.loads(row[0])
        except json.JSONDecodeError:
            return default

    def set(self, key: str, value: Any) -> None:
        payload = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
        with self._lock:
            self._conn.execute(
                """
                INSERT INTO storage (bot_id, key, value)
                VALUES (?, ?, ?)
                ON CONFLICT(bot_id, key) DO UPDATE SET value = excluded.value
                """,
                (self._bot_id, key, payload),
            )
            self._conn.commit()

    def close(self) -> None:
        with self._lock:
            self._conn.close()
