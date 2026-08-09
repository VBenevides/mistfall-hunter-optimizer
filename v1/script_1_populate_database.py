import json
import re
import shutil
import string
from pathlib import Path

import requests


BASE_URL = "https://questlog.gg/mistfall-hunter/api/trpc/database."
DB_DIR = Path(__file__).parent / "db-questlog"
HEADERS = {
    "User-Agent": "Mozilla/5.0",
    "Referer": "https://questlog.gg/mistfall-hunter/en/db/items",
}
CATEGORIES = {"weapon": "equipment", "armor": "equipment", "affix_gem": "gem"}


def fetch(session, method, data):
    response = session.get(
        BASE_URL + method,
        params={"input": json.dumps(data, separators=(",", ":"))},
        timeout=30,
    )
    response.raise_for_status()
    return response.json()["result"]["data"]


def save_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    temporary.replace(path)


def slug(value):
    return re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-")


def category_items(session, category):
    expected = None
    items = {}

    def add_pages(extra):
        nonlocal expected
        first = fetch(session, "getItems", {"language": "en", "page": 1, **extra})
        expected = expected or sum(first.get("facetDistribution", {}).get("grade", {}).values())
        for item in first["pageData"]:
            items[item["id"]] = item
        for page in range(2, first["pageCount"] + 1):
            for item in fetch(session, "getItems", {"language": "en", "page": page, **extra})["pageData"]:
                items[item["id"]] = item

    add_pages({"mainCategory": category})
    if len(items) < expected:
        # The API caps an unfiltered search at 1,000 hits. Letter searches partition it.
        for letter in string.ascii_lowercase:
            add_pages({"mainCategory": category, "searchTerm": letter})
    if len(items) != expected:
        raise RuntimeError(f"{category}: expected {expected} items, found {len(items)}")
    return list(items.values())


def download(session, db_dir=DB_DIR, reset=False):
    if reset and db_dir.exists():
        shutil.rmtree(db_dir)
    db_dir.mkdir(parents=True, exist_ok=True)

    all_items = {}
    for category, raw_group in CATEGORIES.items():
        items = category_items(session, category)
        print(f"{category}: {len(items)} items")
        raw_dir = db_dir / "raw" / raw_group
        raw_index_path = raw_dir / "index.json"
        raw_index = json.loads(raw_index_path.read_text()) if raw_index_path.exists() else {}
        raw_index.update({item["id"]: item["name"] for item in items})
        save_json(raw_index_path, raw_index)

        for number, item in enumerate(items, 1):
            detail = fetch(session, "getItem", {"id": item["id"], "language": "en"})
            save_json(raw_dir / f'{item["id"]}.json', detail)
            all_items[item["id"]] = item["name"]
            if number % 50 == 0:
                print(f"  {number}/{len(items)} details")

            if category == "affix_gem":
                group = slug(detail["name"].rsplit(" ", 1)[-1])
                path = db_dir / "gem" / group / "index.json"
            else:
                slot = slug(detail.get("subName") or "unknown")
                rarity = str(detail.get("grade", item.get("grade", "unknown")))
                path = db_dir / "equipment" / category / slot / rarity / "index.json"

            current = json.loads(path.read_text()) if path.exists() else {}
            current[detail["id"]] = detail["name"]
            save_json(path, current)

    save_json(db_dir / "index.json", all_items)


if __name__ == "__main__":
    with requests.Session() as session:
        session.headers.update(HEADERS)
        download(session, reset=True)
