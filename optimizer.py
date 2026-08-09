import argparse
import json
from collections import defaultdict
from pathlib import Path


ROOT = Path(__file__).parent
EQUIPMENT_DIR = ROOT / "db_simplified" / "equipment"
GEM_DIR = ROOT / "db" / "gem"
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


def _load_database(equipment_dir=EQUIPMENT_DIR, gem_dir=GEM_DIR):
    equipment = [json.loads(path.read_text()) for path in equipment_dir.glob("*.json")]
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
                        selected[slot] = {
                            "id": item["id"],
                            "name": item["name"],
                            "grade": item["grade"],
                            "prices": {
                                "min": _number(item_prices[0]),
                                "max": _number(item_prices[1]),
                                "recommended": _number(item_prices[2]),
                            },
                            "gems": [
                                None
                                if gem_id is None
                                else {
                                    "id": gem_id,
                                    "name": gem_by_id[gem_id]["name"],
                                    "prices": {
                                        "min": _number(_prices(gem_by_id[gem_id])[0]),
                                        "max": _number(_prices(gem_by_id[gem_id])[1]),
                                        "recommended": _number(_prices(gem_by_id[gem_id])[2]),
                                    },
                                }
                                for gem_id in selected_gems
                            ],
                            "itemIncludes": item.get("itemIncludes", []),
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
        "maxPrice": _number(cost[2]),
        "recommendedPrice": _number(cost[0]),
        "gemCost": {"levelSum": cost[3], "count": cost[4]},
        "items": selected,
    }


def _best(requirements, mode, equipment, gems, min_level):
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

    grades = sorted(
        {item["grade"] for item in equipment if item["mainCategory"] == "armor" and item["grade"] >= min_level}
    )
    for armor_level in grades:
        weapon_level = armor_level + (mode == "above")
        result = _solve(equipment, gems, armor_level, weapon_level, positions, names, limits)
        if result:
            return result
    return None


def optimize(requirements, weapon_mode="both", equipment_dir=EQUIPMENT_DIR, gem_dir=GEM_DIR, min_level=1):
    if not 1 <= min_level <= 6:
        raise ValueError("min_level must be between 1 and 6")
    equipment, gems = _load_database(equipment_dir, gem_dir)
    if weapon_mode == "both":
        return {
            mode: _best(requirements, mode, equipment, gems, min_level)
            for mode in ("same", "above")
        }
    if weapon_mode not in {"same", "above"}:
        raise ValueError("weapon_mode must be 'same', 'above', or 'both'")
    return _best(requirements, weapon_mode, equipment, gems, min_level)


def available_affixes(equipment_dir=EQUIPMENT_DIR, gem_dir=GEM_DIR):
    equipment, gems = _load_database(equipment_dir, gem_dir)
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
    help_text = (
        "Rarity (higher is better):\n"
        "  1 Damaged, 2 Common, 3 Rare, 4 Excellent, 5 Epic, 6 Legendary\n\n"
        f"Available affixes:\n  {affix_help}"
    )
    parser = argparse.ArgumentParser(
        description="Find a low-level Mistfall Hunters gear and gem setup.",
        epilog=help_text,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("affixes", nargs="+", help="AFFIX=LEVEL, e.g. 'Aegis=2'")
    parser.add_argument("--weapon", choices=("same", "above", "both"), default="both")
    parser.add_argument("--min-level", type=int, default=1, metavar="LEVEL", help="minimum equipment rarity level (1-6)")
    args = parser.parse_args()
    requirements = {}
    for value in args.affixes:
        name, separator, level = value.rpartition("=")
        if not separator or not name:
            parser.error(f"expected AFFIX=LEVEL, got {value!r}")
        requirements[name] = int(level)
    print(json.dumps(optimize(requirements, args.weapon, min_level=args.min_level), indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
