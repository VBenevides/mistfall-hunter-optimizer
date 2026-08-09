import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

from populate_database import populate


class PopulateTest(unittest.TestCase):
    def test_uses_capture_and_writes_detail_files(self):
        with tempfile.TemporaryDirectory() as directory:
            db = Path(directory) / "db"
            (db / "equipment" / "weapon" / "sword" / "1").mkdir(parents=True)
            (db / "raw" / "gem").mkdir(parents=True)
            (db / "equipment" / "weapon" / "sword" / "1" / "index.json").write_text('{"w1":"Sword"}')
            (db / "raw" / "gem" / "index.json").write_text('{"g1":"Blue Agate"}')
            capture = Path(directory) / "item.txt"
            capture.write_text("curl 'https://example.test/database.getItem?input=old'")

            session = Mock()
            details = [
                {"id": "w1", "name": "Sword", "grade": 1, "subName": "Sword"},
                {"id": "g1", "name": "Blue Agate", "grade": 0},
            ]
            session.get.side_effect = [
                Mock(**{"raise_for_status.return_value": None, "json.return_value": {"result": {"data": detail}}})
                for detail in details
            ]

            populate(session, db, capture)
            self.assertEqual(json.loads((db / "equipment/weapon/sword/1/w1.json").read_text())["id"], "w1")
            self.assertTrue((db / "gem/agate/g1.json").exists())


if __name__ == "__main__":
    unittest.main()
