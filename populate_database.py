import json
import re
from pathlib import Path

import requests

from scrape_items import HEADERS, save_json, slug


ROOT = Path(__file__).parent
SOURCE_DIR = ROOT / "db"
QUESTLOG_DB_DIR = ROOT / "db-questlog"
CAPTURE = Path(__file__).parent / "local" / "db-get-item.txt"


def item_endpoint(capture=CAPTURE):
    match = re.search(r"curl '([^']*database\.getItem)\?", capture.read_text())
    if not match:
        raise ValueError(f"getItem endpoint not found in {capture}")
    return match.group(1)


def indexed_items(db_dir):
    for category in ("weapon", "armor"):
        for path in (db_dir / "equipment" / category).glob("**/index.json"):
            for item_id in json.loads(path.read_text()):
                yield item_id, category
    for item_id in json.loads((db_dir / "raw" / "gem" / "index.json").read_text()):
        yield item_id, "affix_gem"


def fetch_item(session, endpoint, item_id):
    response = session.get(
        endpoint,
        params={"input": json.dumps({"id": item_id, "language": "en"}, separators=(",", ":"))},
        timeout=30,
    )
    response.raise_for_status()
    return response.json()["result"]["data"]


def populate_questlog(session, db_dir=QUESTLOG_DB_DIR, capture=CAPTURE, source_dir=None):
    endpoint = item_endpoint(capture)
    source_dir = SOURCE_DIR if source_dir is None and db_dir == QUESTLOG_DB_DIR else source_dir or db_dir
    items = list(indexed_items(source_dir))
    for number, (item_id, category) in enumerate(items, 1):
        detail = fetch_item(session, endpoint, item_id)
        raw_group = "gem" if category == "affix_gem" else "equipment"
        save_json(db_dir / "raw" / raw_group / f"{item_id}.json", detail)
        if category == "affix_gem":
            folder = db_dir / "gem" / slug(detail["name"].rsplit(" ", 1)[-1])
        else:
            folder = db_dir / "equipment" / category / slug(detail.get("subName") or "unknown") / str(detail.get("grade", "unknown"))
        save_json(folder / f"{item_id}.json", detail)
        if number % 50 == 0:
            print(f"{number}/{len(items)} details")


def populate(session, db_dir=QUESTLOG_DB_DIR, capture=CAPTURE):
    """Backward-compatible wrapper for the Questlog population step."""
    return populate_questlog(session, db_dir, capture)


if __name__ == "__main__":
    with requests.Session() as session:
        session.headers.update(HEADERS)
        populate_questlog(session)
