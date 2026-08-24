package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var rarityNames = map[int]string{1: "Damaged", 2: "Common", 3: "Rare", 4: "Excellent", 5: "Epic", 6: "Legendary"}
var rarityColors = map[int]string{1: "Gray", 2: "White", 3: "Green", 4: "Blue", 5: "Purple", 6: "Gold"}
var gemTypes = map[int]string{1: "Agate", 2: "Amethyst", 3: "Moonstone", 4: "Peridot", 5: "Any"}
var gemColors = map[string]string{"Agate": "Red", "Amethyst": "Pink", "Moonstone": "Blue", "Peridot": "White", "Any": "Any"}
var slotAliases = map[string]string{"clothe": "clothes", "gauntlet": "gauntlets", "boot": "boots", "amulet": "necklace"}
var slotOrder = []string{"weapon", "helmet", "clothes", "gauntlets", "pants", "boots", "necklace", "ring"}
var rarityUpgradeOrder = []string{"weapon"}
var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func rarityPriorityIndexes(_ []string) ([8]int, int) {
	return [8]int{0}, 1
}

var embeddedAffixes []byte

func ConfigureAssets(database, affixes []byte) {
	embeddedAffixes = affixes
	configureDatabase(database)
}

type Affix struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

type Socket struct {
	Type  int `json:"type"`
	Level int `json:"level"`
}

type EquipmentData struct {
	Affixes   []Affix `json:"affixes"`
	HoleGroup []int   `json:"holeGroup"`
}

type GemData struct {
	Affixes       []Affix `json:"affixes"`
	AffixGemType  int     `json:"affixGemType"`
	AffixGemLevel int     `json:"affixGemLevel"`
}

type Item struct {
	ID               string                 `json:"id"`
	SiteID           int                    `json:"siteId"`
	NativeID         int                    `json:"nativeId"`
	Name             string                 `json:"name"`
	Grade            int                    `json:"grade"`
	Category         string                 `json:"category"`
	MainCategory     string                 `json:"mainCategory"`
	SubName          string                 `json:"subName"`
	MinPrice         float64                `json:"minPrice"`
	MaxPrice         float64                `json:"maxPrice"`
	RecommendedPrice float64                `json:"recommendedPrice"`
	Attributes       map[string]interface{} `json:"attributes"`
	Stats            []string               `json:"stats,omitempty"`
	Enabled          bool                   `json:"enabled"`
	IconB64          string                 `json:"iconB64,omitempty"`
	Site             ItemSiteData           `json:"site"`
	Equipment        EquipmentData          `json:"equipment"`
	ItemSockets      []Socket               `json:"itemSockets"`
	Gem              GemData                `json:"gem"`
}

type ItemSiteData struct {
	Group string `json:"group"`
}

type GemRef struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Affixes []Affix `json:"affixes"`
}

type GemSlot struct {
	Type   string  `json:"type"`
	Tier   int     `json:"tier"`
	Gem    *GemRef `json:"gem"`
	Filled bool    `json:"filled,omitempty"`
}

type Piece struct {
	Slot          string        `json:"slot"`
	ItemID        string        `json:"-"`
	Grade         int           `json:"grade"`
	Name          string        `json:"name"`
	NativeAffixes interface{}   `json:"nativeAffixes"`
	Gems          []interface{} `json:"gems"`
	GemSlots      []GemSlot     `json:"gemSlots"`
}

type Cost struct {
	Recommended float64
	Min         float64
	Max         float64
	LevelSum    int
	Count       int
	TierDeficit int
}

func (c Cost) add(other Cost) Cost {
	return Cost{c.Recommended + other.Recommended, c.Min + other.Min, c.Max + other.Max, c.LevelSum + other.LevelSum, c.Count + other.Count, c.TierDeficit + other.TierDeficit}
}

type Requirement struct {
	Key        string
	Name       string
	Level      int
	Max        int
	Thresholds []int
}

const totalAffixLevelKey = "__total_affix_levels"

func isInternalRequirement(requirement Requirement) bool {
	return requirement.Key == totalAffixLevelKey
}

func appendMinimumAffixRequirement(requirements []Requirement, minimum int) []Requirement {
	if minimum <= 0 {
		return requirements
	}
	return append(requirements, Requirement{
		Key:   totalAffixLevelKey,
		Name:  "Minimum equipment affix levels",
		Level: minimum,
		Max:   minimum,
	})
}

type Solution struct {
	Possible            bool
	Closest             bool
	Distance            int
	Message             string
	Reason              string
	MinRarity           int
	MaxRarity           int
	ArmorLevel          int
	WeaponLevel         int
	Effects             map[string]int
	MinPrice            float64
	AveragePrice        float64
	MaxPrice            float64
	GemLevelSum         int
	GemCount            int
	Pieces              []Piece
	LevelCombination    []int
	RequestedAffixes    map[string]int
	IndependentMaximums map[string]int
	RequestedTotal      int
	MaximumTotal        int
	quality             solveState
	raritySum           int
}

func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(value, "_", " ")), " "))
}

func slotKey(value string) string {
	value = strings.ToLower(value)
	if alias, ok := slotAliases[value]; ok {
		return alias
	}
	return value
}

func classSlug(value string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(value), "-"), "-")
}

func weaponClass(item Item) string {
	if item.MainCategory != "weapon" {
		return ""
	}
	if item.SubName != "" {
		if strings.EqualFold(item.SubName, "Polearm and Shield") {
			return "Spear and Shield"
		}
		if strings.EqualFold(item.SubName, "Hammer") {
			return "Warhammer"
		}
		return item.SubName
	}
	if strings.Contains(strings.ToLower(item.ID), "polearm-and-shield") || strings.Contains(strings.ToLower(item.Name), "polearm and shield") {
		return "Spear and Shield"
	}
	return ""
}

func canonicalWeaponClass(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "&", "and"))
	value = strings.Join(strings.Fields(strings.ReplaceAll(value, "-", " ")), " ")
	switch value {
	case "polearm and shield":
		return "spear and shield"
	case "warhammer":
		return "hammer"
	default:
		return value
	}
}

func weaponDamageType(value string) int {
	switch canonicalWeaponClass(value) {
	case "staff", "catalyst", "mace":
		return 1
	default:
		return 0
	}
}

var weaponOnlyAffixes = map[string]map[string]bool{
	"burst":   {"greatsword": true, "spear and shield": true},
	"ranged":  {"bow": true, "staff": true, "javelin": true, "catalyst": true},
	"bulwark": {"sword and shield": true, "spear and shield": true},
	"strife":  {"sword and shield": true, "hammer": true, "dagger": true, "dual blades": true, "mace": true, "greatsword": true, "spear and shield": true, "javelin": true},
}

func hasDisallowedWeaponAffix(affixes []Affix, selectedWeapon string, target map[string]bool, weapon bool) bool {
	for _, affix := range affixes {
		allowedClasses, restricted := weaponOnlyAffixes[normalize(affix.Name)]
		if !restricted || target[normalize(affix.Name)] {
			continue
		}
		if !allowedClasses[canonicalWeaponClass(selectedWeapon)] || !weapon {
			return true
		}
	}
	return false
}

