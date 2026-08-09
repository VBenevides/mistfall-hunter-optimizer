import json
import re
import shutil
from pathlib import Path


ROOT = Path(__file__).parent
SOURCE = ROOT / "db-questlog"
WIKI = ROOT / "db-wiki"
TARGET = ROOT / "db"
CLASS_NAMES = {
    10: "Mercenary",
    11: "Sorcerer",
    12: "Blackarrow",
    13: "Shadowstrix",
    14: "Seer",
    15: "Withered Knight",
}
RARITIES = {
    1: "damaged",
    2: "common",
    3: "rare",
    4: "excellent",
    5: "epic",
    6: "legendary",
}
GRADE_BY_RARITY = {value: key for key, value in RARITIES.items()}


def slug(value):
    return re.sub(r"[^a-z0-9]+", "-", str(value).casefold()).strip("-")


def number(value):
    value = value.strip().replace(",", "")
    return int(value) if value else None


def parse_wiki(path, category):
    rows = {}
    for line in path.read_text().splitlines()[1:]:
        fields = line.split(";")
        if category == "armor":
            if len(fields) != 7:
                continue
            name, slot, classes, rarity, attack, health, combat = fields
            data = {
                "name": name,
                "slot": slot,
                "classes": classes,
                "rarity": rarity,
                "attack": number(attack),
                "health": number(health),
                "combat": number(combat),
            }
        else:
            if len(fields) == 7:
                set_name = None
                name, weapon_type, classes, rarity, attack, combat, durability = fields
            elif len(fields) == 8:
                set_name, name, weapon_type, classes, rarity, attack, combat, durability = fields
            else:
                continue
            data = {
                "name": name,
                "set": set_name,
                "type": weapon_type,
                "classes": classes,
                "rarity": rarity,
                "attack": number(attack),
                "combat": number(combat),
                "durability": number(durability),
            }
        rows.setdefault(name.casefold(), []).append(data)
    return rows


def wiki_classes(value):
    if value.casefold() == "all classes":
        return ["All classes"]
    return [part.strip() for part in value.split(",") if part.strip()]


def questlog_classes(item):
    classes = [CLASS_NAMES.get(int(value), str(value)) for value in item.get("equipment", {}).get("requiredClasses", [])]
    return classes or ["All classes"]


def load_items(source):
    for path in (source / "raw" / "equipment").glob("*.json"):
        if path.name != "index.json":
            yield json.loads(path.read_text())


def build_database(source=SOURCE, wiki_dir=WIKI, target=TARGET):
    wiki = {}
    for category, filename in (("armor", "armor-wiki.txt"), ("weapon", "weapons-wiki.txt")):
        wiki[category] = parse_wiki(wiki_dir / filename, category)

    if target.exists():
        shutil.rmtree(target)
    target.mkdir(parents=True)
    index = {}
    matched = 0
    represented_wiki = {category: set() for category in wiki}

    for item in load_items(source):
        category = item.get("mainCategory")
        if category not in wiki:
            continue
        matches = wiki[category].get(item["name"].casefold(), [])
        questlog_rarity = RARITIES.get(item.get("grade"))
        wiki_item = next(
            (candidate for candidate in matches if slug(candidate["rarity"]) == questlog_rarity),
            matches[0] if matches else None,
        )
        classes = wiki_classes(wiki_item["classes"]) if wiki_item else questlog_classes(item)
        rarity = slug(wiki_item["rarity"]) if wiki_item else RARITIES.get(item.get("grade"), str(item.get("grade", "unknown")))
        enriched = dict(item)
        enriched["classes"] = classes
        enriched["rarity"] = rarity
        if wiki_item:
            enriched["wiki"] = wiki_item
            represented_wiki[category].add((item["name"].casefold(), slug(wiki_item["rarity"])))
            matched += 1
        paths = []
        for class_name in classes:
            path = target / slug(class_name) / category / rarity / f'{item["id"]}.json'
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(enriched, ensure_ascii=False, indent=2) + "\n")
            paths.append(str(path.relative_to(target)))
        index[item["id"]] = {"name": item["name"], "classes": classes, "paths": paths}

    wiki_only = 0
    for category, rows in wiki.items():
        for name, matches in rows.items():
            for serial, wiki_item in enumerate(matches, 1):
                if (name, slug(wiki_item["rarity"])) in represented_wiki[category]:
                    continue
                classes = wiki_classes(wiki_item["classes"])
                rarity = slug(wiki_item["rarity"])
                stem = slug(f"{wiki_item.get('set') or ''}-{wiki_item['name']}-{wiki_item.get('type') or wiki_item.get('slot')}")
                item_id = f"wiki-{stem}" + (f"-{serial}" if len(matches) > 1 else "")
                enriched = {
                    "id": item_id,
                    "name": wiki_item["name"],
                    "mainCategory": category,
                    "grade": GRADE_BY_RARITY.get(rarity, 7),
                    "classes": classes,
                    "rarity": rarity,
                    "wiki": wiki_item,
                }
                paths = []
                for class_name in classes:
                    path = target / slug(class_name) / category / rarity / f"{item_id}.json"
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text(json.dumps(enriched, ensure_ascii=False, indent=2) + "\n")
                    paths.append(str(path.relative_to(target)))
                index[item_id] = {"name": enriched["name"], "classes": classes, "paths": paths}
                wiki_only += 1

    (target / "index.json").write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n")
    print(f"Created {len(index)} items ({matched} matched, {wiki_only} wiki-only)")
    return len(index)


if __name__ == "__main__":
    build_database()
