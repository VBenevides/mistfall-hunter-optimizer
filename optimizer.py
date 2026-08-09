import argparse
import json
import re
from collections import defaultdict
from pathlib import Path


ROOT = Path(__file__).parent
EQUIPMENT_DIR = ROOT / "db"
GEM_DIR = ROOT / "db-questlog" / "gem"
RARITIES = {
    1: "Damaged",
    2: "Common",
    3: "Rare",
    4: "Excellent",
    5: "Epic",
    6: "Legendary",
}
PRICE_FIELDS = ("minPrice", "maxPrice", "recommendedPrice")


def _normalize_affix(name):
    return " ".join(str(name).replace("_", " ").split()).casefold()


def _prices(item):
    return tuple(float(item.get(field) or 0) for field in PRICE_FIELDS)


def _number(value):
    return int(value) if value.is_integer() else round(value, 2)


def _rarity(value):
    text = str(value).replace("_", " ").strip().casefold()
    if text.isdigit():
        level = int(text)
    else:
        level = next((level for level, name in RARITIES.items() if name.casefold() == text), 0)
    if not 1 <= level <= 6:
        raise ValueError("min_rarity must be 1-6 or a rarity name")
    return level


def _class_slug(value):
    return re.sub(r"[^a-z0-9]+", "-", str(value).casefold()).strip("-")


def _load_database(equipment_dir=EQUIPMENT_DIR, gem_dir=GEM_DIR, character_class=None):
    if character_class:
        class_dirs = {equipment_dir / _class_slug(character_class), equipment_dir / "all-classes"}
        paths = [path for directory in class_dirs for path in directory.glob("*/*/*.json")]
    else:
        paths = list(equipment_dir.glob("*.json"))
        if not paths:
            paths = list(equipment_dir.glob("*/*/*.json"))
    equipment = []
    seen = set()
    for path in paths:
        item = json.loads(path.read_text())
        if item.get("id") not in seen:
            equipment.append(item)
            seen.add(item.get("id"))
    gems = [
        json.loads(path.read_text())
        for path in gem_dir.glob("*/*.json")
        if path.name != "index.json"
    ]
    return equipment, gems


def _requirements(value):
    pairs = value.items() if isinstance(value, dict) else ((x["name"], x["level"]) for x in value)
    result = {}
    for name, level in pairs:
        level = int(level)
        if level < 1:
            raise ValueError(f"{name}: level must be positive")
        key = _normalize_affix(name)
        result[key] = (str(name), max(level, result.get(key, ("", 0))[1]))
    if not result:
        raise ValueError("at least one affix is required")
    return result


def _vector(affixes, positions, limits):
    values = [0] * len(positions)
    for affix in affixes:
        position = positions.get(_normalize_affix(affix.get("name", "")))
        if position is not None:
            values[position] += int(affix.get("level", 1))
    return tuple(min(value, limits[index]) for index, value in enumerate(values))


def _compatible(gem, hole):
    hole_type, hole_level = divmod(int(hole), 10)
    data = gem["gem"]
    return data["affixGemLevel"] <= hole_level and (
        data["affixGemType"] == hole_type or hole_type == 5
    )


def _affix_value(affixes, key):
    return sum(
        int(affix.get("level", 1))
        for affix in affixes
        if _normalize_affix(affix.get("name", "")) == key
    )


def _max_affix_levels(equipment, gems, keys, max_rarity):
    groups = defaultdict(list)
    for item in equipment:
        if int(item.get("grade", 0)) > max_rarity:
            continue
        if item.get("mainCategory") == "weapon":
            groups["weapon"].append(item)
        elif item.get("mainCategory") == "armor":
            groups[item.get("subName", "unknown")].append(item)

    maximum = {key: 0 for key in keys}
    for items in groups.values():
        best = {key: 0 for key in keys}
        for item in items:
            data = item.get("equipment", {})
            values = {
                key: _affix_value(data.get("affixes", []), key)
                for key in keys
            }
            for hole in data.get("holeGroup", []):
                for key in keys:
                    values[key] += max(
                        [_affix_value(gem.get("gem", {}).get("affixes", []), key)
                         for gem in gems if _compatible(gem, hole)]
                        or [0]
                    )
            for key in keys:
                best[key] = max(best[key], values[key])
        for key in keys:
            maximum[key] += best[key]
    return maximum