func filterWeaponOnlyAffixes(items []Item, selectedWeapon string, targets map[string]bool, gems bool) []Item {
	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		affixes := item.Equipment.Affixes
		if gems {
			affixes = item.Gem.Affixes
		}
		if hasDisallowedWeaponAffix(affixes, selectedWeapon, targets, !gems && item.MainCategory == "weapon") {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func rarity(value string) (int, error) {
	text := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", " "))
	if level, err := strconv.Atoi(text); err == nil && level >= 1 && level <= 6 {
		return level, nil
	}
	for level, name := range rarityNames {
		if strings.ToLower(name) == text || strings.ToLower(rarityColors[level]) == text {
			return level, nil
		}
	}
	return 0, fmt.Errorf("invalid rarity %q: use 1-6, a rarity name, or Gray/White/Green/Blue/Purple/Gold", value)
}

func accessoryFilter(value string) ([2]string, error) {
	var result [2]string
	parts := strings.Split(strings.ToLower(value), "/")
	if len(parts) != 2 || (parts[0] != "hp" && parts[0] != "atk" && parts[0] != "any") || (parts[1] != "phys" && parts[1] != "mag" && parts[1] != "any") {
		return result, errors.New("accessory filter must be HP, ATK, or Any followed by PHYS, MAG, or Any")
	}
	result[0], result[1] = parts[0], parts[1]
	return result, nil
}

func holes(item Item) []Socket {
	if len(item.ItemSockets) > 0 {
		return item.ItemSockets
	}
	result := make([]Socket, 0, len(item.Equipment.HoleGroup))
	for _, value := range item.Equipment.HoleGroup {
		result = append(result, Socket{Type: value / 10, Level: value % 10})
	}
	return result
}

func gemType(gem Item) int {
	name := strings.ToLower(gem.Name)
	if strings.HasSuffix(name, "onyx") {
		return 1
	}
	if strings.Contains(name, "rhomb") {
		return 2
	}
	return gem.Gem.AffixGemType
}

func compatible(gem Item, socket Socket) bool {
	typ := socket.Type
	if typ == -1 {
		typ = 5
	}
	return gem.Gem.AffixGemLevel <= socket.Level && (typ == 5 || gemType(gem) == 5 || gemType(gem) == typ)
}

func gemChoices(gems []Item, socket Socket) []*Item {
	choices := []*Item{}
	for i := range gems {
		if compatible(gems[i], socket) {
			choices = append(choices, &gems[i])
		}
	}
	return choices
}

func affixHasRelevantStat(details map[string]GUIAffixDetails, name string) bool {
	detail, ok := details[normalize(name)]
	if !ok {
		detail, ok = details[name]
	}
	if !ok {
		return false
	}
	stats := detail.Stats
	if len(stats) == 0 {
		stats = inferredAffixStats(detail)
	}
	for _, stat := range stats {
		if stat != "" && !strings.EqualFold(stat, "None") {
			return true
		}
	}
	return false
}

func filterGemsByStats(gems []Item, targets map[string]bool, details map[string]GUIAffixDetails) []Item {
	filtered := make([]Item, 0, len(gems))
	for _, gem := range gems {
		for _, affix := range gem.Gem.Affixes {
			if targets[normalize(affix.Name)] || affixHasRelevantStat(details, affix.Name) {
				filtered = append(filtered, gem)
				break
			}
		}
	}
	return filtered
}

func affixValue(affixes []Affix, key string) int {
	if key == totalAffixLevelKey {
		total := 0
		for _, affix := range affixes {
			total += affix.Level
		}
		return total
	}
	total := 0
	for _, affix := range affixes {
		if normalize(affix.Name) == key {
			total += affix.Level
		}
	}
	return total
}

func rawVector(affixes []Affix, positions map[string]int, size int) []int {
	values := make([]int, size)
	if position, ok := positions[totalAffixLevelKey]; ok {
		for _, affix := range affixes {
			values[position] += affix.Level
		}
	}
	for _, affix := range affixes {
		if position, ok := positions[normalize(affix.Name)]; ok {
			values[position] += affix.Level
		}
	}
	return values
}

func vector(affixes []Affix, positions map[string]int, limits []int) []int {
	values := rawVector(affixes, positions, len(limits))
	for i := range values {
		if values[i] > limits[i] {
			values[i] = limits[i]
		}
	}
	return values
}

func exceedsExactTarget(values, limits []int, exactPositions []bool) bool {
	for index, exact := range exactPositions {
		if exact && values[index] > limits[index] {
			return true
		}
	}
	return false
}

func coverageKey(values []int) string {
	key := make([]byte, len(values))
	for i, value := range values {
		key[i] = byte(value)
	}
	return string(key)
}

func solveStateKey(coverage []int, minRarity, maxRarity int) string {
	return coverageKey(coverage) + string([]byte{byte(minRarity), byte(maxRarity)})
}

func solveStateKeyAdded(coverage, addition []int, minRarity, maxRarity int, limits []int) string {
	key := make([]byte, len(coverage)+2)
	for i, value := range coverage {
		value += addition[i]
		if value > limits[i] {
			value = limits[i]
		}
		key[i] = byte(value)
	}
	key[len(coverage)] = byte(minRarity)
	key[len(coverage)+1] = byte(maxRarity)
	return string(key)
}

func addCoverage(a, b, limits []int) []int {
	result := make([]int, len(limits))
	for i := range result {
		result[i] = a[i] + b[i]
		if result[i] > limits[i] {
			result[i] = limits[i]
		}
	}
	return result
}

func filterEquipment(items []Item, ring, amulet string) ([]Item, error) {
	filters := map[string]*[2]string{}
	for slot, value := range map[string]string{"ring": ring, "necklace": amulet} {
		if value == "" {
			continue
		}
		parsed, err := accessoryFilter(value)
		if err != nil {
			return nil, err
		}
		filters[slot] = &parsed
	}
	if len(filters) == 0 {
		return items, nil
	}
	result := make([]Item, 0, len(items))
	for _, item := range items {
		filter := filters[slotKey(item.SubName)]
		if filter == nil {
			result = append(result, item)
			continue
		}
		if filter[0] != "any" {
			primary := "attack"
			if filter[0] == "hp" {
				primary = "maxHealth"
			}
			if _, ok := item.Attributes[primary]; !ok {
				continue
			}
		}
		if filter[1] == "any" {
			result = append(result, item)
			continue
		}
		prefix := "magical"
		if filter[1] == "phys" {
			prefix = "physical"
		}
		matches := false
		for key := range item.Attributes {
			if strings.HasPrefix(strings.ToLower(key), prefix) {
				matches = true
			}
		}
		if matches {
			result = append(result, item)
		}
	}
	return result, nil
}

func searchShardForItem(id string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	hash := uint32(2166136261)
	for index := 0; index < len(id); index++ {
		hash ^= uint32(id[index])
		hash *= 16777619
	}
	return int(hash % uint32(shardCount))
}

func maxAffixLevels(equipment, gems []Item, requirements []Requirement, maxRarity int) map[string]int {
	groups := map[string][]Item{}
	for _, item := range equipment {
		slot := slotKey(item.SubName)
		if item.MainCategory == "weapon" {
			slot = "weapon"
		}
		if item.Grade > maxRarity {
			continue
		}
		groups[slot] = append(groups[slot], item)
	}
	maximum := map[string]int{}
	for _, requirement := range requirements {
		maximum[requirement.Key] = 0
	}
	for _, items := range groups {
		best := map[string]int{}
		for _, requirement := range requirements {
			best[requirement.Key] = 0
		}
		for _, item := range items {
			values := map[string]int{}
			for _, requirement := range requirements {
				values[requirement.Key] = affixValue(item.Equipment.Affixes, requirement.Key)
			}
			for _, socket := range holes(item) {
				for _, requirement := range requirements {
					bestGem := 0
					for _, gem := range gemChoices(gems, socket) {
						bestGem = max(bestGem, affixValue(gem.Gem.Affixes, requirement.Key))
					}
					values[requirement.Key] += bestGem
				}
			}
			for _, requirement := range requirements {
				best[requirement.Key] = max(best[requirement.Key], values[requirement.Key])
			}
		}
		for _, requirement := range requirements {
			maximum[requirement.Key] += best[requirement.Key]
		}
	}
	return maximum
}

func buildUpperBounds(equipment, gems []Item, positions map[string]int, limits []int) map[string]map[int][]int {
	bounds := map[string]map[int][]int{}
	socketMaximums := map[[2]int][]int{}
	for _, item := range equipment {
		values := vector(item.Equipment.Affixes, positions, limits)
		for _, socket := range holes(item) {
			key := [2]int{socket.Type, socket.Level}
			best, ok := socketMaximums[key]
			if !ok {
				best = make([]int, len(limits))
				for _, gem := range gemChoices(gems, socket) {
					addition := vector(gem.Gem.Affixes, positions, limits)
					for i := range best {
						best[i] = max(best[i], addition[i])
					}
				}
				socketMaximums[key] = best
			}
			values = addCoverage(values, best, limits)
		}
		slot := slotKey(item.SubName)
		if item.MainCategory == "weapon" {
			slot = "weapon"
		}
		if bounds[slot] == nil {
			bounds[slot] = map[int][]int{}
		}
		if bounds[slot][item.Grade] == nil {
			bounds[slot][item.Grade] = make([]int, len(limits))
		}
		for i := range values {
			bounds[slot][item.Grade][i] = max(bounds[slot][item.Grade][i], values[i])
		}
	}
	return bounds
}

func canMeetRequirements(bounds map[string]map[int][]int, levels map[string]int, limits []int) bool {
	maximum := make([]int, len(limits))
	for _, slot := range slotOrder {
		addition := bounds[slot][levels[slot]]
		if len(addition) != len(limits) {
			return false
		}
		maximum = addCoverage(maximum, addition, limits)
	}
	for i := range limits {
		if maximum[i] < limits[i] {
			return false
		}
	}
	return true
}

func canReachAdded(coverage, addition, remaining, targets, limits []int) bool {
	for i := range targets {
		value := coverage[i] + addition[i]
		if value > limits[i] {
			value = limits[i]
		}
		if value+remaining[i] < targets[i] {
			return false
		}
	}
	return true
}

type optionState struct {
	Coverage    []int
	RawCoverage []int
	Selected    []string
	Cost        Cost
	Stats       [4]float64
	Damage      float64
}

type solveStageOption struct {
	item               Item
	option             optionState
	signature          string
	stats              [4]float64
	damage             float64
	bonusNativeAffixes int
}

func (candidate solveStageOption) betterThan(current solveStageOption, order [4]int) bool {
	if candidate.item.Grade != current.item.Grade {
		return candidate.item.Grade < current.item.Grade
	}
	if candidate.bonusNativeAffixes != current.bonusNativeAffixes {
		return candidate.bonusNativeAffixes < current.bonusNativeAffixes
	}
	if stats := compareBuildStats(candidate.stats, candidate.damage, current.stats, current.damage, order); stats != 0 {
		return stats > 0
	}
	if candidate.option.Cost.TierDeficit != current.option.Cost.TierDeficit {
		return candidate.option.Cost.TierDeficit < current.option.Cost.TierDeficit
	}
	return candidate.signature < current.signature
}

func bonusNativeAffixCount(item Item, option optionState, positions map[string]int) int {
	for _, selected := range option.Selected {
		if selected != "" {
			return 0
		}
	}
	count := 0
	for _, affix := range item.Equipment.Affixes {
		if _, target := positions[normalize(affix.Name)]; !target {
			count++
		}
	}
	return count
}

type socketChoiceKey struct {
	typ, level int
}

type optionCache struct {
	items                 map[string][]optionState
	choice                map[socketChoiceKey][]*Item
	vector                map[string][]int
	stats                 map[string]cachedItemStats
	allowBonusGems        bool
	allowArmorAboveWeapon bool
	exactTargets          bool
	exactPositions        []bool
}

func itemOptions(item Item, gems []Item, positions map[string]int, limits []int) []optionState {
	return itemOptionsWithCache(item, gems, positions, limits, nil, nil, nil, nil, [4]int{}, true, false, nil, nil)
}

type cachedItemStats struct {
	stats  [4]float64
	damage float64
}

func itemStatsKey(item Item, selected []string) string {
	return item.ID + "\x00" + strings.Join(selected, ",")
}

func itemOptionStats(item Item, option optionState, gems map[string]Item, model *statModel, shared map[string]cachedItemStats) ([4]float64, float64) {
	if shared == nil || item.ID == "" {
		return model.itemStats(item, option, gems)
	}
	key := itemStatsKey(item, option.Selected)
	if cached, ok := shared[key]; ok {
		return cached.stats, cached.damage
	}
	stats, damage := model.itemStats(item, option, gems)
	shared[key] = cachedItemStats{stats: stats, damage: damage}
	return stats, damage
}

func itemOptionsWithCache(item Item, gems []Item, positions map[string]int, limits []int, choiceCache map[socketChoiceKey][]*Item, vectorCache map[string][]int, gemsByID map[string]Item, model *statModel, order [4]int, allowBonusGems, exactTargets bool, exactPositions []bool, statsCache map[string]cachedItemStats) []optionState {
	native := vector(item.Equipment.Affixes, positions, limits)
	rawNative := rawVector(item.Equipment.Affixes, positions, len(limits))
	initial := optionState{Coverage: native, RawCoverage: rawNative, Selected: []string{}, Cost: Cost{}}
	initial.Stats, initial.Damage = itemOptionStats(item, initial, gemsByID, model, statsCache)
	states := map[string]optionState{coverageKey(native): initial}
	for _, socket := range holes(item) {
		key := socketChoiceKey{socket.Type, socket.Level}
		choices := choiceCache[key]
		if choices == nil {
			choices = append([]*Item{nil}, gemChoices(gems, socket)...)
			if choiceCache != nil {
				choiceCache[key] = choices
			}
		}
		vectors := make([][]int, len(choices))
		rawVectors := make([][]int, len(choices))
		for index, gem := range choices {
			if gem == nil {
				continue
			}
			vectors[index] = vectorCache[gem.ID]
			if vectors[index] == nil {
				vectors[index] = vector(gem.Gem.Affixes, positions, limits)
				if vectorCache != nil && gem.ID != "" {
					vectorCache[gem.ID] = vectors[index]
				}
			}
			if exactTargets {
				rawVectors[index] = rawVector(gem.Gem.Affixes, positions, len(limits))
			}
		}
		next := map[string]optionState{}
		for _, state := range states {
			for index, gem := range choices {
				if gem != nil && !allowBonusGems {
					contributes := false
					for requirement, value := range vectors[index] {
						if value > 0 && state.Coverage[requirement] < limits[requirement] {
							contributes = true
							break
						}
					}
					if !contributes {
						continue
					}
				}
				coverage := append([]int(nil), state.Coverage...)
				rawCoverage := append([]int(nil), state.RawCoverage...)
				selected := append([]string(nil), state.Selected...)
				cost := state.Cost
				if gem == nil {
					selected = append(selected, "")
				} else {
					coverage = addCoverage(coverage, vectors[index], limits)
					if exactTargets {
						for requirement, value := range rawVectors[index] {
							rawCoverage[requirement] += value
						}
						if exceedsExactTarget(rawCoverage, limits, exactPositions) {
							continue
						}
					}
					selected = append(selected, gem.ID)
					cost = cost.add(Cost{Recommended: gem.RecommendedPrice, Min: gem.MinPrice, Max: gem.MaxPrice, LevelSum: gem.Gem.AffixGemLevel, Count: 1, TierDeficit: socket.Level - gem.Gem.AffixGemLevel})
				}
				candidate := optionState{Coverage: coverage, RawCoverage: rawCoverage, Selected: selected, Cost: cost}
				candidate.Stats, candidate.Damage = itemOptionStats(item, candidate, gemsByID, model, statsCache)
				key := coverageKey(coverage)
				previous, exists := next[key]
				if !exists || optionStateBetter(candidate, previous, model != nil, order) {
					next[key] = candidate
				}
			}
		}
		states = next
	}
	result := make([]optionState, 0, len(states))
	for _, state := range states {
		result = append(result, state)
	}
	return result
}

func optionStateBetter(candidate, current optionState, useStats bool, order [4]int) bool {
	if useStats {
		if comparison := comparePriorityStats(candidate.Stats, candidate.Damage, current.Stats, current.Damage, order); comparison != 0 {
			return comparison > 0
		}
	}
	if candidate.Cost.TierDeficit != current.Cost.TierDeficit {
		return candidate.Cost.TierDeficit < current.Cost.TierDeficit
	}
	return strings.Join(candidate.Selected, "\x00") < strings.Join(current.Selected, "\x00")
}

type solveState struct {
	Coverage            []int
	Cost                Cost
	Node                *solveNode
	Stats               [4]float64
	Damage              float64
	StatOrder           [4]int
	Rarities            [8]int
	RarityPriority      [8]int
	RarityPriorityCount int
	RaritySum           int
	MinRarity           int
	MaxRarity           int
	Signature           string
	signaturePrefix     string
	signatureSuffix     string
	Targets             []int
	Requirements        []Requirement
}

func (state solveState) fullSignature() string {
	if state.Signature != "" || state.signatureSuffix == "" {
		return state.Signature
	}
	return state.signaturePrefix + "\x00" + state.signatureSuffix
}

type solveNode struct {
	parent   *solveNode
	item     Item
	selected []string
}

func itemAttribute(item Item, keys ...string) float64 {
	for _, key := range keys {
		value, ok := item.Attributes[key]
		if !ok {
			continue
		}
		switch number := value.(type) {
		case float64:
			return number
		case float32:
			return float64(number)
		case int:
			return float64(number)
		case int64:
			return float64(number)
		}
	}
	return 0
}

func priorityStats(item Item) [4]float64 {
	return [4]float64{
		weaponDamage(item),
		itemAttribute(item, "attack"),
		itemAttribute(item, "defence", "defense"),
		itemAttribute(item, "maxHealth"),
	}
}

type statEffect struct {
	flat, percent      [4]float64
	damage             [2]float64
	defensePenetration float64
}

var affixAttackStatsPattern = regexp.MustCompile(`(?i)\b(?:attack|physical damage|magic damage|defense penetration)\b`)
var affixDefenseStatsPattern = regexp.MustCompile(`(?i)\b(?:physical resistance|magic resistance|resistance|defense)\b`)
var affixHPStatsPattern = regexp.MustCompile(`(?i)\b(?:health|hp)\b`)

var statEffectPattern = regexp.MustCompile(`(?i)(Maximum Health|Maximum Energy|Physical Damage(?:\s+per stack)?|Magic Damage(?:\s+per stack)?|Attack|Defense Penetration|Defense)\s*\+\s*([\d.]+)(%)?`)
var stackingUpToPattern = regexp.MustCompile(`(?i)stacking\s+up\s+to\s+([\d.]+)`)

func parseStatEffect(text string) statEffect {
	var result statEffect
	stackCount := 1.0
	if match := stackingUpToPattern.FindStringSubmatch(text); len(match) == 2 {
		if parsed, err := strconv.ParseFloat(match[1], 64); err == nil {
			stackCount = parsed
		}
	}
	lowerText := strings.ToLower(text)
	for _, match := range statEffectPattern.FindAllStringSubmatch(text, -1) {
		value, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			continue
		}
		index := -1
		switch strings.ToLower(match[1]) {
		case "physical damage", "physical damage per stack":
			if stackCount > 1 && !strings.Contains(lowerText, "physical damage per stack") {
				value *= stackCount
			}
			result.damage[0] += value
			continue
		case "magic damage", "magic damage per stack":
			if stackCount > 1 && !strings.Contains(lowerText, "magic damage per stack") {
				value *= stackCount
			}
			result.damage[1] += value
			continue
		case "defense penetration":
			result.defensePenetration += value
			continue
		case "attack":
			index = 1
		case "defense":
			index = 2
		case "maximum health":
			index = 3
		case "maximum energy":
			continue
		}
		if index < 0 {
			continue
		}
		if match[3] == "%" {
			result.percent[index] += value
		} else {
			result.flat[index] += value
		}
	}
	return result
}

func inferredAffixStats(detail GUIAffixDetails) []string {
	levels := make([]string, 0, len(detail.Levels))
	for _, level := range detail.Levels {
		levels = append(levels, level)
	}
	text := strings.ReplaceAll(strings.ToLower(strings.Join(levels, " ")), "defense penetration", "")
	stats := []string{}
	if affixAttackStatsPattern.MatchString(text) {
		stats = append(stats, "Attack")
	}
	if affixDefenseStatsPattern.MatchString(text) {
		stats = append(stats, "Defense")
	}
	if affixHPStatsPattern.MatchString(text) {
		stats = append(stats, "HP")
	}
	if len(stats) == 0 {
		return []string{"None"}
	}
	return stats
}

func (model *statModel) affixEffect(affix Affix) statEffect {
	key := normalize(affix.Name)
	if effects := model.effects[key]; effects != nil {
		if effect, ok := effects[affix.Level]; ok {
			return effect
		}
	}
	detail := model.details[key]
	return parseStatEffect(detail.Description + " " + detail.Levels[strconv.Itoa(affix.Level)])
}

type statModel struct {
	base       [4]float64
	details    map[string]GUIAffixDetails
	effects    map[string]map[int]statEffect
	thresholds map[string]map[int]int
	damageType int
}

func newStatModel(base ClassStats, details map[string]GUIAffixDetails, damageType int) *statModel {
	model := &statModel{details: map[string]GUIAffixDetails{}, effects: map[string]map[int]statEffect{}, thresholds: map[string]map[int]int{}, damageType: damageType}
	if base.Attack != nil {
		model.base[1] = *base.Attack
	}
	if base.Defense != nil {
		model.base[2] = *base.Defense
	}
	if base.Health != nil {
		model.base[3] = *base.Health
	}
	for name, detail := range details {
		key := normalize(name)
		model.details[key] = detail
		effects := make(map[int]statEffect, len(detail.Levels))
		thresholds := make(map[int]int, len(detail.Levels))
		for levelText, text := range detail.Levels {
			level, err := strconv.Atoi(levelText)
			if err == nil {
				effects[level] = parseStatEffect(detail.Description + " " + text)
				thresholds[level] = affixThresholdCount(detail, level)
			}
		}
		model.effects[key] = effects
		model.thresholds[key] = thresholds
	}
	if base == (ClassStats{}) && len(model.details) == 0 {
		return nil
	}
	return model
}

func (model *statModel) addAffixes(stats *[4]float64, damage *float64, affixes []Affix) {
	if model == nil {
		return
	}
	for _, affix := range affixes {
		effect := model.affixEffect(affix)
		for index := range stats {
			stats[index] += effect.flat[index] + model.base[index]*effect.percent[index]/100
		}
		if damage != nil {
			*damage += effect.damage[model.damageType]
		}
	}
}

func (model *statModel) affixDefensePenetration(affixes []Affix) float64 {
	if model == nil {
		return 0
	}
	total := 0.0
	for _, affix := range affixes {
		total += model.affixEffect(affix).defensePenetration
	}
	return total
}

func (model *statModel) itemStats(item Item, option optionState, gems map[string]Item) ([4]float64, float64) {
	stats := priorityStats(item)
	damage := itemDamage(item, modelDamageType(model))
	if model == nil {
		return stats, damage
	}
	model.addAffixes(&stats, &damage, item.Equipment.Affixes)
	for _, id := range option.Selected {
		if gem, ok := gems[id]; ok {
			model.addAffixes(&stats, &damage, gem.Gem.Affixes)
		}
	}
	return stats, damage
}

func (model *statModel) itemDefensePenetration(item Item, option optionState, gems map[string]Item) float64 {
	total := model.affixDefensePenetration(item.Equipment.Affixes)
	for _, id := range option.Selected {
		if gem, ok := gems[id]; ok {
			total += model.affixDefensePenetration(gem.Gem.Affixes)
		}
	}
	return total
}

func solutionStats(result *Solution, equipment, gems map[string]Item, model *statModel) ([4]float64, float64) {
	var stats [4]float64
	var damage float64
	for _, piece := range result.Pieces {
		item, ok := equipment[piece.ItemID]
		if !ok {
			continue
		}
		selected := make([]string, 0, len(piece.GemSlots))
		for _, slot := range piece.GemSlots {
			if slot.Gem != nil {
				selected = append(selected, slot.Gem.ID)
			}
		}
		pieceStats, pieceDamage := model.itemStats(item, optionState{Selected: selected}, gems)
		for index := range stats {
			stats[index] += pieceStats[index]
		}
		damage += pieceDamage
	}
	return stats, damage
}

func solutionDefensePenetration(result *Solution, equipment, gems map[string]Item, model *statModel) float64 {
	total := 0.0
	for _, piece := range result.Pieces {
		item, ok := equipment[piece.ItemID]
		if !ok {
			continue
		}
		selected := make([]string, 0, len(piece.GemSlots))
		for _, slot := range piece.GemSlots {
			if slot.Gem != nil {
				selected = append(selected, slot.Gem.ID)
			}
		}
		total += model.itemDefensePenetration(item, optionState{Selected: selected}, gems)
	}
	return total
}

func modelDamageType(model *statModel) int {
	if model == nil {
		return 0
	}
	return model.damageType
}

func itemDamage(item Item, damageType int) float64 {
	keys := []string{"physicalIncrease", "physicalDamage"}
	if damageType == 1 {
		keys = []string{"magicalIncrease", "magicIncrease", "magicalDamage", "magicDamage"}
	}
	return itemAttribute(item, keys...) * 100
}

func weaponDamage(item Item) float64 {
	if item.MainCategory != "weapon" {
		return 0
	}
	return itemAttribute(item, "weaponDamage", "damage", "attack")
}

func compareStats(candidate, current [4]float64, order [4]int) int {
	return compareBuildStats(candidate, 0, current, 0, order)
}

func compareBuildStats(candidate [4]float64, candidateDamage float64, current [4]float64, currentDamage float64, order [4]int) int {
	if order == [4]int{} {
		order = [4]int{0, 1, 2, 3}
	}
	for _, index := range order {
		if candidate[index] != current[index] {
			if candidate[index] > current[index] {
				return 1
			}
			return -1
		}
		if index == 1 && candidateDamage != currentDamage {
			if candidateDamage > currentDamage {
				return 1
			}
			return -1
		}
	}
	return 0
}

func itemSignature(item Item, option optionState) string {
	return strings.Join([]string{item.ID, item.Name, item.SubName, strconv.Itoa(item.Grade), strings.Join(option.Selected, ",")}, "\x00")
}

func (candidate solveState) betterThan(current *solveState, limits []int) bool {
	if current == nil {
		return true
	}
	targets := candidate.Targets
	if len(targets) != len(limits) {
		targets = limits
	}
	currentTargets := current.Targets
	if len(currentTargets) != len(limits) {
		currentTargets = limits
	}
	distance, currentDistance := coverageDistance(candidate.Coverage, targets), coverageDistance(current.Coverage, currentTargets)
	if distance != currentDistance {
		return distance < currentDistance
	}
	if candidate.RaritySum != current.RaritySum {
		return candidate.RaritySum < current.RaritySum
	}
	for index := 0; index < candidate.RarityPriorityCount; index++ {
		slot := candidate.RarityPriority[index]
		if candidate.Rarities[slot] != current.Rarities[slot] {
			return candidate.Rarities[slot] > current.Rarities[slot]
		}
	}
	if comparison := compareBuildStats(candidate.Stats, candidate.Damage, current.Stats, current.Damage, candidate.StatOrder); comparison != 0 {
		return comparison > 0
	}
	if candidate.Cost.TierDeficit != current.Cost.TierDeficit {
		return candidate.Cost.TierDeficit < current.Cost.TierDeficit
	}
	return candidate.fullSignature() < current.fullSignature()
}

type optimizationProgress struct {
	tested, reported int
	updated          time.Time
	report           func(mode, stage string, current, total, tested int)
	milestone        func(string)
	restrictGems     bool
}

func (p *optimizationProgress) start(mode, stage string, current, total int) {
	if p == nil || p.report == nil {
		return
	}
	p.reported, p.updated = p.tested, time.Now()
	p.report(mode, stage, current, total, p.tested)
}

func (p *optimizationProgress) test(mode, stage string, current, total int) {
	if p == nil || p.report == nil {
		return
	}
	p.tested++
	if p.tested-p.reported < 100 || time.Since(p.updated) < 100*time.Millisecond {
		return
	}
	p.reported, p.updated = p.tested, time.Now()
	p.report(mode, stage, current, total, p.tested)
}

func (p *optimizationProgress) note(message string) {
	if p == nil || p.milestone == nil {
		return
	}
	p.milestone(message)
}

func (p *optimizationProgress) add(mode, stage string, current, total, count int) {
	if p == nil || p.report == nil {
		return
	}
	p.tested += count
	if p.tested-p.reported < 100 || time.Since(p.updated) < 100*time.Millisecond {
		return
	}
	p.reported, p.updated = p.tested, time.Now()
	p.report(mode, stage, current, total, p.tested)
}

func makePiece(item Item, selected []string, gemsByID map[string]Item) Piece {
	var native interface{} = "No Native Affix"
	if len(item.Equipment.Affixes) > 0 {
		native = item.Equipment.Affixes
	}
	slot := slotKey(item.SubName)
	if item.MainCategory == "weapon" {
		slot = "weapon"
	}
	piece := Piece{Slot: slot, ItemID: item.ID, Grade: item.Grade, Name: item.Name, NativeAffixes: native, Gems: []interface{}{}, GemSlots: []GemSlot{}}
	for _, id := range selected {
		if id == "" {
			piece.Gems = append(piece.Gems, nil)
			continue
		}
		gem := gemsByID[id]
		piece.Gems = append(piece.Gems, map[string]interface{}{"id": gem.ID, "name": gem.Name})
	}
	for index, socket := range holes(item) {
		typ := socket.Type
		if typ == -1 {
			typ = 5
		}
		slot := GemSlot{Type: gemTypes[typ], Tier: socket.Level}
		if index < len(selected) && selected[index] != "" {
			gem := gemsByID[selected[index]]
			slot.Gem = &GemRef{ID: gem.ID, Name: gem.Name, Affixes: gem.Gem.Affixes}
		}
		piece.GemSlots = append(piece.GemSlots, slot)
	}
	return piece
}

type fillNode struct {
	parent *fillNode
	gemID  string
}

type fillState struct {
	coverage    []int
	node        *fillNode
	bonus       int
	bonusStats  [4]float64
	bonusDamage float64
	cost        Cost
	signature   string
}

type emptyGemSlot struct {
	pieceIndex int
	slotIndex  int
	typ        int
	tier       int
}

func fillThresholdScore(coverage []int, requirements []Requirement) int {
	score := 0
	for i, requirement := range requirements {
		for _, threshold := range requirement.Thresholds {
			if coverage[i] >= threshold {
				score++
			}
		}
	}
	return score
}

func comparePriorityStats(candidate [4]float64, candidateDamage float64, current [4]float64, currentDamage float64, order [4]int) int {
	for _, index := range order {
		if candidate[index] != current[index] {
			if candidate[index] > current[index] {
				return 1
			}
			return -1
		}
	}
	if candidateDamage != currentDamage {
		if candidateDamage > currentDamage {
			return 1
		}
		return -1
	}
	return 0
}

func fillStateBetter(candidate, current fillState, requirements []Requirement, limits []int, order [4]int, useStats bool) bool {
	thresholds, currentThresholds := fillThresholdScore(candidate.coverage, requirements), fillThresholdScore(current.coverage, requirements)
	if thresholds != currentThresholds {
		return thresholds > currentThresholds
	}
	target, currentTarget := 0, 0
	for i := range limits {
		target += min(candidate.coverage[i], limits[i])
		currentTarget += min(current.coverage[i], limits[i])
	}
	if target != currentTarget {
		return target > currentTarget
	}
	if useStats {
		if comparison := compareAttackPriorityStats(candidate.bonusStats, candidate.bonusDamage, current.bonusStats, current.bonusDamage, order); comparison != 0 {
			return comparison > 0
		}
	}
	if candidate.bonus != current.bonus {
		return candidate.bonus > current.bonus
	}
	if candidate.cost.TierDeficit != current.cost.TierDeficit {
		return candidate.cost.TierDeficit < current.cost.TierDeficit
	}
	return candidate.signature < current.signature
}

func fillGemSlotsGlobal(result *Solution, gems []Item, requirements []Requirement, order [4]int, model *statModel) error {
	limits := make([]int, len(requirements))
	positions := map[string]bool{}
	for i, requirement := range requirements {
		limits[i] = max(requirement.Level, requirement.Max)
		positions[requirement.Key] = true
	}
	coverage := make([]int, len(requirements))
	empty := []emptyGemSlot{}
	for pieceIndex := range result.Pieces {
		piece := &result.Pieces[pieceIndex]
		if native, ok := piece.NativeAffixes.([]Affix); ok {
			for i, requirement := range requirements {
				coverage[i] += affixValue(native, requirement.Key)
			}
		}
		for slotIndex := range piece.GemSlots {
			slot := &piece.GemSlots[slotIndex]
			if slot.Gem != nil {
				for i, requirement := range requirements {
					coverage[i] += affixValue(slot.Gem.Affixes, requirement.Key)
				}
				continue
			}
			typ := 5
			for value, name := range gemTypes {
				if name == slot.Type {
					typ = value
				}
			}
			empty = append(empty, emptyGemSlot{pieceIndex, slotIndex, typ, slot.Tier})
		}
	}
	if len(empty) == 0 {
		return nil
	}
	if model != nil {
		targets := make(map[string]bool, len(requirements))
		for _, requirement := range requirements {
			targets[requirement.Key] = true
		}
		gems = filterGemsByStats(gems, targets, model.details)
	}
	gemsByID := map[string]Item{}
	for _, gem := range gems {
		gemsByID[gem.ID] = gem
	}
	states := map[string]fillState{coverageKey(coverage): {coverage: append([]int(nil), coverage...)}}
	for _, slot := range empty {
		choices := gemChoices(gems, Socket{Type: slot.typ, Level: slot.tier})
		if len(choices) == 0 {
			return fmt.Errorf("no %s gem up to tier %d", gemTypes[slot.typ], slot.tier)
		}
		sort.SliceStable(choices, func(i, j int) bool {
			targetGem := func(gem *Item) bool {
				for _, affix := range gem.Gem.Affixes {
					if positions[normalize(affix.Name)] {
						return true
					}
				}
				return false
			}
			left, right := targetGem(choices[i]), targetGem(choices[j])
			if left != right {
				return left
			}
			return choices[i].ID < choices[j].ID
		})
		next := map[string]fillState{}
		for _, state := range states {
			for index, gem := range choices {
				addition := make([]int, len(requirements))
				for i, requirement := range requirements {
					addition[i] = affixValue(gem.Gem.Affixes, requirement.Key)
				}
				candidateCoverage := addCoverage(state.coverage, addition, limits)
				bonus := state.bonus
				bonusStats, bonusDamage := state.bonusStats, state.bonusDamage
				if model != nil {
					var gemStats [4]float64
					model.addAffixes(&gemStats, &bonusDamage, gem.Gem.Affixes)
					for stat := range bonusStats {
						bonusStats[stat] += gemStats[stat]
					}
				}
				for _, affix := range gem.Gem.Affixes {
					if !positions[normalize(affix.Name)] {
						bonus += affix.Level
					}
				}
				candidate := fillState{coverage: candidateCoverage, node: &fillNode{parent: state.node, gemID: gem.ID}, bonus: bonus, bonusStats: bonusStats, bonusDamage: bonusDamage, signature: state.signature + fmt.Sprintf("%03d", index), cost: state.cost.add(Cost{Recommended: gem.RecommendedPrice, Min: gem.MinPrice, Max: gem.MaxPrice, LevelSum: gem.Gem.AffixGemLevel, Count: 1, TierDeficit: slot.tier - gem.Gem.AffixGemLevel})}
				key := coverageKey(candidateCoverage)
				old, exists := next[key]
				if !exists || fillStateBetter(candidate, old, requirements, limits, order, model != nil) {
					next[key] = candidate
				}
			}
		}
		states = next
		if len(states) == 0 {
			return fmt.Errorf("no compatible gem assignment for %s sockets", gemTypes[slot.typ])
		}
	}
	var best *fillState
	for _, state := range states {
		if best == nil || fillStateBetter(state, *best, requirements, limits, order, model != nil) {
			candidate := state
			best = &candidate
		}
	}
	ids := make([]string, len(empty))
	for node, index := best.node, len(ids)-1; node != nil; node, index = node.parent, index-1 {
		ids[index] = node.gemID
	}
	for index, slot := range empty {
		gem := gemsByID[ids[index]]
		gemSlot := &result.Pieces[slot.pieceIndex].GemSlots[slot.slotIndex]
		gemSlot.Gem, gemSlot.Filled = &GemRef{ID: gem.ID, Name: gem.Name, Affixes: gem.Gem.Affixes}, true
		result.Pieces[slot.pieceIndex].Gems[slot.slotIndex] = map[string]interface{}{"id": gem.ID, "name": gem.Name}
		result.MinPrice += gem.MinPrice
		result.AveragePrice += gem.RecommendedPrice
		result.MaxPrice += gem.MaxPrice
		result.GemLevelSum += gem.Gem.AffixGemLevel
		result.GemCount++
	}
	return nil
}

func fillGemSlots(result *Solution, gems []Item, requirements []Requirement) error {
	return fillGemSlotsGlobal(result, gems, requirements, [4]int{0, 1, 2, 3}, nil)
}

func fillGemSlotsWithStats(result *Solution, gems []Item, requirements []Requirement, order [4]int, model *statModel) error {
	return fillGemSlotsGlobal(result, gems, requirements, order, model)
}

func unusedSlots(result *Solution, affixSlots bool) int {
	positions := map[string]int{}
	limits := make([]int, 0, len(result.Effects))
	for name, level := range result.Effects {
		positions[normalize(name)] = len(limits)
		limits = append(limits, level)
	}
	coverage := make([]int, len(limits))
	total := 0
	additions := [][]Affix{}
	for _, piece := range result.Pieces {
		if affixes, ok := piece.NativeAffixes.([]Affix); ok {
			coverage = addCoverage(coverage, vector(affixes, positions, limits), limits)
		}
		for _, slot := range piece.GemSlots {
			if affixSlots {
				total += slot.Tier
			} else {
				total++
			}
			if slot.Gem != nil {
				if affixSlots {
					for _, affix := range slot.Gem.Affixes {
						additions = append(additions, []Affix{affix})
					}
				} else {
					additions = append(additions, slot.Gem.Affixes)
				}
			}
		}
	}
	type gemState struct {
		coverage []int
		count    int
	}
	states := map[string]gemState{coverageKey(coverage): {coverage: coverage}}
	for _, affixes := range additions {
		next := make(map[string]gemState, len(states))
		for key, state := range states {
			next[key] = state
		}
		addition := vector(affixes, positions, limits)
		for _, state := range states {
			combined := addCoverage(state.coverage, addition, limits)
			key := coverageKey(combined)
			if old, exists := next[key]; !exists || state.count+1 < old.count {
				next[key] = gemState{combined, state.count + 1}
			}
		}
		states = next
	}
	return total - states[coverageKey(limits)].count
}

func unusedGemSlots(result *Solution) int   { return unusedSlots(result, false) }
func unusedAffixSlots(result *Solution) int { return unusedSlots(result, true) }

func coverageDistance(coverage, limits []int) int {
	distance := 0
	for i, value := range coverage {
		if value < limits[i] {
			distance += limits[i] - value
		}
	}
	return distance
}

func rarityCombinationSum(levels map[string]int) int {
	total := 0
	for _, slot := range slotOrder {
		total += levels[slot]
	}
	return total
}

func solutionFromState(state solveState, distance int, levels map[string]int, requirements []Requirement, stages []string, gemsByID map[string]Item) *Solution {
	if levels == nil {
		levels = make(map[string]int, len(stages))
		for index, stage := range stages {
			levels[stage] = state.Rarities[index]
		}
	}
	pieces := make([]Piece, len(stages))
	maxArmor := 0
	combination := make([]int, len(stages))
	for i, stage := range stages {
		combination[i] = levels[stage]
		if stage != "weapon" {
			maxArmor = max(maxArmor, levels[stage])
		}
	}
	for node, index := state.Node, len(stages)-1; node != nil; node, index = node.parent, index-1 {
		pieces[index] = makePiece(node.item, node.selected, gemsByID)
	}
	effects := map[string]int{}
	for _, requirement := range requirements {
		if isInternalRequirement(requirement) {
			continue
		}
		effects[requirement.Name] = requirement.Level
	}
	return &Solution{Possible: true, Closest: distance > 0, Distance: distance, ArmorLevel: maxArmor, WeaponLevel: levels["weapon"], Effects: effects, MinPrice: state.Cost.Min, AveragePrice: state.Cost.Recommended, MaxPrice: state.Cost.Max, GemLevelSum: state.Cost.LevelSum, GemCount: state.Cost.Count, Pieces: pieces, LevelCombination: combination, quality: state, raritySum: state.RaritySum}
}

func (candidate *Solution) betterThan(current *Solution) bool {
	if current == nil {
		return true
	}
	if candidate.Distance != current.Distance {
		return candidate.Distance < current.Distance
	}
	if candidate.raritySum != current.raritySum {
		return candidate.raritySum < current.raritySum
	}
	if candidate.quality.betterThan(&current.quality, candidate.quality.Targets) {
		return true
	}
	if current.quality.betterThan(&candidate.quality, current.quality.Targets) {
		return false
	}
	return candidate.quality.Signature < current.quality.Signature
}

type boundedSolveStates struct {
	states     map[string]solveState
	limits     []int
	stateLimit int
	worstKey   string
}

// ponytail: keep one state per coverage, apply safe dominance before this X-state WASM memory guard.
const maxSolveStates = 768

func newBoundedSolveStates(limits []int, stateLimit int) *boundedSolveStates {
	return &boundedSolveStates{states: map[string]solveState{}, limits: limits, stateLimit: stateLimit}
}

func (states *boundedSolveStates) add(key string, candidate solveState) bool {
	if old, exists := states.states[key]; exists {
		if candidate.betterThan(&old, states.limits) {
			candidate.Signature = candidate.fullSignature()
			states.states[key] = candidate
			if key == states.worstKey {
				states.refreshWorst()
			}
			return true
		}
		return false
	}
	if states.stateLimit > 0 && len(states.states) == states.stateLimit {
		worst := states.states[states.worstKey]
		if !candidate.betterThan(&worst, states.limits) {
			return false
		}
		for _, state := range states.states {
			if state.dominates(candidate) {
				return false
			}
		}
		for existingKey, state := range states.states {
			if candidate.dominates(state) {
				delete(states.states, existingKey)
			}
		}
		delete(states.states, states.worstKey)
		candidate.Signature = candidate.fullSignature()
		states.states[key] = candidate
		states.refreshWorst()
		return true
	}
	candidate.Signature = candidate.fullSignature()
	states.states[key] = candidate
	if states.worstKey == "" || states.states[states.worstKey].betterThan(&candidate, states.limits) {
		states.worstKey = key
	}
	return true
}

func (states *boundedSolveStates) refreshWorst() {
	states.worstKey = ""
	for key, state := range states.states {
		if states.worstKey == "" || states.states[states.worstKey].betterThan(&state, states.limits) {
			states.worstKey = key
		}
	}
}

func (candidate solveState) dominates(other solveState) bool {
	if candidate.MinRarity != other.MinRarity || candidate.MaxRarity != other.MaxRarity || len(candidate.Coverage) != len(other.Coverage) {
		return false
	}
	strict := false
	for index, value := range candidate.Coverage {
		if value < other.Coverage[index] {
			return false
		}
		strict = strict || value > other.Coverage[index]
	}
	if candidate.RaritySum > other.RaritySum {
		return false
	}
	if candidate.RaritySum < other.RaritySum {
		strict = true
	} else {
		statsStrict := false
		for index, value := range candidate.Stats {
			if value < other.Stats[index] {
				return false
			}
			statsStrict = statsStrict || value > other.Stats[index]
		}
		if candidate.Damage < other.Damage {
			return false
		}
		statsStrict = statsStrict || candidate.Damage > other.Damage
		if !statsStrict && candidate.Cost.TierDeficit > other.Cost.TierDeficit {
			return false
		}
		strict = strict || statsStrict || candidate.Cost.TierDeficit < other.Cost.TierDeficit
	}
	return strict
}

func (states *boundedSolveStates) pruneDominated() {
	const maxDominanceGroup = maxSolveStates
	groups := map[[2]int][]solveState{}
	for _, state := range states.states {
		groups[[2]int{state.MinRarity, state.MaxRarity}] = append(groups[[2]int{state.MinRarity, state.MaxRarity}], state)
	}
	for _, group := range groups {
		if len(group) > maxDominanceGroup {
			continue
		}
		frontier := make([]solveState, 0, len(group))
		for _, candidate := range group {
			dominated := false
			for _, other := range frontier {
				if other.dominates(candidate) {
					dominated = true
					break
				}
			}
			if dominated {
				continue
			}
			kept := frontier[:0]
			for _, other := range frontier {
				if !candidate.dominates(other) {
					kept = append(kept, other)
				}
			}
			frontier = append(kept, candidate)
		}
		for _, state := range group {
			delete(states.states, solveStateKey(state.Coverage, state.MinRarity, state.MaxRarity))
		}
		for _, state := range frontier {
			states.states[solveStateKey(state.Coverage, state.MinRarity, state.MaxRarity)] = state
		}
	}
}

func solveBoundedWithCaps(equipment, gems []Item, levels map[string]int, requirements []Requirement, positions map[string]int, targets, limits []int, bounds map[string]map[int][]int, mode string, progress *optimizationProgress, closest bool, cache *optionCache, rarityLimit, stateLimit int, model *statModel, rarityPriority []string, statOrder ...[4]int) *Solution {
	priority := [4]int{0, 1, 2, 3}
	if len(statOrder) > 0 {
		priority = statOrder[0]
	}
	rarityOrder, rarityOrderCount := rarityPriorityIndexes(rarityPriority)
	groups := map[string][]Item{}
	for _, item := range equipment {
		slot := slotKey(item.SubName)
		if item.MainCategory == "weapon" {
			slot = "weapon"
		}
		if levels == nil || item.Grade == levels[slot] {
			groups[slot] = append(groups[slot], item)
		}
	}
	stages := append([]string{"weapon"}, slotOrder[1:]...)
	for _, stage := range stages {
		if len(groups[stage]) == 0 {
			return nil
		}
	}
	minimumRemainingRarity := make([]int, len(stages)+1)
	for i := len(stages) - 1; i >= 0; i-- {
		for _, item := range groups[stages[i]] {
			if minimumRemainingRarity[i] == 0 || item.Grade < minimumRemainingRarity[i] {
				minimumRemainingRarity[i] = item.Grade
			}
		}
		minimumRemainingRarity[i] += minimumRemainingRarity[i+1]
	}
	remaining := make([][]int, len(stages)+1)
	remaining[len(stages)] = make([]int, len(limits))
	for i := len(stages) - 1; i >= 0; i-- {
		addition := bounds[stages[i]][levels[stages[i]]]
		if levels == nil {
			addition = make([]int, len(limits))
			for _, candidate := range bounds[stages[i]] {
				for index, value := range candidate {
					addition[index] = max(addition[index], value)
				}
			}
		}
		remaining[i] = addCoverage(remaining[i+1], addition, limits)
	}
	gemsByID := map[string]Item{}
	for _, gem := range gems {
		gemsByID[gem.ID] = gem
	}
	states := newBoundedSolveStates(limits, stateLimit)
	zero := make([]int, len(limits))
	initial := solveState{Coverage: zero, StatOrder: priority, RarityPriority: rarityOrder, RarityPriorityCount: rarityOrderCount}
	if model != nil {
		initial.Stats = model.base
	}
	if levels != nil {
		initial.RaritySum = rarityCombinationSum(levels)
	}
	states.add(solveStateKey(zero, 0, 0), initial)
	allowBonusGems := cache == nil || cache.allowBonusGems
	exactTargets := cache != nil && cache.exactTargets
	var exactPositions []bool
	if cache != nil {
		exactPositions = cache.exactPositions
	}
	reportProgress := progress != nil && progress.report != nil
	recycledCoverage := [][]int{}
	for stageIndex, stage := range stages {
		pendingProgress := 0
		progress.start(mode, stage, stageIndex+1, len(stages))
		next := newBoundedSolveStates(limits, stateLimit)
		stageOptions := map[string]solveStageOption{}
		for _, item := range groups[stage] {
			var options []optionState
			if cache != nil && item.ID != "" {
				options = cache.items[item.ID]
				if options == nil {
					options = itemOptionsWithCache(item, gems, positions, limits, cache.choice, cache.vector, gemsByID, model, priority, allowBonusGems, exactTargets, exactPositions, cache.stats)
					cache.items[item.ID] = options
				}
			} else {
				var choices map[socketChoiceKey][]*Item
				var vectors map[string][]int
				var statsCache map[string]cachedItemStats
				if cache != nil {
					choices = cache.choice
					vectors = cache.vector
					statsCache = cache.stats
				}
				options = itemOptionsWithCache(item, gems, positions, limits, choices, vectors, gemsByID, model, priority, allowBonusGems, exactTargets, exactPositions, statsCache)
			}
			for _, option := range options {
				stats, damage := option.Stats, option.Damage
				bonusNativeAffixes := 0
				if cache != nil && !cache.allowBonusGems {
					bonusNativeAffixes = bonusNativeAffixCount(item, option, positions)
				}
				candidate := solveStageOption{item: item, option: option, signature: itemSignature(item, option), stats: stats, damage: damage, bonusNativeAffixes: bonusNativeAffixes}
				key := coverageKey(option.Coverage) + string(byte(item.Grade))
				if current, exists := stageOptions[key]; !exists || candidate.betterThan(current, priority) {
					stageOptions[key] = candidate
				}
			}
		}
		for _, stageOption := range stageOptions {
			for _, previous := range states.states {
				if reportProgress {
					pendingProgress++
					if pendingProgress == 100 {
						progress.add(mode, stage, stageIndex+1, len(stages), pendingProgress)
						pendingProgress = 0
					}
				}
				if exactTargets {
					over := false
					for requirement, exact := range exactPositions {
						if exact && previous.Coverage[requirement]+stageOption.option.RawCoverage[requirement] > limits[requirement] {
							over = true
							break
						}
					}
					if over {
						continue
					}
				}
				weaponRarity := previous.Rarities[0]
				if levels != nil {
					weaponRarity = levels["weapon"]
				}
				if stage != "weapon" && (cache == nil || !cache.allowArmorAboveWeapon) && stageOption.item.Grade > weaponRarity {
					continue
				}
				if !closest && !canReachAdded(previous.Coverage, stageOption.option.Coverage, remaining[stageIndex+1], targets, limits) {
					continue
				}
				minRarity, maxRarity := previous.MinRarity, previous.MaxRarity
				if minRarity == 0 {
					minRarity, maxRarity = stageOption.item.Grade, stageOption.item.Grade
				} else {
					minRarity = min(minRarity, stageOption.item.Grade)
					maxRarity = max(maxRarity, stageOption.item.Grade)
				}
				if maxRarity-minRarity > 1 {
					continue
				}
				stats := previous.Stats
				for index, value := range stageOption.stats {
					stats[index] += value
				}
				damage := previous.Damage + stageOption.damage
				rarityLevels := previous.Rarities
				raritySum := previous.RaritySum
				if levels == nil {
					rarityLevels[stageIndex] = stageOption.item.Grade
					raritySum += stageOption.item.Grade
				}
				if rarityLimit > 0 && raritySum+minimumRemainingRarity[stageIndex+1] > rarityLimit {
					continue
				}
				key := solveStateKeyAdded(previous.Coverage, stageOption.option.Coverage, minRarity, maxRarity, limits)
				coverage := next.states[key].Coverage
				allocatedCoverage := false
				if coverage == nil {
					if len(recycledCoverage) > 0 {
						last := len(recycledCoverage) - 1
						coverage = recycledCoverage[last]
						recycledCoverage = recycledCoverage[:last]
					} else {
						coverage = make([]int, len(limits))
					}
					for i := range limits {
						coverage[i] = previous.Coverage[i] + stageOption.option.Coverage[i]
						if coverage[i] > limits[i] {
							coverage[i] = limits[i]
						}
					}
					allocatedCoverage = true
				}
				candidate := solveState{Coverage: coverage, Targets: targets, Requirements: requirements, Stats: stats, Damage: damage, StatOrder: priority, Rarities: rarityLevels, RarityPriority: previous.RarityPriority, RarityPriorityCount: previous.RarityPriorityCount, RaritySum: raritySum, MinRarity: minRarity, MaxRarity: maxRarity, signaturePrefix: previous.Signature, signatureSuffix: stageOption.signature, Cost: previous.Cost.add(stageOption.option.Cost).add(Cost{Recommended: stageOption.item.RecommendedPrice, Min: stageOption.item.MinPrice, Max: stageOption.item.MaxPrice})}
				if next.add(key, candidate) {
					stored := next.states[key]
					stored.Node = &solveNode{parent: previous.Node, item: stageOption.item, selected: stageOption.option.Selected}
					next.states[key] = stored
				} else if allocatedCoverage {
					recycledCoverage = append(recycledCoverage, coverage)
				}
			}
		}
		if reportProgress && pendingProgress > 0 {
			progress.add(mode, stage, stageIndex+1, len(stages), pendingProgress)
		}
		next.pruneDominated()
		states = next
		if len(states.states) == 0 {
			return nil
		}
		if mode == "optimal" && !closest {
			progress.note(fmt.Sprintf("Finished evaluating %s candidates.", titleCase(stage)))
		}
	}
	var best *solveState
	for _, candidate := range states.states {
		if !closest && coverageDistance(candidate.Coverage, targets) > 0 {
			continue
		}
		if candidate.betterThan(best, limits) {
			copy := candidate
			best = &copy
		}
	}
	if best == nil {
		return nil
	}
	return solutionFromState(*best, coverageDistance(best.Coverage, targets), levels, requirements, stages, gemsByID)
}

func solve(equipment, gems []Item, levels map[string]int, requirements []Requirement, positions map[string]int, limits []int, bounds map[string]map[int][]int, mode string, progress *optimizationProgress, parallel bool, closest ...bool) *Solution {
	return solveBounded(equipment, gems, levels, requirements, positions, limits, bounds, mode, progress, len(closest) > 0 && closest[0])
}

func solveBounded(equipment, gems []Item, levels map[string]int, requirements []Requirement, positions map[string]int, limits []int, bounds map[string]map[int][]int, mode string, progress *optimizationProgress, closest bool) *Solution {
	return solveBoundedWithCaps(equipment, gems, levels, requirements, positions, limits, limits, bounds, mode, progress, closest, nil, 0, 0, nil, nil)
}

func solveWithCaps(equipment, gems []Item, levels map[string]int, requirements []Requirement, positions map[string]int, targets, limits []int, bounds map[string]map[int][]int, mode string, progress *optimizationProgress, closest bool, cache *optionCache, rarityLimit, stateLimit int, model *statModel, rarityPriority []string, statOrder ...[4]int) *Solution {
	return solveBoundedWithCaps(equipment, gems, levels, requirements, positions, targets, limits, bounds, mode, progress, closest, cache, rarityLimit, stateLimit, model, rarityPriority, statOrder...)
}

func impossible(reason string, minRarity, maxRarity int, requirements []Requirement, maximum map[string]int) *Solution {
	message := "No single equipment set can provide all requested affix levels together."
	if strings.HasPrefix(reason, "Requested") {
		message = "The requested affix levels exceed the available levels at the maximum rarity."
	}
	requested := map[string]int{}
	independent := map[string]int{}
	requestedTotal, maximumTotal := 0, 0
	for _, requirement := range requirements {
		if isInternalRequirement(requirement) {
			continue
		}
		requested[requirement.Name] = requirement.Level
		independent[requirement.Name] = maximum[requirement.Key]
		requestedTotal += requirement.Level
		maximumTotal += maximum[requirement.Key]
	}
	return &Solution{Possible: false, Message: message, Reason: reason, MinRarity: minRarity, MaxRarity: maxRarity, RequestedAffixes: requested, IndependentMaximums: independent, RequestedTotal: requestedTotal, MaximumTotal: maximumTotal}
}

func parseRequirements(values []string) ([]Requirement, error) {
	result := []Requirement{}
	positions := map[string]int{}
	for _, value := range values {
		name, levelText, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("expected AFFIX=LEVEL, got %q", value)
		}
		level, err := strconv.Atoi(levelText)
		if err != nil || level < 1 {
			return nil, fmt.Errorf("%s: level must be positive", name)
		}
		key := normalize(name)
		if index, exists := positions[key]; exists {
			if level > result[index].Level {
				result[index].Level = level
			}
		} else {
			positions[key] = len(result)
			result = append(result, Requirement{Key: key, Name: strings.TrimSpace(name), Level: level})
		}
	}
	if len(result) == 0 {
		return result, nil
	}
	return result, nil
}

