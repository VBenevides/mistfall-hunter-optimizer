//go:build !js

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"sync"

	_ "modernc.org/sqlite"
)

type databaseDeserializer interface {
	Deserialize([]byte) error
}

var (
	embeddedDatabase          []byte
	embeddedDatabaseOnce      sync.Once
	embeddedDatabaseHandle    *sql.DB
	embeddedDatabaseRestore   *sql.Conn
	embeddedDatabaseQueryConn *sql.Conn
	embeddedDatabaseError     error
)

func configureDatabase(database []byte) {
	embeddedDatabase = database
	embeddedDatabaseOnce = sync.Once{}
	embeddedDatabaseHandle = nil
	embeddedDatabaseRestore = nil
	embeddedDatabaseQueryConn = nil
	embeddedDatabaseError = nil
}

func openEmbeddedDatabase() (*sql.Conn, error) {
	embeddedDatabaseOnce.Do(func() {
		db, err := sql.Open("sqlite", "file::memory:")
		if err != nil {
			embeddedDatabaseError = err
			return
		}
		// ponytail: retain the deserialization connection because modernc's
		// sqlite3_deserialize invalidates it before database/sql can close it.
		db.SetMaxOpenConns(2)
		conn, err := db.Conn(context.Background())
		if err != nil {
			embeddedDatabaseError = err
			return
		}
		embeddedDatabaseRestore = conn
		embeddedDatabaseError = conn.Raw(func(driverConn any) error {
			deserializer, ok := driverConn.(databaseDeserializer)
			if !ok {
				return errors.New("sqlite driver does not support in-memory deserialization")
			}
			return deserializer.Deserialize(embeddedDatabase)
		})
		if embeddedDatabaseError == nil {
			embeddedDatabaseQueryConn = conn
			embeddedDatabaseHandle = db
		}
	})
	return embeddedDatabaseQueryConn, embeddedDatabaseError
}

func loadDatabase(characterClass string) ([]Item, []Item, error) {
	db, err := openEmbeddedDatabase()
	if err != nil {
		return nil, nil, err
	}
	equipmentQuery := "SELECT data FROM items WHERE category IN ('weapon', 'armor')"
	args := []interface{}{}
	if characterClass != "" {
		equipmentQuery += " AND EXISTS (\n            SELECT 1 FROM item_classes\n            WHERE item_classes.item_id = items.id\n              AND item_classes.class_slug IN (?, 'all-classes')\n        )"
		args = append(args, classSlug(characterClass))
	}
	equipment, err := loadItems(db, equipmentQuery, args...)
	if err != nil {
		return nil, nil, err
	}
	validEquipment := equipment[:0]
	for _, item := range equipment {
		if validDatabaseEquipment(item) {
			validEquipment = append(validEquipment, item)
		}
	}
	equipment = validEquipment
	gems, err := loadItems(db, "SELECT data FROM items WHERE category = 'affix_gem'")
	if err != nil {
		return nil, nil, err
	}
	gemIcons, err := loadItemIcons("affix_gem")
	if err != nil {
		return nil, nil, err
	}
	validGems := gems[:0]
	for _, item := range gems {
		if validDatabaseGem(item) {
			item.IconB64 = gemIcons[item.ID]
			validGems = append(validGems, item)
		}
	}
	gems = validGems
	return equipment, gems, err
}

func loadItemIcons(category string) (map[string]string, error) {
	db, err := openEmbeddedDatabase()
	if err != nil {
		return nil, err
	}
	var available int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM pragma_table_info('items') WHERE name = 'icon_b64'").Scan(&available); err != nil {
		return nil, err
	}
	icons := map[string]string{}
	if available == 0 {
		return icons, nil
	}
	rows, err := db.QueryContext(context.Background(), "SELECT id, name, icon_b64 FROM items WHERE category = ? AND icon_b64 IS NOT NULL AND icon_b64 != ''", category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name, icon string
		if err := rows.Scan(&id, &name, &icon); err != nil {
			return nil, err
		}
		icons[id] = icon
		icons[name] = icon
	}
	return icons, rows.Err()
}

func loadAffixCategories() (map[string]string, error) {
	db, err := openEmbeddedDatabase()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(context.Background(), "SELECT data FROM items WHERE category = 'affix' AND enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	categories := map[string]string{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item Item
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		if item.Name != "" && item.Site.Group != "" {
			categories[item.Name] = item.Site.Group
		}
	}
	return categories, rows.Err()
}

type queryer interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
}

func loadItems(db queryer, query string, args ...interface{}) ([]Item, error) {
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item Item
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadClassStats() (map[string]ClassStats, error) {
	db, err := openEmbeddedDatabase()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(context.Background(), "SELECT name, attack, defense, health, stamina FROM classes ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := map[string]ClassStats{}
	for rows.Next() {
		var name string
		var attack, defense, health, stamina sql.NullFloat64
		if err := rows.Scan(&name, &attack, &defense, &health, &stamina); err != nil {
			return nil, err
		}
		stats[name] = ClassStats{
			Attack: nullableFloat(attack), Defense: nullableFloat(defense),
			Health: nullableFloat(health), Stamina: nullableFloat(stamina),
		}
	}
	return stats, rows.Err()
}

func nullableFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func loadWeaponClasses() (map[string][]string, error) {
	db, err := openEmbeddedDatabase()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(context.Background(), "SELECT item_classes.class_slug, items.data FROM item_classes JOIN items ON items.id = item_classes.item_id WHERE items.category = 'weapon' ORDER BY item_classes.class_slug, json_extract(items.data, '$.subName'), json_extract(items.data, '$.name')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	classes := map[string]map[string]bool{}
	for rows.Next() {
		var class, raw string
		if err := rows.Scan(&class, &raw); err != nil {
			return nil, err
		}
		var item Item
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, err
		}
		if weapon := weaponClass(item); weapon != "" && validDatabaseEquipment(item) {
			title := titleCase(class)
			if classes[title] == nil {
				classes[title] = map[string]bool{}
			}
			classes[title][weapon] = true
		}
	}
	result := map[string][]string{}
	for class, weapons := range classes {
		for weapon := range weapons {
			result[class] = append(result[class], weapon)
		}
		sort.Strings(result[class])
	}
	return result, rows.Err()
}

func loadPriceLookup() (map[int]float64, error) {
	db, err := openEmbeddedDatabase()
	if err != nil {
		return nil, err
	}
	lookup := map[int]float64{}
	var exists int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'stat_first_price_lookup'").Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return lookup, nil
	}
	var samples int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM pragma_table_info('stat_first_price_lookup') WHERE name = 'sample_count'").Scan(&samples); err != nil {
		return nil, err
	}
	if samples == 0 {
		return lookup, nil
	}
	rows, err := db.QueryContext(context.Background(), "SELECT total_affix_level, average_price FROM stat_first_price_lookup WHERE sample_count > 0 AND average_price IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var level int
		var price float64
		if err := rows.Scan(&level, &price); err != nil {
			return nil, err
		}
		lookup[level] = price
	}
	return lookup, rows.Err()
}
