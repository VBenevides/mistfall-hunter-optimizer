import json
import sqlite3
import tempfile
import unittest
from pathlib import Path

from optimizer import _compatible, _load_database, format_text, optimize


class OptimizerTest(unittest.TestCase):
    def test_gem_requires_socket_type_and_tier(self):
        gem = {"gem": {"affixGemType": 1, "affixGemLevel": 1}}
        self.assertTrue(_compatible(gem, 11))
        self.assertTrue(_compatible(gem, {"type": -1, "level": 1}))
        self.assertFalse(_compatible({"gem": {"affixGemType": 2, "affixGemLevel": 1}}, 11))
        self.assertFalse(_compatible({"gem": {"affixGemType": 1, "affixGemLevel": 2}}, 11))

    def test_class_loader_includes_all_classes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            database = root / "db.sqlite"
            with sqlite3.connect(database) as connection:
                connection.executescript(
                    """
                    CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT, category TEXT, grade INTEGER, rarity TEXT, data TEXT);
                    CREATE TABLE item_classes (item_id TEXT, class_slug TEXT, PRIMARY KEY (item_id, class_slug));
                    """
                )
                for class_name, item_id in (("mercenary", "m"), ("sorcerer", "s"), ("all-classes", "a")):
                    item = {"id": item_id, "mainCategory": "weapon"}
                    connection.execute(
                        "INSERT INTO items VALUES (?, ?, ?, ?, ?, ?)",
                        (item_id, item_id, "weapon", 1, "damaged", json.dumps(item)),
                    )
                    connection.execute("INSERT INTO item_classes VALUES (?, ?)", (item_id, class_name))
                gem = {"id": "g", "mainCategory": "affix_gem", "gem": {"affixes": []}}
                connection.execute(
                    "INSERT INTO items VALUES (?, ?, ?, ?, ?, ?)",
                    ("g", "Ruby", "affix_gem", 0, None, json.dumps(gem)),
                )

            equipment, gems = _load_database(database, root / "gem", "mercenary")
            self.assertEqual({item["id"] for item in equipment}, {"m", "a"})
            self.assertEqual({item["id"] for item in gems}, {"g"})

    def test_uniform_armor_and_weapon_options(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            equipment = root / "equipment"
            gems = root / "gem" / "ruby"
            equipment.mkdir(parents=True)
            gems.mkdir(parents=True)
            slots = ["boot", "clothe", "gauntlet", "helmet", "necklace", "pants", "ring"]
            records = [("weapon-t2", "weapon", 2), ("weapon-t3", "weapon", 3)]
            records += [(f"{slot}-t2", slot, 2) for slot in slots]
            for item_id, slot, grade in records:
                (equipment / f"{item_id}.json").write_text(json.dumps({
                    "id": item_id,
                    "name": item_id,
                    "mainCategory": "weapon" if slot == "weapon" else "armor",
                    "subName": slot,
                    "grade": grade,
                    "minPrice": 1,
                    "maxPrice": 1,
                    "recommendedPrice": 1,
                    "equipment": {"affixes": [], "holeGroup": [11]},
                }))
            (gems / "g1.json").write_text(json.dumps({
                "id": "g1",
                "name": "Aegis Ruby",
                "minPrice": 1,
                "maxPrice": 3,
                "recommendedPrice": 2,
                "gem": {"affixGemType": 1, "affixGemLevel": 1, "affixes": [{"name": "Aegis", "level": 1}]},
            }))
            (gems / "g2.json").write_text(json.dumps({
                "id": "g2",
                "name": "Iron Helmet Ruby",
                "minPrice": 1,
                "maxPrice": 3,
                "recommendedPrice": 2,
                "gem": {"affixGemType": 1, "affixGemLevel": 1, "affixes": [{"name": "Iron Helmet", "level": 1}]},
            }))

            result = optimize({"Aegis": 2}, "both", equipment, root / "gem")
            self.assertEqual(result["same"]["levelCombination"], [2] * 8)
            self.assertEqual(result["above"]["levelCombination"], [3, 2, 2, 2, 2, 2, 2, 2])
            self.assertEqual(result["same"]["effects"], {"Aegis": 2})
            self.assertEqual(result["same"]["gemCost"]["count"], 2)
            self.assertEqual(result["same"]["minPrice"], 10)
            self.assertEqual(result["same"]["maxPrice"], 14)
            self.assertEqual(result["same"]["averagePrice"], 12)
            self.assertIn("pieces", result["same"])
            self.assertEqual(
                [piece["slot"] for piece in result["same"]["pieces"]],
                ["weapon", "helmet", "clothe", "gauntlet", "pants", "boot", "necklace", "ring"],
            )
            self.assertEqual(result["same"]["pieces"][0]["nativeAffixes"], "No Native Affix")
            text = format_text(result["same"])
            self.assertIn("Rarity: Armor (Common) - Weapon (Common)", text)
            self.assertIn("Affixes: Aegis (2)", text)
            self.assertIn("Price: 10 / 12 / 14", text)
            self.assertIn("No Native Affix: Agate (Aegis Ruby)", text)
            unavailable = optimize({"Aegis": 2}, "same", equipment, root / "gem", min_rarity="RARE")
            self.assertFalse(unavailable["possible"])
            self.assertIn("No set", unavailable["reason"])
            self.assertEqual(unavailable["independentMaximums"], {"Aegis": 8})
            limited = optimize({"Aegis": 2}, "same", equipment, root / "gem", max_rarity="damaged")
            self.assertFalse(limited["possible"])
            self.assertEqual(limited["independentMaximums"], {"Aegis": 0})
            self.assertEqual(
                optimize({"Iron Helmet": 9}, "same", equipment, root / "gem", max_rarity=2)["independentMaximums"],
                {"Iron Helmet": 8},
            )
            self.assertIsNotNone(optimize({"iRoN_hElMeT": 1}, "same", equipment, root / "gem"))

            (gems / "g3.json").write_text(json.dumps({
                "id": "g3",
                "name": "Aegis IV Ruby",
                "minPrice": 1,
                "maxPrice": 1,
                "recommendedPrice": 1,
                "gem": {"affixGemType": 1, "affixGemLevel": 1, "affixes": [{"name": "Aegis", "level": 4}]},
            }))
            minimum = optimize({"Aegis": 3}, "same", equipment, root / "gem")
            self.assertTrue(minimum["possible"])
            self.assertEqual(minimum["effects"], {"Aegis": 3})
            self.assertEqual(minimum["gemCost"]["count"], 1)


if __name__ == "__main__":
    unittest.main()