func optimize(equipment, gems []Item, requirements []Requirement, weaponRarity, minRarity, maxRarity int, progress *optimizationProgress, parallel bool) (*Solution, error) {
	fixed := map[string]int{}
	if weaponRarity != 0 {
		fixed["weapon"] = weaponRarity
	}
	return optimizeConfigured(equipment, gems, requirements, minRarity, maxRarity, progress, parallel, fixed, rarityUpgradeOrder, weaponRarity)
}

func optimizeConfigured(equipment, gems []Item, requirements []Requirement, minRarity, maxRarity int, progress *optimizationProgress, parallel bool, fixedRarities map[string]int, priority []string, legacyWeaponRarity int, statOrder ...[4]int) (*Solution, error) {
	return optimizeConfiguredWithStats(equipment, gems, requirements, minRarity, maxRarity, progress, parallel, fixedRarities, priority, legacyWeaponRarity, ClassStats{}, nil, statOrder...)
}

func optimizeConfiguredWithStats(equipment, gems []Item, requirements []Requirement, minRarity, maxRarity int, progress *optimizationProgress, parallel bool, fixedRarities map[string]int, rarityPriority []string, legacyWeaponRarity int, baseStats ClassStats, affixDetails map[string]GUIAffixDetails, statOrder ...[4]int) (*Solution, error) {
	return optimizeConfiguredWithStatsCache(equipment, gems, requirements, minRarity, maxRarity, progress, parallel, fixedRarities, rarityPriority, 0, baseStats, affixDetails, nil, statOrder...)
}

