# Mistfall Hunter Affix Calculator

This project finds equipment and gem sets for Mistfall Hunter.

The optimizer uses item rarity, native affixes, gem sockets, gem tiers, item classes, accessory attributes, and prices. It returns the lowest viable rarity combination that meets the selected affix levels.

The main application is v2. It provides a Wails desktop GUI and a command-line interface. The v1 directory contains the earlier Python optimizer.

Read [INSTRUCTIONS.md](INSTRUCTIONS.md) for the user guide.

## Features

- Select a character class and weapon type.
- Select a secondary weapon: None, White (default), or Match primary.
- Set a minimum and maximum rarity.
- Set a fixed weapon rarity or allow any weapon rarity in the selected range.
- Filter rings and amulets by primary and damage attributes.
- Set equipment affix levels.
- Set wine levels with an eight-level total limit.
- Select compatible gems by socket color and tier.
- Fill empty gem slots after the optimizer finds a set.
- Compare minimum, recommended, and maximum prices.
- Show native affixes, gem sockets, gem names, gem affixes, and unused slots.
- Save and load the current session.
- Save successful results by name.
- Run the optimizer from the command line as text or JSON.
- Show a closest result when no exact result exists.

## Project layout

| Path | Purpose |
| --- | --- |
| v2/ | Main Go and Wails application |
| v2/main.go | Optimizer, database access, solver, and CLI |
| v2/gui.go | Wails service methods, GUI data types, and session handling |
| v2/frontend/index.html | GUI markup, styling, and browser logic |
| v2/frontend/bindings/ | Generated Wails JavaScript bindings |
| v2/build.sh | Copies data and builds Linux and Windows binaries |
| v2/main_test.go | Go optimizer and GUI tests |
| v1/optimizer.py | Legacy Python optimizer and CLI |
| v1/test_optimizer.py | Legacy Python tests |
| database/db.sqlite | Enriched SQLite database used by the application |
| database/affixes.json | Affix names, descriptions, levels, and effects |
| database/db-questlog/ | Downloaded Questlog item and gem data |
| database/db-wiki/ | Local wiki reference files |
| database/script_1_populate_database.py | Downloads Questlog data |
| database/script_2_enrich_database.py | Merges data and creates SQLite |
| database/test_*.py | Database pipeline tests |

The files v2/db.sqlite and v2/affixes.json are local build copies. The build script creates them from the files in database/. Do not edit the v2 copies as the source of record.

## Requirements

### Main application

- Go 1.26
- A desktop environment for the GUI
- Wails v3 beta dependencies from v2/go.mod

### Database pipeline

- Python 3
- The requests package for the Questlog downloader

The database tests use temporary data and do not need a network connection.

## Quick start

Copy the current data into v2:

~~~sh
cd v2
cp ../database/db.sqlite .
cp ../database/affixes.json .
~~~

Start the GUI:

~~~sh
go run .
~~~

The default program mode opens the desktop GUI. Use --help to show CLI help.

## Build release binaries

Run the build script from the v2 directory:

~~~sh
cd v2
./build.sh
~~~

The script copies the database and affix data, then writes these files:

- v2/dist/mistfall-hunter-equipment-optimizer-linux-amd64
- v2/dist/mistfall-hunter-equipment-optimizer-windows-amd64.exe

The build script uses the current database files in database/.

## Command-line example

Run the v2 CLI with a class and one or more affix requirements:

~~~sh
cd v2
go run . --cli --class=Mercenary --affixes Aegis=3 Fervor=2
~~~

Request JSON output and apply filters:

~~~sh
go run . --cli \
  --class=Mercenary \
  --affixes Aegis=3 Fervor=2 \
  --weapon-rarity=Any \
  --min-rarity=Green \
  --max-rarity=Gold \
  --ring=HP/PHYS \
  --amulet=ATK/MAG \
  --format=json
~~~

The CLI accepts rarity numbers from 1 to 6, rarity names such as Rare, and rarity colors such as Green.

## How the v2 solver works

The solver uses these eight equipment slots:

1. Weapon
2. Helmet
3. Clothes
4. Gauntlets
5. Pants
6. Boots
7. Necklace
8. Ring

The generated set can also include an optional secondary weapon. White uses the default plain weapon; Match primary copies the primary weapon's rarity and native affixes. Classes with one weapon type use None.

The solver loads equipment allowed for the selected class. It removes equipment that does not match the selected weapon, ring filter, or amulet filter.

Each item contributes native affixes or socket options. The solver checks gem compatibility by socket type and tier. A tier-2 socket accepts tier-1 and tier-2 gems. The solver prefers a tier-2 gem when available.

The solver evaluates the allowed equipment combinations with bounded dynamic programming. It keeps the lowest total rarity combination that satisfies every target affix. Among combinations with the same total rarity, it compares Weapon Damage, Attack, Defense, and Health in the selected priority order. Price is reported but does not affect optimization.

When no exact set exists, the solver can return the closest set. The GUI marks that result as invalid and shows its target shortfall.

When Match Target Strictly is disabled, the optimizer keeps selected gems and assigns empty sockets to meet thresholds. It raises selected affixes before it adds bonus affixes. When enabled, it uses exact target levels and leaves unused sockets empty.

## Rarity capacity labels

The GUI shows the full-set equipment-affix capacity beside each rarity:

| Rarity | Full-set capacity |
| --- | ---: |
| Gray | 0 |
| White | 0 |
| Green | 8 |
| Blue | 16 |
| Purple | 24 |
| Gold | 32 |

The GUI uses the equipment level total to recommend a rarity range. Wine levels do not change this recommendation.

## Data pipeline

The database pipeline has two stages.

### 1. Download Questlog data

script_1_populate_database.py requests item and gem data from the Questlog API. It writes raw JSON files and category indexes under database/db-questlog/.

The script uses reset=True when run as a program. This removes the existing database/db-questlog/ directory before the download starts.

### 2. Build the SQLite database

script_2_enrich_database.py reads the downloaded data and the three files in database/db-wiki/.

It creates database/db.sqlite with:

- An items table with JSON item data.
- An item_classes table with class links.
- Indexes for item category, rarity grade, and class.
- Wiki data matched to Questlog items.
- Wiki-only items that are not in Questlog data.
- Gems parsed from the gem wiki file.

The application embeds db.sqlite and affixes.json into the v2 binary at build time.

## Tests

Run the v2 tests:

~~~sh
cd v2
go test ./...
~~~

Run the v1 tests:

~~~sh
python -m unittest discover -s v1 -p 'test_*.py'
~~~

Run the database tests:

~~~sh
python -m unittest discover -s database -p 'test_*.py'
~~~

## Web deployment

The shared Go core is used by both the Wails desktop application and the WebAssembly web application.

Build the web version locally:

~~~sh
cd desktop
./web/build.sh
~~~

The generated desktop/web/dist/ directory contains the static site. GitHub Pages deploys it automatically from .github/workflows/pages.yml.

The web version runs the optimizer in the browser. It stores sessions and saved results in browser local storage.

## License

See [LICENSE](LICENSE).
