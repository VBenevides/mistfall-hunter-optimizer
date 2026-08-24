package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"mistfall/v2/core"
	_ "modernc.org/sqlite"
)

const maxLookupLevel = 32

type lookupConfiguration struct {
	class  string
	weapon string
}

type priceStats struct {
	sum      float64
	min      float64
	max      float64
	samples  int
	failures int
}

func main() {
	databasePath := flag.String("database", "../database/db_mistfalldb.sqlite", "SQLite database to update")
	affixesPath := flag.String("affixes", "../database/affixes.json", "Affix definitions used by the optimizer")
	classFilter := flag.String("class", "", "Optional character class filter; empty means all classes")
	weaponFilter := flag.String("weapon", "", "Optional weapon filter; empty means all weapons")
	minRarity := flag.String("min-rarity", "Gray", "Minimum equipment rarity")
	maxRarity := flag.String("max-rarity", "Gold", "Maximum equipment rarity")
	from := flag.Int("from", 0, "First total affix level to generate")
	to := flag.Int("to", maxLookupLevel, "Last total affix level to generate")
	matchTargetStrictly := flag.Bool("match-target-strictly", false, "Require exact target levels and leave empty sockets unused")
	flag.Parse()

	if *from < 0 || *to > maxLookupLevel || *from > *to {
		fail("total affix level range must be between 0 and 32")
	}
	database, err := os.ReadFile(*databasePath)
	if err != nil {
		fail("read database: %v", err)
	}
	affixes, err := os.ReadFile(*affixesPath)
	if err != nil {
		fail("read affixes: %v", err)
	}
	core.ConfigureAssets(database, affixes)
	engine, err := core.NewEngine()
	if err != nil {
		fail("load optimizer: %v", err)
	}
	configurations := configurations(engine.Options(), *classFilter, *weaponFilter)
	if len(configurations) == 0 {
		fail("no class and weapon combinations match the filters")
	}

	stats := make([]priceStats, *to+1)
	for _, configuration := range configurations {
		fmt.Printf("Generating %s / %s\n", configuration.class, configuration.weapon)
		request := core.GUIRequest{
			CharacterClass:      configuration.class,
			WeaponClass:         configuration.weapon,
			MinRarity:           *minRarity,
			MaxRarity:           *maxRarity,
			Ring:                "Any",
			Amulet:              "Any",
			MatchTargetStrictly: *matchTargetStrictly,
		}
		for level := *from; level <= *to; level++ {
			request.MinimumAffixLevel = level
			result, runErr := engine.Execute(request)
			if runErr != nil || !result.Possible || result.Closest || result.OptimizationRank == nil {
				stats[level].failures++
				continue
			}
			stats[level].add(result.OptimizationRank.AveragePrice)
		}
	}

	if err := writeLookup(*databasePath, stats, *from, *to); err != nil {
		fail("write lookup table: %v", err)
	}
	for level := *from; level <= *to; level++ {
		stat := stats[level]
		if stat.samples == 0 {
			fmt.Printf("%2d: unavailable (%d failures)\n", level, stat.failures)
			continue
		}
		fmt.Printf("%2d: %.2f (range %.2f–%.2f, %d samples, %d failures)\n", level, stat.average(), stat.min, stat.max, stat.samples, stat.failures)
	}
}

func configurations(options core.GUIOptions, classFilter, weaponFilter string) []lookupConfiguration {
	result := []lookupConfiguration{}
	classes := append([]string(nil), options.Classes...)
	sort.Strings(classes)
	for _, class := range classes {
		if classFilter != "" && class != classFilter {
			continue
		}
		weapons := append([]string(nil), options.WeaponClasses[class]...)
		sort.Strings(weapons)
		for _, weapon := range weapons {
			if weaponFilter == "" || weapon == weaponFilter {
				result = append(result, lookupConfiguration{class: class, weapon: weapon})
			}
		}
	}
	return result
}

func (stats *priceStats) add(price float64) {
	if stats.samples == 0 || price < stats.min {
		stats.min = price
	}
	if stats.samples == 0 || price > stats.max {
		stats.max = price
	}
	stats.sum += price
	stats.samples++
}

func (stats priceStats) average() float64 {
	if stats.samples == 0 {
		return 0
	}
	return stats.sum / float64(stats.samples)
}

func writeLookup(path string, stats []priceStats, from, to int) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	// This table is derived output owned by this generator; replace it after all searches succeed.
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS stat_first_price_lookup`); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE stat_first_price_lookup (
			total_affix_level INTEGER PRIMARY KEY CHECK (total_affix_level BETWEEN 0 AND 32),
			average_price REAL,
			min_price REAL,
			max_price REAL,
			sample_count INTEGER NOT NULL,
			failure_count INTEGER NOT NULL,
			generated_at TEXT NOT NULL
		)
	`); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO stat_first_price_lookup
		(total_affix_level, average_price, min_price, max_price, sample_count, failure_count, generated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	for level := from; level <= to; level++ {
		stat := stats[level]
		var average, minimum, maximum interface{}
		if stat.samples > 0 {
			average, minimum, maximum = stat.average(), stat.min, stat.max
		}
		if _, err := statement.ExecContext(ctx, level, average, minimum, maximum, stat.samples, stat.failures, generatedAt); err != nil {
			statement.Close()
			tx.Rollback()
			return err
		}
	}
	if err := statement.Close(); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
