import json
import tempfile
import unittest
from pathlib import Path

from optimizer import optimize


class OptimizerTest(unittest.TestCase):
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
                    "equipment": {"affixes": [], "holeGroup": [11]},
                }))
            (gems / "g1.json").write_text(json.dumps({
                "id": "g1",
                "name": "Aegis Ruby",
                "gem": {"affixGemType": 1, "affixGemLevel": 1, "affixes": [{"name": "Aegis", "level": 1}]},
            }))
            (gems / "g2.json").write_text(json.dumps({
                "id": "g2",
                "name": "Iron Helmet Ruby",
                "gem": {"affixGemType": 1, "affixGemLevel": 1, "affixes": [{"name": "Iron Helmet", "level": 1}]},
            }))

            result = optimize({"Aegis": 2}, "both", equipment, root / "gem")
            self.assertEqual(result["same"]["levelCombination"], [2] * 8)
            self.assertEqual(result["above"]["levelCombination"], [3, 2, 2, 2, 2, 2, 2, 2])
            self.assertEqual(result["same"]["effects"], {"Aegis": 2})
            self.assertEqual(result["same"]["gemCost"]["count"], 2)
            self.assertIsNone(optimize({"Aegis": 2}, "same", equipment, root / "gem", min_level=3))
            self.assertIsNotNone(optimize({"iRoN_hElMeT": 1}, "same", equipment, root / "gem"))


if __name__ == "__main__":
    unittest.main()