def _not_possible(reason, min_rarity, max_rarity, requested, maximum):
    requested_levels = {name: level for name, level in requested.values()}
    maximum_levels = {name: maximum[key] for key, (name, _) in requested.items()}
    if reason.startswith("Requested"):
        message = "The requested affix levels exceed the available levels at the maximum rarity."
    else:
        message = "No single equipment set can provide all requested affix levels together."
    return {
        "possible": False,
        "message": message,
        "reason": reason,
        "rarityRange": {
            "min": {"level": min_rarity, "name": RARITIES[min_rarity]},
            "max": {"level": max_rarity, "name": RARITIES[max_rarity]},
        },
        "requestedAffixes": requested_levels,
        "independentMaximums": maximum_levels,
        "requestedTotalLevels": sum(requested_levels.values()),
        "independentMaximumTotal": sum(maximum_levels.values()),
        "note": "Each independent maximum is calculated separately; they cannot necessarily be combined into one set.",
    }


def _item_options(item, gems, positions, limits):
    equipment = item.get("equipment", {})
    states = {(_vector(equipment.get("affixes", []), positions, limits), ()): (0, 0, 0, 0, 0)}
    choices_by_hole = []
    for hole in equipment.get("holeGroup", []):
        choices_by_hole.append([None] + [gem for gem in gems if _compatible(gem, hole)])

    for choices in choices_by_hole:
        next_states = {}
        for (coverage, selected), cost in states.items():
            for gem in choices:
                if gem is None:
                    addition, gem_cost, gem_id = (0,) * len(limits), (0, 0, 0, 0, 0), None
                else:
                    addition = _vector(gem["gem"].get("affixes", []), positions, limits)
                    gem_level = int(gem["gem"]["affixGemLevel"])
                    prices = _prices(gem)
                    gem_cost = (prices[2], prices[0], prices[1], gem_level, 1)
                    gem_id = gem["id"]
                result = tuple(min(coverage[i] + addition[i], limits[i]) for i in range(len(limits)))
                candidate = (result, selected + (gem_id,))
                total_cost = tuple(cost[index] + gem_cost[index] for index in range(5))
                previous = next_states.get((result,))
                if previous is None or total_cost < previous[0]:
                    next_states[(result,)] = (total_cost, candidate[1])
        states = {(coverage, selected): cost for (coverage,), (cost, selected) in next_states.items()}

    return [(coverage, selected, cost) for (coverage, selected), cost in states.items()]


def _solve(equipment, gems, armor_level, weapon_level, positions, labels, limits):
    groups = defaultdict(list)
    for item in equipment:
        if item["mainCategory"] == "weapon" and item["grade"] == weapon_level:
            groups["weapon"].append(item)
        elif item["mainCategory"] == "armor" and item["grade"] == armor_level:
            groups[item["subName"]].append(item)
    armor_slots = sorted(slot for slot in groups if slot != "weapon")
    if not armor_slots or "weapon" not in groups:
        return None

    stages = [("weapon", groups["weapon"])] + [(slot, groups[slot]) for slot in armor_slots]
    zero = (0,) * len(limits)
    states = {zero: ((0, 0, 0, 0, 0), {})}
    gem_by_id = {gem["id"]: gem for gem in gems}

    for slot, items in stages:
        next_states = {}
        for item in items:
            for coverage, selected_gems, option_cost in _item_options(item, gems, positions, limits):
                for previous_coverage, (previous_cost, previous_items) in states.items():
                    combined = tuple(
                        min(previous_coverage[i] + coverage[i], limits[i])
                        for i in range(len(limits))
                    )
                    item_prices = _prices(item)
                    item_cost = (item_prices[2], item_prices[0], item_prices[1], 0, 0)
                    cost = tuple(
                        previous_cost[index] + option_cost[index] + item_cost[index]
                        for index in range(5)
                    )
                    if combined not in next_states or cost < next_states[combined][0]:
                        selected = dict(previous_items)
                        item_equipment = item.get("equipment", {})
                        selected[slot] = {
                            "slot": slot,
                            "name": item["name"],
                            "nativeAffixes": [
                                {"name": affix["name"], "level": affix.get("level", 1)}
                                for affix in item_equipment.get("affixes", [])
                            ] or "No Native Affix",
                            "gems": [
                                None
                                if gem_id is None
                                else {
                                    "id": gem_id,
                                    "name": gem_by_id[gem_id]["name"],
                                }
                                for gem_id in selected_gems
                            ],
                        }
                        next_states[combined] = (cost, selected)
        states = next_states
        if not states:
            return None

    target = tuple(limits)
    if target not in states:
        return None
    cost, selected = states[target]
    return {
        "armorLevel": armor_level,
        "weaponLevel": weapon_level,
        "levelCombination": [weapon_level] + [armor_level] * len(armor_slots),
        "effects": {labels[key][0]: limits[i] for i, key in enumerate(positions)},
        "minPrice": _number(cost[1]),
        "averagePrice": _number(cost[0]),
        "maxPrice": _number(cost[2]),
        "gemCost": {"levelSum": cost[3], "count": cost[4]},
        "pieces": list(selected.values()),
    }


