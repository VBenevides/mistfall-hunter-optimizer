import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock

from scrape_items import download


class ScraperTest(unittest.TestCase):
    def test_writes_raw_and_categorized_indexes(self):
        session = Mock()
        listings = [
            {"pageData": [{"id": "w1", "name": "Sword"}], "pageCount": 1, "facetDistribution": {"grade": {"1": 1}}},
            {"pageData": [{"id": "a1", "name": "Helmet"}], "pageCount": 1, "facetDistribution": {"grade": {"1": 1}}},
            {"pageData": [{"id": "g1", "name": "Blue Agate"}], "pageCount": 1, "facetDistribution": {"grade": {"0": 1}}},
        ]
        details = [
            {"id": "w1", "name": "Sword", "grade": 1, "subName": "Sword and Shield", "equipment": {}},
            {"id": "a1", "name": "Helmet", "grade": 2, "subName": "Helmet", "equipment": {}},
            {"id": "g1", "name": "Blue Agate", "grade": 0},
        ]
        values = [
            {"result": {"data": value}}
            for listing, detail in zip(listings, details)
            for value in (listing, detail)
        ]
        session.get.side_effect = [Mock(**{"raise_for_status.return_value": None, "json.return_value": value}) for value in values]

        with tempfile.TemporaryDirectory() as directory:
            db = Path(directory) / "db"
            download(session, db)
            self.assertEqual(json.loads((db / "index.json").read_text()), {"w1": "Sword", "a1": "Helmet", "g1": "Blue Agate"})
            self.assertTrue((db / "raw" / "equipment" / "w1.json").exists())
            self.assertTrue((db / "equipment" / "armor" / "helmet" / "2" / "index.json").exists())
            self.assertTrue((db / "gem" / "agate" / "index.json").exists())


if __name__ == "__main__":
    unittest.main()
