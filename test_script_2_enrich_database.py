import json
import tempfile
import unittest
from pathlib import Path

from script_2_enrich_database import build_database


class EnrichDatabaseTest(unittest.TestCase):
    def test_merges_wiki_classes_and_keeps_wiki_only_items(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "questlog" / "raw" / "equipment"
            wiki = root / "wiki"
            target = root / "db"
            source.mkdir(parents=True)
            wiki.mkdir()
            (source / "a.json").write_text(json.dumps({
                "id": "a", "name": "Sword", "mainCategory": "weapon", "grade": 3,
                "equipment": {"requiredClasses": [10]},
            }))
            (wiki / "armor-wiki.txt").write_text("Armor - Slot - Class - Rarity - Attack - Health - Combat\n")
            (wiki / "weapons-wiki.txt").write_text(
                "Weapon;Type;Class;Rarity;Attack;Combat;Durability\n"
                "Sword;Mace;Seer;Rare;23;300;1,200\n"
                "Only Wiki;Mace;Mercenary;Common;20;200;1,100\n"
            )

            build_database(root / "questlog", wiki, target)
            sword = target / "seer" / "weapon" / "rare" / "a.json"
            wiki_only = target / "mercenary" / "weapon" / "common" / "wiki-only-wiki-mace.json"
            self.assertEqual(json.loads(sword.read_text())["classes"], ["Seer"])
            self.assertTrue(wiki_only.exists())


if __name__ == "__main__":
    unittest.main()
