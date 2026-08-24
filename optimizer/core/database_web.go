//go:build js

package core

import (
	"encoding/json"
	"fmt"
	"sort"
)

type webDatabase struct {
	Items       []Item                `json:"items"`
	Classes     map[string][]string   `json:"classes"`
	ClassStats  map[string]ClassStats `json:"classStats"`
	PriceLookup map[int]float64       `json:"priceLookup"`
}

var browserDatabase webDatabase
var browserDatabaseCache map[string]struct {
	equipment []Item
	gems      []Item
}

func configureDatabase(data []byte) {
	browserDatabase = webDatabase{}
	browserDatabaseCache = nil
	if err := json.Unmarshal(data, &browserDatabase); err != nil {
		panic(fmt.Sprintf("invalid browser database: %v", err))
	}
}

func loadItemIcons(category string) (map[string]string, error) {
	icons := map[string]string{}
	for _, item := range browserDatabase.Items {
		if item.Category == category && item.IconB64 != "" {
			icons[item.ID] = item.IconB64
			icons[item.Name] = item.IconB64
		}
	}
	return icons, nil
}

func loadDatabase(characterClass string) ([]Item, []Item, error) {
	class := classSlug(characterClass)
	if cached, ok := browserDatabaseCache[class]; ok {
		return append([]Item(nil), cached.equipment...), append([]Item(nil), cached.gems...), nil
	}
	allowed := func(item Item) bool {
		if class == "" {
			return true
		}
		for _, itemClass := range browserDatabase.Classes[item.ID] {
			if itemClass == class || itemClass == "all-classes" {
				return true
			}
		}
		return false
	}
	equipment := []Item{}
	gems := []Item{}
	for _, item := range browserDatabase.Items {
		switch item.Category {
		case "weapon", "armor":
			if allowed(item) && validDatabaseEquipment(item) {
				equipment = append(equipment, item)
			}
		case "affix_gem":
			if validDatabaseGem(item) {
				gems = append(gems, item)
			}
		}
	}
	if browserDatabaseCache == nil {
		browserDatabaseCache = map[string]struct {
			equipment []Item
			gems      []Item
		}{}
	}
	browserDatabaseCache[class] = struct {
		equipment []Item
		gems      []Item
	}{equipment: equipment, gems: gems}
	return append([]Item(nil), equipment...), append([]Item(nil), gems...), nil
}

func loadAffixCategories() (map[string]string, error) {
	categories := map[string]string{}
	for _, item := range browserDatabase.Items {
		if item.Category == "affix" && item.Enabled && item.Name != "" && item.Site.Group != "" {
			categories[item.Name] = item.Site.Group
		}
	}
	return categories, nil
}

func loadClassStats() (map[string]ClassStats, error) {
	return browserDatabase.ClassStats, nil
}

func loadWeaponClasses() (map[string][]string, error) {
	classes := map[string]map[string]bool{}
	for _, item := range browserDatabase.Items {
		weapon := weaponClass(item)
		if item.Category != "weapon" || weapon == "" || !validDatabaseEquipment(item) {
			continue
		}
		for _, class := range browserDatabase.Classes[item.ID] {
			if class == "all-classes" {
				continue
			}
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
	return result, nil
}

func loadPriceLookup() (map[int]float64, error) {
	if browserDatabase.PriceLookup == nil {
		return map[int]float64{}, nil
	}
	return browserDatabase.PriceLookup, nil
}
