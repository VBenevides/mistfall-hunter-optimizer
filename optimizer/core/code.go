package core

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

const nativeCodeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var nativeClassIDs = map[string]int{
	"mercenary":       10,
	"sorcerer":        11,
	"blackarrow":      12,
	"shadowstrix":     13,
	"seer":            14,
	"withered-knight": 15,
}

var nativeClassNames = map[int]string{
	10: "mercenary", 11: "sorcerer", 12: "blackarrow",
	13: "shadowstrix", 14: "seer", 15: "withered-knight",
}

var nativeCodeSlots = []int{0, 1, 2, 3, 4, 5, 6, 10, 11}

var nativeSlotIDs = map[string]int{
	"head": 0, "helmet": 0, "chest": 1, "clothes": 1,
	"hands": 2, "gauntlets": 2, "gloves": 2, "legs": 3,
	"pants": 3, "feet": 4, "boots": 4, "necklace": 5,
	"amulet": 5, "ring": 6, "weapon": 10, "primary": 10,
	"secondary": 11,
}

var nativeDisplaySlots = map[string]string{
	"head": "Helmet", "chest": "Clothes", "gloves": "Gauntlets", "pants": "Pants",
	"shoes": "Boots", "necklace": "Necklace", "ring": "Ring", "primary": "Weapon",
	"secondary": "Secondary",
}

const (
	secondaryWeaponNone    = "None"
	secondaryWeaponWhite   = "White"
	secondaryWeaponMatched = "Match primary"
)

type nativeEquipmentConfig struct {
	ClassID int
	Slot    string
	ID      int
	Name    string
	Rarity  string
	Affix   string
	Holes   []int
}

type nativeGemConfig struct {
	ID   int
	Name string
}

type nativeCodeTables struct {
	Head                 int
	Version              int
	EquipmentByClassSlot map[string]map[string][]int
	GemIDs               []int
	Equipment            []nativeEquipmentConfig
	Gems                 []nativeGemConfig
}

var nativeTables = loadNativeCodeTables()

