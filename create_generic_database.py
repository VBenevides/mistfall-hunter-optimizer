import json
import shutil
from collections import defaultdict
from pathlib import Path


SOURCE = Path(__file__).parent / "db" / "raw" / "equipment"
TARGET = Path(__file__).parent / "db_simplified"
SLOTS = {
    "Boots": "boot",
    "Clothes": "clothe",
    "Gauntlets": "gauntlet",
    "Helmet": "helmet",
    "Necklace": "necklace",
    "Pants": "pants",
    "Ring": "ring",
}


def signature(item):
    equipment = item.get("equipment", {})
    affixes = tuple(sorted((a.get("name"), a.get("level", 1)) for a in equipment.get("affixes", [])))
    holes = tuple(sorted(equipment.get("holeGroup", [])))
    slot = "weapon" if item["mainCategory"] == "weapon" else SLOTS[item["subName"]]
    return item["mainCategory"], slot, int(item["grade"]), affixes, holes


def create_database(source=SOURCE, target=TARGET):
    groups = defaultdict(list)
    for path in source.glob("*.json"):
        if path.name == "index.json":
            continue
        item = json.loads(path.read_text())
        groups[signature(item)].append(item)

    if target.exists():
        shutil.rmtree(target)
    output = target / "equipment"
    output.mkdir(parents=True)
    index = {}
    serials = defaultdict(int)

    for key in sorted(groups):
        category, slot, grade, affixes, holes = key
        serials[(slot, grade)] += 1
        serial = serials[(slot, grade)]
        stem = f"{slot}-t{grade}" + (f"-{serial}" if serial > 1 else "")
        items = sorted(groups[key], key=lambda item: item["id"])
        generic = {
            "id": stem,
            "name": f"Generic {slot.title()} T{grade}" + (f" #{serial}" if serial > 1 else ""),
            "mainCategory": category,
            "subName": slot,
            "grade": grade,
            "equipment": {
                "affixes": [{"name": name, "level": level} for name, level in affixes],
                "holeGroup": list(holes),
            },
            "sourceIds": [item["id"] for item in items],
            "itemIncludes": [{"id": item["id"], "name": item["name"]} for item in items],
        }
        path = output / f"{stem}.json"
        path.write_text(json.dumps(generic, ensure_ascii=False, indent=2) + "\n")
        index[stem] = {"category": category, "slot": slot, "grade": grade, "sourceCount": len(items)}

    (target / "index.json").write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n")
    return len(groups)


if __name__ == "__main__":
    print(f"Created {create_database()} generic equipment records")
