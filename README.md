# Mistfall Hunter Optimizer

An open-source equipment and gem optimizer for [Mistfall Hunter](https://mistfallhunter.com/).

The optimizer finds equipment sets that satisfy selected affix targets while considering item rarity, native affixes, gem sockets, gem tiers, class restrictions, accessory attributes, and prices.

## Features

- Wails desktop GUI and command-line interface.
- Lowest Rarity and Stat First optimization modes.
- Primary and optional secondary weapon selection.
- Equipment rarity ranges and per-slot rarity filters.
- Ring and amulet attribute filters.
- Exact or flexible affix target matching.
- Compatible gem selection by socket color and tier.
- Minimum, recommended, and maximum price comparisons.
- Closest-result suggestions when an exact set is unavailable.
- Session and saved-result storage.
- WebAssembly build for running the optimizer in a browser.

## Repository layout

| Path | Purpose |
| --- | --- |
| `optimizer/` | Go application, solver, GUI, CLI, and web build |
| `optimizer/core/` | Shared optimizer engine |
| `optimizer/frontend/` | Wails and browser interface |
| `optimizer/web/` | WebAssembly entrypoint and static-site build |
| `optimizer/build.sh` | Builds Linux and Windows desktop binaries |
| `optimizer/rules/` | Optimizer behavior and design notes |
| `database/` | SQLite snapshot, affix data, and wiki reference data |

The build scripts copy `database/db_mistfalldb.sqlite` and `database/affixes.json` into `optimizer/` for embedding. These copies are generated build inputs; edit the files in `database/` instead.

## Requirements

- Go 1.26
- A desktop environment for the GUI
- Python 3 for the web build
- A Go build toolchain suitable for the target platform

Go dependencies are declared in [`optimizer/go.mod`](optimizer/go.mod).

## Run locally

Clone the repository, copy the embedded data, and start the GUI:

```sh
git clone https://github.com/VBenevides/mistfall-hunter-optimizer.git
cd mistfall-hunter-optimizer/optimizer
cp ../database/db_mistfalldb.sqlite ../database/affixes.json .
go run .
```

Use `go run . --help` to list the available options.

## Command-line usage

Run the CLI with a class and one or more affix requirements:

```sh
go run . --cli --class=Mercenary --affixes Aegis=3 Fervor=2
```

Request JSON output and apply filters:

```sh
go run . --cli \
  --class=Mercenary \
  --affixes Aegis=3 Fervor=2 \
  --weapon-rarity=Any \
  --min-rarity=Green \
  --max-rarity=Gold \
  --ring=HP/PHYS \
  --amulet=ATK/MAG \
  --format=json
```

Rarities can be specified by number (`1`–`6`) or name: Gray, White, Green, Blue, Purple, and Gold.

## Build desktop binaries

From the repository root:

```sh
cd optimizer
./build.sh
```

The binaries are written to:

- `optimizer/dist/mistfall-hunter-equipment-optimizer-linux-amd64`
- `optimizer/dist/mistfall-hunter-equipment-optimizer-windows-amd64.exe`

## Build the web version

```sh
cd optimizer
cp ../database/db_mistfalldb.sqlite ../database/affixes.json .
cd web
./build.sh
```

The static files are written to `optimizer/web/dist/` and can be served by any static web server. The browser version stores sessions and saved results in browser local storage.

The web build uses [`garble`](https://github.com/burrowers/garble) when available and falls back to a stripped Go WebAssembly build when it is not installed.

## Tests

```sh
cd optimizer
cp ../database/db_mistfalldb.sqlite ../database/affixes.json .
go test ./...
```

## Data attribution

The item and gem data is based on [MistfallDB](https://mistfalldb.com/), a separate project. This repository is not affiliated with or maintained by the MistfallDB team.

## License

MIT. See [`LICENSE`](LICENSE).

Report problems or request features in the [GitHub issue tracker](https://github.com/VBenevides/mistfall-hunter-optimizer/issues).