func optimizeConfiguredWithStatsCache(equipment, gems []Item, requirements []Requirement, minRarity, maxRarity int, progress *optimizationProgress, parallel bool, fixedRarities map[string]int, rarityPriority []string, legacyWeaponRarity int, baseStats ClassStats, affixDetails map[string]GUIAffixDetails, statsCache map[string]cachedItemStats, statOrder ...[4]int) (*Solution, error) {
	selectedStatOrder := [4]int{0, 1, 2, 3}
	if len(statOrder) > 0 {
		selectedStatOrder = statOrder[0]
	}
	configuredFixed := make(map[string]int, len(fixedRarities)+1)
	for slot, level := range fixedRarities {
		configuredFixed[slot] = level
	}
	if legacyWeaponRarity != 0 {
		configuredFixed["weapon"] = legacyWeaponRarity
	}
	fixedRarities = configuredFixed
	if len(fixedRarities) > 0 {
		configured := make([]Item, 0, len(equipment))
		for _, item := range equipment {
			slot := slotKey(item.SubName)
			if item.MainCategory == "weapon" {
				slot = "weapon"
			}
			if fixed, ok := fixedRarities[slot]; ok && item.Grade != fixed {
				continue
			}
			if item.Grade < minRarity || item.Grade > maxRarity {
				continue
			}
			configured = append(configured, item)
		}
		equipment = configured
	} else {
		configured := make([]Item, 0, len(equipment))
		for _, item := range equipment {
			if item.Grade >= minRarity && item.Grade <= maxRarity {
				configured = append(configured, item)
			}
		}
		equipment = configured
	}
	positions := map[string]int{}
	targets, limits := make([]int, len(requirements)), make([]int, len(requirements))
	for i, requirement := range requirements {
		positions[requirement.Key] = i
		targets[i], limits[i] = requirement.Level, requirement.Level
	}
	exactPositions := make([]bool, len(requirements))
	for index, requirement := range requirements {
		exactPositions[index] = !isInternalRequirement(requirement)
	}
	damageType := 0
	for _, item := range equipment {
		if item.MainCategory == "weapon" {
			damageType = weaponDamageType(weaponClass(item))
			break
		}
	}
	model := newStatModel(baseStats, affixDetails, damageType)
	if model != nil {
		targets := make(map[string]bool, len(positions))
		for name := range positions {
			targets[name] = true
		}
		gems = filterGemsByStats(gems, targets, model.details)
	}
	bounds := buildUpperBounds(equipment, gems, positions, limits)
	available := map[string]bool{}
	for _, item := range equipment {
		for _, affix := range item.Equipment.Affixes {
			available[normalize(affix.Name)] = true
		}
	}
	for _, gem := range gems {
		for _, affix := range gem.Gem.Affixes {
			available[normalize(affix.Name)] = true
		}
	}
	for _, requirement := range requirements {
		if isInternalRequirement(requirement) {
			continue
		}
		if !available[requirement.Key] {
			return nil, fmt.Errorf("unknown affix: %s", requirement.Name)
		}
	}
	maximum := maxAffixLevels(equipment, gems, requirements, maxRarity)
	cache := &optionCache{items: map[string][]optionState{}, choice: map[socketChoiceKey][]*Item{}, vector: map[string][]int{}, stats: statsCache, allowBonusGems: progress == nil || !progress.restrictGems, allowArmorAboveWeapon: fixedRarities["weapon"] > 0, exactTargets: progress != nil && progress.restrictGems, exactPositions: exactPositions}
	seedResult := solveWithCaps(equipment, gems, nil, requirements, positions, targets, limits, bounds, "seed", progress, false, cache, 0, maxSolveStates, model, rarityPriority, selectedStatOrder)
	rarityLimit := 0
	if seedResult != nil {
		rarityLimit = seedResult.raritySum
	}
	search := func(findClosest bool) *Solution {
		if findClosest {
			rarityLimit = 0
		}
		return solveWithCaps(equipment, gems, nil, requirements, positions, targets, limits, bounds, "optimal", progress, findClosest, cache, rarityLimit, maxSolveStates, model, rarityPriority, selectedStatOrder)
	}
	if result := search(false); result != nil {
		return result, nil
	}
	if result := search(true); result != nil {
		return result, nil
	}
	return impossible("No set matching the requested effects and rarity constraints was found", minRarity, maxRarity, requirements, maximum), nil
}

