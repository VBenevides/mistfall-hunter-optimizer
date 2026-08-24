import json
import re
import shutil
import sqlite3
from pathlib import Path


ROOT = Path(__file__).parent
SOURCE = ROOT / "db_mistfalldb.sqlite"
TARGET = SOURCE
AFFIX_DEFINITIONS = ROOT / "affixes.json"

STAT_PATTERNS = {
    "Attack": re.compile(
        r"\b(?:attack|physical damage|magic damage|defense penetration|defence penetration)\b",
        re.IGNORECASE,
    ),
    "Defense": re.compile(
        r"\b(?:physical resistance|magic resistance|resistance|defense(?!\s+penetration))\b",
        re.IGNORECASE,
    ),
    "HP": re.compile(r"\b(?:health|hp)\b", re.IGNORECASE),
}


def key(value):
    return " ".join(str(value).casefold().split())


def ensure_schema(connection):
    columns = {row[1] for row in connection.execute("PRAGMA table_info(items)")}
    if "enabled" not in columns:
        connection.execute("ALTER TABLE items ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1")
    if "stats" not in columns:
        connection.execute("ALTER TABLE items ADD COLUMN stats TEXT NOT NULL DEFAULT '[\"None\"]'")


def affix_stats(affix):
    text = " ".join(affix.get("levels", {}).values())
    return [name for name, pattern in STAT_PATTERNS.items() if pattern.search(text)] or ["None"]


def load_affix_stats():
    with AFFIX_DEFINITIONS.open(encoding="utf-8") as file:
        source = json.load(file)
    return {
        key(affix["name"]): affix_stats(affix)
        for affix in source.get("affixes", [])
        if affix.get("name")
    }


def affixes_in_items(connection):
    names = set()
    for (raw,) in connection.execute(
        "SELECT data FROM items WHERE category IN ('weapon', 'armor', 'affix_gem')"
    ):
        item = json.loads(raw)
        for group in (
            item.get("equipment", {}).get("affixes", []),
            item.get("gem", {}).get("affixes", []),
        ):
            names.update(key(affix["name"]) for affix in group if affix.get("name"))
    return names


def enrich_database(source=SOURCE, target=None):
    source = Path(source)
    target = Path(target or source)
    target.parent.mkdir(parents=True, exist_ok=True)
    if source.resolve() != target.resolve():
        shutil.copy2(source, target)

    with sqlite3.connect(target) as connection:
        ensure_schema(connection)
        stats_by_name = load_affix_stats()
        used = affixes_in_items(connection)
        rows = connection.execute(
            "SELECT id, name, data FROM items WHERE category = 'affix'"
        ).fetchall()
        updates = []
        for item_id, name, raw in rows:
            item = json.loads(raw)
            enabled = int(key(name) in used)
            stats = stats_by_name.get(key(name), ["None"])
            item["stats"] = stats
            item["enabled"] = bool(enabled)
            updates.append(
                (enabled, json.dumps(stats), json.dumps(item, ensure_ascii=False), item_id)
            )
        connection.executemany(
            "UPDATE items SET enabled = ?, stats = ?, data = ? WHERE id = ?", updates
        )
        connection.execute(
            "CREATE INDEX IF NOT EXISTS items_enabled_idx ON items(category, enabled)"
        )
        enabled = sum(row[0] for row in updates)
        print(f"Enriched {target}: {enabled}/{len(updates)} affixes enabled; stats extracted")
        return {"enabled": enabled, "disabled": len(updates) - enabled}


if __name__ == "__main__":
    enrich_database()
