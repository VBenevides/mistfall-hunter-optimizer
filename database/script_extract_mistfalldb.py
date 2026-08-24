import base64
import gzip
from io import BytesIO
import json
import re
import sqlite3
import time
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urljoin, urlparse

import requests
from bs4 import BeautifulSoup
from PIL import Image


BASE_URL = "https://mistfalldb.com"
ROOT = Path(__file__).parent
TARGET = ROOT / "db_mistfalldb.sqlite"
CATEGORIES = ("affixes", "weapons", "armor", "gems")
HEADERS = {"User-Agent": "MistfallHunterDatabaseExtractor/1.0"}
RARITY = {"Damaged": 1, "Common": 2, "Rare": 3, "Excellent": 4, "Epic": 5, "Legendary": 6, "Holy": 7}
ARMOR_SLOTS = {1: "helmet", 2: "clothes", 3: "gauntlets", 4: "pants", 5: "boots", 6: "necklace", 7: "ring"}
NATIVE_SLOT = {"head": "helmet", "chest": "clothes", "gloves": "gauntlets", "pants": "pants", "shoes": "boots"}


def clean(value):
    return " ".join(value.split())


def encode_icon(data):
    with Image.open(BytesIO(data)) as source:
        image = source.convert("RGBA")
        image.thumbnail((128, 128), Image.Resampling.LANCZOS)
        canvas = Image.new("RGBA", (128, 128), (0, 0, 0, 0))
        canvas.paste(image, ((128 - image.width) // 2, (128 - image.height) // 2), image)
        output = BytesIO()
        canvas.save(output, format="WEBP", quality=82, method=6)
    return base64.b64encode(output.getvalue()).decode("ascii")


def download_icon(session, item, category, cache):
    if category not in ("affixes", "gems"):
        return None
    icon = item.get("site", {}).get("icon")
    if not icon:
        return None
    url = urljoin(BASE_URL, icon)
    if url not in cache:
        response = session.get(url, timeout=30)
        response.raise_for_status()
        cache[url] = encode_icon(response.content)
    return cache[url]


def json_ld(soup):
    values = []
    for script in soup.select('script[type="application/ld+json"]'):
        try:
            value = json.loads(script.get_text())
        except json.JSONDecodeError:
            continue
        values.extend(value if isinstance(value, list) else [value])
    return values


def cell_data(cell):
    return {
        "text": clean(cell.get_text(" ", strip=True)),
        "links": [
            {"text": clean(link.get_text(" ", strip=True)), "url": urljoin(BASE_URL, link.get("href", ""))}
            for link in cell.select("a[href]")
        ],
    }


def table_data(table):
    rows = table.select("tr")
    if not rows:
        return {"headers": [], "rows": []}
    headers = [clean(cell.get_text(" ", strip=True)) for cell in rows[0].select("th")]
    data_rows = rows[1:] if headers else rows
    return {
        "headers": headers,
        "rows": [[cell_data(cell) for cell in row.find_all(["th", "td"], recursive=False)] for row in data_rows],
    }


def page_data(url, category, response):
    soup = BeautifulSoup(response.text, "html.parser")
    main = soup.select_one("main") or soup
    sections = []
    for section in main.select("section, article"):
        heading = section.find(["h1", "h2", "h3"])
        facts = []
        for definition in section.select("dl"):
            for term in definition.select("dt"):
                value = term.find_next_sibling("dd")
                if value:
                    facts.append({"name": clean(term.get_text(" ", strip=True)), "value": clean(value.get_text(" ", strip=True))})
        sections.append({
            "id": section.get("id"),
            "title": clean(heading.get_text(" ", strip=True)) if heading else None,
            "text": clean(section.get_text(" ", strip=True)),
            "facts": facts,
            "tables": [table_data(table) for table in section.select("table")],
        })
    title = soup.find("h1")
    return {
        "category": category,
        "slug": urlparse(url).path.rstrip("/").rsplit("/", 1)[-1],
        "name": clean(title.get_text(" ", strip=True)) if title else urlparse(url).path.rstrip("/").rsplit("/", 1)[-1],
        "url": url,
        "title": clean(soup.title.get_text(" ", strip=True)) if soup.title else None,
        "text": clean(main.get_text(" ", strip=True)),
        "links": [{"text": clean(link.get_text(" ", strip=True)), "url": urljoin(BASE_URL, link["href"])} for link in main.select("a[href]")],
        "structured_data": json_ld(soup),
        "sections": sections,
    }


def discover_class_urls(session, delay):
    response = session.get(f"{BASE_URL}/classes", timeout=30)
    response.raise_for_status()
    host = urlparse(BASE_URL).netloc
    urls = set()
    for link in BeautifulSoup(response.text, "html.parser").select("a[href]"):
        url = urljoin(BASE_URL, link["href"])
        parsed = urlparse(url)
        if parsed.netloc == host and parsed.path.strip("/").split("/")[:1] == ["classes"] and len(parsed.path.strip("/").split("/")) == 2:
            urls.add(url)
    if not urls:
        raise RuntimeError("No class detail links found")
    time.sleep(delay)
    return sorted(urls)


def class_page_data(url, response):
    soup = BeautifulSoup(response.text, "html.parser")
    source = page_data(url, "classes", response)
    stats = {}
    for term in soup.select("main dl dt"):
        value = term.find_next_sibling("dd")
        if value:
            stats[clean(term.get_text(" ", strip=True))] = clean(value.get_text(" ", strip=True))
    hero = soup.select_one(".gdb-entity-hero")
    icon = hero.find("img", src=True) if hero else None
    description = hero.find("p") if hero else None
    source.update(
        stats=stats,
        description=clean(description.get_text(" ", strip=True)) if description else None,
        icon_url=urljoin(BASE_URL, icon["src"]) if icon else None,
    )
    return source


def fetch_class_pages(session, delay):
    pages = []
    for url in discover_class_urls(session, delay):
        response = session.get(url, timeout=30)
        response.raise_for_status()
        pages.append(class_page_data(url, response))
        time.sleep(delay)
    return pages


def stat_number(value):
    if not re.fullmatch(r"[+-]?\d+(?:\.\d+)?", value or ""):
        return None
    number = float(value)
    return int(number) if number.is_integer() else number


def balanced(source, start):
    opening = source[start]
    closing = {"{": "}", "[": "]"}[opening]
    depth = 0
    quote = False
    escaped = False
    for index in range(start, len(source)):
        char = source[index]
        if quote:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == '"':
                quote = False
            continue
        if char == '"':
            quote = True
        elif char == opening:
            depth += 1
        elif char == closing:
            depth -= 1
            if depth == 0:
                return source[start:index + 1]
    raise ValueError("unterminated SSR value")


def field_value(source, name):
    match = re.search(rf"\b{re.escape(name)}:\s*(?:\$R\[\d+\]=)?([\{{\[])", source)
    return balanced(source, match.start(1)) if match else None


def string_value(source, name):
    match = re.search(rf'\b{re.escape(name)}:"((?:\\.|[^"\\])*)"', source)
    if not match:
        return None
    try:
        return json.loads('"' + match.group(1) + '"')
    except json.JSONDecodeError:
        return match.group(1).replace('\\"', '"').replace("\\\\", "\\")


def number_value(source, name):
    match = re.search(rf"\b{re.escape(name)}:\s*(-?\d+(?:\.\d+)?)", source)
    if not match:
        return None
    value = float(match.group(1))
    return int(value) if value.is_integer() else value


def strings_value(source, name):
    value = field_value(source, name) or "[]"
    return [match.group(1) for match in re.finditer(r'"((?:\\.|[^"\\])*)"', value)]


def objects(value):
    result = []
    index = 0
    while index < len(value):
        if value[index] == "{":
            try:
                result.append(balanced(value, index))
                index += len(result[-1])
                continue
            except ValueError:
                break
        index += 1
    return result


def affixes_value(source):
    result = []
    for value in objects(field_value(source, "affixes") or "[]"):
        name = string_value(value, "name")
        if name:
            result.append({"name": name, "level": number_value(value, "level") or 1})
    return result


def attributes_value(source):
    value = field_value(source, "attributes") or "{}"
    result = {}
    for match in re.finditer(r"([A-Za-z][A-Za-z0-9_]*):\s*(-?\d+(?:\.\d+)?)", value):
        number = float(match.group(2))
        result[match.group(1)] = int(number) if number.is_integer() else number
    return result


def parse_object(source):
    result = {"id": number_value(source, "id"), "slug": string_value(source, "slug"), "name": string_value(source, "name")}
    for key in ("icon", "desc", "rarity", "weaponType", "group", "color"):
        value = string_value(source, key)
        if value is not None:
            result[key] = value
    for key in ("rarityTier", "armorType", "gemLevel", "level", "itemCount", "minPrice", "maxPrice", "recommendedPrice", "combatValue", "durability", "repairCost", "affixLibraryId"):
        value = number_value(source, key)
        if value is not None:
            result[key] = value
    result["classes"] = strings_value(source, "classes")
    result["attributes"] = attributes_value(source)
    result["affixes"] = affixes_value(source)
    levels = []
    for value in objects(field_value(source, "levels") or "[]"):
        level = number_value(value, "level")
        desc = string_value(value, "desc")
        if level is not None:
            levels.append({"level": level, "desc": desc})
    if levels:
        result["levels"] = levels
    return result


def parse_ssr(html):
    match = re.search(r"\bitem:\$R\[\d+\]=", html)
    if not match:
        raise RuntimeError("MistfallDB item payload was not found")
    item = parse_object(balanced(html, html.find("{", match.end())))
    variants = []
    match = re.search(r"\bvariants:\$R\[\d+\]=", html)
    if match:
        for value in objects(balanced(html, html.find("[", match.end()))):
            variants.append(parse_object(value))
    item["variants"] = variants
    return item


def discover_urls(session, category, delay):
    queue = [f"{BASE_URL}/{category}"]
    visited = set()
    urls = set()
    while queue:
        page_url = queue.pop(0)
        if page_url in visited:
            continue
        visited.add(page_url)
        response = session.get(page_url, timeout=30)
        response.raise_for_status()
        soup = BeautifulSoup(response.text, "html.parser")
        for link in soup.select("a[href]"):
            parsed = urlparse(urljoin(BASE_URL, link["href"]))
            parts = parsed.path.strip("/").split("/")
            if parts == [category] and parsed.query.startswith("page="):
                queue.append(urljoin(BASE_URL, link["href"]))
            elif len(parts) == 2 and parts[0] == category and parts[1]:
                urls.add(urljoin(BASE_URL, link["href"]))
        time.sleep(delay)
    if not urls:
        raise RuntimeError(f"No {category} detail links found")
    return sorted(urls)


def native_configs():
    path = ROOT.parent / "optimizer" / "core" / "native_code_data.go"
    try:
        source = path.read_text()
        encoded = re.search(r"`([^`]*)`", source).group(1)
        data = json.loads(gzip.decompress(base64.b64decode(encoded)))
    except (OSError, AttributeError, TypeError, ValueError, json.JSONDecodeError, gzip.BadGzipFile):
        return {}
    result = {}
    for row in data.get("equipment", []):
        if len(row) == 7:
            result.setdefault(str(row[2]), {"slot": row[1], "holes": row[6], "affix": row[5]})
    result["_gem_ids"] = {str(row[0]) for row in data.get("gems", []) if len(row) == 2}
    return result


def native_id(item, category, canonical_slug):
    if category in ("weapons", "armor"):
        if item.get("slug") == canonical_slug:
            return None
        # The canonical HTML embeds each variant's item-page slug, e.g. -3060801.
        match = re.search(r"-(\d+)$", item.get("slug") or "")
        return int(match.group(1)) if match else None
    return item.get("id")


def page_item(page, category, variant, configs):
    item = dict(page)
    for key in ("slug", "name", "rarity", "rarityTier", "combatValue", "affixes"):
        if variant.get(key) is not None:
            item[key] = variant[key]
    item["variants"] = page.get("variants", [])
    return item


def optimizer_item(page, category, variant, source, configs):
    item = page_item(page, category, variant, configs)
    canonical_slug = page.get("slug")
    candidate_native = native_id(item, category, canonical_slug)
    native = candidate_native if (
        category in ("weapons", "armor") and str(candidate_native) in configs
        or category == "gems" and str(candidate_native) in configs["_gem_ids"]
    ) else None
    site_id = candidate_native or item.get("id")
    record_id = site_id
    config = configs.get(str(native), {})
    main_category = {"affixes": "affix", "weapons": "weapon", "armor": "armor", "gems": "affix_gem"}[category]
    if category == "weapons":
        sub_name = item.get("weaponType", "")
    elif category == "armor":
        sub_name = NATIVE_SLOT.get(config.get("slot", ""), ARMOR_SLOTS.get(item.get("armorType"), ""))
    else:
        sub_name = ""
    holes = config.get("holes", []) if main_category in ("weapon", "armor") else []
    sockets = [{"type": 5 if hole >= 50 else hole // 10, "level": hole % 10} for hole in holes]
    gem_type = 5
    lower_name = (item.get("name") or "").lower()
    for suffix, value in (("agate", 1), ("amethyst", 2), ("moonstone", 3), ("peridot", 4)):
        if lower_name.endswith(suffix):
            gem_type = value
            break
    affixes = item.get("affixes", [])
    data = {
        "id": str(record_id),
        "siteId": site_id,
        "name": item.get("name") or source["name"],
        "grade": item.get("rarityTier", 0),
        "category": main_category,
        "mainCategory": main_category,
        "subName": sub_name,
        "minPrice": item.get("minPrice", 0),
        "maxPrice": item.get("maxPrice", 0),
        "recommendedPrice": item.get("recommendedPrice", 0),
        "attributes": item.get("attributes", {}),
        "classes": item.get("classes", []),
        "slug": item.get("slug"),
        "url": source["url"],
        "description": item.get("desc"),
        "site": item,
        "page": {key: source[key] for key in ("title", "text", "links", "structured_data", "sections")},
        "equipment": {"affixes": affixes, "holeGroup": holes},
        "itemSockets": sockets,
        "gem": {"affixes": affixes, "affixGemType": gem_type, "affixGemLevel": item.get("gemLevel", 0)},
    }
    if native is not None:
        data["nativeId"] = native
    return data


def create_classes_schema(connection):
    connection.executescript(
        """
        CREATE TABLE IF NOT EXISTS classes (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            slug TEXT NOT NULL UNIQUE,
            source_url TEXT NOT NULL,
            description TEXT,
            icon_url TEXT,
            attack REAL,
            defense REAL,
            health REAL,
            stamina REAL,
            data TEXT NOT NULL,
            fetched_at TEXT NOT NULL
        );
        CREATE INDEX IF NOT EXISTS classes_slug_idx ON classes(slug);
        """
    )


def insert_class(connection, page, fetched_at):
    stats = page.get("stats", {})
    connection.execute(
        """INSERT INTO classes
        (id, name, slug, source_url, description, icon_url, attack, defense, health, stamina, data, fetched_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            name=excluded.name, slug=excluded.slug, source_url=excluded.source_url,
            description=excluded.description, icon_url=excluded.icon_url,
            attack=excluded.attack, defense=excluded.defense, health=excluded.health,
            stamina=excluded.stamina, data=excluded.data, fetched_at=excluded.fetched_at""",
        (
            page["slug"], page["name"], page["slug"], page["url"], page.get("description"), page.get("icon_url"),
            stat_number(stats.get("Attack")), stat_number(stats.get("Defense")),
            stat_number(stats.get("Max Health") or stats.get("Health")),
            stat_number(stats.get("Maximum Energy") or stats.get("Stamina")),
            json.dumps(page, ensure_ascii=False), fetched_at,
        ),
    )


def save_classes(connection, pages, fetched_at):
    create_classes_schema(connection)
    connection.execute("DELETE FROM classes")
    for page in pages:
        insert_class(connection, page, fetched_at)


def create_schema(connection):
    connection.executescript(
        """
        CREATE TABLE items (
            id TEXT PRIMARY KEY,
            name TEXT NOT NULL,
            category TEXT NOT NULL,
            grade INTEGER,
            rarity TEXT,
            affix_group TEXT,
            enabled INTEGER NOT NULL DEFAULT 1,
            site_id INTEGER,
            native_id INTEGER,
            slug TEXT NOT NULL,
            source_url TEXT NOT NULL,
            slot TEXT,
            weapon_type TEXT,
            native_affix TEXT,
            icon_b64 TEXT,
            socket_types TEXT NOT NULL,
            socket_tiers TEXT NOT NULL,
            socket_count INTEGER NOT NULL,
            min_price REAL,
            max_price REAL,
            recommended_price REAL,
            data TEXT NOT NULL,
            fetched_at TEXT NOT NULL
        );
        CREATE TABLE item_classes (
            item_id TEXT NOT NULL,
            class_slug TEXT NOT NULL,
            PRIMARY KEY (item_id, class_slug)
        );
        CREATE INDEX items_category_grade_idx ON items(category, grade);
        CREATE INDEX items_native_id_idx ON items(native_id);
        CREATE INDEX item_classes_class_idx ON item_classes(class_slug);
        """
    )
    create_classes_schema(connection)


def insert_item(connection, item, source_url, fetched_at, configs, icon_b64=None):
    category = item["mainCategory"]
    native_id = item.get("nativeId")
    sockets = item.get("itemSockets", [])
    native_affix = configs.get(str(native_id), {}).get("affix") or None
    connection.execute(
        """INSERT INTO items
        (id, name, category, grade, rarity, affix_group, site_id, native_id, slug, source_url, slot, weapon_type,
         native_affix, icon_b64, socket_types, socket_tiers, socket_count, min_price, max_price,
         recommended_price, data, fetched_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
        (
            item["id"], item["name"], category, item.get("grade"), item["site"].get("rarity"), item["site"].get("group"), item.get("siteId"), native_id,
            item.get("slug") or item["id"], source_url, item.get("subName") if category == "armor" else None,
            item.get("subName") if category == "weapon" else None, native_affix, icon_b64,
            json.dumps([socket["type"] for socket in sockets]), json.dumps([socket["level"] for socket in sockets]),
            len(sockets), item.get("minPrice"), item.get("maxPrice"), item.get("recommendedPrice"),
            json.dumps(item, ensure_ascii=False), fetched_at,
        ),
    )
    classes = item.get("classes", []) or (["All classes"] if category in ("weapon", "armor") else [])
    connection.executemany(
        "INSERT INTO item_classes(item_id, class_slug) VALUES (?, ?)",
        [(item["id"], re.sub(r"[^a-z0-9]+", "-", value.casefold()).strip("-")) for value in classes],
    )


def extract_classes(target=TARGET, session=None, delay=0.1):
    session = session or requests.Session()
    session.headers.update(HEADERS)
    pages = fetch_class_pages(session, delay)
    fetched_at = datetime.now(timezone.utc).isoformat()
    with sqlite3.connect(target) as connection:
        save_classes(connection, pages, fetched_at)
    print(f"classes: {len(pages)} pages")
    return len(pages)


def extract(target=TARGET, session=None, delay=0.1):
    target = Path(target)
    temporary = target.with_suffix(target.suffix + ".tmp")
    temporary.unlink(missing_ok=True)
    session = session or requests.Session()
    session.headers.update(HEADERS)
    fetched_at = datetime.now(timezone.utc).isoformat()
    configs = native_configs()
    icon_cache = {}
    counts = {}
    try:
        with sqlite3.connect(temporary) as connection:
            create_schema(connection)
            for category in CATEGORIES:
                urls = discover_urls(session, category, delay)
                count = 0
                for number, url in enumerate(urls, 1):
                    response = session.get(url, timeout=30)
                    response.raise_for_status()
                    source = page_data(url, category, response)
                    page = parse_ssr(response.text)
                    variants = page.get("variants") if category in ("weapons", "armor") else []
                    variants = variants or [page]
                    for variant in variants:
                        item = optimizer_item(page, category, variant, source, configs)
                        icon_b64 = download_icon(session, item, category, icon_cache)
                        insert_item(connection, item, urljoin(BASE_URL, f"/{category}/{item['slug']}"), fetched_at, configs, icon_b64)
                        count += 1
                    if number % 50 == 0:
                        print(f"{category}: {number}/{len(urls)} pages, {count} items")
                    time.sleep(delay)
                counts[category] = count
                print(f"{category}: {count} items from {len(urls)} pages")
            pages = fetch_class_pages(session, delay)
            save_classes(connection, pages, fetched_at)
            counts["classes"] = len(pages)
            print(f"classes: {len(pages)} pages")
    except Exception:
        temporary.unlink(missing_ok=True)
        raise
    temporary.replace(target)
    print(f"Created {target}: {sum(counts.values())} records")
    return counts


if __name__ == "__main__":
    extract()
