package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var gemActualColors = map[int]string{1: "Red", 2: "Pink", 3: "Blue", 4: "Green"}

type GUIAffix struct {
	Name    string `json:"name"`
	Level   int    `json:"level"`
	Wine    int    `json:"wine"`
	Enabled bool   `json:"enabled"`
	Blocked bool   `json:"blocked"`
}

type GUIRequest struct {
	CharacterClass                        string            `json:"characterClass"`
	WeaponClass                           string            `json:"weaponClass"`
	SecondaryWeapon                       string            `json:"secondaryWeapon,omitempty"`
	WeaponRarity                          string            `json:"weaponRarity"`
	MinRarity                             string            `json:"minRarity"`
	MaxRarity                             string            `json:"maxRarity"`
	Ring                                  string            `json:"ring"`
	Amulet                                string            `json:"amulet"`
	FixedRarities                         map[string]string `json:"fixedRarities,omitempty"`
	StatPriority                          []string          `json:"statPriority,omitempty"`
	Affixes                               []GUIAffix        `json:"affixes"`
	MinimumAffixLevel                     int               `json:"minimumAffixLevel,omitempty"`
	MatchTargetStrictly                   bool              `json:"matchTargetStrictly"`
	DisableItemRarityDifferenceConstraint bool              `json:"disableItemRarityDifferenceConstraint"`
	LowPerformance                        bool              `json:"lowPerformance"`
	StatFirst                             bool              `json:"statFirst,omitempty"`
	StatFirstReferenceCost                int               `json:"statFirstReferenceCost,omitempty"`
	StatFirstCostCeiling                  int               `json:"statFirstCostCeiling,omitempty"` // legacy session field
	SearchShard                           int               `json:"searchShard,omitempty"`
	SearchShards                          int               `json:"searchShards,omitempty"`
	StatFirstCandidateShard               int               `json:"statFirstCandidateShard,omitempty"`
	StatFirstCandidateShards              int               `json:"statFirstCandidateShards,omitempty"`
	StatFirstCandidates                   [][]GUIAffix      `json:"statFirstCandidates,omitempty"`
	StatFirstGenerateOnly                 bool              `json:"statFirstGenerateOnly,omitempty"`
	prepared                              *standardDatabase `json:"-"`
}

type standardDatabase struct {
	equipment []Item
	gems      []Item
	stats     map[string]cachedItemStats
}

type ClassStats struct {
	Attack  *float64 `json:"attack"`
	Defense *float64 `json:"defense"`
	Health  *float64 `json:"health"`
	Stamina *float64 `json:"stamina"`
}

type GUIOptions struct {
	Classes          []string                   `json:"classes"`
	ClassStats       map[string]ClassStats      `json:"classStats"`
	WeaponClasses    map[string][]string        `json:"weaponClasses"`
	Affixes          []string                   `json:"affixes"`
	AffixCategories  map[string]string          `json:"affixCategories"`
	AffixDetails     map[string]GUIAffixDetails `json:"affixDetails"`
	Rarities         []string                   `json:"rarities"`
	AccessoryFilters []string                   `json:"accessoryFilters"`
}

type GUIAffixDetails struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Levels      map[string]string `json:"levels"`
	Stats       []string          `json:"stats,omitempty"`
	IconB64     string            `json:"iconB64,omitempty"`
}

type GUIPiece struct {
	Type          string             `json:"type"`
	Rarity        string             `json:"rarity"`
	Name          string             `json:"name"`
	Attributes    map[string]float64 `json:"attributes,omitempty"`
	NativeAffixes string             `json:"nativeAffixes"`
	NativeID      int                `json:"nativeId,omitempty"`
	Gems          []GUIGem           `json:"gems"`
}

