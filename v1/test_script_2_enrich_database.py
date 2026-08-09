import json
import sqlite3
import tempfile
import unittest
from pathlib import Path

from v1.script_2_enrich_database import build_database


class EnrichDatabaseTest(unittest.TestCase):
    def test_merges_wiki_classes_and_keeps_wiki_only_items(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "questlog" / "raw" / "equipment"
            wiki = root / "wiki"
            target = root / "db.sqlite"
            source.mkdir(parents=True)
            wiki.mkdir()
            (source / "a.json").write_text(
                json.dumps(
                    {
                        "id": "a",
                        "name": "Sword",
                        "mainCategory": "weapon",
                        "grade": 3,
                        "equipment": {"requiredClasses": [10]},
                    }
                )
            )
            (wiki / "armor-wiki.txt").write_text(
                "Armor - Slot - Class - Rarity - Attack - Health - Combat\n"
            )
            (wiki / "weapons-wiki.txt").write_text(
                "Weapon;Type;Class;Rarity;Attack;Combat;Durability\n"
                "Sword;Mace;Seer;Rare;23;300;1,200\n"
                "Only Wiki;Mace;Mercenary;Common;20;200;1,100\n"
            )
            (wiki / "gem-wiki.txt").write_text(
                "Gem;Level;Affixes;Combat;Price\nGuardian Agate;1;Aegis;25;75\n"
            )

            build_database(root / "questlog", wiki, target)
            with sqlite3.connect(target) as connection:
                sword = json.loads(
                    connection.execute("SELECT data FROM items WHERE id = 'a'").fetchone()[0]
                )
                wiki_only = connection.execute(
                    "SELECT 1 FROM items WHERE id = 'wiki-only-wiki-mace'"
                ).fetchone()
                gem = json.loads(
                    connection.execute(
                        "SELECT data FROM items WHERE category = 'affix_gem'"
                    ).fetchone()[0]
                )
            self.assertEqual(sword["classes"], ["Seer"])
            self.assertIsNotNone(wiki_only)
            self.assertEqual(gem["name"], "Guardian Agate")
            self.assertEqual(gem["gem"]["affixGemType"], 1)


if __name__ == "__main__":
    unittest.main()