func loadNativeCodeTables() nativeCodeTables {
	compressed, err := base64.StdEncoding.DecodeString(nativeCodeDataBase64)
	if err != nil {
		panic(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		panic(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		panic(err)
	}
	if err := reader.Close(); err != nil {
		panic(err)
	}
	var raw struct {
		Head                 int                         `json:"head"`
		Version              int                         `json:"version"`
		EquipmentByClassSlot map[string]map[string][]int `json:"equipmentByClassAndSlot"`
		GemIDs               []int                       `json:"gemIds"`
		Equipment            [][]json.RawMessage         `json:"equipment"`
		Gems                 [][]json.RawMessage         `json:"gems"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		panic(err)
	}
	tables := nativeCodeTables{Head: raw.Head, Version: raw.Version, EquipmentByClassSlot: raw.EquipmentByClassSlot, GemIDs: raw.GemIDs}
	for _, row := range raw.Equipment {
		var item nativeEquipmentConfig
		if len(row) != 7 || json.Unmarshal(row[0], &item.ClassID) != nil || json.Unmarshal(row[1], &item.Slot) != nil || json.Unmarshal(row[2], &item.ID) != nil || json.Unmarshal(row[3], &item.Name) != nil || json.Unmarshal(row[4], &item.Rarity) != nil || json.Unmarshal(row[5], &item.Affix) != nil || json.Unmarshal(row[6], &item.Holes) != nil {
			panic("invalid native equipment table")
		}
		tables.Equipment = append(tables.Equipment, item)
	}
	for _, row := range raw.Gems {
		var gem nativeGemConfig
		if len(row) != 2 || json.Unmarshal(row[0], &gem.ID) != nil || json.Unmarshal(row[1], &gem.Name) != nil {
			panic("invalid native gem table")
		}
		tables.Gems = append(tables.Gems, gem)
	}
	return tables
}

func nativeClassID(value string) (int, error) {
	class := classSlug(value)
	if id, ok := nativeClassIDs[class]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("unsupported class: %s", value)
}

func nativeKey(value string) string {
	value = strings.ReplaceAll(strings.ToLower(value), "helmet", "helm")
	return strings.Join(strings.Fields(nonAlnum.ReplaceAllString(value, " ")), " ")
}

func databaseEquipmentID(classID int, item Item) (int, error) {
	id := item.NativeID
	if id == 0 {
		var err error
		id, err = strconv.Atoi(item.ID)
		if err != nil {
			return 0, fmt.Errorf("invalid database equipment ID %q", item.ID)
		}
	}
	config, ok := nativeEquipment(classID, id)
	if !ok {
		return 0, fmt.Errorf("database equipment ID %d is not valid for class %d", id, classID)
	}
	return config.ID, nil
}

func databaseGemID(item Item) (int, error) {
	if item.NativeID != 0 {
		if _, ok := nativeGem(item.NativeID); ok {
			return item.NativeID, nil
		}
	}
	return nativeGemID(item.Name)
}

func validDatabaseEquipment(item Item) bool {
	id := item.NativeID
	if id == 0 {
		var err error
		id, err = strconv.Atoi(item.ID)
		if err != nil {
			return item.SiteID == 0
		}
	}
	for _, config := range nativeTables.Equipment {
		if config.ID == id {
			return true
		}
	}
	return false
}

func validDatabaseGem(item Item) bool {
	_, err := databaseGemID(item)
	return err == nil
}

func nativeGemID(name string) (int, error) {
	wanted := nativeKey(name)
	for _, gem := range nativeTables.Gems {
		if nativeKey(gem.Name) == wanted {
			return gem.ID, nil
		}
	}
	return 0, fmt.Errorf("no native gem match for %s", name)
}

func nativeEquipment(classID, id int) (nativeEquipmentConfig, bool) {
	for _, item := range nativeTables.Equipment {
		if item.ClassID == classID && item.ID == id {
			return item, true
		}
	}
	return nativeEquipmentConfig{}, false
}

func secondaryWeapon(classID int, primary GUIPiece, mode string) (GUIPiece, bool) {
	primaryConfig, ok := nativeEquipment(classID, primary.NativeID)
	if !ok {
		return GUIPiece{}, false
	}
	weaponType := nativeWeaponType(primaryConfig)
	if mode == secondaryWeaponMatched {
		variant := primary.NativeID % 100
		matches := func(config nativeEquipmentConfig) bool {
			return config.ClassID == classID && config.Slot == "primary" && config.Rarity == primaryConfig.Rarity && nativeWeaponType(config) != weaponType
		}
		for _, config := range nativeTables.Equipment {
			if matches(config) && config.ID%100 == variant {
				secondary := primary
				secondary.Type, secondary.Name, secondary.NativeID = "Secondary", config.Name, config.ID
				return secondary, true
			}
		}
		for _, config := range nativeTables.Equipment {
			if matches(config) {
				secondary := primary
				secondary.Type, secondary.Name, secondary.NativeID = "Secondary", config.Name, config.ID
				return secondary, true
			}
		}
		return GUIPiece{}, false
	}
	if mode != secondaryWeaponWhite {
		return GUIPiece{}, false
	}
	for _, config := range nativeTables.Equipment {
		if config.ClassID == classID && config.Slot == "primary" && config.Rarity == "Common" && config.Affix == "" && nativeWeaponType(config) != weaponType {
			return GUIPiece{Type: "Secondary", Rarity: "White", Name: config.Name, NativeAffixes: "-", NativeID: config.ID}, true
		}
	}
	return GUIPiece{}, false
}

func nativeWeaponType(config nativeEquipmentConfig) string {
	if weaponType := nativeWeaponTypesByID[config.ID]; weaponType != "" {
		return weaponType
	}
	name := strings.ToLower(strings.TrimSpace(config.Name))
	for _, weaponType := range []string{
		"sword and shield", "polearm and shield", "dual blades", "greatsword", "javelin",
		"catalyst", "hammer", "dagger", "staff", "bow", "mace",
	} {
		if strings.HasSuffix(name, weaponType) {
			return canonicalWeaponClass(weaponType)
		}
	}
	return strconv.Itoa(config.ID / 100 % 100)
}

func nativeGem(id int) (nativeGemConfig, bool) {
	for _, gem := range nativeTables.Gems {
		if gem.ID == id {
			return gem, true
		}
	}
	return nativeGemConfig{}, false
}

func codeSlot(piece GUIPiece) (int, bool) {
	id, ok := nativeSlotIDs[strings.ToLower(strings.TrimSpace(piece.Type))]
	return id, ok
}

func writeCodeBits(bits *[]bool, width, value int) error {
	if value < 0 || value >= 1<<width {
		return fmt.Errorf("value does not fit in %d bits: %d", width, value)
	}
	start := len(*bits)
	*bits = append(*bits, make([]bool, width)...)
	for bit := 0; bit < width; bit++ {
		if value&(1<<bit) != 0 {
			(*bits)[start+bit] = true
		}
	}
	return nil
}

func base62Encode(payload []byte) string {
	value := new(big.Int).SetBytes(payload)
	if value.Sign() == 0 {
		return "0"
	}
	base := big.NewInt(62)
	result := make([]byte, 0)
	for value.Sign() > 0 {
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(value, base, remainder)
		result = append(result, nativeCodeAlphabet[remainder.Int64()])
		value = quotient
	}
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return string(result)
}

func base62Decode(code string) ([]byte, error) {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 256 {
		return nil, fmt.Errorf("invalid build code")
	}
	value, base := new(big.Int), big.NewInt(62)
	for _, character := range code {
		digit := strings.IndexRune(nativeCodeAlphabet, character)
		if digit < 0 {
			return nil, fmt.Errorf("invalid build-code character: %q", character)
		}
		value.Mul(value, base)
		value.Add(value, big.NewInt(int64(digit)))
	}
	payload := value.Bytes()
	if len(payload) == 0 {
		payload = []byte{0}
	}
	if base62Encode(payload) != code {
		return nil, fmt.Errorf("non-canonical build code")
	}
	return payload, nil
}

func readCodeBits(payload []byte, position, width int) (int, int, error) {
	if position+width > len(payload)*8 {
		return 0, position, fmt.Errorf("truncated build code")
	}
	value := 0
	for bit := 0; bit < width; bit++ {
		value |= int((payload[(position+bit)/8]>>((position+bit)%8))&1) << bit
	}
	return value, position + width, nil
}

func encodeNativeBuild(classID int, pieces []GUIPiece) (string, error) {
	selected := map[int]GUIPiece{}
	for _, piece := range pieces {
		slot, ok := codeSlot(piece)
		if !ok {
			return "", fmt.Errorf("unsupported equipment slot: %s", piece.Type)
		}
		if _, exists := selected[slot]; exists {
			return "", fmt.Errorf("duplicate equipment slot: %s", piece.Type)
		}
		selected[slot] = piece
	}
	bits := []bool{}
	for _, value := range []int{nativeTables.Head, nativeTables.Version, classID} {
		width := 24
		if value != nativeTables.Head {
			width = 10
		}
		if value == classID {
			width = 4
		}
		if err := writeCodeBits(&bits, width, value); err != nil {
			return "", err
		}
	}
	configs := nativeTables.EquipmentByClassSlot[strconv.Itoa(classID)]
	for _, slot := range nativeCodeSlots {
		piece, exists := selected[slot]
		if !exists {
			if err := writeCodeBits(&bits, 10, 0); err != nil {
				return "", err
			}
			continue
		}
		choices := configs[strconv.Itoa(slot)]
		index := slices.Index(choices, piece.NativeID)
		if index < 0 {
			return "", fmt.Errorf("equipment is not valid for class %d and slot %d: %d", classID, slot, piece.NativeID)
		}
		if err := writeCodeBits(&bits, 10, index); err != nil {
			return "", err
		}
		config, ok := nativeEquipment(classID, piece.NativeID)
		if !ok {
			return "", fmt.Errorf("unknown native equipment config: %d", piece.NativeID)
		}
		if len(piece.Gems) > len(config.Holes) {
			return "", fmt.Errorf("too many gems for equipment %d", piece.NativeID)
		}
		for socket := range config.Holes {
			gemID := 0
			if socket < len(piece.Gems) {
				gemID = piece.Gems[socket].NativeID
			}
			index := slices.Index(nativeTables.GemIDs, gemID)
			if index < 0 {
				return "", fmt.Errorf("gem is not valid in the native table: %d", gemID)
			}
			if err := writeCodeBits(&bits, 10, index); err != nil {
				return "", err
			}
		}
	}
	payload := make([]byte, (len(bits)+7)/8)
	for index, bit := range bits {
		if bit {
			payload[index/8] |= 1 << (index % 8)
		}
	}
	return base62Encode(payload), nil
}

func ExportCode(characterClass string, set GUISet) (string, error) {
	classID, err := nativeClassID(characterClass)
	if err != nil {
		return "", err
	}
	return encodeNativeBuild(classID, set.Pieces)
}

func findDatabaseEquipment(classID int, config nativeEquipmentConfig, equipment []Item) Item {
	for _, item := range equipment {
		if id, err := databaseEquipmentID(classID, item); err == nil && id == config.ID {
			return item
		}
	}
	return Item{}
}

func findDatabaseGem(config nativeGemConfig, gems []Item) Item {
	for _, gem := range gems {
		if nativeKey(gem.Name) == nativeKey(config.Name) {
			return gem
		}
	}
	return Item{}
}

func guiItemAttributes(item Item) map[string]float64 {
	attributes := map[string]float64{}
	for key, value := range item.Attributes {
		switch number := value.(type) {
		case float64:
			attributes[key] = number
		case float32:
			attributes[key] = float64(number)
		case int:
			attributes[key] = float64(number)
		case int64:
			attributes[key] = float64(number)
		}
	}
	return attributes
}

func DecodeCode(code string) (GUISession, error) {
	payload, err := base62Decode(code)
	if err != nil {
		return GUISession{}, err
	}
	position := 0
	read := func(width int) (int, error) {
		value, next, err := readCodeBits(payload, position, width)
		position = next
		return value, err
	}
	header, err := read(24)
	if err != nil || header != nativeTables.Head {
		return GUISession{}, fmt.Errorf("invalid build-code header")
	}
	version, err := read(10)
	if err != nil || version != nativeTables.Version {
		return GUISession{}, fmt.Errorf("unsupported build-code version: %d", version)
	}
	classID, err := read(4)
	className, ok := nativeClassNames[classID]
	if err != nil || !ok {
		return GUISession{}, fmt.Errorf("unsupported class ID: %d", classID)
	}
	equipment, gems, err := loadDatabase(className)
	if err != nil {
		return GUISession{}, err
	}
	configs := nativeTables.EquipmentByClassSlot[strconv.Itoa(classID)]
	set := GUISet{Code: strings.TrimSpace(code), Pieces: []GUIPiece{}}
	affixOrder := []string{}
	affixLevels := map[string]int{}
	affixNames := map[string]string{}
	addAffixes := func(affixes []Affix) {
		for _, affix := range affixes {
			key := normalize(affix.Name)
			if _, exists := affixLevels[key]; !exists {
				affixOrder = append(affixOrder, key)
				affixNames[key] = affix.Name
			}
			affixLevels[key] += affix.Level
		}
	}
	request := GUIRequest{CharacterClass: titleCase(className), MatchTargetStrictly: true}
	var primaryConfig nativeEquipmentConfig
	hasPrimary := false
	primaryNativeAffixes := ""
	price := 0.0
	for _, slot := range nativeCodeSlots {
		index, err := read(10)
		if err != nil {
			return GUISession{}, err
		}
		choices := configs[strconv.Itoa(slot)]
		if index >= len(choices) {
			return GUISession{}, fmt.Errorf("invalid equipment index %d for slot %d", index, slot)
		}
		configID := choices[index]
		if configID == 0 {
			continue
		}
		config, ok := nativeEquipment(classID, configID)
		if !ok {
			return GUISession{}, fmt.Errorf("unknown native equipment config: %d", configID)
		}
		item := findDatabaseEquipment(classID, config, equipment)
		name, itemClass := config.Name, ""
		nativeAffixes := config.Affix
		if item.ID != "" {
			price += item.RecommendedPrice
			name, itemClass = item.Name, item.SubName
			addAffixes(item.Equipment.Affixes)
			names := make([]string, len(item.Equipment.Affixes))
			for index, affix := range item.Equipment.Affixes {
				names[index] = affix.Name
			}
			nativeAffixes = strings.Join(names, ", ")
		}
		piece := GUIPiece{Type: nativeDisplaySlots[config.Slot], Rarity: rarityColor(config.Rarity), Name: name, NativeAffixes: nativeAffixes, NativeID: config.ID, Gems: []GUIGem{}}
		if slot == nativeSlotIDs["primary"] {
			primaryConfig, hasPrimary = config, true
			primaryNativeAffixes = nativeAffixes
		}
		if slot == nativeSlotIDs["secondary"] {
			piece.Type = "Secondary"
			if config.Rarity == "Common" && config.Affix == "" {
				request.SecondaryWeapon = secondaryWeaponWhite
			} else if hasPrimary && config.Rarity == primaryConfig.Rarity && config.ID%100 == primaryConfig.ID%100 && nativeWeaponType(config) != nativeWeaponType(primaryConfig) {
				request.SecondaryWeapon = secondaryWeaponMatched
				piece.NativeAffixes = primaryNativeAffixes
			}
		}
		piece.Attributes = guiItemAttributes(item)
		if piece.NativeAffixes == "" {
			piece.NativeAffixes = "-"
		}
		if slot == 10 && itemClass != "" {
			request.WeaponClass = weaponClass(item)
		}
		for _, hole := range config.Holes {
			socketType := hole / 10
			if hole >= 50 {
				socketType = 5
			}
			gemIndex, err := read(10)
			if err != nil || gemIndex >= len(nativeTables.GemIDs) {
				return GUISession{}, fmt.Errorf("invalid gem index")
			}
			gemID := nativeTables.GemIDs[gemIndex]
			gem := GUIGem{Color: gemColors[gemTypes[socketType]], Tier: hole % 10, NativeID: gemID}
			if gemID != 0 {
				native, ok := nativeGem(gemID)
				if !ok {
					return GUISession{}, fmt.Errorf("unknown native gem ID: %d", gemID)
				}
				databaseGem := findDatabaseGem(native, gems)
				gem.Name = native.Name
				if databaseGem.ID != "" {
					price += databaseGem.RecommendedPrice
					addAffixes(databaseGem.Gem.Affixes)
					gem.Name = formatGemName(GemRef{Name: databaseGem.Name})
					affixes := make([]string, len(databaseGem.Gem.Affixes))
					for i, affix := range databaseGem.Gem.Affixes {
						affixes[i] = affix.Name
					}
					gem.Affixes = strings.Join(affixes, " / ")
					gem.GemColor = gemActualColors[gemType(databaseGem)]
				}
			}
			piece.Gems = append(piece.Gems, gem)
		}
		set.Pieces = append(set.Pieces, piece)
	}
	canonicalCode, err := encodeNativeBuild(classID, set.Pieces)
	if err != nil {
		return GUISession{}, err
	}
	set.Code = canonicalCode
	set.Price = formatNumber(price)
	for _, key := range affixOrder {
		set.Affixes = append(set.Affixes, GUIResultAffix{Name: affixNames[key], Result: affixLevels[key]})
	}
	return GUISession{
		Request:   request,
		Result:    GUIResult{Possible: true, Sets: []GUISet{set}, Rules: []string{"Imported native build code", "Class: " + request.CharacterClass}},
		HasResult: true,
	}, nil
}

func rarityColor(value string) string {
	for level, name := range rarityNames {
		if name == value {
			return rarityColors[level]
		}
	}
	return value
}