func formatNumber(value float64) string {
	if math.Trunc(value) == value {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatGemName(gem GemRef) string {
	name := strings.TrimSpace(gem.Name)
	if lastSpace := strings.LastIndexByte(name, ' '); lastSpace >= 0 {
		name = strings.TrimSpace(name[:lastSpace])
	}
	return name
}

func formatGem(slot GemSlot) string {
	color := gemColors[slot.Type]
	if color == "" {
		color = slot.Type
	}
	if slot.Gem == nil {
		return color + " (empty)"
	}
	affixes := make([]string, len(slot.Gem.Affixes))
	for i, affix := range slot.Gem.Affixes {
		affixes[i] = affix.Name
	}
	return fmt.Sprintf("%s (%s - %s)", color, formatGemName(*slot.Gem), strings.Join(affixes, "/"))
}

func formatTable(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(header)
	}
	for _, row := range rows {
		for i, value := range row {
			widths[i] = max(widths[i], len(value))
		}
	}
	rowText := func(row []string) string {
		parts := make([]string, len(row))
		for i, value := range row {
			parts[i] = value + strings.Repeat(" ", widths[i]-len(value))
		}
		return strings.TrimRight(strings.Join(parts, " | "), " ")
	}
	divider := make([]string, len(widths))
	for i, width := range widths {
		divider[i] = strings.Repeat("-", width)
	}
	lines := []string{rowText(headers), strings.Join(divider, "-+-")}
	for _, row := range rows {
		lines = append(lines, rowText(row))
	}
	return strings.Join(lines, "\n")
}

func formatSolution(result *Solution) string {
	if !result.Possible {
		lines := []string{"Not possible: " + result.Message}
		requested := []string{}
		for name, level := range result.RequestedAffixes {
			requested = append(requested, fmt.Sprintf("%s (%d)", name, level))
		}
		sort.Strings(requested)
		if len(requested) > 0 {
			lines = append(lines, "Requested: "+strings.Join(requested, ", "))
		}
		maximum := []string{}
		for name, level := range result.IndependentMaximums {
			maximum = append(maximum, fmt.Sprintf("%s (%d)", name, level))
		}
		sort.Strings(maximum)
		if len(maximum) > 0 {
			lines = append(lines, "Independent maximums: "+strings.Join(maximum, ", "))
		}
		return strings.Join(lines, "\n")
	}
	type effect struct {
		name  string
		level int
	}
	effects := make([]effect, 0, len(result.Effects))
	for name, level := range result.Effects {
		effects = append(effects, effect{name, level})
	}
	sort.Slice(effects, func(i, j int) bool {
		if effects[i].level != effects[j].level {
			return effects[i].level > effects[j].level
		}
		return strings.ToLower(effects[i].name) < strings.ToLower(effects[j].name)
	})
	effectText := make([]string, len(effects))
	for i, effect := range effects {
		effectText[i] = fmt.Sprintf("%s (%d)", effect.name, effect.level)
	}
	minArmor, maxArmor := 7, 0
	for _, piece := range result.Pieces {
		if piece.Slot != "weapon" {
			minArmor, maxArmor = min(minArmor, piece.Grade), max(maxArmor, piece.Grade)
		}
	}
	armorRarity := rarityColors[maxArmor]
	if minArmor != maxArmor {
		armorRarity = rarityColors[minArmor] + "–" + armorRarity
	}
	lines := []string{}
	if result.Closest {
		lines = append(lines, fmt.Sprintf("Closest combination (target shortfall: %d)", result.Distance))
	}
	lines = append(lines,
		fmt.Sprintf("Rarity: Armor %s - Weapon %s", armorRarity, rarityColors[result.WeaponLevel]),
		"Affixes: "+strings.Join(effectText, ", "),
		fmt.Sprintf("Unused Gem Slots: %d", unusedGemSlots(result)),
		fmt.Sprintf("Unused Affix Slots: %d", unusedAffixSlots(result)),
		fmt.Sprintf("Recommended Price: %s", formatNumber(result.AveragePrice)),
		"Pieces:",
	)
	rows := [][]string{}
	for _, piece := range result.Pieces {
		native := "-"
		if affixes, ok := piece.NativeAffixes.([]Affix); ok {
			names := make([]string, len(affixes))
			for i, affix := range affixes {
				names[i] = affix.Name
			}
			native = strings.Join(names, ", ")
		}
		gems := make([]string, len(piece.GemSlots))
		for i, slot := range piece.GemSlots {
			gems[i] = formatGem(slot)
		}
		rows = append(rows, []string{strings.Title(piece.Slot), rarityColors[piece.Grade], piece.Name, native, strings.Join(gems, ", ")})
	}
	lines = append(lines, formatTable([]string{"Type", "Rarity", "Name", "Native Affixes", "Gems"}, rows))
	return strings.Join(lines, "\n")
}

type cliOptions struct {
	CharacterClass, WeaponRarity, MinRarity, MaxRarity, Ring, Amulet, Format string
	Affixes                                                                  []string
}

var cliValueOptions = map[string]bool{"class": true, "weapon-rarity": true, "min-rarity": true, "max-rarity": true, "ring": true, "amulet": true, "format": true}

func parseCLI(args []string) (cliOptions, bool, error) {
	options := cliOptions{WeaponRarity: "Any", MinRarity: "1", MaxRarity: "6", Format: "text"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" {
			return options, true, nil
		}
		if arg == "--affixes" {
			for i++; i < len(args) && !isCLIOption(args[i]); i++ {
				options.Affixes = append(options.Affixes, args[i])
			}
			i--
			continue
		}
		key, value, ok := strings.Cut(arg, "=")
		key = strings.TrimPrefix(key, "--")
		if !ok || !cliValueOptions[key] {
			return options, false, fmt.Errorf("use key=value options and --affixes AFFIX=LEVEL; invalid argument %q", arg)
		}
		switch key {
		case "class":
			options.CharacterClass = value
		case "weapon-rarity":
			options.WeaponRarity = value
		case "min-rarity":
			options.MinRarity = value
		case "max-rarity":
			options.MaxRarity = value
		case "ring":
			options.Ring = value
		case "amulet":
			options.Amulet = value
		case "format":
			options.Format = value
		}
	}
	if options.CharacterClass == "" || len(options.Affixes) == 0 {
		return options, false, errors.New("--class=CLASS and --affixes AFFIX=LEVEL are required")
	}
	if options.Format != "text" && options.Format != "json" {
		return options, false, errors.New("format must be text or json")
	}
	return options, false, nil
}

