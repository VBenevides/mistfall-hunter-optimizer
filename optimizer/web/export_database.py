import json
import sqlite3
import sys


database, output = sys.argv[1:]
connection = sqlite3.connect(database)
has_icons = any(row[1] == "icon_b64" for row in connection.execute("PRAGMA table_info(items)"))
has_enabled = any(row[1] == "enabled" for row in connection.execute("PRAGMA table_info(items)"))
items = []
icon_column = "icon_b64" if has_icons else "NULL"
enabled_column = "enabled" if has_enabled else "NULL"
query = f"SELECT id, category, data, {icon_column}, {enabled_column} FROM items"
for item_id, category, data, icon_b64, enabled in connection.execute(query):
    item = json.loads(data)
    exported = {
        "id": item_id,
        "siteId": item.get("siteId", 0),
        "nativeId": item.get("nativeId", 0),
        "name": item.get("name", ""),
        "grade": item.get("grade", 0),
        "category": category,
        "mainCategory": item.get("mainCategory", ""),
        "subName": item.get("subName", ""),
        "minPrice": item.get("minPrice", 0),
        "maxPrice": item.get("maxPrice", 0),
        "recommendedPrice": item.get("recommendedPrice", 0),
        "attributes": item.get("attributes", {}),
        "enabled": bool(enabled) if has_enabled else item.get("enabled", True),
        "site": {"group": item.get("site", {}).get("group", "")},
        "equipment": {
            "affixes": item.get("equipment", {}).get("affixes", []),
            "holeGroup": item.get("equipment", {}).get("holeGroup", []),
        },
        "itemSockets": item.get("itemSockets", []),
        "gem": {
            "affixes": item.get("gem", {}).get("affixes", []),
            "affixGemType": item.get("gem", {}).get("affixGemType", 0),
            "affixGemLevel": item.get("gem", {}).get("affixGemLevel", 0),
        },
    }
    if category in ("affix", "affix_gem") and icon_b64:
        exported["iconB64"] = icon_b64
    if category == "affix":
        exported["stats"] = item.get("stats", ["None"])
    items.append(exported)

classes = {}
for item_id, class_slug in connection.execute("SELECT item_id, class_slug FROM item_classes"):
    classes.setdefault(item_id, []).append(class_slug)

class_stats = {
    name: {"attack": attack, "defense": defense, "health": health, "stamina": stamina}
    for name, attack, defense, health, stamina in connection.execute(
        "SELECT name, attack, defense, health, stamina FROM classes"
    )
}

price_lookup = {}
if connection.execute("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'stat_first_price_lookup'").fetchone()[0]:
    columns = {row[1] for row in connection.execute("PRAGMA table_info(stat_first_price_lookup)")}
    if "sample_count" in columns:
        price_lookup = {
            total: average
            for total, average in connection.execute(
                "SELECT total_affix_level, average_price FROM stat_first_price_lookup WHERE sample_count > 0 AND average_price IS NOT NULL"
            )
        }

with open(output, "w", encoding="utf-8") as file:
    json.dump({"items": items, "classes": classes, "classStats": class_stats, "priceLookup": price_lookup}, file, separators=(",", ":"))
