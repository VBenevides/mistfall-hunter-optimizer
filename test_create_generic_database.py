import json
import tempfile
import unittest
from pathlib import Path

from create_generic_database import create_database


class GenericDatabaseTest(unittest.TestCase):
    def test_groups_matching_affixes_and_holes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "raw"
            target = root / "simplified"
            source.mkdir()
            base = {
                "mainCategory": "weapon",
                "subName": "Greatsword",
                "grade": 4,
                "equipment": {"affixes": [{"name": "Aegis", "level": 1}], "holeGroup": [11, 21]},
            }
            for item_id in ("1", "2"):
                (source / f"{item_id}.json").write_text(json.dumps({**base, "id": item_id, "name": f"Item {item_id}"}))
            different = {**base, "id": "3", "name": "Item 3", "equipment": {"affixes": [], "holeGroup": [11, 21]}}
            (source / "3.json").write_text(json.dumps(different))

            self.assertEqual(create_database(source, target), 2)
            self.assertEqual(len(list((target / "equipment").glob("*.json"))), 2)
            records = [json.loads(path.read_text()) for path in (target / "equipment").glob("weapon-t4*.json")]
            grouped = next(record for record in records if record["sourceIds"] == ["1", "2"])
            self.assertEqual(grouped["equipment"]["holeGroup"], [11, 21])
            self.assertEqual(grouped["itemIncludes"], [{"id": "1", "name": "Item 1"}, {"id": "2", "name": "Item 2"}])


if __name__ == "__main__":
    unittest.main()