func isCLIOption(value string) bool {
	if strings.HasPrefix(value, "--") {
		return true
	}
	key, _, ok := strings.Cut(value, "=")
	return ok && cliValueOptions[key]
}

func printHelp() {
	fmt.Println("usage: optimizer [GUI] | optimizer --cli --class=CLASS --affixes AFFIX=LEVEL [options]")
	fmt.Println("options: weapon-rarity=Any|RARITY min-rarity=RARITY max-rarity=RARITY")
	fmt.Println("         ring=[HP/ATK/Any]/[PHYS/MAG/Any] amulet=[HP/ATK/Any]/[PHYS/MAG/Any] format=text|json")
	fmt.Println("rarities: 1 Gray, 2 White, 3 Green, 4 Blue, 5 Purple, 6 Gold")
	if equipment, gems, err := loadDatabase(""); err == nil {
		fmt.Println("affixes: " + strings.Join(affixNames(equipment, gems), ", "))
	}
}

func solutionJSON(result *Solution) map[string]interface{} {
	if !result.Possible {
		return map[string]interface{}{
			"possible":                false,
			"message":                 result.Message,
			"reason":                  result.Reason,
			"rarityRange":             map[string]interface{}{"min": map[string]interface{}{"level": result.MinRarity, "name": rarityNames[result.MinRarity]}, "max": map[string]interface{}{"level": result.MaxRarity, "name": rarityNames[result.MaxRarity]}},
			"requestedAffixes":        result.RequestedAffixes,
			"independentMaximums":     result.IndependentMaximums,
			"requestedTotalLevels":    result.RequestedTotal,
			"independentMaximumTotal": result.MaximumTotal,
			"note":                    "Each independent maximum is calculated separately; they cannot necessarily be combined into one set.",
		}
	}
	return map[string]interface{}{"possible": true, "armorLevel": result.ArmorLevel, "weaponLevel": result.WeaponLevel, "levelCombination": result.LevelCombination, "effects": result.Effects, "minPrice": result.MinPrice, "averagePrice": result.AveragePrice, "maxPrice": result.MaxPrice, "gemCost": map[string]int{"levelSum": result.GemLevelSum, "count": result.GemCount}, "pieces": result.Pieces}
}