def _best(requirements, mode, equipment, gems, min_rarity, max_rarity):
    names = _requirements(requirements)
    positions = {key: index for index, key in enumerate(names)}
    limits = tuple(value[1] for value in names.values())
    available = {
        _normalize_affix(affix.get("name", ""))
        for item in equipment
        for affix in item.get("equipment", {}).get("affixes", [])
    } | {
        _normalize_affix(affix.get("name", ""))
        for gem in gems
        for affix in gem.get("gem", {}).get("affixes", [])
    }
    unknown = [original for key, (original, _) in names.items() if key not in available]
    if unknown:
        raise ValueError(f"unknown affix: {', '.join(unknown)}")

    maximum = _max_affix_levels(equipment, gems, names, max_rarity)
    if any(limits[index] > maximum[key] for key, index in positions.items()):
        return _not_possible(
            "Requested affix levels exceed the maximum allowed by max rarity",
            min_rarity,
            max_rarity,
            names,
            maximum,
        )

    grades = sorted(
        {
            item["grade"]
            for item in equipment
            if item["mainCategory"] == "armor"
            and min_rarity <= item["grade"] <= max_rarity
        }
    )
    for armor_level in grades:
        weapon_level = armor_level + (mode == "above")
        if weapon_level > max_rarity:
            continue
        result = _solve(equipment, gems, armor_level, weapon_level, positions, names, limits)
        if result:
            result["possible"] = True
            return result
    return _not_possible(
        "No set matching the requested effects and rarity constraints was found",
        min_rarity,
        max_rarity,
        names,
        maximum,
    )


def optimize(
    requirements,
    weapon_mode="both",
    equipment_dir=EQUIPMENT_DIR,
    gem_dir=GEM_DIR,
    min_rarity=1,
    max_rarity=6,
    character_class=None,
):
    min_rarity = _rarity(min_rarity)
    max_rarity = _rarity(max_rarity)
    if min_rarity > max_rarity:
        raise ValueError("min_rarity cannot exceed max_rarity")
    equipment, gems = _load_database(equipment_dir, gem_dir, character_class)
    if weapon_mode == "both":
        return {
            mode: _best(requirements, mode, equipment, gems, min_rarity, max_rarity)
            for mode in ("same", "above")
        }
    if weapon_mode not in {"same", "above"}:
        raise ValueError("weapon_mode must be 'same', 'above', or 'both'")
    return _best(requirements, weapon_mode, equipment, gems, min_rarity, max_rarity)


def available_affixes(equipment_dir=EQUIPMENT_DIR, gem_dir=GEM_DIR, character_class=None):
    equipment, gems = _load_database(equipment_dir, gem_dir, character_class)
    return sorted({
        str(affix.get("name", ""))
        for item in equipment
        for affix in item.get("equipment", {}).get("affixes", [])
    } | {
        str(affix.get("name", ""))
        for gem in gems
        for affix in gem.get("gem", {}).get("affixes", [])
    })


def main():
    try:
        affix_help = ", ".join(available_affixes())
    except (FileNotFoundError, json.JSONDecodeError):
        affix_help = "database unavailable"
    parser = argparse.ArgumentParser(
        description="Find a low-level Mistfall Hunters gear and gem setup.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "Rarity (higher is better):\n"
            "  1 Damaged, 2 Common, 3 Rare, 4 Excellent, 5 Epic, 6 Legendary\n\n"
            f"Available affixes:\n  {affix_help}"
        ),
    )
    parser.add_argument("affixes", nargs="+", help="AFFIX=LEVEL, e.g. 'Aegis=2'")
    parser.add_argument("--class", dest="character_class", required=True, help="class using the equipment")
    parser.add_argument("--weapon", choices=("same", "above", "both"), default="both")
    parser.add_argument("--min-rarity", default="1", metavar="RARITY", help="minimum rarity: 1-6 or name")
    parser.add_argument("--max-rarity", default="6", metavar="RARITY", help="maximum rarity: 1-6 or name")
    args = parser.parse_args()
    requirements = {}
    for value in args.affixes:
        name, separator, level = value.rpartition("=")
        if not separator or not name:
            parser.error(f"expected AFFIX=LEVEL, got {value!r}")
        requirements[name] = int(level)
    print(json.dumps(
        optimize(
            requirements,
            args.weapon,
            min_rarity=args.min_rarity,
            max_rarity=args.max_rarity,
            character_class=args.character_class,
        ),
        indent=2,
        ensure_ascii=False,
    ))


if __name__ == "__main__":
    main()