func (piece *GUIPiece) UnmarshalJSON(data []byte) error {
	var value struct {
		Type          string             `json:"type"`
		Rarity        string             `json:"rarity"`
		Name          string             `json:"name"`
		Attributes    map[string]float64 `json:"attributes"`
		NativeAffixes string             `json:"nativeAffixes"`
		NativeID      int                `json:"nativeId"`
		Gems          json.RawMessage    `json:"gems"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	piece.Type, piece.Rarity, piece.Name, piece.Attributes, piece.NativeAffixes, piece.NativeID = value.Type, value.Rarity, value.Name, value.Attributes, value.NativeAffixes, value.NativeID
	if len(value.Gems) == 0 || string(value.Gems) == "null" {
		return nil
	}
	if value.Gems[0] == '[' {
		return json.Unmarshal(value.Gems, &piece.Gems)
	}
	var legacy string
	if err := json.Unmarshal(value.Gems, &legacy); err != nil {
		return err
	}
	for _, text := range strings.Split(strings.TrimSuffix(legacy, ")"), "), ") {
		color, description, ok := strings.Cut(text, " (")
		if !ok {
			continue
		}
		if description == "empty" {
			piece.Gems = append(piece.Gems, GUIGem{Color: color, Tier: 1})
			continue
		}
		name, affixes := description, ""
		if separator := strings.LastIndex(description, " - "); separator >= 0 {
			name, affixes = description[:separator], strings.ReplaceAll(description[separator+3:], "/", " / ")
		}
		tier := 1
		if affixes != "" {
			tier = len(strings.Split(affixes, " / "))
		}
		gemColor := color
		if color == "White" {
			if strings.Contains(strings.ToLower(name), "rhomb") {
				gemColor = "Pink"
			} else {
				gemColor = "Green"
			}
		}
		piece.Gems = append(piece.Gems, GUIGem{Color: color, GemColor: gemColor, Name: name, Affixes: affixes, Tier: tier})
	}
	return nil
}

type GUIGem struct {
	Color    string `json:"color"`
	GemColor string `json:"gemColor"`
	Name     string `json:"name"`
	Affixes  string `json:"affixes"`
	IconB64  string `json:"iconB64,omitempty"`
	Tier     int    `json:"tier"`
	Filled   bool   `json:"filled"`
	NativeID int    `json:"nativeId,omitempty"`
}

type GUIResultAffix struct {
	Name   string `json:"name"`
	Result int    `json:"result"`
	Target int    `json:"target"`
	Wine   int    `json:"wine,omitempty"`
}

type GUISet struct {
	Code             string           `json:"code,omitempty"`
	Affixes          []GUIResultAffix `json:"affixes"`
	Price            string           `json:"price"`
	UnusedGemSlots   int              `json:"unusedGemSlots"`
	UnusedAffixSlots int              `json:"unusedAffixSlots"`
	Pieces           []GUIPiece       `json:"pieces"`
	Legacy           bool             `json:"legacy,omitempty"`
	TargetAffixes    string           `json:"targetAffixes,omitempty"`
	SelectedAffixes  string           `json:"selectedAffixes,omitempty"`
}

func (set *GUISet) UnmarshalJSON(data []byte) error {
	var value struct {
		Affixes          json.RawMessage `json:"affixes"`
		Code             string          `json:"code"`
		Price            string          `json:"price"`
		UnusedGemSlots   int             `json:"unusedGemSlots"`
		UnusedAffixSlots int             `json:"unusedAffixSlots"`
		Pieces           []GUIPiece      `json:"pieces"`
		Legacy           bool            `json:"legacy"`
		TargetAffixes    string          `json:"targetAffixes"`
		SelectedAffixes  string          `json:"selectedAffixes"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	set.Code, set.Price, set.UnusedGemSlots, set.UnusedAffixSlots, set.Pieces = value.Code, value.Price, value.UnusedGemSlots, value.UnusedAffixSlots, value.Pieces
	set.Legacy, set.TargetAffixes, set.SelectedAffixes = value.Legacy, value.TargetAffixes, value.SelectedAffixes
	if len(value.Affixes) == 0 || string(value.Affixes) == "null" {
		return nil
	}
	if value.Affixes[0] == '[' {
		return json.Unmarshal(value.Affixes, &set.Affixes)
	}
	var legacy string
	if err := json.Unmarshal(value.Affixes, &legacy); err != nil {
		return err
	}
	set.Legacy, set.TargetAffixes, set.SelectedAffixes = true, legacy, legacy
	return nil
}

func legacyAffixes(targetText, selectedText string) []GUIResultAffix {
	parse := func(text string) ([]string, map[string]GUIResultAffix) {
		order, values := []string{}, map[string]GUIResultAffix{}
		for _, entry := range strings.Split(text, ", ") {
			separator := strings.LastIndex(entry, " ")
			if separator < 0 {
				continue
			}
			levels := strings.Split(entry[separator+1:], "/")
			level, err := strconv.Atoi(levels[0])
			if err != nil {
				continue
			}
			name := entry[:separator]
			order = append(order, name)
			values[name] = GUIResultAffix{Name: name, Result: level}
		}
		return order, values
	}
	targetOrder, targets := parse(targetText)
	selectedOrder, selected := parse(selectedText)
	result := make([]GUIResultAffix, 0, len(selected))
	seen := map[string]bool{}
	for _, name := range append(targetOrder, selectedOrder...) {
		if seen[name] {
			continue
		}
		seen[name] = true
		value := selected[name]
		value.Name, value.Target = name, targets[name].Result
		result = append(result, value)
	}
	return result
}

func formatGUIAffixes(request []GUIAffix, result *Solution, details map[string]GUIAffixDetails) []GUIResultAffix {
	order, extras := []string{}, []string{}
	names, levels, targets, wines := map[string]string{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, requested := range request {
		if requested.Blocked || requested.Level <= 0 {
			continue
		}
		key := normalize(requested.Name)
		order = append(order, key)
		names[key], levels[key] = requested.Name, 0
		targets[key], wines[key] = requested.Level, requested.Wine
	}
	add := func(affixes []Affix) {
		for _, affix := range affixes {
			key := normalize(affix.Name)
			if _, exists := names[key]; !exists {
				names[key] = affix.Name
				extras = append(extras, key)
			}
			levels[key] += affix.Level
		}
	}
	for _, piece := range result.Pieces {
		if native, ok := piece.NativeAffixes.([]Affix); ok {
			add(native)
		}
		for _, slot := range piece.GemSlots {
			if slot.Gem != nil {
				add(slot.Gem.Affixes)
			}
		}
	}
	sort.Slice(extras, func(i, j int) bool { return strings.ToLower(names[extras[i]]) < strings.ToLower(names[extras[j]]) })
	affixes := make([]GUIResultAffix, 0, len(order)+len(extras))
	for _, key := range append(order, extras...) {
		maximum := len(details[names[key]].Levels)
		affixes = append(affixes, GUIResultAffix{Name: names[key], Result: min(levels[key], maximum), Target: targets[key], Wine: wines[key]})
	}
	return affixes
}

func blockedAffixNames(affixes []GUIAffix) map[string]bool {
	blocked := map[string]bool{}
	for _, affix := range affixes {
		if affix.Blocked && strings.TrimSpace(affix.Name) != "" {
			blocked[normalize(affix.Name)] = true
		}
	}
	return blocked
}

func hasBlockedAffix(affixes []Affix, blocked map[string]bool) bool {
	for _, affix := range affixes {
		if blocked[normalize(affix.Name)] {
			return true
		}
	}
	return false
}

type GUIResult struct {
	Possible               bool                      `json:"possible"`
	Closest                bool                      `json:"closest,omitempty"`
	Message                string                    `json:"message"`
	Sets                   []GUISet                  `json:"sets"`
	Tested                 int                       `json:"tested"`
	Seconds                float64                   `json:"seconds"`
	Distance               int                       `json:"distance,omitempty"`
	Rules                  []string                  `json:"rules"`
	OptimizationRank       *GUIOptimizationRank      `json:"optimizationRank,omitempty"`
	Debug                  *GUIDebug                 `json:"debug,omitempty"`
	StatFirstAlternatives  []GUIStatFirstAlternative `json:"statFirstAlternatives,omitempty"`
	StatFirstCandidateSets [][]GUIAffix              `json:"statFirstCandidateSets,omitempty"`
}

type GUIStatFirstAlternative struct {
	CandidateNumber  int                  `json:"candidateNumber"`
	Possible         bool                 `json:"possible"`
	Closest          bool                 `json:"closest,omitempty"`
	Distance         int                  `json:"distance,omitempty"`
	Sets             []GUISet             `json:"sets,omitempty"`
	OptimizationRank *GUIOptimizationRank `json:"optimizationRank,omitempty"`
}

type GUIOptimizationRank struct {
	RaritySum          int        `json:"raritySum"`
	Stats              [4]float64 `json:"stats"`
	Damage             float64    `json:"damage"`
	DefensePenetration float64    `json:"defensePenetration"`
	AveragePrice       float64    `json:"averagePrice"`
	TierDeficit        int        `json:"tierDeficit"`
	Signature          string     `json:"signature"`
	StatOrder          [4]int     `json:"statOrder"`
}

type GUIProgress struct {
	Mode      string `json:"mode"`
	Stage     string `json:"stage"`
	Current   int    `json:"current"`
	Total     int    `json:"total"`
	Tested    int    `json:"tested"`
	Milestone string `json:"milestone,omitempty"`
}

type GUIDebug struct {
	Candidates []GUIDebugCandidate `json:"candidates,omitempty"`
}

type GUIDebugCandidate struct {
	Number                      int        `json:"number"`
	Affixes                     []GUIAffix `json:"affixes"`
	Status                      string     `json:"status"`
	EstimatedStats              [4]float64 `json:"estimatedStats"`
	EstimatedDamage             float64    `json:"estimatedDamage"`
	EstimatedDefensePenetration float64    `json:"estimatedDefensePenetration"`
	Price                       float64    `json:"price,omitempty"`
	Stats                       [4]float64 `json:"stats,omitempty"`
	Damage                      float64    `json:"damage,omitempty"`
	DefensePenetration          float64    `json:"defensePenetration,omitempty"`
	Score                       *float64   `json:"score,omitempty"`
	EstimatedScore              *float64   `json:"estimatedScore,omitempty"`
	Ranked                      bool       `json:"ranked,omitempty"`
	Frontier                    bool       `json:"frontier,omitempty"`
	Selected                    bool       `json:"selected,omitempty"`
}

type GUISession struct {
	Request   GUIRequest `json:"request"`
	Result    GUIResult  `json:"result"`
	HasResult bool       `json:"hasResult"`
	Help      bool       `json:"help"`
}

func (session *GUISession) UnmarshalJSON(data []byte) error {
	type sessionAlias GUISession
	var value sessionAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var envelope struct {
		Request map[string]json.RawMessage `json:"request"`
		Help    *bool                      `json:"help"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if envelope.Help == nil {
		value.Help = true
	}
	if _, exists := envelope.Request["matchTargetStrictly"]; !exists {
		if raw, legacyExists := envelope.Request["fillGemSlots"]; legacyExists {
			var fillGemSlots bool
			if err := json.Unmarshal(raw, &fillGemSlots); err == nil {
				value.Request.MatchTargetStrictly = !fillGemSlots
			}
		}
	}
	for i := range value.Result.Sets {
		set := &value.Result.Sets[i]
		if len(set.Affixes) == 0 && (set.TargetAffixes != "" || set.SelectedAffixes != "") {
			set.Affixes = legacyAffixes(set.TargetAffixes, set.SelectedAffixes)
			set.Legacy = true
		}
	}
	*session = GUISession(value)
	return nil
}

type GUISavedResult struct {
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

type Engine struct {
	options     GUIOptions
	priceLookup map[int]float64
}

func NewEngine() (*Engine, error) {
	details, err := loadAffixDetails()
	if err != nil {
		return nil, err
	}
	categories, err := loadAffixCategories()
	if err != nil {
		return nil, err
	}
	classStats, err := loadClassStats()
	if err != nil {
		return nil, err
	}
	classes := make([]string, 0, len(classStats))
	for class := range classStats {
		classes = append(classes, class)
	}
	sort.Slice(classes, func(i, j int) bool { return strings.ToLower(classes[i]) < strings.ToLower(classes[j]) })
	weaponClasses, err := loadWeaponClasses()
	if err != nil {
		return nil, err
	}
	priceLookup, err := loadPriceLookup()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(details))
	for name := range details {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return &Engine{options: GUIOptions{
		Classes:          classes,
		ClassStats:       classStats,
		WeaponClasses:    weaponClasses,
		Affixes:          names,
		AffixCategories:  categories,
		AffixDetails:     details,
		Rarities:         []string{"Gray", "White", "Green", "Blue", "Purple", "Gold"},
		AccessoryFilters: []string{"Any", "HP/PHYS", "HP/MAG", "ATK/PHYS", "ATK/MAG"},
	}, priceLookup: priceLookup}, nil
}

func loadAffixDetails() (map[string]GUIAffixDetails, error) {
	var file struct {
		Affixes []GUIAffixDetails `json:"affixes"`
	}
	if err := json.Unmarshal(embeddedAffixes, &file); err != nil {
		return nil, err
	}
	icons, err := loadItemIcons("affix")
	if err != nil {
		return nil, err
	}
	details := make(map[string]GUIAffixDetails, len(file.Affixes))
	for _, affix := range file.Affixes {
		if affix.Name == "" || len(affix.Levels) == 0 {
			return nil, errors.New("affixes.json contains an affix without a name or levels")
		}
		if len(affix.Stats) == 0 {
			affix.Stats = inferredAffixStats(affix)
		}
		affix.IconB64 = icons[affix.Name]
		details[affix.Name] = affix
	}
	return details, nil
}

func effectThresholds(levels map[string]string) []int {
	keys := make([]int, 0, len(levels))
	for key := range levels {
		if level, err := strconv.Atoi(key); err == nil {
			keys = append(keys, level)
		}
	}
	sort.Ints(keys)
	thresholds, previous := []int{}, -1
	for _, level := range keys {
		count := effectCount(levels[strconv.Itoa(level)])
		if previous >= 0 && count > previous {
			thresholds = append(thresholds, level)
		}
		previous = count
	}
	return thresholds
}

func effectCount(text string) int {
	count := 0
	for i := range text {
		if (text[i] == '.' || text[i] == ',') && (i+1 == len(text) || strings.ContainsRune(" \t\r\n", rune(text[i+1]))) {
			count++
		}
	}
	return count
}

func titleCase(value string) string {
	parts := strings.Fields(strings.ReplaceAll(value, "-", " "))
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

func affixNames(equipment, gems []Item) []string {
	names := map[string]string{}
	for _, item := range append(equipment, gems...) {
		for _, affix := range item.Equipment.Affixes {
			if key := normalize(affix.Name); key != "" {
				names[key] = affix.Name
			}
		}
		for _, affix := range item.Gem.Affixes {
			if key := normalize(affix.Name); key != "" {
				names[key] = affix.Name
			}
		}
	}
	values := make([]string, 0, len(names))
	for _, name := range names {
		values = append(values, name)
	}
	sort.Slice(values, func(i, j int) bool { return strings.ToLower(values[i]) < strings.ToLower(values[j]) })
	return values
}

func (engine *Engine) Options() GUIOptions {
	return engine.options
}

func fastTrackMinimum(total int) int {
	switch {
	case total > 32:
		return 0
	case total >= 32:
		return 6
	case total >= 24:
		return 5
	case total >= 16:
		return 4
	case total >= 8:
		return 3
	default:
		return 2
	}
}

func maxEquipmentAffixLevels(rarity int) int {
	if rarity <= 2 {
		return 0
	}
	return (rarity - 2) * 8
}

func maxRarityForFixed(maxRarity int, fixed map[string]int) int {
	for _, rarity := range fixed {
		maxRarity = min(maxRarity, rarity+1)
	}
	return maxRarity
}

func maxEquipmentAffixLevelsForFixed(maxRarity int, fixed map[string]int, disableRarityDifferenceConstraint bool) int {
	if !disableRarityDifferenceConstraint {
		maxRarity = maxRarityForFixed(maxRarity, fixed)
	}
	total := 0
	for _, slot := range slotOrder {
		rarity := fixed[slot]
		if rarity == 0 {
			rarity = maxRarity
		}
		total += maxEquipmentAffixLevels(rarity) / 8
	}
	return total
}

func rarityConfiguration(request GUIRequest, minLevel, maxLevel int) (map[string]int, error) {
	fixed := map[string]int{}
	rangeErrorSlot := ""
	validSlots := map[string]bool{}
	for _, slot := range slotOrder {
		validSlots[slot] = true
	}
	for slot, value := range request.FixedRarities {
		if value == "" || strings.EqualFold(value, "Any") {
			continue
		}
		if !validSlots[slot] {
			return nil, fmt.Errorf("unknown equipment slot %q", slot)
		}
		level, err := rarity(value)
		if err != nil {
			return nil, err
		}
		if level < minLevel || level > maxLevel {
			rangeErrorSlot = slot
		}
		fixed[slot] = level
	}
	if request.WeaponRarity != "" && !strings.EqualFold(request.WeaponRarity, "Any") {
		level, err := rarity(request.WeaponRarity)
		if err != nil {
			return nil, err
		}
		if level < minLevel || level > maxLevel {
			rangeErrorSlot = "weapon"
		}
		if existing, ok := fixed["weapon"]; ok && existing != level {
			return nil, errors.New("weapon rarity is configured twice")
		}
		fixed["weapon"] = level
	}
	if len(fixed) > 1 && !request.DisableItemRarityDifferenceConstraint {
		lowest, highest := 0, 0
		lowestSlot, highestSlot := "", ""
		for _, slot := range slotOrder {
			level, ok := fixed[slot]
			if !ok {
				continue
			}
			if lowest == 0 || level < lowest {
				lowest = level
				lowestSlot = slot
			}
			if level > highest {
				highest = level
				highestSlot = slot
			}
		}
		if highest-lowest > 1 {
			return nil, fmt.Errorf(`Hard constraints violated: The difference between "%s" and "%s" is higher than 1 tier`, titleCase(lowestSlot), titleCase(highestSlot))
		}
	}
	if rangeErrorSlot != "" {
		return nil, fmt.Errorf(`Hard constraints violated: "%s" rarity is outside the selected rarity range`, titleCase(rangeErrorSlot))
	}
	return fixed, nil
}

func statPriorityConfiguration(request GUIRequest) ([4]int, []string, error) {
	defaultOrder := [4]int{0, 1, 2, 3}
	labels := []string{"Weapon Damage", "Attack", "Defense", "Health"}
	if len(request.StatPriority) == 0 {
		return defaultOrder, labels, nil
	}
	if len(request.StatPriority) != len(defaultOrder) {
		return defaultOrder, nil, errors.New("stat priority must contain Weapon Damage, Attack, Defense, and Health exactly once")
	}
	indexes := map[string]int{"weapon damage": 0, "weapondamage": 0, "damage": 0, "attack": 1, "defense": 2, "defence": 2, "health": 3}
	order := [4]int{}
	seen := map[int]bool{}
	for i, value := range request.StatPriority {
		index, ok := indexes[strings.ToLower(strings.TrimSpace(value))]
		if !ok || seen[index] {
			return defaultOrder, nil, errors.New("stat priority must contain Weapon Damage, Attack, Defense, and Health exactly once")
		}
		order[i] = index
		seen[index] = true
	}
	orderedLabels := make([]string, len(order))
	for i, index := range order {
		orderedLabels[i] = labels[index]
	}
	return order, orderedLabels, nil
}

func (engine *Engine) Execute(request GUIRequest, reports ...func(GUIProgress)) (GUIResult, error) {
	request, err := engine.normalizeSecondaryWeapon(request)
	if err != nil {
		return GUIResult{}, err
	}
	if request.StatFirst {
		return engine.executeStatFirst(request, reports...)
	}
	return engine.executeStandard(request, reports...)
}

func (engine *Engine) normalizeSecondaryWeapon(request GUIRequest) (GUIRequest, error) {
	mode := request.SecondaryWeapon
	if mode == "" {
		mode = secondaryWeaponNone
	}
	if len(engine.options.WeaponClasses[request.CharacterClass]) <= 1 {
		mode = secondaryWeaponNone
	}
	if mode != secondaryWeaponNone && mode != secondaryWeaponWhite && mode != secondaryWeaponMatched {
		return GUIRequest{}, fmt.Errorf("invalid secondary weapon option %q", request.SecondaryWeapon)
	}
	request.SecondaryWeapon = mode
	return request, nil
}

func (engine *Engine) prepareStandardDatabase(request GUIRequest, blocked map[string]bool) (*standardDatabase, error) {
	equipment, gems, err := loadDatabase(request.CharacterClass)
	if err != nil {
		return nil, err
	}
	equipment = slices.DeleteFunc(equipment, func(item Item) bool {
		return hasBlockedAffix(item.Equipment.Affixes, blocked)
	})
	gems = slices.DeleteFunc(gems, func(item Item) bool {
		return hasBlockedAffix(item.Gem.Affixes, blocked)
	})
	weaponFound := false
	equipment = slices.DeleteFunc(equipment, func(item Item) bool {
		if item.MainCategory != "weapon" {
			return false
		}
		class := weaponClass(item)
		weaponFound = weaponFound || class == request.WeaponClass
		return class != request.WeaponClass
	})
	if !weaponFound {
		return nil, fmt.Errorf("weapon class %q is not available to %s", request.WeaponClass, request.CharacterClass)
	}
	ringFilter, amuletFilter := request.Ring, request.Amulet
	if ringFilter == "Any" {
		ringFilter = ""
	}
	if amuletFilter == "Any" {
		amuletFilter = ""
	}
	equipment, err = filterEquipment(equipment, ringFilter, amuletFilter)
	if err != nil {
		return nil, err
	}
	if request.SearchShards > 1 {
		equipment = slices.DeleteFunc(equipment, func(item Item) bool {
			return item.MainCategory == "weapon" && searchShardForItem(item.ID, request.SearchShards) != request.SearchShard
		})
	}
	return &standardDatabase{equipment: equipment, gems: gems}, nil
}

func (engine *Engine) executeStandard(request GUIRequest, reports ...func(GUIProgress)) (GUIResult, error) {
	var report func(GUIProgress)
	if len(reports) > 0 {
		report = reports[0]
	}
	if request.SearchShard < 0 || request.SearchShards < 0 || (request.SearchShards > 0 && request.SearchShard >= request.SearchShards) {
		return GUIResult{}, errors.New("invalid search shard")
	}
	started := time.Now()
	minLevel, err := rarity(request.MinRarity)
	if err != nil {
		return GUIResult{}, err
	}
	maxLevel, err := rarity(request.MaxRarity)
	if err != nil {
		return GUIResult{}, err
	}
	if minLevel > maxLevel {
		return GUIResult{}, fmt.Errorf("min-rarity cannot exceed max-rarity")
	}
	configuredMinLevel := minLevel
	if request.MinimumAffixLevel < 0 || request.MinimumAffixLevel > maxEquipmentAffixLevels(maxLevel) {
		return GUIResult{}, fmt.Errorf("minimum-affix-level must be between 0 and %d for the selected maximum rarity", maxEquipmentAffixLevels(maxLevel))
	}
	blocked := blockedAffixNames(request.Affixes)
	equipmentTotal := 0
	for _, affix := range request.Affixes {
		if affix.Blocked {
			continue
		}
		equipmentTotal += affix.Level
	}
	minLevel = max(minLevel, fastTrackMinimum(max(equipmentTotal, request.MinimumAffixLevel)))
	if minLevel > maxLevel {
		return GUIResult{}, fmt.Errorf("min-rarity cannot exceed max-rarity")
	}
	fixedRarities, err := rarityConfiguration(request, configuredMinLevel, maxLevel)
	if err != nil {
		return GUIResult{}, err
	}
	if !request.DisableItemRarityDifferenceConstraint {
		maxLevel = maxRarityForFixed(maxLevel, fixedRarities)
	}
	for _, fixed := range fixedRarities {
		minLevel = min(minLevel, fixed)
	}
	statPriority, statPriorityLabels, err := statPriorityConfiguration(request)
	if err != nil {
		return GUIResult{}, err
	}
	values := make([]string, 0, len(request.Affixes))
	wineTotal := 0
	for _, affix := range request.Affixes {
		if affix.Blocked {
			continue
		}
		if affix.Wine < 0 || affix.Wine > 2 {
			return GUIResult{}, fmt.Errorf("%s: wine level must be between 0 and 2", affix.Name)
		}
		wineTotal += affix.Wine
		maxLevel := len(engine.options.AffixDetails[affix.Name].Levels)
		if affix.Level < 0 || affix.Level > maxLevel {
			return GUIResult{}, fmt.Errorf("%s: level must be between 0 and %d", affix.Name, maxLevel)
		}
		if affix.Level > 0 {
			values = append(values, fmt.Sprintf("%s=%d", affix.Name, affix.Level))
		}
	}
	if wineTotal > 8 {
		return GUIResult{}, errors.New("total wine levels cannot exceed 8")
	}
	requirements, err := parseRequirements(values)
	if err != nil {
		return GUIResult{}, err
	}
	for i := range requirements {
		details := engine.options.AffixDetails[requirements[i].Name]
		requirements[i].Max = len(details.Levels)
		requirements[i].Thresholds = effectThresholds(details.Levels)
	}
	requirements = appendMinimumAffixRequirement(requirements, request.MinimumAffixLevel)
	targets := map[string]bool{}
	for _, requirement := range requirements {
		targets[requirement.Key] = true
	}
	data := request.prepared
	if data == nil {
		data, err = engine.prepareStandardDatabase(request, blocked)
		if err != nil {
			return GUIResult{}, err
		}
	}
	equipment, gems := data.equipment, data.gems
	equipment = filterWeaponOnlyAffixes(equipment, request.WeaponClass, targets, false)
	optimizationGems := filterWeaponOnlyAffixes(gems, request.WeaponClass, targets, true)
	var progressReport func(string, string, int, int, int)
	var milestone func(string)
	if report != nil {
		progressReport = func(mode, stage string, current, total, tested int) {
			report(GUIProgress{Mode: mode, Stage: titleCase(stage), Current: current, Total: total, Tested: tested})
		}
		milestone = func(message string) { report(GUIProgress{Milestone: message}) }
	}
	progress := &optimizationProgress{report: progressReport, milestone: milestone, restrictGems: request.MatchTargetStrictly, disableRarityDifferenceConstraint: request.DisableItemRarityDifferenceConstraint}
	result, err := optimizeConfiguredWithStatsCache(equipment, optimizationGems, requirements, minLevel, maxLevel, progress, !request.LowPerformance, fixedRarities, rarityUpgradeOrder, 0, engine.options.ClassStats[request.CharacterClass], engine.options.AffixDetails, data.stats, statPriority)
	if err != nil {
		return GUIResult{}, err
	}
	if result.Possible && !result.Closest {
		progress.note("Found equipment that matches target affixes.")
		for _, requirement := range requirements {
			if isInternalRequirement(requirement) {
				continue
			}
			progress.note(fmt.Sprintf(`Target affix satisfied: "%s".`, requirement.Name))
		}
	}
	targetRules, maximumRules := []string{}, []string{}
	for _, affix := range request.Affixes {
		if affix.Level > 0 {
			targetRules = append(targetRules, fmt.Sprintf("%s ≥ %d", affix.Name, affix.Level))
			maximumRules = append(maximumRules, fmt.Sprintf("%s ≤ %d", affix.Name, len(engine.options.AffixDetails[affix.Name].Levels)))
		}
	}
	if len(targetRules) == 0 {
		targetRules = append(targetRules, "none")
		maximumRules = append(maximumRules, "none")
	}
	damageLabel := "Physical"
	if weaponDamageType(request.WeaponClass) == 1 {
		damageLabel = "Magic"
	}
	rarityRule := "Highest and lowest equipment rarities may differ by at most one rarity level"
	if request.DisableItemRarityDifferenceConstraint {
		rarityRule = "Item rarity difference constraint: disabled"
	}
	rules := []string{"Targets: " + strings.Join(targetRules, ", "), "Maximums: " + strings.Join(maximumRules, ", "), fmt.Sprintf("Rarity: %s–%s", rarityColors[minLevel], request.MaxRarity), rarityRule, "Wine reference: 0–2 per affix, 8 total; ignored by optimization", "Gem color/type is a hard constraint", "Tier-2 sockets accept tier-1 and tier-2 gems, with tier 2 preferred", "Weapon-only affixes: non-target Burst, Ranged, Bulwark, and Strife stay on allowed weapons; targets and fill gems are exceptions", "Class: " + request.CharacterClass, "Weapon: " + request.WeaponClass, "Attack priority: Attack + " + damageLabel + " Damage"}
	if request.WeaponRarity != "" && !strings.EqualFold(request.WeaponRarity, "Any") {
		rules = append(rules, "Weapon rarity: "+request.WeaponRarity)
	}
	if request.MinimumAffixLevel > 0 {
		rules = append(rules, fmt.Sprintf("Minimum equipment affix levels: %d", request.MinimumAffixLevel))
	}
	if request.Ring != "" && !strings.EqualFold(request.Ring, "Any/Any") {
		rules = append(rules, "Ring: "+request.Ring)
	}
	if request.Amulet != "" && !strings.EqualFold(request.Amulet, "Any/Any") {
		rules = append(rules, "Amulet: "+request.Amulet)
	}
	for _, slot := range slotOrder {
		if level, ok := fixedRarities[slot]; ok {
			rules = append(rules, fmt.Sprintf("Fixed rarity: %s = %s", titleCase(slot), rarityColors[level]))
		}
	}
	rules = append(rules,
		"Stat priority: "+strings.Join(statPriorityLabels, ", "),
		"Optimization: minimize the total rarity levels of all equipment while satisfying targets",
		"Optimization: among equal-rarity combinations, maximize stats in the selected priority order",
	)
	if fixedRarities["weapon"] == 0 && !request.DisableItemRarityDifferenceConstraint {
		rules = append(rules, "Weapon rarity constraint: no other equipment may have a higher rarity than the Weapon")
	}
	if request.LowPerformance {
		rules = append(rules, "Optimization mode: sequential (Low Performance)")
	} else {
		rules = append(rules, "Optimization mode: exhaustive dynamic programming (bounded by affix coverage)")
	}
	if !request.MatchTargetStrictly {
		rules = append(rules,
			"Match Target Strictly: disabled; fill compatible empty sockets and allow target overshoot",
			"Match Target Strictly: globally assign empty sockets to reach thresholds, then raise selected affixes to their maximums before adding bonus affixes",
			"Match Target Strictly fallback: use a compatible gem without changing socket color/type",
		)
	} else {
		rules = append(rules, "Match Target Strictly: exact target levels; leave unused sockets empty")
	}
	if result.Closest {
		rules = append(rules, "Fallback: minimize target shortfall when no exact combination is available")
	}
	response := GUIResult{Possible: result.Possible, Closest: result.Closest, Distance: result.Distance, Tested: progress.tested, Seconds: time.Since(started).Seconds(), Rules: rules}
	if result.Possible {
		response.OptimizationRank = &GUIOptimizationRank{RaritySum: result.raritySum, Stats: result.quality.Stats, Damage: result.quality.Damage, AveragePrice: result.AveragePrice, TierDeficit: result.quality.Cost.TierDeficit, Signature: result.quality.Signature, StatOrder: result.quality.StatOrder}
	}
	if !result.Possible {
		response.Message = "Not possible: " + result.Message
		return response, nil
	}
	if !request.MatchTargetStrictly {
		progress.note("Starting gem assignment for remaining slots.")
		if fillErr := fillGemSlotsWithStats(result, optimizationGems, requirements, statPriority, newStatModel(engine.options.ClassStats[request.CharacterClass], engine.options.AffixDetails, weaponDamageType(request.WeaponClass))); fillErr != nil {
			response.Possible = false
			response.Seconds = time.Since(started).Seconds()
			response.Message = "Not possible: " + fillErr.Error()
			return response, nil
		}
		progress.note("Gem assignment complete.")
	}
	if response.OptimizationRank != nil {
		response.OptimizationRank.AveragePrice = result.AveragePrice
	}
	pieceRows := make([]GUIPiece, 0, len(result.Pieces))
	gemsByID := map[string]Item{}
	for _, gem := range gems {
		gemsByID[gem.ID] = gem
	}
	equipmentByID := map[string]Item{}
	for _, item := range equipment {
		equipmentByID[item.ID] = item
	}
	if response.OptimizationRank != nil {
		model := newStatModel(engine.options.ClassStats[request.CharacterClass], engine.options.AffixDetails, weaponDamageType(request.WeaponClass))
		stats, damage := solutionStats(result, equipmentByID, gemsByID, model)
		response.OptimizationRank.Stats = stats
		response.OptimizationRank.Damage = damage
		response.OptimizationRank.DefensePenetration = solutionDefensePenetration(result, equipmentByID, gemsByID, model)
	}
	classID, err := nativeClassID(request.CharacterClass)
	if err != nil {
		return GUIResult{}, err
	}
	for _, piece := range result.Pieces {
		item, ok := equipmentByID[piece.ItemID]
		if !ok {
			return GUIResult{}, fmt.Errorf("optimized item %q was not found", piece.ItemID)
		}
		nativeID, _ := databaseEquipmentID(classID, item)
		native := "-"
		attributes := map[string]float64{}
		for key, value := range item.Attributes {
			if number, ok := value.(float64); ok {
				attributes[key] = number
			}
		}
		if affixes, ok := piece.NativeAffixes.([]Affix); ok {
			names := make([]string, len(affixes))
			for i, affix := range affixes {
				names[i] = affix.Name
			}
			native = strings.Join(names, ", ")
		}
		gems := make([]GUIGem, len(piece.GemSlots))
		for i, slot := range piece.GemSlots {
			gems[i] = GUIGem{Color: gemColors[slot.Type], Tier: slot.Tier, Filled: slot.Filled}
			if slot.Gem != nil {
				gem := gemsByID[slot.Gem.ID]
				gems[i].IconB64 = gem.IconB64
				if nativeID, err := databaseGemID(gem); err == nil {
					gems[i].NativeID = nativeID
				}
				gems[i].GemColor = gemActualColors[gemType(gem)]
				names := make([]string, len(slot.Gem.Affixes))
				for j, affix := range slot.Gem.Affixes {
					names[j] = affix.Name
				}
				gems[i].Name, gems[i].Affixes = formatGemName(*slot.Gem), strings.Join(names, " / ")
			}
		}
		primary := GUIPiece{Type: titleCase(piece.Slot), Rarity: rarityColors[piece.Grade], Name: piece.Name, Attributes: attributes, NativeAffixes: native, NativeID: nativeID, Gems: gems}
		pieceRows = append(pieceRows, primary)
		if piece.Slot == "weapon" && request.SecondaryWeapon != secondaryWeaponNone {
			secondary, ok := secondaryWeapon(classID, primary, request.SecondaryWeapon, data.gems)
			if !ok {
				return GUIResult{}, fmt.Errorf("secondary weapon %q is unavailable", request.SecondaryWeapon)
			}
			pieceRows = append(pieceRows, secondary)
		}
	}
	response.Possible = true
	response.Seconds = time.Since(started).Seconds()
	unusedGems, unusedAffixes := 0, 0
	if request.MatchTargetStrictly {
		unusedGems, unusedAffixes = unusedGemSlots(result), unusedAffixSlots(result)
	}
	set := GUISet{Affixes: formatGUIAffixes(request.Affixes, result, engine.options.AffixDetails), Price: formatNumber(result.AveragePrice), UnusedGemSlots: unusedGems, UnusedAffixSlots: unusedAffixes, Pieces: pieceRows}
	if code, exportErr := ExportCode(request.CharacterClass, set); exportErr == nil {
		set.Code = code
	}
	response.Sets = []GUISet{set}
	return response, nil
}