func runCLI(args []string) {
	options, help, err := parseCLI(args)
	if help {
		printHelp()
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	minLevel, err := rarity(options.MinRarity)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	maxLevel, err := rarity(options.MaxRarity)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if minLevel > maxLevel {
		fmt.Fprintln(os.Stderr, "min-rarity cannot exceed max-rarity")
		os.Exit(2)
	}
	weaponLevel := 0
	if !strings.EqualFold(options.WeaponRarity, "Any") {
		weaponLevel, err = rarity(options.WeaponRarity)
		if err != nil || weaponLevel < minLevel || weaponLevel > maxLevel {
			fmt.Fprintln(os.Stderr, "weapon rarity must be Any or within the rarity range")
			os.Exit(2)
		}
	}
	equipment, gems, err := loadDatabase(options.CharacterClass)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	equipment, err = filterEquipment(equipment, options.Ring, options.Amulet)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	requirements, err := parseRequirements(options.Affixes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	result, err := optimize(equipment, gems, requirements, weaponLevel, minLevel, maxLevel, nil, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if options.Format == "json" {
		encoded, _ := json.MarshalIndent(solutionJSON(result), "", "  ")
		fmt.Println(string(encoded))
		return
	}
	fmt.Println(formatSolution(result))
}

// RunCLI preserves the command-line entry point for the desktop wrapper.
func RunCLI(args []string) { runCLI(args) }
