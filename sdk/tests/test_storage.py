import tempfile
import unittest
from pathlib import Path

from clawquant.storage import Storage


class StorageTests(unittest.TestCase):
    def test_storage_round_trip(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            store = Storage(str(Path(temp_dir) / "agent.db"), "bot-1")

            self.assertIsNone(store.get("missing"))
            self.assertEqual(store.get("missing", 7), 7)

            store.set("state", {"count": 1})
            self.assertEqual(store.get("state"), {"count": 1})

            store.set("state", {"count": 2})
            self.assertEqual(store.get("state"), {"count": 2})
            store.close()


if __name__ == "__main__":
    unittest.main()
